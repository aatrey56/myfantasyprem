package fpl

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// The league endpoints (provider.LeagueMirrorSource, docs/adr/0012), in
// the shapes FPL Draft served on 14 October 2026 (testdata/real-gw6).
// FPL names a team by its league-entry id in matches and standings and by
// its entry id in choices, transactions, trades, element-status and the
// entry endpoints; every reference leaves this file as the league-entry
// id. Element ids leave it as player codes.

var _ provider.LeagueMirrorSource = (*Client)(nil)

type rawLeague struct {
	ID                 int64      `json:"id"`
	Name               string     `json:"name"`
	Scoring            string     `json:"scoring"`
	Trades             string     `json:"trades"`
	TransactionMode    string     `json:"transaction_mode"`
	StartEvent         int        `json:"start_event"`
	StopEvent          int        `json:"stop_event"`
	AdminEntry         *int64     `json:"admin_entry"`
	DraftDT            *time.Time `json:"draft_dt"`
	DraftPickTimeLimit int        `json:"draft_pick_time_limit"`
	Drafts             []struct {
		DraftCompleted *time.Time `json:"draft_completed"`
		Event          int        `json:"event"`
	} `json:"drafts"`
}

type rawLeagueEntry struct {
	ID              int64      `json:"id"`
	EntryID         *int64     `json:"entry_id"`
	EntryName       string     `json:"entry_name"`
	PlayerFirstName string     `json:"player_first_name"`
	PlayerLastName  string     `json:"player_last_name"`
	ShortName       string     `json:"short_name"`
	WaiverPick      int        `json:"waiver_pick"`
	JoinedTime      *time.Time `json:"joined_time"`
}

type rawMatch struct {
	Event        int   `json:"event"`
	LeagueEntry1 int64 `json:"league_entry_1"`
	Points1      int   `json:"league_entry_1_points"`
	LeagueEntry2 int64 `json:"league_entry_2"`
	Points2      int   `json:"league_entry_2_points"`
	Started      bool  `json:"started"`
	Finished     bool  `json:"finished"`
}

type rawStanding struct {
	LeagueEntry   *int64 `json:"league_entry"`
	Rank          int    `json:"rank"`
	MatchesWon    int    `json:"matches_won"`
	MatchesDrawn  int    `json:"matches_drawn"`
	MatchesLost   int    `json:"matches_lost"`
	PointsFor     int    `json:"points_for"`
	PointsAgainst int    `json:"points_against"`
	Total         int    `json:"total"`
}

type rawLeagueDetails struct {
	League        rawLeague        `json:"league"`
	LeagueEntries []rawLeagueEntry `json:"league_entries"`
	Matches       []rawMatch       `json:"matches"`
	Standings     []rawStanding    `json:"standings"`
}

// leagueIndex translates a league's entry ids to league-entry ids.
type leagueIndex struct {
	byEntry map[int64]string // entry id -> league-entry id
}

func id64(n int64) string { return strconv.FormatInt(n, 10) }

func decodeLeagueDetails(body []byte, leagueID string) (*provider.LeagueDetails, *leagueIndex, error) {
	var raw rawLeagueDetails
	if err := decodeStrict(body, &raw); err != nil {
		return nil, nil, err
	}
	l := raw.League
	if id64(l.ID) != leagueID {
		return nil, nil, fmt.Errorf("league id %d, asked for %s", l.ID, leagueID)
	}
	if strings.TrimSpace(l.Name) == "" {
		return nil, nil, errors.New("league without a name")
	}
	if l.StartEvent < 1 || l.StopEvent > 38 || l.StartEvent > l.StopEvent {
		return nil, nil, fmt.Errorf("gameweeks %d to %d", l.StartEvent, l.StopEvent)
	}
	d := &provider.LeagueDetails{
		ExternalID: leagueID, Name: l.Name, StartGameweek: l.StartEvent, EndGameweek: l.StopEvent,
		TradeModeSource: l.Trades, TransactionModeSource: l.TransactionMode, PickTimeSeconds: l.DraftPickTimeLimit,
	}
	switch l.Scoring {
	case "h":
		d.Scoring = provider.ScoringH2H
	case "c":
		d.Scoring = provider.ScoringClassic
	default:
		d.Scoring = provider.ScoringUnknown
	}
	switch l.Trades {
	case "m":
		d.TradeMode = provider.TradeModeCommissioner
	case "n":
		d.TradeMode = provider.TradeModeNoTrades
	default:
		d.TradeMode = provider.TradeModeUnknown
	}
	// transaction_mode is the league's current phase, not a setting:
	// "waivers" until the gameweek's waiver run, "free-agency" from then to
	// the deadline. Both are our waivers-then-free-agency model.
	switch l.TransactionMode {
	case "waivers", "free-agency":
		d.TransactionMode = provider.TransactionModeWaivers
	default:
		d.TransactionMode = provider.TransactionModeUnknown
	}
	if l.DraftDT != nil {
		t := l.DraftDT.UTC()
		d.DraftAt = &t
	}
	for _, dr := range l.Drafts {
		d.DraftGameweek = dr.Event
		if dr.DraftCompleted != nil {
			t := dr.DraftCompleted.UTC()
			d.DraftCompleted, d.DraftCompletedAt = true, &t
		}
	}
	if d.DraftGameweek == 0 {
		d.DraftGameweek = l.StartEvent
	}

	idx := &leagueIndex{byEntry: map[int64]string{}}
	seen := map[int64]bool{}
	real := 0
	for _, e := range raw.LeagueEntries {
		if e.ID <= 0 || seen[e.ID] {
			return nil, nil, fmt.Errorf("league entry %d: missing or duplicate id", e.ID)
		}
		seen[e.ID] = true
		le := provider.LeagueEntry{
			ExternalID: id64(e.ID), TeamName: e.EntryName, ShortName: e.ShortName, WaiverPick: e.WaiverPick,
			ManagerName: strings.TrimSpace(e.PlayerFirstName + " " + e.PlayerLastName),
		}
		if e.JoinedTime != nil {
			t := e.JoinedTime.UTC()
			le.JoinedAt = &t
		}
		if e.EntryID == nil {
			le.IsAverage = true
		} else {
			if *e.EntryID <= 0 {
				return nil, nil, fmt.Errorf("league entry %d: bad entry id", e.ID)
			}
			if _, dup := idx.byEntry[*e.EntryID]; dup {
				return nil, nil, fmt.Errorf("league entry %d: duplicate entry %d", e.ID, *e.EntryID)
			}
			if strings.TrimSpace(e.EntryName) == "" {
				return nil, nil, fmt.Errorf("league entry %d: no team name", e.ID)
			}
			idx.byEntry[*e.EntryID] = le.ExternalID
			le.EntryID = id64(*e.EntryID)
			real++
			if l.AdminEntry != nil && *l.AdminEntry == *e.EntryID {
				d.AdminEntry = le.ExternalID
			}
		}
		d.Teams = append(d.Teams, le)
	}
	if real == 0 {
		return nil, nil, errors.New("no teams")
	}
	for _, m := range raw.Matches {
		if m.Event < 1 || m.Event > 38 || !seen[m.LeagueEntry1] || !seen[m.LeagueEntry2] || m.LeagueEntry1 == m.LeagueEntry2 {
			return nil, nil, fmt.Errorf("match in gameweek %d: entries %d and %d", m.Event, m.LeagueEntry1, m.LeagueEntry2)
		}
		d.Matches = append(d.Matches, provider.LeagueMatch{
			Gameweek: m.Event, HomeEntry: id64(m.LeagueEntry1), AwayEntry: id64(m.LeagueEntry2),
			HomePts: m.Points1, AwayPts: m.Points2, Started: m.Started || m.Finished, Finished: m.Finished,
		})
	}
	for _, s := range raw.Standings {
		if s.LeagueEntry == nil || !seen[*s.LeagueEntry] {
			return nil, nil, fmt.Errorf("standing for unknown entry %v", s.LeagueEntry)
		}
		d.Standings = append(d.Standings, provider.LeagueStanding{
			Rank: s.Rank, LeagueEntry: id64(*s.LeagueEntry), Wins: s.MatchesWon, Draws: s.MatchesDrawn,
			Losses: s.MatchesLost, PointsFor: s.PointsFor, PointsAgainst: s.PointsAgainst, Total: s.Total,
		})
	}
	return d, idx, nil
}

// LeagueDetails implements provider.LeagueMirrorSource.
func (c *Client) LeagueDetails(ctx context.Context, leagueID string) (*provider.LeagueDetails, error) {
	if !validID(leagueID) {
		return nil, fmt.Errorf("fpl: league %q: %w", leagueID, provider.ErrNotFound)
	}
	path := "/league/" + leagueID + "/details"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	d, idx, err := decodeLeagueDetails(body, leagueID)
	if err != nil {
		c.forget(path)
		return nil, invalid("league details", err)
	}
	c.mu.Lock()
	if c.leagues == nil {
		c.leagues = map[string]*leagueIndex{}
	}
	c.leagues[leagueID] = idx
	c.mu.Unlock()
	return d, nil
}

func validID(s string) bool {
	n, err := strconv.ParseInt(s, 10, 64)
	return err == nil && n > 0 && strconv.FormatInt(n, 10) == s
}

// leagueRefs holds what a league endpoint's references are checked
// against: the league's entries and the bootstrap's elements.
type leagueRefs struct {
	league   *leagueIndex
	elements map[int64]string
}

func (r leagueRefs) entry(id int64) (string, error) {
	le, ok := r.league.byEntry[id]
	if !ok {
		return "", fmt.Errorf("unknown entry %d", id)
	}
	return le, nil
}

func (r leagueRefs) element(id int64) (string, error) {
	code, ok := r.elements[id]
	if !ok {
		return "", fmt.Errorf("unknown element %d", id)
	}
	return code, nil
}

// leagueGet fetches a league endpoint and decodes it against the league's
// entries and the bootstrap's elements. A reference to an entry or element
// it does not know refreshes the details and the bootstrap once (a player
// added since the last bootstrap) before the response counts as invalid.
func leagueGet[T any](ctx context.Context, c *Client, leagueID, path, what string, decode func([]byte, leagueRefs) (T, error)) (T, error) {
	var zero T
	if !validID(leagueID) {
		return zero, fmt.Errorf("fpl: league %q: %w", leagueID, provider.ErrNotFound)
	}
	refs := func(refresh bool) (leagueRefs, error) {
		c.mu.Lock()
		li := c.leagues[leagueID]
		c.mu.Unlock()
		if li == nil || refresh {
			if _, err := c.LeagueDetails(ctx, leagueID); err != nil {
				return leagueRefs{}, err
			}
			c.mu.Lock()
			li = c.leagues[leagueID]
			c.mu.Unlock()
		}
		var idx *bootstrapIndex
		var err error
		if refresh {
			_, idx, err = c.bootstrap(ctx)
		} else {
			idx, err = c.index(ctx)
		}
		if err != nil {
			return leagueRefs{}, err
		}
		return leagueRefs{league: li, elements: idx.elementCode}, nil
	}
	body, err := c.get(ctx, path)
	if err != nil {
		return zero, err
	}
	r, err := refs(false)
	if err != nil {
		return zero, err
	}
	out, err := decode(body, r)
	if err != nil {
		if r2, rerr := refs(true); rerr == nil {
			out, err = decode(body, r2)
		}
	}
	if err != nil {
		c.forget(path)
		return zero, invalid(what, err)
	}
	return out, nil
}

// ElementStatus implements provider.LeagueMirrorSource.
func (c *Client) ElementStatus(ctx context.Context, leagueID string) ([]provider.ElementStatus, error) {
	return leagueGet(ctx, c, leagueID, "/league/"+leagueID+"/element-status", "element status", decodeElementStatus)
}

func decodeElementStatus(body []byte, r leagueRefs) ([]provider.ElementStatus, error) {
	var raw struct {
		ElementStatus []struct {
			Element         int64  `json:"element"`
			Owner           *int64 `json:"owner"`
			Status          string `json:"status"`
			InAcceptedTrade bool   `json:"in_accepted_trade"`
		} `json:"element_status"`
	}
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	out := make([]provider.ElementStatus, 0, len(raw.ElementStatus))
	for _, e := range raw.ElementStatus {
		if seen[e.Element] {
			return nil, fmt.Errorf("element %d listed twice", e.Element)
		}
		seen[e.Element] = true
		code, err := r.element(e.Element)
		if err != nil {
			return nil, err
		}
		s := provider.ElementStatus{PlayerExternalID: code, InAcceptedTrade: e.InAcceptedTrade}
		switch e.Status {
		case "o":
			s.Ownership = provider.OwnershipOwned
		case "a":
			s.Ownership = provider.OwnershipFreeAgent
		case "w":
			s.Ownership = provider.OwnershipWaived
		default:
			s.Ownership = provider.OwnershipUnknown
		}
		if e.Owner != nil {
			le, err := r.entry(*e.Owner)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", e.Element, err)
			}
			s.OwnerEntry = &le
		} else if s.Ownership == provider.OwnershipOwned {
			return nil, fmt.Errorf("element %d owned without an owner", e.Element)
		}
		out = append(out, s)
	}
	return out, nil
}

// DraftChoices implements provider.LeagueMirrorSource. FPL serves a
// league's draft at /draft/{league id}/choices.
func (c *Client) DraftChoices(ctx context.Context, leagueID string) ([]provider.DraftChoice, error) {
	return leagueGet(ctx, c, leagueID, "/draft/"+leagueID+"/choices", "draft choices", decodeChoices)
}

func decodeChoices(body []byte, r leagueRefs) ([]provider.DraftChoice, error) {
	var raw struct {
		Choices []struct {
			Index      int       `json:"index"`
			Round      int       `json:"round"`
			Pick       int       `json:"pick"`
			Entry      int64     `json:"entry"`
			Element    int64     `json:"element"`
			WasAuto    bool      `json:"was_auto"`
			ChoiceTime time.Time `json:"choice_time"`
		} `json:"choices"`
	}
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	out := make([]provider.DraftChoice, 0, len(raw.Choices))
	seenIndex, seenElement := map[int]bool{}, map[int64]bool{}
	for _, ch := range raw.Choices {
		if ch.Index < 1 || seenIndex[ch.Index] || seenElement[ch.Element] || ch.Round < 1 || ch.Pick < 1 {
			return nil, fmt.Errorf("choice %d: bad or duplicate index, round, pick or element", ch.Index)
		}
		seenIndex[ch.Index], seenElement[ch.Element] = true, true
		le, err := r.entry(ch.Entry)
		if err != nil {
			return nil, fmt.Errorf("choice %d: %w", ch.Index, err)
		}
		code, err := r.element(ch.Element)
		if err != nil {
			return nil, fmt.Errorf("choice %d: %w", ch.Index, err)
		}
		out = append(out, provider.DraftChoice{
			Overall: ch.Index, Round: ch.Round, Pick: ch.Pick, LeagueEntry: le, PlayerExternalID: code,
			WasAuto: ch.WasAuto, At: ch.ChoiceTime.UTC(),
		})
	}
	slices.SortFunc(out, func(a, b provider.DraftChoice) int { return cmp.Compare(a.Overall, b.Overall) })
	return out, nil
}

// Transactions implements provider.LeagueMirrorSource.
func (c *Client) Transactions(ctx context.Context, leagueID string) ([]provider.Transaction, error) {
	return leagueGet(ctx, c, leagueID, "/draft/league/"+leagueID+"/transactions", "transactions", decodeTransactions)
}

func decodeTransactions(body []byte, r leagueRefs) ([]provider.Transaction, error) {
	var raw struct {
		Transactions []struct {
			ID         int64     `json:"id"`
			Entry      int64     `json:"entry"`
			ElementIn  int64     `json:"element_in"`
			ElementOut int64     `json:"element_out"`
			Event      int       `json:"event"`
			Kind       string    `json:"kind"`
			Result     string    `json:"result"`
			Index      int       `json:"index"`
			Priority   *int      `json:"priority"`
			Added      time.Time `json:"added"`
		} `json:"transactions"`
	}
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	out := make([]provider.Transaction, 0, len(raw.Transactions))
	seen := map[int64]bool{}
	for _, x := range raw.Transactions {
		if x.ID <= 0 || seen[x.ID] || x.Event < 1 || x.Event > 38 || x.Added.IsZero() {
			return nil, fmt.Errorf("transaction %d: bad or duplicate id, gameweek or time", x.ID)
		}
		seen[x.ID] = true
		t := provider.Transaction{
			ExternalID: id64(x.ID), SourceState: x.Kind + "/" + x.Result, Gameweek: x.Event, Index: x.Index,
			Priority: x.Priority, ProcessedAt: x.Added.UTC(),
		}
		switch x.Kind {
		case "w":
			t.Kind = provider.TransactionWaiver
		case "f":
			t.Kind = provider.TransactionFreeAgent
		default:
			return nil, fmt.Errorf("transaction %d: unknown kind %q", x.ID, x.Kind)
		}
		// An unknown result could be a move that happened: refuse rather
		// than guess (the roster replay depends on it).
		switch x.Result {
		case "a":
			t.Result = provider.ClaimApproved
		case "di":
			t.Result = provider.ClaimDeniedPlayerInUnavailable
		case "do":
			t.Result = provider.ClaimDeniedPlayerOutUnavailable
		default:
			return nil, fmt.Errorf("transaction %d: unknown result %q", x.ID, x.Result)
		}
		var err error
		if t.LeagueEntry, err = r.entry(x.Entry); err != nil {
			return nil, fmt.Errorf("transaction %d: %w", x.ID, err)
		}
		if t.PlayerIn, err = r.element(x.ElementIn); err != nil {
			return nil, fmt.Errorf("transaction %d: %w", x.ID, err)
		}
		if t.PlayerOut, err = r.element(x.ElementOut); err != nil {
			return nil, fmt.Errorf("transaction %d: %w", x.ID, err)
		}
		out = append(out, t)
	}
	return out, nil
}

// Trades implements provider.LeagueMirrorSource.
func (c *Client) Trades(ctx context.Context, leagueID string) ([]provider.Trade, error) {
	return leagueGet(ctx, c, leagueID, "/draft/league/"+leagueID+"/trades", "trades", decodeTrades)
}

func decodeTrades(body []byte, r leagueRefs) ([]provider.Trade, error) {
	var raw struct {
		Trades []struct {
			ID            int64      `json:"id"`
			Event         int        `json:"event"`
			OfferedEntry  int64      `json:"offered_entry"`
			ReceivedEntry int64      `json:"received_entry"`
			State         string     `json:"state"`
			OfferTime     time.Time  `json:"offer_time"`
			ResponseTime  *time.Time `json:"response_time"`
			Items         []struct {
				ElementIn  int64 `json:"element_in"`
				ElementOut int64 `json:"element_out"`
			} `json:"tradeitem_set"`
		} `json:"trades"`
	}
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	out := make([]provider.Trade, 0, len(raw.Trades))
	seen := map[int64]bool{}
	for _, x := range raw.Trades {
		if x.ID <= 0 || seen[x.ID] || x.Event < 1 || x.Event > 38 || len(x.Items) == 0 || x.OfferedEntry == x.ReceivedEntry {
			return nil, fmt.Errorf("trade %d: bad or duplicate id, gameweek, entries or no items", x.ID)
		}
		seen[x.ID] = true
		t := provider.Trade{ExternalID: id64(x.ID), SourceState: x.State, Gameweek: x.Event, OfferedAt: x.OfferTime.UTC()}
		// Only the codes seen in a real league are mapped; FPL's other
		// states (proposed, rejected, withdrawn, invalid, expired) have
		// codes we have not seen, so they stay unknown (docs/adr/0012).
		switch x.State {
		case "p":
			t.State = provider.TradeProcessed
		case "v":
			t.State = provider.TradeVetoed
		case "a":
			t.State = provider.TradeAccepted
		default:
			t.State = provider.TradeUnknown
		}
		if x.ResponseTime != nil {
			rt := x.ResponseTime.UTC()
			t.ResolvedAt = &rt
		}
		var err error
		if t.OfferEntry, err = r.entry(x.OfferedEntry); err != nil {
			return nil, fmt.Errorf("trade %d: %w", x.ID, err)
		}
		if t.ReceiveEntry, err = r.entry(x.ReceivedEntry); err != nil {
			return nil, fmt.Errorf("trade %d: %w", x.ID, err)
		}
		for _, it := range x.Items {
			// element_out leaves the offering entry; element_in arrives
			// there (the capture's replay, docs/adr/0012).
			out, err := r.element(it.ElementOut)
			if err != nil {
				return nil, fmt.Errorf("trade %d: %w", x.ID, err)
			}
			in, err := r.element(it.ElementIn)
			if err != nil {
				return nil, fmt.Errorf("trade %d: %w", x.ID, err)
			}
			t.Offered, t.Requested = append(t.Offered, out), append(t.Requested, in)
		}
		out = append(out, t)
	}
	return out, nil
}

// noPicks is FPL's body for a gameweek whose picks are not public yet.
const noPicks = "No pick history"

// EntryEventPicks implements provider.LeagueMirrorSource.
func (c *Client) EntryEventPicks(ctx context.Context, entryID string, gameweek int) (*provider.EntryPicks, error) {
	if !validID(entryID) || gameweek < 1 || gameweek > 38 {
		return nil, fmt.Errorf("fpl: entry %q gameweek %d: %w", entryID, gameweek, provider.ErrNotFound)
	}
	path := "/entry/" + entryID + "/event/" + strconv.Itoa(gameweek)
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	if s := bytes.TrimSpace(body); len(s) > 0 && s[0] == '"' {
		var msg string
		if json.Unmarshal(s, &msg) == nil && msg == noPicks {
			c.forget(path)
			return nil, fmt.Errorf("fpl: GET %s: %w", path, provider.ErrNotYetAvailable)
		}
	}
	idx, err := c.index(ctx)
	if err != nil {
		return nil, err
	}
	p, err := decodePicks(body, entryID, gameweek, idx.elementCode)
	if err != nil {
		if _, idx2, berr := c.bootstrap(ctx); berr == nil {
			p, err = decodePicks(body, entryID, gameweek, idx2.elementCode)
		}
	}
	if err != nil {
		c.forget(path)
		return nil, invalid("entry picks", err)
	}
	return p, nil
}

func decodePicks(body []byte, entryID string, gameweek int, elements map[int64]string) (*provider.EntryPicks, error) {
	var raw struct {
		Picks []struct {
			Element  int64 `json:"element"`
			Position int   `json:"position"`
		} `json:"picks"`
		Subs []struct {
			ElementIn  int64 `json:"element_in"`
			ElementOut int64 `json:"element_out"`
		} `json:"subs"`
		EntryHistory map[string]json.RawMessage `json:"entry_history"`
	}
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	if len(raw.Picks) != 15 {
		return nil, fmt.Errorf("%d picks", len(raw.Picks))
	}
	p := &provider.EntryPicks{EntryID: entryID, Gameweek: gameweek, Picks: make([]provider.Pick, 15)}
	inPicks := map[int64]bool{}
	for _, pk := range raw.Picks {
		if pk.Position < 1 || pk.Position > 15 || p.Picks[pk.Position-1].PlayerExternalID != "" || inPicks[pk.Element] {
			return nil, fmt.Errorf("pick %d at position %d: bad or duplicate", pk.Element, pk.Position)
		}
		code, ok := elements[pk.Element]
		if !ok {
			return nil, fmt.Errorf("unknown element %d", pk.Element)
		}
		inPicks[pk.Element] = true
		p.Picks[pk.Position-1] = provider.Pick{Slot: pk.Position, PlayerExternalID: code}
	}
	for _, s := range raw.Subs {
		if !inPicks[s.ElementIn] || !inPicks[s.ElementOut] || s.ElementIn == s.ElementOut {
			return nil, fmt.Errorf("sub %d for %d: not in the picks", s.ElementIn, s.ElementOut)
		}
		p.AutoSubs = append(p.AutoSubs, provider.AutoSub{InExternalID: elements[s.ElementIn], OutExternalID: elements[s.ElementOut]})
	}
	if v, ok := raw.EntryHistory["points"]; ok {
		var n int
		if err := json.Unmarshal(v, &n); err != nil {
			return nil, fmt.Errorf("entry_history.points: %w", err)
		}
		p.Points = &n
	}
	return p, nil
}
