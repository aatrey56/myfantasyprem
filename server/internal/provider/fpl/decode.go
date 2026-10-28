package fpl

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// The upstream shapes (RESEARCH.md §6). Only the fields we use are
// declared; unknown fields are ignored so FPL can add fields freely, but
// every declared required field is checked after decoding.

type rawElement struct {
	ID          int64  `json:"id"`
	Code        int64  `json:"code"`
	FirstName   string `json:"first_name"`
	SecondName  string `json:"second_name"`
	WebName     string `json:"web_name"`
	Team        int64  `json:"team"`
	ElementType int    `json:"element_type"`
	Status      string `json:"status"`
	News        string `json:"news"`
	// NewsAdded and ChanceNext are absent from older feeds (and null when
	// FPL has none).
	NewsAdded  *time.Time `json:"news_added"`
	ChanceNext *int       `json:"chance_of_playing_next_round"`
	DraftRank  int        `json:"draft_rank"`
	BirthDate  *string    `json:"birth_date"`
}

type rawElementType struct {
	ID                int    `json:"id"`
	SingularNameShort string `json:"singular_name_short"`
}

type rawTeam struct {
	ID        int64  `json:"id"`
	Code      int64  `json:"code"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

type rawEvent struct {
	ID           int        `json:"id"`
	Name         string     `json:"name"`
	DeadlineTime time.Time  `json:"deadline_time"`
	WaiversTime  *time.Time `json:"waivers_time"`
	TradesTime   *time.Time `json:"trades_time"`
	Finished     bool       `json:"finished"`
	DataChecked  bool       `json:"data_checked"`
}

// checked reports whether the event's data is final. FPL Draft events
// have no data_checked (classic FPL's flag); Draft marks an event
// finished once its scores are settled, so finished counts as checked
// (ADR 0005). A later change to the live data is still ingested and
// rescored as a correction.
func (ev rawEvent) checked() bool { return ev.DataChecked || ev.Finished }

type rawEvents struct {
	Current *int       `json:"current"`
	Next    *int       `json:"next"`
	Data    []rawEvent `json:"data"`
}

type rawFixture struct {
	ID                  int64      `json:"id"`
	Code                int64      `json:"code"`
	Event               *int       `json:"event"`
	TeamH               int64      `json:"team_h"`
	TeamA               int64      `json:"team_a"`
	KickoffTime         *time.Time `json:"kickoff_time"`
	Started             *bool      `json:"started"`
	Finished            bool       `json:"finished"`
	FinishedProvisional bool       `json:"finished_provisional"`
	TeamHScore          *int       `json:"team_h_score"`
	TeamAScore          *int       `json:"team_a_score"`
	// Stats are the fixture's per-player stats, one entry per identifier
	// ("bps", "bonus", ...) with home and away lists.
	Stats []rawFixtureStat `json:"stats"`
}

// rawFixtureStat names its stat "s" in FPL Draft's live fixtures and
// "identifier" in classic FPL; both are accepted.
type rawFixtureStat struct {
	S          string             `json:"s"`
	Identifier string             `json:"identifier"`
	H          []rawFixtureStatEl `json:"h"`
	A          []rawFixtureStatEl `json:"a"`
}

type rawFixtureStatEl struct {
	Value   int   `json:"value"`
	Element int64 `json:"element"`
}

// fixtureBPS returns each element's BPS in the fixture from its "bps"
// stat, or nil when the fixture has none.
func fixtureBPS(f rawFixture) map[int64]int {
	var out map[int64]int
	for _, st := range f.Stats {
		if st.S != "bps" && st.Identifier != "bps" {
			continue
		}
		out = map[int64]int{}
		for _, el := range append(append([]rawFixtureStatEl(nil), st.H...), st.A...) {
			out[el.Element] = el.Value
		}
	}
	return out
}

// rawFixtures accepts both shapes seen in FPL payloads: a plain array, or
// an object of arrays keyed by gameweek.
type rawFixtures []rawFixture

func (f *rawFixtures) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if bytes.Equal(b, []byte("null")) {
		return nil
	}
	if len(b) > 0 && b[0] == '[' {
		return json.Unmarshal(b, (*[]rawFixture)(f))
	}
	var byGW map[string][]rawFixture
	if err := json.Unmarshal(b, &byGW); err != nil {
		return err
	}
	keys := make([]string, 0, len(byGW))
	for k := range byGW {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		*f = append(*f, byGW[k]...)
	}
	return nil
}

// rawSettings accepts FPL Draft's squad keys ("size", "play") and the
// older "squad_size" and "starting_size".
type rawSettings struct {
	Squad struct {
		SquadSize    int `json:"squad_size"`
		StartingSize int `json:"starting_size"`
		Size         int `json:"size"`
		Play         int `json:"play"`
	} `json:"squad"`
	Scoring map[string]int `json:"scoring"`
}

type rawBootstrap struct {
	Elements     []rawElement     `json:"elements"`
	ElementTypes []rawElementType `json:"element_types"`
	Teams        []rawTeam        `json:"teams"`
	Events       rawEvents        `json:"events"`
	Fixtures     rawFixtures      `json:"fixtures"`
	Settings     rawSettings      `json:"settings"`
}

// bootstrapIndex holds the per-season id joins live data needs.
type bootstrapIndex struct {
	elementCode map[int64]string // element id -> code
	teamCode    map[int64]string // team id -> code
	fixtureCode map[int64]string // fixture id -> code
	fixtures    []provider.Fixture
	events      map[int]rawEvent
}

func decodeStrict(body []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

func code(n int64) string { return strconv.FormatInt(n, 10) }

var positions = map[string]provider.Position{"GKP": provider.GKP, "DEF": provider.DEF, "MID": provider.MID, "FWD": provider.FWD}

// availability translates FPL's single-letter status (ADR 0004).
func availability(s string) provider.Availability {
	switch s {
	case "a":
		return provider.AvailabilityAvailable
	case "d":
		return provider.AvailabilityDoubtful
	case "i":
		return provider.AvailabilityInjured
	case "s":
		return provider.AvailabilitySuspended
	case "u", "n":
		return provider.AvailabilityUnavailable
	}
	return provider.AvailabilityUnknown
}

// fixtureStatus derives our status from FPL's flags. A fixture without a
// kickoff time or gameweek is postponed.
func fixtureStatus(f rawFixture) provider.FixtureStatus {
	switch {
	case f.KickoffTime == nil || f.Event == nil:
		return provider.FixturePostponed
	case f.Finished:
		return provider.FixtureFinished
	case f.FinishedProvisional:
		return provider.FixtureFinishedProvisional
	case f.Started != nil && *f.Started:
		return provider.FixtureLive
	}
	return provider.FixtureScheduled
}

// mapFixtures validates fixtures against the team index and maps them.
func mapFixtures(raw []rawFixture, teamCode map[int64]string) ([]provider.Fixture, map[int64]string, error) {
	out := make([]provider.Fixture, 0, len(raw))
	codes := make(map[int64]string, len(raw))
	for _, f := range raw {
		if f.ID <= 0 || f.Code <= 0 {
			return nil, nil, fmt.Errorf("fixture %d: missing id or code", f.ID)
		}
		home, okH := teamCode[f.TeamH]
		away, okA := teamCode[f.TeamA]
		if !okH || !okA || f.TeamH == f.TeamA {
			return nil, nil, fmt.Errorf("fixture %d: unknown or equal teams %d and %d", f.ID, f.TeamH, f.TeamA)
		}
		gw := 0
		if f.Event != nil {
			if *f.Event < 1 || *f.Event > 38 {
				return nil, nil, fmt.Errorf("fixture %d: gameweek %d out of range", f.ID, *f.Event)
			}
			gw = *f.Event
		}
		for _, sc := range []*int{f.TeamHScore, f.TeamAScore} {
			if sc != nil && *sc < 0 {
				return nil, nil, fmt.Errorf("fixture %d: negative score", f.ID)
			}
		}
		fx := provider.Fixture{
			ExternalID:         code(f.Code),
			SeasonFixtureID:    f.ID,
			Gameweek:           gw,
			HomeClubExternalID: home,
			AwayClubExternalID: away,
			Status:             fixtureStatus(f),
			HomeScore:          f.TeamHScore,
			AwayScore:          f.TeamAScore,
		}
		if f.KickoffTime != nil {
			k := f.KickoffTime.UTC()
			fx.KickoffAt = &k
		}
		out = append(out, fx)
		codes[f.ID] = fx.ExternalID
	}
	return out, codes, nil
}

func decodeBootstrap(body []byte) (*provider.Bootstrap, *bootstrapIndex, error) {
	var raw rawBootstrap
	if err := decodeStrict(body, &raw); err != nil {
		return nil, nil, err
	}
	if len(raw.Teams) < 2 {
		return nil, nil, fmt.Errorf("%d teams", len(raw.Teams))
	}
	if len(raw.Elements) == 0 {
		return nil, nil, errors.New("no elements")
	}
	if len(raw.Events.Data) == 0 || len(raw.Events.Data) > 38 {
		return nil, nil, fmt.Errorf("%d events", len(raw.Events.Data))
	}

	idx := &bootstrapIndex{
		elementCode: map[int64]string{},
		teamCode:    map[int64]string{},
		events:      map[int]rawEvent{},
	}
	b := &provider.Bootstrap{Payload: body}

	seenCodes := map[int64]bool{}
	for _, t := range raw.Teams {
		if t.ID <= 0 || t.Code <= 0 || strings.TrimSpace(t.Name) == "" || len(t.ShortName) < 2 || len(t.ShortName) > 4 {
			return nil, nil, fmt.Errorf("team %d: missing id, code, name or short name", t.ID)
		}
		if _, dup := idx.teamCode[t.ID]; dup || seenCodes[t.Code] {
			return nil, nil, fmt.Errorf("team %d: duplicate id or code", t.ID)
		}
		seenCodes[t.Code] = true
		idx.teamCode[t.ID] = code(t.Code)
		b.Clubs = append(b.Clubs, provider.Club{
			ExternalID: code(t.Code), SeasonTeamID: t.ID,
			ShortName: strings.ToUpper(t.ShortName), Name: t.Name,
		})
	}

	typePos := map[int]provider.Position{}
	for _, et := range raw.ElementTypes {
		if p, ok := positions[strings.ToUpper(et.SingularNameShort)]; ok {
			typePos[et.ID] = p
		}
	}
	if len(typePos) == 0 { // FPL's fixed numbering
		typePos = map[int]provider.Position{1: provider.GKP, 2: provider.DEF, 3: provider.MID, 4: provider.FWD}
	}

	seenCodes = map[int64]bool{}
	for _, e := range raw.Elements {
		if e.ID <= 0 || e.Code <= 0 || strings.TrimSpace(e.WebName) == "" {
			return nil, nil, fmt.Errorf("element %d: missing id, code or web_name", e.ID)
		}
		if _, dup := idx.elementCode[e.ID]; dup || seenCodes[e.Code] {
			return nil, nil, fmt.Errorf("element %d: duplicate id or code", e.ID)
		}
		club, ok := idx.teamCode[e.Team]
		if !ok {
			return nil, nil, fmt.Errorf("element %d: unknown team %d", e.ID, e.Team)
		}
		pos, ok := typePos[e.ElementType]
		if !ok {
			return nil, nil, fmt.Errorf("element %d: unknown element_type %d", e.ID, e.ElementType)
		}
		if e.ChanceNext != nil && (*e.ChanceNext < 0 || *e.ChanceNext > 100) {
			e.ChanceNext = nil // out of range: treat as unknown
		}
		if e.News == "" {
			e.NewsAdded = nil // no news, no news time
		}
		if e.DraftRank < 0 {
			return nil, nil, fmt.Errorf("element %d: negative draft_rank", e.ID)
		}
		seenCodes[e.Code] = true
		idx.elementCode[e.ID] = code(e.Code)
		p := provider.Player{
			ExternalID:      code(e.Code),
			SeasonElementID: e.ID,
			FirstName:       e.FirstName,
			LastName:        e.SecondName,
			DisplayName:     e.WebName,
			ClubExternalID:  club,
			Position:        pos,
			Availability:    availability(e.Status),
			News:            e.News,
			NewsAt:          e.NewsAdded,
			ChanceNext:      e.ChanceNext,
			DraftRank:       e.DraftRank,
		}
		if e.BirthDate != nil {
			if d, err := time.Parse("2006-01-02", *e.BirthDate); err == nil {
				p.BirthDate = &d
			}
		}
		b.Players = append(b.Players, p)
	}

	for _, ev := range raw.Events.Data {
		if ev.ID < 1 || ev.ID > 38 || ev.DeadlineTime.IsZero() {
			return nil, nil, fmt.Errorf("event %d: bad id or deadline", ev.ID)
		}
		if _, dup := idx.events[ev.ID]; dup {
			return nil, nil, fmt.Errorf("event %d: duplicate", ev.ID)
		}
		idx.events[ev.ID] = ev
		deadline := ev.DeadlineTime.UTC()
		gw := provider.Gameweek{
			Number:           ev.ID,
			Name:             ev.Name,
			DeadlineAt:       deadline,
			WaiverDeadlineAt: deadline.Add(-24 * time.Hour),
			TradeDeadlineAt:  deadline.Add(-48 * time.Hour),
			Finished:         ev.Finished,
			DataChecked:      ev.checked(),
		}
		if gw.Name == "" {
			gw.Name = "Gameweek " + strconv.Itoa(ev.ID)
		}
		if ev.WaiversTime != nil {
			gw.WaiverDeadlineAt = ev.WaiversTime.UTC()
		}
		if ev.TradesTime != nil {
			gw.TradeDeadlineAt = ev.TradesTime.UTC()
		}
		b.Gameweeks = append(b.Gameweeks, gw)
	}
	sort.Slice(b.Gameweeks, func(i, j int) bool { return b.Gameweeks[i].Number < b.Gameweeks[j].Number })

	fixtures, fxCodes, err := mapFixtures(raw.Fixtures, idx.teamCode)
	if err != nil {
		return nil, nil, err
	}
	idx.fixtures, idx.fixtureCode = fixtures, fxCodes

	b.Settings = gameSettings(raw.Settings)
	return b, idx, nil
}

// gameSettings maps FPL's settings; missing keys keep FPL's documented
// 2026-27 values (RESEARCH.md §7). FPL Draft publishes the defensive
// contribution as "defensive_contribution_limit_X" (threshold) and
// "defensive_contribution_X" (points, 0 for goalkeepers, who cannot earn
// it); the single points value is the outfield one.
func gameSettings(s rawSettings) provider.GameSettings {
	v := func(key string, def int) int {
		if n, ok := s.Scoring[key]; ok {
			return n
		}
		return def
	}
	pp := func(stat string, gk, def, mid, fwd int) provider.PositionPoints {
		return provider.PositionPoints{
			GKP: v(stat+"_GKP", gk), DEF: v(stat+"_DEF", def), MID: v(stat+"_MID", mid), FWD: v(stat+"_FWD", fwd),
		}
	}
	threshold := pp("defensive_contribution_limit", 0, 10, 12, 12)
	threshold = pp("defensive_contribution_threshold", threshold.GKP, threshold.DEF, threshold.MID, threshold.FWD)
	out := provider.GameSettings{
		SquadSize:    s.Squad.SquadSize,
		StartingSize: s.Squad.StartingSize,
		Scoring: provider.ScoringTable{
			MinutesUnder60:                 v("short_play", 1),
			Minutes60Plus:                  v("long_play", 2),
			Goal:                           pp("goals_scored", 10, 6, 5, 4),
			Assist:                         v("assists", 3),
			CleanSheet:                     pp("clean_sheets", 4, 4, 1, 0),
			GoalsConcededPerTwo:            pp("goals_conceded", -1, -1, 0, 0),
			SavesPerThree:                  v("saves", 1),
			PenaltySave:                    v("penalties_saved", 5),
			PenaltyMiss:                    v("penalties_missed", -2),
			OwnGoal:                        v("own_goals", -2),
			YellowCard:                     v("yellow_cards", -1),
			RedCard:                        v("red_cards", -3),
			DefensiveContributionThreshold: threshold,
			DefensiveContribution:          v("defensive_contribution", v("defensive_contribution_DEF", 2)),
		},
	}
	if out.SquadSize == 0 {
		out.SquadSize = cmp.Or(s.Squad.Size, 15)
	}
	if out.StartingSize == 0 {
		out.StartingSize = cmp.Or(s.Squad.Play, 11)
	}
	return out
}

// ------------------------------------------------------------ live

type rawLiveStats struct {
	Minutes                       *int `json:"minutes"`
	GoalsScored                   int  `json:"goals_scored"`
	Assists                       int  `json:"assists"`
	CleanSheets                   int  `json:"clean_sheets"`
	GoalsConceded                 int  `json:"goals_conceded"`
	OwnGoals                      int  `json:"own_goals"`
	PenaltiesSaved                int  `json:"penalties_saved"`
	PenaltiesMissed               int  `json:"penalties_missed"`
	YellowCards                   int  `json:"yellow_cards"`
	RedCards                      int  `json:"red_cards"`
	Saves                         int  `json:"saves"`
	Bonus                         int  `json:"bonus"`
	BPS                           int  `json:"bps"`
	ClearancesBlocksInterceptions int  `json:"clearances_blocks_interceptions"`
	Recoveries                    int  `json:"recoveries"`
	Tackles                       int  `json:"tackles"`
	DefensiveContribution         int  `json:"defensive_contribution"`
	TotalPoints                   *int `json:"total_points"`
	// FPL sends these as decimal strings ("12.4").
	Influence                *decimal `json:"influence"`
	Creativity               *decimal `json:"creativity"`
	Threat                   *decimal `json:"threat"`
	ICTIndex                 *decimal `json:"ict_index"`
	ExpectedGoals            *decimal `json:"expected_goals"`
	ExpectedAssists          *decimal `json:"expected_assists"`
	ExpectedGoalInvolvements *decimal `json:"expected_goal_involvements"`
	ExpectedGoalsConceded    *decimal `json:"expected_goals_conceded"`
	Starts                   *int     `json:"starts"`
}

// decimal is one of FPL's decimal stats (ICT, expected goals), sent as a
// string such as "12.4"; a JSON number is accepted too. They score nothing,
// so a value that does not parse is dropped (nil) rather than failing the
// snapshot and holding up live scores.
type decimal struct{ v *float64 }

func (d *decimal) UnmarshalJSON(b []byte) error {
	f, err := strconv.ParseFloat(strings.Trim(string(b), `"`), 64)
	if err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
		d.v = &f
	}
	return nil
}

// value is the parsed number, nil when absent or unreadable.
func (d *decimal) value() *float64 {
	if d == nil {
		return nil
	}
	return d.v
}

type rawExplainLine struct {
	Stat   string `json:"stat"`
	Points int    `json:"points"`
	Value  int    `json:"value"`
}

// rawExplain is one fixture's breakdown. FPL Draft sends it as a pair,
// [[lines...], fixtureId]; classic FPL as {"fixture": id, "stats": [...]}
// with "identifier" for the stat. Both are accepted.
type rawExplain struct {
	Fixture int64
	Lines   []rawExplainLine
}

func (e *rawExplain) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		var pair []json.RawMessage
		if err := json.Unmarshal(b, &pair); err != nil {
			return err
		}
		if len(pair) != 2 {
			return fmt.Errorf("explain pair has %d items", len(pair))
		}
		if err := json.Unmarshal(pair[0], &e.Lines); err != nil {
			return err
		}
		return json.Unmarshal(pair[1], &e.Fixture)
	}
	var obj struct {
		Fixture int64 `json:"fixture"`
		Stats   []struct {
			Identifier string `json:"identifier"`
			Points     int    `json:"points"`
			Value      int    `json:"value"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	e.Fixture = obj.Fixture
	for _, s := range obj.Stats {
		e.Lines = append(e.Lines, rawExplainLine{Stat: s.Identifier, Points: s.Points, Value: s.Value})
	}
	return nil
}

type rawLiveElement struct {
	Stats   *rawLiveStats `json:"stats"`
	Explain []rawExplain  `json:"explain"`
}

type rawLive struct {
	Elements map[string]rawLiveElement `json:"elements"`
	Fixtures []rawFixture              `json:"fixtures"`
}

func decodeLive(body []byte, gameweek int, idx *bootstrapIndex) (*provider.EventLive, error) {
	var raw rawLive
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	if raw.Elements == nil {
		return nil, errors.New("no elements object")
	}
	out := &provider.EventLive{Gameweek: gameweek, Payload: body}

	// Fixture ids resolve through the live payload's own fixtures first,
	// then the bootstrap's.
	fxCode := map[int64]string{}
	for id, c := range idx.fixtureCode {
		fxCode[id] = c
	}
	// Per-fixture BPS, by fixture id then element id: the only split of BPS
	// in a double gameweek.
	bpsByFixture := map[int64]map[int64]int{}
	for _, f := range raw.Fixtures {
		if m := fixtureBPS(f); m != nil {
			for el := range m {
				if _, ok := idx.elementCode[el]; !ok {
					return nil, fmt.Errorf("fixture %d: bps for element %d, not in bootstrap-static", f.ID, el)
				}
			}
			bpsByFixture[f.ID] = m
		}
	}
	if len(raw.Fixtures) > 0 {
		fixtures, codes, err := mapFixtures(raw.Fixtures, idx.teamCode)
		if err != nil {
			return nil, err
		}
		for _, f := range fixtures {
			if f.Gameweek != 0 && f.Gameweek != gameweek {
				return nil, fmt.Errorf("fixture %s is in gameweek %d, not %d", f.ExternalID, f.Gameweek, gameweek)
			}
		}
		for id, c := range codes {
			fxCode[id] = c
		}
		out.Fixtures = fixtures
	}

	// FPL lists every player once a gameweek is under way. An empty
	// element map after kickoff is a broken snapshot: storing it would
	// zero every score (ADR 0005).
	if len(raw.Elements) == 0 {
		gwFixtures := out.Fixtures
		if len(gwFixtures) == 0 {
			for _, f := range idx.fixtures {
				if f.Gameweek == gameweek {
					gwFixtures = append(gwFixtures, f)
				}
			}
		}
		for _, f := range gwFixtures {
			switch f.Status {
			case provider.FixtureLive, provider.FixtureFinishedProvisional, provider.FixtureFinished:
				return nil, fmt.Errorf("no elements although fixture %s has started", f.ExternalID)
			}
		}
	}

	keys := make([]int64, 0, len(raw.Elements))
	for k := range raw.Elements {
		id, err := strconv.ParseInt(k, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("element key %q is not a number", k)
		}
		keys = append(keys, id)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })

	for _, id := range keys {
		e := raw.Elements[strconv.FormatInt(id, 10)]
		ext, ok := idx.elementCode[id]
		if !ok {
			return nil, fmt.Errorf("element %d is not in bootstrap-static", id)
		}
		s := e.Stats
		if s == nil || s.Minutes == nil {
			return nil, fmt.Errorf("element %d: missing stats", id)
		}
		if *s.Minutes < 0 || *s.Minutes > 300 || s.BPS < -100 || s.Bonus < 0 || s.Bonus > 6 {
			return nil, fmt.Errorf("element %d: implausible stats", id)
		}
		for _, n := range []int{s.GoalsScored, s.Assists, s.CleanSheets, s.GoalsConceded, s.OwnGoals,
			s.PenaltiesSaved, s.PenaltiesMissed, s.YellowCards, s.RedCards, s.Saves,
			s.ClearancesBlocksInterceptions, s.Recoveries, s.Tackles, s.DefensiveContribution} {
			if n < 0 {
				return nil, fmt.Errorf("element %d: negative stat", id)
			}
		}
		pl := provider.PlayerLive{
			PlayerExternalID:              ext,
			Minutes:                       *s.Minutes,
			Bonus:                         s.Bonus,
			BPS:                           s.BPS,
			ClearancesBlocksInterceptions: s.ClearancesBlocksInterceptions,
			Recoveries:                    s.Recoveries,
			Tackles:                       s.Tackles,
			DefensiveContribution:         s.DefensiveContribution,
			GoalsScored:                   s.GoalsScored,
			Assists:                       s.Assists,
			CleanSheets:                   s.CleanSheets,
			GoalsConceded:                 s.GoalsConceded,
			OwnGoals:                      s.OwnGoals,
			PenaltiesSaved:                s.PenaltiesSaved,
			PenaltiesMissed:               s.PenaltiesMissed,
			YellowCards:                   s.YellowCards,
			RedCards:                      s.RedCards,
			Saves:                         s.Saves,
			Influence:                     s.Influence.value(),
			Creativity:                    s.Creativity.value(),
			Threat:                        s.Threat.value(),
			ICTIndex:                      s.ICTIndex.value(),
			ExpectedGoals:                 s.ExpectedGoals.value(),
			ExpectedAssists:               s.ExpectedAssists.value(),
			ExpectedGoalInvolvements:      s.ExpectedGoalInvolvements.value(),
			ExpectedGoalsConceded:         s.ExpectedGoalsConceded.value(),
		}
		if s.Starts != nil && *s.Starts >= 0 {
			pl.Starts = s.Starts
		}
		if s.TotalPoints != nil {
			pl.TotalPoints = *s.TotalPoints
		}
		for _, ex := range e.Explain {
			fc, ok := fxCode[ex.Fixture]
			if !ok {
				return nil, fmt.Errorf("element %d: explain references unknown fixture %d", id, ex.Fixture)
			}
			fe := provider.FixtureExplain{FixtureExternalID: fc}
			// FPL lists only non-zero values, so a fixture with a BPS
			// stat that leaves the player out gives him 0 in it.
			if m, ok := bpsByFixture[ex.Fixture]; ok {
				b := m[id]
				fe.BPS = &b
			}
			for _, l := range ex.Lines {
				if l.Stat == "" {
					return nil, fmt.Errorf("element %d: explain line without a stat", id)
				}
				fe.Lines = append(fe.Lines, provider.ExplainLine{Stat: provider.Stat(l.Stat), Value: l.Value, Points: l.Points})
			}
			pl.Explain = append(pl.Explain, fe)
		}
		out.Players = append(out.Players, pl)
	}
	return out, nil
}

// ------------------------------------------------------------ game status

type rawGame struct {
	CurrentEvent     *int  `json:"current_event"`
	NextEvent        *int  `json:"next_event"`
	WaiversProcessed bool  `json:"waivers_processed"`
	CurrentFinished  *bool `json:"current_event_finished"`
}

type rawEventStatus struct {
	Status []struct {
		BonusAdded bool   `json:"bonus_added"`
		Date       string `json:"date"`
		Event      int    `json:"event"`
		Points     string `json:"points"`
	} `json:"status"`
	Leagues string `json:"leagues"`
}

func decodeGame(body []byte) (*rawGame, error) {
	var g rawGame
	if err := decodeStrict(body, &g); err != nil {
		return nil, err
	}
	if g.CurrentEvent == nil || *g.CurrentEvent < 1 || *g.CurrentEvent > 38 {
		return nil, errors.New("missing or bad current_event")
	}
	if g.NextEvent != nil && (*g.NextEvent < 1 || *g.NextEvent > 38) {
		return nil, errors.New("bad next_event")
	}
	return &g, nil
}

func decodeEventStatus(body []byte) (*rawEventStatus, error) {
	var es rawEventStatus
	if err := decodeStrict(body, &es); err != nil {
		return nil, err
	}
	for _, s := range es.Status {
		if s.Event < 1 || s.Event > 38 {
			return nil, fmt.Errorf("event-status: event %d out of range", s.Event)
		}
	}
	return &es, nil
}

func gameweekStatus(g *rawGame, es *rawEventStatus, idx *bootstrapIndex) *provider.GameweekStatus {
	cur := *g.CurrentEvent
	st := &provider.GameweekStatus{Current: cur, Next: g.NextEvent, WaiversProcessed: g.WaiversProcessed}
	days := 0
	added := 0
	var latest time.Time
	for _, s := range es.Status {
		if s.Event != cur {
			continue
		}
		days++
		if s.BonusAdded {
			added++
			if _, err := time.Parse("2006-01-02", s.Date); err == nil {
				if st.BonusAddedOn == nil {
					st.BonusAddedOn = map[string]bool{}
				}
				st.BonusAddedOn[s.Date] = true
			}
		}
		if d, err := time.Parse("2006-01-02", s.Date); err == nil && d.After(latest) {
			latest = d
		}
	}
	st.BonusAdded = days > 0 && added == days
	st.LeaguesUpdatedFor = latest
	// /game says when the current event is finished on every poll, so the
	// flip is seen without waiting for the next bootstrap.
	st.DataChecked = g.CurrentFinished != nil && *g.CurrentFinished
	if ev, ok := idx.events[cur]; ok {
		st.DataChecked = st.DataChecked || ev.checked()
	}
	return st
}
