package league

import (
	"cmp"
	"maps"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// State is everything about one league that the draft, lineup, waiver and
// trade rules change (ADR 0010): the league, its game state, the draft,
// waiver and trade records. A mutation loads it with Tx.LoadState (the league row
// locked for update, so changes to one league queue up), changes it in Go
// and saves it with Tx.SaveState, which writes only what changed. The rules
// live in the draft, lineup, waivers and trades packages; this package only
// loads and stores.
//
// Closed records are not loaded: Claims holds the pending claims and Trades
// the open trades (offered, accepted, in_review). Records a mutation closes
// stay in the slice with their new status and are saved. The activity feed
// is append-only: NewTxns are inserted, never loaded.
type State struct {
	League *League
	Game   *Game
	// Draft is nil until the draft row exists (the draft package creates it).
	Draft *Draft
	// Priority is the order the last waiver run ended with (the draft's
	// reverse before any), team ids first to last: the next run's start
	// when the league's waiver order is rolling (WaiverOrder).
	Priority []string
	Claims   []Claim
	Trades   []Trade
	Locks    []WaiverLock
	// Block is the league's trade block: players their teams offer around,
	// oldest first. SaveState drops an entry once its team no longer holds
	// the player (a drop, trade or waiver), so no rule package has to.
	Block []BlockEntry
	// Runs records once-per-gameweek work done (RunKey -> when).
	Runs map[RunKey]time.Time
	// Refs map upstream ids of an imported league's rows (teams, matches,
	// transactions, trades) to ours (docs/adr/0012). They are saved with
	// the rows they name, so a re-run of an import finds what it made.
	// Refs are only ever added.
	Refs map[RefKey]string

	NewTxns     []Txn
	NewMatchups []Matchup

	// DropLocalMoves makes SaveState delete, before anything else, the
	// closed claims, closed trades and feed entries that no ref names:
	// the moves made here rather than imported. A re-sync of an imported
	// league sets it (docs/adr/0012).
	DropLocalMoves bool

	orig *State
}

// RefKey is an upstream id of one of the league's rows.
type RefKey struct {
	Entity     string // RefTeam | RefMatch | RefTransaction | RefTrade
	Provider   string // "fpl"
	ExternalID string
}

// Ref entities.
const (
	RefTeam        = "team"
	RefMatch       = "match"
	RefTransaction = "transaction"
	RefTrade       = "trade"
)

// Ref returns our id for an upstream one.
func (s *State) Ref(entity, provider, externalID string) (string, bool) {
	id, ok := s.Refs[RefKey{entity, provider, externalID}]
	return id, ok
}

// SetRef records our id for an upstream one.
func (s *State) SetRef(entity, provider, externalID, internalID string) {
	if s.Refs == nil {
		s.Refs = map[RefKey]string{}
	}
	s.Refs[RefKey{entity, provider, externalID}] = internalID
}

// RunKey identifies a once-per-gameweek job for a league.
type RunKey struct {
	Kind     string // RunWaivers | RunLineupLock
	Gameweek int
}

// Run kinds.
const (
	RunWaivers    = "waivers"
	RunLineupLock = "lineup_lock"
)

// Ran reports whether the run happened and when.
func (s *State) Ran(kind string, gw int) (time.Time, bool) {
	t, ok := s.Runs[RunKey{kind, gw}]
	return t, ok
}

// Draft statuses (DraftStatus in the contract).
const (
	DraftScheduled  = "scheduled"
	DraftInProgress = "in_progress"
	DraftPaused     = "paused"
	DraftComplete   = "complete"
)

// Draft is a league's snake draft and the teams' queues.
type Draft struct {
	ID             string
	Status         string
	PickTimerS     int
	ClockStartedAt *time.Time
	PausedAt       *time.Time
	CompletedAt    *time.Time
	// Order is round 1, team ids; empty until set, randomised or started.
	Order    []string
	Autopick map[string]bool
	// StayManual are the teams that turned "autopick when my clock runs
	// out" off: a missed clock still picks for them (queue, then rank) but
	// leaves their autopick off.
	StayManual map[string]bool
	Picks      []Pick
	// Queues are the teams' private queues (player ids, in order).
	Queues map[string][]string
}

// Pick methods (DraftPickMethod in the contract).
const (
	PickManual       = "manual"
	PickQueue        = "queue"
	PickRank         = "rank"
	PickCommissioner = "commissioner"
)

// Pick is one draft pick.
type Pick struct {
	ID       string
	Overall  int
	TeamID   string
	PlayerID string
	Method   string
	PickedAt time.Time
	// MissedClock: the team's clock ran out and autopick chose the player.
	MissedClock bool
}

// Claim statuses (WaiverClaimStatus) and failure reasons
// (WaiverClaimFailure in the contract).
const (
	ClaimPending   = "pending"
	ClaimWon       = "won"
	ClaimLost      = "lost"
	ClaimCancelled = "cancelled"

	ClaimInUnavailable  = "player_in_unavailable"
	ClaimOutUnavailable = "player_out_unavailable"
	ClaimRosterLocked   = "roster_locked"
)

// Claim is a waiver claim.
type Claim struct {
	ID            string
	TeamID        string
	AddPlayerID   string
	DropPlayerID  string
	Order         int
	Gameweek      int
	Status        string
	FailureReason string
	ProcessedAt   *time.Time
	CreatedAt     time.Time
}

// Trade statuses (TradeStatus) and failures (TradeFailure).
const (
	TradeOffered   = "offered"
	TradeAccepted  = "accepted"
	TradeInReview  = "in_review"
	TradeProcessed = "processed"
	TradeDeclined  = "declined"
	TradeWithdrawn = "withdrawn"
	TradeExpired   = "expired"
	TradeVetoed    = "vetoed"
	TradeFailed    = "failed"

	TradeFailNotOnRoster   = "player_no_longer_on_roster"
	TradeFailRosterInvalid = "roster_invalid"
	// TradeFailResynced: the imported league was re-synced from FPL while
	// the trade was open (docs/adr/0012).
	TradeFailResynced = "league_resynced"
)

// TradeOpen reports whether a trade can still change.
func TradeOpen(status string) bool {
	return status == TradeOffered || status == TradeAccepted || status == TradeInReview
}

// Trade is a trade offer and its review.
type Trade struct {
	ID              string
	OfferingTeamID  string
	ReceivingTeamID string
	// Offered move from the offering team, Requested from the receiving one.
	Offered        []string
	Requested      []string
	Status         string
	Message        string
	CreatedAt      time.Time
	ExpiresAt      time.Time
	RespondedAt    *time.Time
	ReviewEndsAt   *time.Time
	ProcessedAt    *time.Time
	ReviewMode     string // set when accepted into review
	DecidedBy      string // the commissioner's user id, when one decided
	DecisionReason string
	FailureReason  string
	EffectiveGW    int
	// Votes are league-vote ballots by team id: veto | allow.
	Votes map[string]string
}

// BlockEntry is a player his team put on the trade block.
type BlockEntry struct {
	TeamID   string
	PlayerID string
	// Note is the team's short pitch ("open to offers"), may be empty.
	Note string
	// Wanted are the positions the team wants back (GKP, DEF, MID, FWD).
	Wanted  []string
	AddedAt time.Time
}

// BlockOf returns the player's trade-block entry, or nil.
func (s *State) BlockOf(playerID string) *BlockEntry {
	for i := range s.Block {
		if s.Block[i].PlayerID == playerID {
			return &s.Block[i]
		}
	}
	return nil
}

// Holds reports whether the team holds the player with no end set: on its
// roster now and not leaving in a processed trade or a drop.
func (g *Game) Holds(teamID, playerID string) bool {
	for _, r := range g.Rosters {
		if r.TeamID == teamID && r.PlayerID == playerID && r.ToGW == 0 {
			return true
		}
	}
	return false
}

// heldSince reports whether the team holds the player on a roster slot
// that was already stored: the same, unbroken stint since before this
// change. A slot added in this change (id 0) starts a new stint, even when
// the change also closed his old one (a re-sync that replays an exit and a
// return to the same team).
func (g *Game) heldSince(teamID, playerID string) bool {
	for _, r := range g.Rosters {
		if r.TeamID == teamID && r.PlayerID == playerID && r.ToGW == 0 {
			return r.ID != 0
		}
	}
	return false
}

// pruneBlock drops the trade-block entries whose team no longer holds the
// player in the stint he was listed in, and orders the rest oldest first,
// then by player id (as loaded). Every save runs it, so an entry kept is
// one whose stint has been unbroken since the player was listed.
func (s *State) pruneBlock() {
	s.Block = slices.DeleteFunc(s.Block, func(e BlockEntry) bool { return !s.Game.heldSince(e.TeamID, e.PlayerID) })
	slices.SortStableFunc(s.Block, func(a, b BlockEntry) int {
		return cmp.Or(a.AddedAt.Compare(b.AddedAt), cmp.Compare(a.PlayerID, b.PlayerID))
	})
}

// WaiverLock reasons.
const (
	LockAdded   = "added"
	LockDropped = "dropped"
)

// WaiverLock keeps a player that moved locked until the waiver run of
// UntilGW: a dropped player is on waivers, an added one cannot be dropped.
type WaiverLock struct {
	PlayerID string
	UntilGW  int
	Reason   string
}

// Transaction kinds (TransactionKind in the contract).
const (
	TxnWaiver    = "waiver"
	TxnFreeAgent = "free_agent"
	TxnTrade     = "trade"
	// TxnDraftAutopick is a draft pick made because the clock ran out.
	TxnDraftAutopick = "draft_autopick"
)

// Txn is one activity-feed entry. Waiver and free-agent entries use the
// team and player fields; trade entries the trade fields.
type Txn struct {
	ID         string
	Kind       string
	Gameweek   int
	OccurredAt time.Time

	TeamID    string `json:"teamId,omitempty"`
	PlayerIn  string `json:"playerIn,omitempty"`
	PlayerOut string `json:"playerOut,omitempty"`
	// Approved, or denied with Reason (ClaimDeniedReason).
	Approved bool   `json:"approved,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Priority int    `json:"priority,omitempty"`

	// SourceState is an imported entry's raw provider code (FPL kind/result
	// or trade state), for support only.
	SourceState string `json:"sourceState,omitempty"`

	// Draft autopick entries: the pick (overall) and how autopick chose
	// (PickQueue or PickRank); the player is PlayerIn.
	Overall int    `json:"overall,omitempty"`
	Method  string `json:"method,omitempty"`

	TradeID         string   `json:"tradeId,omitempty"`
	Status          string   `json:"status,omitempty"`
	OfferingTeamID  string   `json:"offeringTeamId,omitempty"`
	ReceivingTeamID string   `json:"receivingTeamId,omitempty"`
	Offered         []string `json:"offered,omitempty"`
	Requested       []string `json:"requested,omitempty"`
}

// snapshot records the loaded state, so SaveState can write the difference.
func (s *State) snapshot() { s.orig = s.clone() }

// clone deep-copies the state (without orig).
func (s *State) clone() *State {
	c := &State{
		Priority:    slices.Clone(s.Priority),
		Runs:        maps.Clone(s.Runs),
		Refs:        maps.Clone(s.Refs),
		Locks:       slices.Clone(s.Locks),
		Block:       cloneBlock(s.Block),
		NewTxns:     slices.Clone(s.NewTxns),
		NewMatchups: slices.Clone(s.NewMatchups),
	}
	c.DropLocalMoves = s.DropLocalMoves
	if s.League != nil {
		c.League = clone(s.League)
	}
	if s.Game != nil {
		c.Game = cloneGame(s.Game)
	}
	if s.Draft != nil {
		c.Draft = s.Draft.clone()
	}
	for _, cl := range s.Claims {
		cl.ProcessedAt = clonePtr(cl.ProcessedAt)
		c.Claims = append(c.Claims, cl)
	}
	for _, t := range s.Trades {
		c.Trades = append(c.Trades, t.clone())
	}
	return c
}

func cloneBlock(b []BlockEntry) []BlockEntry {
	if b == nil {
		return nil
	}
	out := make([]BlockEntry, len(b))
	for i, e := range b {
		e.Wanted = slices.Clone(e.Wanted)
		out[i] = e
	}
	return out
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (d *Draft) clone() *Draft {
	c := *d
	c.ClockStartedAt, c.PausedAt, c.CompletedAt = clonePtr(d.ClockStartedAt), clonePtr(d.PausedAt), clonePtr(d.CompletedAt)
	c.Order = slices.Clone(d.Order)
	c.Autopick = maps.Clone(d.Autopick)
	c.StayManual = maps.Clone(d.StayManual)
	c.Picks = slices.Clone(d.Picks)
	c.Queues = map[string][]string{}
	for k, v := range d.Queues {
		c.Queues[k] = slices.Clone(v)
	}
	return &c
}

// Clone deep-copies the trade.
func (t Trade) Clone() Trade { return t.clone() }

func (t Trade) clone() Trade {
	t.Offered, t.Requested = slices.Clone(t.Offered), slices.Clone(t.Requested)
	t.RespondedAt, t.ReviewEndsAt, t.ProcessedAt = clonePtr(t.RespondedAt), clonePtr(t.ReviewEndsAt), clonePtr(t.ProcessedAt)
	t.Votes = maps.Clone(t.Votes)
	return t
}

// Trade returns the loaded trade with id, or nil.
func (s *State) Trade(id string) *Trade {
	for i := range s.Trades {
		if s.Trades[i].ID == id {
			return &s.Trades[i]
		}
	}
	return nil
}

// Claim returns the pending claim with id, or nil.
func (s *State) Claim(id string) *Claim {
	for i := range s.Claims {
		if s.Claims[i].ID == id {
			return &s.Claims[i]
		}
	}
	return nil
}

// Lock returns the player's waiver lock that is still in force before the
// waiver run of gameweek runGW, or nil.
func (s *State) Lock(playerID string, runGW int) *WaiverLock {
	for i := range s.Locks {
		if s.Locks[i].PlayerID == playerID && s.Locks[i].UntilGW >= runGW {
			return &s.Locks[i]
		}
	}
	return nil
}

// SetLock locks a player until the waiver run of untilGW.
func (s *State) SetLock(playerID string, untilGW int, reason string) {
	s.Locks = slices.DeleteFunc(s.Locks, func(l WaiverLock) bool { return l.PlayerID == playerID })
	s.Locks = append(s.Locks, WaiverLock{PlayerID: playerID, UntilGW: untilGW, Reason: reason})
}

// PruneLocks drops locks that ended before the waiver run of gw.
func (s *State) PruneLocks(gw int) {
	s.Locks = slices.DeleteFunc(s.Locks, func(l WaiverLock) bool { return l.UntilGW < gw })
}

// ---------------------------------------------------------------- roster moves

// Release ends the team's ownership of the player from gameweek gw on. A
// slot that would only have started at gw (or later) is removed.
func (g *Game) Release(teamID, playerID string, gw int) {
	out := g.Rosters[:0]
	for _, r := range g.Rosters {
		if r.TeamID == teamID && r.PlayerID == playerID && (r.ToGW == 0 || r.ToGW > gw) {
			if r.FromGW >= gw {
				continue
			}
			r.ToGW = gw
		}
		out = append(out, r)
	}
	g.Rosters = out
}

// Acquire gives the player to the team from gameweek gw on.
func (g *Game) Acquire(teamID, playerID string, gw int, via string) {
	g.Rosters = append(g.Rosters, RosterSlot{TeamID: teamID, PlayerID: playerID, FromGW: gw, AcquiredVia: via})
}

// Swap replaces out with in in the team's lineups from gameweek gw on, so a
// saved lineup survives a like-for-like roster move (same position, so the
// formation stays legal). A lineup that started before gw is copied to a new
// one starting at gw. A team with no saved lineup keeps the default.
func (g *Game) Swap(teamID, out, in string, gw int, now time.Time) {
	cur, ok := g.LineupAt(teamID, gw)
	if !ok {
		return
	}
	if cur.FromGW < gw {
		g.Lineups = append(g.Lineups, Lineup{TeamID: teamID, FromGW: gw, Slots: slices.Clone(cur.Slots), UpdatedAt: now})
	}
	for i := range g.Lineups {
		l := &g.Lineups[i]
		if l.TeamID != teamID || l.FromGW < gw {
			continue
		}
		for j, id := range l.Slots {
			if id == out {
				l.Slots = slices.Clone(l.Slots)
				l.Slots[j] = in
			}
		}
	}
}

// SetLineup stores slots as the team's lineup from gw on, replacing only
// the lineup saved for gw itself. A lineup saved for a later gameweek stays:
// gw's lineup carries over only to the gameweeks before it.
func (g *Game) SetLineup(teamID string, gw int, slots []string, now time.Time) {
	g.Lineups = slices.DeleteFunc(g.Lineups, func(l Lineup) bool { return l.TeamID == teamID && l.FromGW == gw })
	g.Lineups = append(g.Lineups, Lineup{TeamID: teamID, FromGW: gw, Slots: slices.Clone(slots), UpdatedAt: now})
}

// PendingTrade reports whether the player is in a processed trade that
// takes effect after gameweek gw (one accepted after gw's trade deadline):
// his ownership changes after gw, so the rosters hold a slot of his that
// starts, or a slot covering gw that ends, after gw. Until then he cannot
// be dropped, claimed away or traded again; otherwise the team losing him
// would end its slot early while the trade still adds his replacement.
func (g *Game) PendingTrade(playerID string, gw int) bool {
	for _, r := range g.Rosters {
		if r.PlayerID == playerID && (r.FromGW > gw || (r.OnRosterAt(gw) && r.ToGW != 0)) {
			return true
		}
	}
	return false
}

// RepairLineup fits a saved lineup to the roster it no longer matches:
// players still on the roster keep their slots, and each slot of a player
// who left goes to the arrival of the same position (best draft rank
// first). ok is false when that cannot fill every slot exactly.
func RepairLineup(slots, roster []string, players map[string]stats.Player) (out []string, ok bool) {
	on := map[string]bool{}
	for _, id := range roster {
		on[id] = true
	}
	out = make([]string, len(slots))
	used := map[string]bool{}
	var holes []int
	for i, id := range slots {
		if on[id] && !used[id] {
			out[i], used[id] = id, true
		} else {
			holes = append(holes, i)
		}
	}
	var arrivals []string
	for _, id := range roster {
		if !used[id] {
			arrivals = append(arrivals, id)
		}
	}
	slices.SortFunc(arrivals, func(a, b string) int {
		return cmp.Or(cmp.Compare(players[a].DraftRank, players[b].DraftRank), cmp.Compare(a, b))
	})
	for _, i := range holes {
		left, known := players[slots[i]]
		j := slices.IndexFunc(arrivals, func(id string) bool { return known && players[id].Position == left.Position })
		if j < 0 {
			return nil, false
		}
		out[i] = arrivals[j]
		arrivals = slices.Delete(arrivals, j, j+1)
	}
	return out, len(arrivals) == 0
}
