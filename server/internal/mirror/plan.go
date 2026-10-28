package mirror

import (
	"cmp"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Snapshot is everything fetched from upstream for one league.
type Snapshot struct {
	Details      *provider.LeagueDetails
	Status       []provider.ElementStatus
	Choices      []provider.DraftChoice
	Transactions []provider.Transaction
	Trades       []provider.Trade
	// Picks are by league-entry ExternalID, then gameweek: every gameweek
	// whose deadline has passed and whose picks upstream publishes.
	Picks map[string]map[int]*provider.EntryPicks
}

// Plan is the league as upstream says it should be, in our player ids and
// upstream seat ids (LeagueEntry.ExternalID): what Apply makes the State
// match. Building it is pure.
type Plan struct {
	Name     string
	Settings league.Settings
	// Seats are the real teams in seat order (seat n is Seats[n-1]).
	Seats   []Seat
	Draft   PlanDraft
	Rosters []PlanSlot
	Lineups []PlanLineup
	Matches []PlanMatch
	Txns    []PlanTxn
	Trades  []PlanTrade
	// Priority is the next waiver run's order (upstream's waiver_pick),
	// seats first to last.
	Priority []string
	// Problems are settings we could not map: unverified report items.
	Problems []Item

	players map[string]string // upstream player id -> ours, as built
}

// Seat is one real team.
type Seat struct {
	ExternalID  string
	EntryID     int64
	TeamName    string
	ManagerName string
	JoinedAt    *time.Time
	WaiverPick  int
}

// PlanDraft is the completed draft.
type PlanDraft struct {
	Gameweek    int
	Order       []string // round 1, seats
	Picks       []PlanPick
	CompletedAt *time.Time
	PickTimerS  int
}

// PlanPick is one draft pick.
type PlanPick struct {
	Overall  int
	Seat     string
	PlayerID string
	Auto     bool
	At       time.Time
}

// PlanSlot is a player on a seat for gameweeks [FromGW, ToGW) (ToGW 0:
// still there).
type PlanSlot struct {
	Seat     string
	PlayerID string
	FromGW   int
	ToGW     int
	Via      string
}

// PlanLineup is a seat's locked lineup of a gameweek, rebuilt from the
// picks with the auto-subs undone, and the subs upstream made.
type PlanLineup struct {
	Seat     string
	Gameweek int
	Slots    []string
	Subs     []Sub
}

// Sub is an auto-sub: Out left the XI, In came off the bench.
type Sub struct{ Out, In string }

// PlanMatch is one head-to-head match; an empty side is the league average.
type PlanMatch struct {
	Key        string
	Gameweek   int
	Home, Away string
	HomePts    int
	AwayPts    int
	Finished   bool
}

// PlanTxn is one waiver or free-agent move for the feed.
type PlanTxn struct {
	ExternalID  string
	Kind        string // league.TxnWaiver | league.TxnFreeAgent
	Gameweek    int
	At          time.Time
	Seat        string
	In, Out     string
	Approved    bool
	Reason      string // league.ClaimInUnavailable | league.ClaimOutUnavailable
	Priority    int
	SourceState string
}

// PlanTrade is one trade FPL shows: processed, vetoed, accepted and
// waiting (in review here), or closed without happening.
type PlanTrade struct {
	ExternalID string
	// Status is league.TradeProcessed, TradeVetoed, TradeInReview
	// (accepted on FPL, waiting for its approval or processing), or
	// TradeDeclined, TradeWithdrawn, TradeExpired or TradeFailed (closed
	// without moving anyone; it only closes a trade imported while it was
	// pending).
	Status      string
	SourceState string
	Offer       string
	Receive     string
	Offered     []string
	Requested   []string
	Gameweek    int
	OfferedAt   time.Time
	ResolvedAt  *time.Time
}

// Unsupported reports why a league cannot be imported, or nil.
func Unsupported(d *provider.LeagueDetails) *league.Error {
	switch {
	case d.Scoring != provider.ScoringH2H:
		return league.Errorf(league.KindInvalid, "unsupported-league", "Unsupported league",
			"Only head-to-head FPL Draft leagues can be imported.")
	case !d.DraftCompleted:
		return league.Errorf(league.KindInvalid, "draft-not-complete", "Draft not complete",
			"This league has not finished its draft yet. Import it after the draft.")
	}
	n := len(realSeats(d))
	if n < 2 || n > league.MaxTeams {
		return league.Errorf(league.KindInvalid, "unsupported-league", "Unsupported league",
			"An imported league needs 2 to 20 teams.")
	}
	return nil
}

// realSeats are the league's real entries in seat order: by league-entry
// id, so a seat's number never changes.
func realSeats(d *provider.LeagueDetails) []provider.LeagueEntry {
	var out []provider.LeagueEntry
	for _, e := range d.Teams {
		if !e.IsAverage {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b provider.LeagueEntry) int {
		x, _ := strconv.ParseInt(a.ExternalID, 10, 64)
		y, _ := strconv.ParseInt(b.ExternalID, 10, 64)
		return cmp.Compare(x, y)
	})
	return out
}

// Build turns a snapshot into a plan. players maps upstream player ids to
// ours and position gives our players' positions; a player we do not
// know fails the build (the stats must be ingested first). It also checks that the moves replay: every move takes
// a player its team owns and gives one nobody owns.
func Build(snap *Snapshot, players map[string]string, position func(playerID string) scoring.Position) (*Plan, error) {
	d := snap.Details
	if err := Unsupported(d); err != nil {
		return nil, err
	}
	player := func(ext string) (string, error) {
		id, ok := players[ext]
		if !ok {
			return "", fmt.Errorf("mirror: player %s is not in our stats yet", ext)
		}
		return id, nil
	}
	p := &Plan{Name: d.Name, players: players}
	seat := map[string]bool{}
	for _, e := range realSeats(d) {
		s := Seat{ExternalID: e.ExternalID, TeamName: e.TeamName, ManagerName: e.ManagerName, JoinedAt: e.JoinedAt, WaiverPick: e.WaiverPick}
		s.EntryID, _ = strconv.ParseInt(e.EntryID, 10, 64)
		p.Seats = append(p.Seats, s)
		seat[e.ExternalID] = true
	}
	side := func(ext string) string {
		if seat[ext] {
			return ext
		}
		return "" // the average entry
	}

	p.Settings = league.Settings{
		TeamCount: len(p.Seats), PickTimerSeconds: cmp.Or(d.PickTimeSeconds, 60), DraftStartsAt: d.DraftAt,
		TradeReview: league.ReviewCommissioner, ReviewHours: 24, VetoThreshold: 0.5,
		StartGameweek: d.StartGameweek, EndGameweek: d.EndGameweek,
		// FPL Draft's waiver order (ADR 0008 rule 12, amended 2026-10-15).
		WaiverOrder: league.WaiverReverseStandings,
	}
	if d.TradeMode != provider.TradeModeCommissioner {
		p.Problems = append(p.Problems, Item{Key: "setting:trades", Kind: KindSetting, Status: ItemUnverified,
			Ours: league.ReviewCommissioner, Theirs: d.TradeModeSource,
			Note: "FPL's trade setting has no equivalent here; trades will need the commissioner's review."})
	}
	if d.TransactionMode != provider.TransactionModeWaivers {
		p.Problems = append(p.Problems, Item{Key: "setting:transactions", Kind: KindSetting, Status: ItemUnverified,
			Ours: "waivers", Theirs: d.TransactionModeSource,
			Note: "FPL's transaction setting has no equivalent here; players will move by waivers and free agency."})
	}

	// The draft.
	p.Draft = PlanDraft{Gameweek: cmp.Or(d.DraftGameweek, d.StartGameweek), CompletedAt: d.DraftCompletedAt, PickTimerS: p.Settings.PickTimerSeconds}
	for _, ch := range snap.Choices {
		if !seat[ch.LeagueEntry] {
			return nil, fmt.Errorf("mirror: draft pick %d by a team that is not in the league", ch.Overall)
		}
		id, err := player(ch.PlayerExternalID)
		if err != nil {
			return nil, err
		}
		p.Draft.Picks = append(p.Draft.Picks, PlanPick{Overall: ch.Overall, Seat: ch.LeagueEntry, PlayerID: id, Auto: ch.WasAuto, At: ch.At})
		if ch.Round == 1 {
			p.Draft.Order = append(p.Draft.Order, ch.LeagueEntry)
		}
	}
	if len(p.Draft.Picks) == 0 {
		return nil, fmt.Errorf("mirror: the draft has no picks")
	}

	// Moves for the feed, and the ones that happened for the replay.
	type move struct {
		gw  int
		at  time.Time
		ext string
		txn *PlanTxn
		trd *PlanTrade
	}
	var moves []move
	for _, x := range snap.Transactions {
		in, err := player(x.PlayerIn)
		if err != nil {
			return nil, err
		}
		out, err := player(x.PlayerOut)
		if err != nil {
			return nil, err
		}
		t := PlanTxn{ExternalID: x.ExternalID, Gameweek: x.Gameweek, At: x.ProcessedAt, Seat: x.LeagueEntry, In: in, Out: out,
			SourceState: x.SourceState, Kind: league.TxnWaiver}
		if x.Kind == provider.TransactionFreeAgent {
			t.Kind = league.TxnFreeAgent
		}
		if x.Priority != nil {
			t.Priority = *x.Priority
		}
		switch x.Result {
		case provider.ClaimApproved:
			t.Approved = true
		case provider.ClaimDeniedPlayerInUnavailable:
			t.Reason = league.ClaimInUnavailable
		case provider.ClaimDeniedPlayerOutUnavailable:
			t.Reason = league.ClaimOutUnavailable
		default:
			return nil, fmt.Errorf("mirror: transaction %s has an unknown result", x.ExternalID)
		}
		p.Txns = append(p.Txns, t)
	}
	for i := range p.Txns {
		if t := &p.Txns[i]; t.Approved {
			moves = append(moves, move{gw: t.Gameweek, at: t.At, ext: "t" + t.ExternalID, txn: t})
		}
	}
	for _, x := range snap.Trades {
		tr := PlanTrade{ExternalID: x.ExternalID, SourceState: x.SourceState, Offer: x.OfferEntry, Receive: x.ReceiveEntry,
			Gameweek: x.Gameweek, OfferedAt: x.OfferedAt, ResolvedAt: x.ResolvedAt}
		switch x.State {
		case provider.TradeProcessed:
			tr.Status = league.TradeProcessed
		case provider.TradeVetoed:
			tr.Status = league.TradeVetoed
		case provider.TradeAccepted:
			// Accepted, waiting for FPL's approval or processing: pending
			// here too; nobody moves until FPL processes it.
			tr.Status = league.TradeInReview
		case provider.TradeRejected:
			tr.Status = league.TradeDeclined
		case provider.TradeWithdrawn:
			tr.Status = league.TradeWithdrawn
		case provider.TradeExpired:
			tr.Status = league.TradeExpired
		case provider.TradeInvalid:
			tr.Status = league.TradeFailed
		case provider.TradeOffered:
			continue // an open offer: private in FPL, nothing to mirror
		default:
			// A code we do not know (FPL's rejected, withdrawn, expired and
			// invalid have none on record). If nobody ever moved, it closed
			// without happening and needs no counterpart, like a declined one.
			if !tradeMoved(snap, x) {
				continue
			}
			p.Problems = append(p.Problems, Item{Key: "trade-state:" + x.ExternalID, Kind: KindSetting, Status: ItemUnverified,
				Ours: "not imported", Theirs: x.SourceState, Note: "An FPL trade in a state we do not recognise."})
			continue
		}
		for i := range x.Offered {
			a, err := player(x.Offered[i])
			if err != nil {
				return nil, err
			}
			b, err := player(x.Requested[i])
			if err != nil {
				return nil, err
			}
			tr.Offered, tr.Requested = append(tr.Offered, a), append(tr.Requested, b)
		}
		p.Trades = append(p.Trades, tr)
	}
	for i := range p.Trades {
		if t := &p.Trades[i]; t.Status == league.TradeProcessed {
			at := t.OfferedAt
			if t.ResolvedAt != nil {
				at = *t.ResolvedAt
			}
			moves = append(moves, move{gw: t.Gameweek, at: at, ext: "r" + t.ExternalID, trd: t})
		}
	}
	// A move's gameweek is the one it counts from; within one, in the
	// order they happened.
	slices.SortStableFunc(moves, func(a, b move) int {
		return cmp.Or(cmp.Compare(a.gw, b.gw), a.at.Compare(b.at), cmp.Compare(a.ext, b.ext))
	})

	// The replay: the draft, then every move.
	r := replay{owner: map[string]int{}}
	for _, pk := range p.Draft.Picks {
		if err := r.acquire(pk.Seat, pk.PlayerID, p.Draft.Gameweek, "draft"); err != nil {
			return nil, fmt.Errorf("mirror: draft pick %d: %w", pk.Overall, err)
		}
	}
	for _, m := range moves {
		var err error
		if t := m.txn; t != nil {
			if err = r.release(t.Seat, t.Out, t.Gameweek); err == nil {
				err = r.acquire(t.Seat, t.In, t.Gameweek, t.Kind)
			}
			if err != nil {
				return nil, fmt.Errorf("mirror: transaction %s does not replay: %w", t.ExternalID, err)
			}
			continue
		}
		t := m.trd
		for i := range t.Offered {
			if err = r.release(t.Offer, t.Offered[i], t.Gameweek); err != nil {
				break
			}
			if err = r.release(t.Receive, t.Requested[i], t.Gameweek); err != nil {
				break
			}
		}
		for i := range t.Offered {
			if err != nil {
				break
			}
			if err = r.acquire(t.Receive, t.Offered[i], t.Gameweek, "trade"); err == nil {
				err = r.acquire(t.Offer, t.Requested[i], t.Gameweek, "trade")
			}
		}
		if err != nil {
			return nil, fmt.Errorf("mirror: trade %s does not replay: %w", t.ExternalID, err)
		}
	}
	for _, s := range r.slots {
		if s.ToGW >= 0 {
			p.Rosters = append(p.Rosters, s)
		}
	}

	// Lineups, with the subs undone.
	for _, s := range p.Seats {
		gws := make([]int, 0, len(snap.Picks[s.ExternalID]))
		for gw := range snap.Picks[s.ExternalID] {
			gws = append(gws, gw)
		}
		slices.Sort(gws)
		for _, gw := range gws {
			lu, err := lineup(snap.Picks[s.ExternalID][gw], player, position)
			if err != nil {
				return nil, fmt.Errorf("mirror: %s gameweek %d: %w", s.TeamName, gw, err)
			}
			lu.Seat = s.ExternalID
			p.Lineups = append(p.Lineups, lu)
		}
	}

	for _, m := range d.Matches {
		home, away := side(m.HomeEntry), side(m.AwayEntry)
		p.Matches = append(p.Matches, PlanMatch{
			Key: strconv.Itoa(m.Gameweek) + ":" + m.HomeEntry + ":" + m.AwayEntry, Gameweek: m.Gameweek,
			Home: home, Away: away, HomePts: m.HomePts, AwayPts: m.AwayPts, Finished: m.Finished,
		})
	}

	order := slices.Clone(p.Seats)
	slices.SortStableFunc(order, func(a, b Seat) int { return cmp.Compare(a.WaiverPick, b.WaiverPick) })
	for _, s := range order {
		p.Priority = append(p.Priority, s.ExternalID)
	}
	return p, nil
}

// lineup rebuilds the locked lineup from upstream's picks, which are after
// auto-subs. FPL lists them with the bench as it was, except that each
// player who came off the bench left the player he replaced in his bench
// slot, and with the XI that scored sorted by position (GKP, DEF, MID,
// FWD). So the bench is rebuilt by swapping back in place, and the XI by
// position, with the players who were subbed out first within their
// position, in the order FPL subbed them: FPL takes the starters in XI
// order, so a starter it subbed came before a same-position starter that
// also did not play but stayed (once one is subbed, a later one of the
// same position finds no other bench player). The order of starters who
// played does not matter to the auto-subs.
func lineup(ep *provider.EntryPicks, player func(string) (string, error), position func(string) scoring.Position) (PlanLineup, error) {
	lu := PlanLineup{Gameweek: ep.Gameweek}
	slots := make([]string, len(ep.Picks))
	for i, pk := range ep.Picks {
		id, err := player(pk.PlayerExternalID)
		if err != nil {
			return lu, err
		}
		slots[i] = id
	}
	if len(slots) != 15 {
		return lu, fmt.Errorf("%d picks", len(slots))
	}
	in := map[string]bool{}
	for _, s := range ep.AutoSubs {
		a, err := player(s.InExternalID)
		if err != nil {
			return lu, err
		}
		b, err := player(s.OutExternalID)
		if err != nil {
			return lu, err
		}
		lu.Subs = append(lu.Subs, Sub{Out: b, In: a})
		in[a] = true
	}
	xi, bench := slices.Clone(slots[:11]), slices.Clone(slots[11:])
	var out []string
	for _, sb := range lu.Subs {
		i := slices.Index(bench, sb.Out)
		if i < 0 || !slices.Contains(xi, sb.In) {
			return lu, fmt.Errorf("sub %s for %s is not in the picks", sb.In, sb.Out)
		}
		bench[i] = sb.In
		out = append(out, sb.Out)
	}
	stayed := slices.DeleteFunc(xi, func(id string) bool { return in[id] })
	lu.Slots = make([]string, 0, 15)
	for _, pos := range []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD} {
		for _, group := range [][]string{out, stayed} {
			for _, id := range group {
				if position(id) == pos {
					lu.Slots = append(lu.Slots, id)
				}
			}
		}
	}
	if len(lu.Slots) != 11 {
		return lu, fmt.Errorf("the XI has %d players of known positions", len(lu.Slots))
	}
	lu.Slots = append(lu.Slots, bench...)
	return lu, nil
}

// replay rebuilds ownership over gameweek ranges.
type replay struct {
	slots []PlanSlot
	owner map[string]int // player -> index of his open slot
}

func (r *replay) acquire(seat, player string, gw int, via string) error {
	if i, ok := r.owner[player]; ok {
		return fmt.Errorf("player %s is already on %s", player, r.slots[i].Seat)
	}
	r.owner[player] = len(r.slots)
	r.slots = append(r.slots, PlanSlot{Seat: seat, PlayerID: player, FromGW: gw, Via: via})
	return nil
}

func (r *replay) release(seat, player string, gw int) error {
	i, ok := r.owner[player]
	if !ok || r.slots[i].Seat != seat {
		return fmt.Errorf("player %s is not on %s", player, seat)
	}
	delete(r.owner, player)
	if r.slots[i].FromGW >= gw {
		r.slots[i].ToGW = -1 // never started: dropped from the result
		return nil
	}
	r.slots[i].ToGW = gw
	return nil
}

// tradeMoved reports whether trade x's players may have changed hands:
// false only when every offered player stayed with the offering entry and
// every requested one with the receiving entry, both now (element-status)
// and in each gameweek's picks from the trade's gameweek to the last one
// upstream shows. A move later undone leaves the current owners as they
// were, so the history decides; a gameweek without both teams' picks
// cannot show that nobody moved. Neither can a move and move-back
// between two deadlines, which no picks show: any other transaction or
// trade, in any state, touching the trade's players at or after its offer
// time counts as a possible move.
func tradeMoved(snap *Snapshot, x provider.Trade) bool {
	if len(x.Offered) == 0 && len(x.Requested) == 0 {
		return true
	}
	// Without an offer time nothing can be dated after it, so no evidence
	// can show that nobody moved.
	if x.OfferedAt.IsZero() {
		return true
	}
	if touchedSince(snap, x) {
		return true
	}
	owner := map[string]string{}
	for _, e := range snap.Status {
		if e.OwnerEntry != nil {
			owner[e.PlayerExternalID] = *e.OwnerEntry
		}
	}
	if !stayed(x, func(id string) string { return owner[id] }) {
		return true
	}
	last := 0
	for _, gws := range snap.Picks {
		for gw := range gws {
			last = max(last, gw)
		}
	}
	// No picks fetched at all: the history is missing, not empty. (A trade
	// newer than the last picks needs none: the owners now and the events
	// since it already cover that span.)
	if last == 0 {
		return true
	}
	for gw := x.Gameweek; gw <= last; gw++ {
		in := map[string]string{}
		for _, seat := range []string{x.OfferEntry, x.ReceiveEntry} {
			ep := snap.Picks[seat][gw]
			if ep == nil {
				return true
			}
			for _, pk := range ep.Picks {
				in[pk.PlayerExternalID] = seat
			}
		}
		if !stayed(x, func(id string) string { return in[id] }) {
			return true
		}
	}
	return false
}

// touchedSince reports whether a transaction or another trade involving
// one of trade x's players happened at or after x's offer time. A zero
// time is unknown, so it counts.
func touchedSince(snap *Snapshot, x provider.Trade) bool {
	ours := map[string]bool{}
	for _, id := range append(slices.Clone(x.Offered), x.Requested...) {
		ours[id] = true
	}
	since := func(t time.Time) bool { return t.IsZero() || !t.Before(x.OfferedAt) }
	for _, tx := range snap.Transactions {
		if (ours[tx.PlayerIn] || ours[tx.PlayerOut]) && since(tx.ProcessedAt) {
			return true
		}
	}
	for _, o := range snap.Trades {
		if o.ExternalID == x.ExternalID || !since(o.OfferedAt) {
			continue
		}
		for _, id := range append(slices.Clone(o.Offered), o.Requested...) {
			if ours[id] {
				return true
			}
		}
	}
	return false
}

// stayed reports whether owner has trade x's players with their original
// teams.
func stayed(x provider.Trade, owner func(player string) string) bool {
	for _, id := range x.Offered {
		if owner(id) != x.OfferEntry {
			return false
		}
	}
	for _, id := range x.Requested {
		if owner(id) != x.ReceiveEntry {
			return false
		}
	}
	return true
}
