package waivers

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Roster statuses (RosterStatus in the contract).
const (
	Rostered  = "rostered"
	OnWaivers = "waivers"
	FreeAgent = "free_agent"
)

// Cycle is where a league stands in the waiver cycle at a moment.
type Cycle struct {
	// Upcoming is the first gameweek whose deadline has not passed (39
	// after the last deadline): the gameweek free-agent moves are for.
	Upcoming int
	// RunGW is the gameweek of the next waiver run whose deadline has not
	// passed: claims made now are processed then and count from then. 39
	// when none is left.
	RunGW int
	// RunAt is RunGW's waiver deadline (zero when none is left).
	RunAt time.Time
	// FreeAgency: Upcoming's waiver run is done and its deadline has not
	// passed, so free agents can be added.
	FreeAgency bool
	// LastRunAt is the newest waiver run of the league, if any.
	LastRunAt *time.Time
	// Phase is the transaction window (PhaseAt).
	Phase Phase
}

// CycleAt computes the cycle from the league's run markers and the
// gameweek calendar.
func CycleAt(st *league.State, w *league.World, now time.Time) Cycle {
	c := Cycle{Upcoming: w.UpcomingGW(now)}
	c.Phase = PhaseAt(now, w.Gameweeks, func(n int) bool {
		_, ran := st.Ran(league.RunWaivers, n)
		return ran || fplRan(st, w, n, now)
	})
	c.RunGW = c.Upcoming
	if _, ran := st.Ran(league.RunWaivers, c.Upcoming); ran || fplRan(st, w, c.Upcoming, now) {
		c.RunGW++
		c.FreeAgency = c.Upcoming <= 38
	} else if g, ok := w.Gameweeks[c.Upcoming]; ok && !now.Before(g.WaiverDeadlineAt) {
		// The deadline passed and the run is due: its claims are frozen,
		// new ones wait for the next run.
		c.RunGW++
	}
	if g, ok := w.Gameweeks[c.RunGW]; ok {
		c.RunAt = g.WaiverDeadlineAt.UTC()
	}
	for k, at := range st.Runs {
		if k.Kind == league.RunWaivers && (c.LastRunAt == nil || at.After(*c.LastRunAt)) {
			t := at
			c.LastRunAt = &t
		}
	}
	return c
}

// fplRan reports that an imported league's waiver run of gw is done
// upstream: FPL processes it at gw's waiver time (WaiverDeadlineAt, from
// FPL's events), whether or not our jobs have stamped a marker yet (a
// server that was down then, a re-sync after it). Until our own run has
// settled claims made here, it is not counted as done, so those claims
// are decided before anyone can add a free agent.
func fplRan(st *league.State, w *league.World, gw int, now time.Time) bool {
	if st.League.Source != league.SourceFPL {
		return false
	}
	g, ok := w.Gameweeks[gw]
	if !ok || now.Before(g.WaiverDeadlineAt) {
		return false
	}
	for _, cl := range st.Claims {
		if cl.Status == league.ClaimPending && cl.Gameweek <= gw {
			return false
		}
	}
	return true
}

// PlayerStatus is a player's place in the league: on a team (owner set),
// locked until clearsAt (the next waiver run) while the gameweek is
// locked, on waivers until clearsAt, or a free agent.
func PlayerStatus(st *league.State, w *league.World, c Cycle, owners map[string]string, playerID string) (status string, clearsAt *time.Time, owner string) {
	if team, ok := owners[playerID]; ok {
		return Rostered, nil, team
	}
	if st.League.Status != league.StatusInSeason || c.Upcoming > 38 {
		return FreeAgent, nil, ""
	}
	if c.Phase.Phase == PhaseLocked {
		return Locked, c.Phase.Until, ""
	}
	if !c.FreeAgency {
		if g, ok := w.Gameweeks[c.Upcoming]; ok {
			t := g.WaiverDeadlineAt.UTC()
			return OnWaivers, &t, ""
		}
	}
	if lk := st.Lock(playerID, c.RunGW); lk != nil && lk.Reason == league.LockDropped {
		if g, ok := w.Gameweeks[lk.UntilGW]; ok {
			t := g.WaiverDeadlineAt.UTC()
			return OnWaivers, &t, ""
		}
	}
	return FreeAgent, nil, ""
}

// Owners maps every player on a roster in gameweek gw to the team.
func Owners(g *league.Game, gw int) map[string]string { return g.OwnerAt(gw) }

// Service runs claims, free-agent moves and waiver runs.
type Service struct {
	repo   league.Repo
	stats  stats.Repo
	now    func() time.Time
	notify league.Notifier
}

// New returns a Service. notify may be nil.
func New(repo league.Repo, st stats.Repo, now func() time.Time, notify league.Notifier) *Service {
	if notify == nil {
		notify = league.NopNotifier{}
	}
	return &Service{repo: repo, stats: st, notify: notify,
		now: func() time.Time { return now().UTC().Truncate(time.Microsecond) }}
}

// View is a league's waiver state for one member.
type View struct {
	League *league.League
	State  *league.State
	World  *league.World
	Cycle  Cycle
	// TeamID is the viewer's team.
	TeamID string
}

// Get returns the waiver order and the next run (members).
func (s *Service) Get(ctx context.Context, actor, leagueID string) (*View, error) {
	st, w, err := s.read(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if err := league.Member(st.League, actor); err != nil {
		return nil, err
	}
	t, _ := st.League.TeamOf(actor)
	return &View{League: st.League, State: st, World: w, Cycle: CycleAt(st, w, s.now()), TeamID: t.ID}, nil
}

func (s *Service) read(ctx context.Context, leagueID string) (*league.State, *league.World, error) {
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return nil, nil, err
	}
	st, err := s.repo.State(ctx, leagueID)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		return nil, nil, league.ErrLeagueNotFound
	}
	return st, w, nil
}

// Claims lists the caller's claims: pending ones in the team's order, or
// processed ones (won and lost) newest first.
func (s *Service) Claims(ctx context.Context, actor, leagueID string, processed bool) (*View, []league.Claim, error) {
	v, err := s.Get(ctx, actor, leagueID)
	if err != nil {
		return nil, nil, err
	}
	if _, err := league.OwnTeam(v.League, actor); err != nil {
		return nil, nil, err
	}
	if v.League.Status != league.StatusInSeason {
		return nil, nil, league.ErrSeasonNotStarted
	}
	if !processed {
		var out []league.Claim
		for _, c := range v.State.Claims {
			if c.TeamID == v.TeamID {
				out = append(out, c)
			}
		}
		sortPending(out)
		return v, out, nil
	}
	all, err := s.repo.Claims(ctx, leagueID, v.TeamID)
	if err != nil {
		return nil, nil, err
	}
	var out []league.Claim
	for _, c := range all {
		if c.Status == league.ClaimWon || c.Status == league.ClaimLost {
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b league.Claim) int {
		return cmp.Or(timeDesc(a.ProcessedAt, b.ProcessedAt), cmp.Compare(a.Order, b.Order), cmp.Compare(a.ID, b.ID))
	})
	return v, out, nil
}

func timeDesc(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return b.Compare(*a)
}

func sortPending(cs []league.Claim) {
	slices.SortStableFunc(cs, func(a, b league.Claim) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
}

// change runs fn on the league's State in one transaction and reports the
// changes after commit.
func (s *Service) change(ctx context.Context, leagueID string,
	fn func(st *league.State, w *league.World, now time.Time, ch *[]league.Change) error,
) (*View, error) {
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return nil, err
	}
	var (
		out     *league.State
		changes []league.Change
	)
	err = s.repo.Tx(ctx, func(tx league.Tx) error {
		changes = nil
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return league.ErrLeagueNotFound
		}
		if err := fn(st, w, s.now(), &changes); err != nil {
			return err
		}
		blocked := len(st.Block)
		if err := tx.SaveState(ctx, st); err != nil {
			return err
		}
		if len(st.Block) < blocked {
			// A player who left his team came off the trade block.
			changes = append(changes, league.Change{LeagueID: leagueID, Kind: league.ChangeTradeBlock})
		}
		out = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.notify.Notify(ctx, changes...)
	return &View{League: out.League, State: out, World: w, Cycle: CycleAt(out, w, s.now())}, nil
}

// inSeason checks the actor's team and that the league plays.
func inSeason(st *league.State, actor string) (league.Team, error) {
	t, err := league.OwnTeam(st.League, actor)
	if err != nil {
		return t, err
	}
	if st.League.Status != league.StatusInSeason {
		return t, league.ErrSeasonNotStarted
	}
	return t, nil
}

func notFound(what string) error {
	return league.Errorf(league.KindNotFound, "not-found", "Not found", what+" not found.")
}

// checkSwap validates a like-for-like move for team in gameweek gw: the
// add player exists, the drop player is on the roster then and plays the
// same position, and neither is locked.
func checkSwap(st *league.State, w *league.World, team string, gw int, addID, dropID string) error {
	add, ok := w.Players[addID]
	if !ok {
		return notFound("Player")
	}
	drop, ok := w.Players[dropID]
	if !ok || !slices.Contains(st.Game.RosterAt(team, gw), dropID) {
		return league.Errorf(league.KindInvalid, "drop-not-on-roster", "Not on your team", "The player to drop is not on your team.",
			league.FieldError{Code: "player_not_on_roster", Message: "The player to drop is not on your team.", Field: "/dropPlayerId", PlayerID: dropID})
	}
	if add.Position != drop.Position {
		return league.Errorf(league.KindInvalid, "position-mismatch", "Position mismatch", "Drop a player of the same position.",
			league.FieldError{Code: "position_mismatch", Message: add.DisplayName + " is a " + string(add.Position) + "; drop a " + string(add.Position) + ".", Field: "/dropPlayerId", PlayerID: dropID})
	}
	return nil
}

// transactionsLocked refuses a free-agent move while the gameweek is
// locked, saying when signings reopen.
func transactionsLocked(p Phase) error {
	detail := "Free-agent moves are locked until the gameweek is over and waivers have run."
	if p.Until != nil {
		detail = "Free-agent moves are locked until waivers run on " + p.Until.Format("Mon 2 Jan 15:04 MST") + "."
	}
	e := league.Errorf(league.KindConflict, "transactions-locked", "Transactions locked", detail)
	e.ReopensAt = p.Until
	return e
}

func lockedErr(name string) error {
	return league.Errorf(league.KindConflict, "player-locked", "Player locked", name+" moved recently and is locked until the next waiver run.")
}

func inReviewErr(name string) error {
	return league.Errorf(league.KindConflict, "player-in-trade-review", "Player in a trade", name+" is in a trade waiting to be processed.")
}

// inTradeErr: the player is in a processed trade that counts from a later
// gameweek (league.Game.PendingTrade).
func inTradeErr(name string) error {
	return league.Errorf(league.KindConflict, "player-in-trade", "Player in a trade",
		name+" is in a trade that takes effect from next gameweek and cannot move until then.")
}

// squadsLegal reports whether the team's squad is 2/5/5/3 in gameweek gw
// and in every later gameweek where its roster changes (a trade that
// counts from a later gameweek): the backstop after a roster move.
func squadsLegal(g *league.Game, w *league.World, team string, gw int) bool {
	gws := []int{gw}
	for _, r := range g.Rosters {
		if r.TeamID == team {
			gws = append(gws, r.FromGW, r.ToGW)
		}
	}
	rules := scoring.StandardLineup()
	for _, n := range gws {
		if n < gw {
			continue
		}
		pos := map[string]scoring.Position{}
		for _, id := range g.RosterAt(team, n) {
			pos[id] = w.Players[id].Position
		}
		if len(rules.ValidateSquad(pos)) > 0 {
			return false
		}
	}
	return true
}

// CreateClaim adds a claim at the end of the caller's list.
func (s *Service) CreateClaim(ctx context.Context, actor, leagueID, addID, dropID string) (*View, league.Claim, error) {
	var made league.Claim
	v, err := s.change(ctx, leagueID, func(st *league.State, w *league.World, now time.Time, _ *[]league.Change) error {
		t, err := inSeason(st, actor)
		if err != nil {
			return err
		}
		c := CycleAt(st, w, now)
		if c.RunGW > 38 {
			return league.ErrSeasonOver
		}
		if err := checkSwap(st, w, t.ID, c.RunGW, addID, dropID); err != nil {
			return err
		}
		add := w.Players[addID]
		status, _, _ := PlayerStatus(st, w, c, st.Game.OwnerAt(c.RunGW), addID)
		if status == Locked && !ClaimsWhileLocked {
			return transactionsLocked(c.Phase)
		}
		if status != OnWaivers && status != Locked {
			detail := add.DisplayName + " is on a team."
			if status == FreeAgent {
				detail = add.DisplayName + " is a free agent: add him straight away."
			}
			return league.Errorf(league.KindConflict, "player-not-on-waivers", "Not on waivers", detail)
		}
		if lk := st.Lock(dropID, c.RunGW); lk != nil && lk.Reason == league.LockAdded {
			return lockedErr(w.Players[dropID].DisplayName)
		}
		if st.Game.PendingTrade(dropID, c.RunGW) {
			return inTradeErr(w.Players[dropID].DisplayName)
		}
		last := 0
		for _, cl := range st.Claims {
			if cl.TeamID != t.ID || cl.Status != league.ClaimPending {
				continue
			}
			last = max(last, cl.Order)
			if cl.AddPlayerID == addID && cl.DropPlayerID == dropID {
				return league.Errorf(league.KindConflict, "duplicate-claim", "Duplicate claim", "You already have this claim.")
			}
		}
		made = league.Claim{
			ID: ids.New("wv"), TeamID: t.ID, AddPlayerID: addID, DropPlayerID: dropID, Order: last + 1,
			Gameweek: c.RunGW, Status: league.ClaimPending, CreatedAt: now,
		}
		st.Claims = append(st.Claims, made)
		return nil
	})
	return v, made, err
}

func pendingOf(st *league.State, team, claimID string) (*league.Claim, error) {
	c := st.Claim(claimID)
	if c == nil || c.TeamID != team {
		return nil, notFound("Claim")
	}
	if c.Status != league.ClaimPending {
		return nil, league.Errorf(league.KindConflict, "claim-not-pending", "Claim decided", "That claim was already processed.")
	}
	return c, nil
}

// errProcessing: the claim's waiver deadline has passed, so it waits,
// unchangeable, for the run (which the jobs start within a tick).
var errProcessing = league.Errorf(league.KindConflict, "waivers-processing", "Waivers processing",
	"The waiver deadline for that claim has passed; it is being processed.")

// frozen reports whether the claim's waiver deadline has passed.
func frozen(w *league.World, c league.Claim, now time.Time) bool {
	g, ok := w.Gameweeks[c.Gameweek]
	return ok && !now.Before(g.WaiverDeadlineAt)
}

// decided turns a not-found for a claim the team has but that is no
// longer pending into 409 claim-not-pending.
func (s *Service) decided(ctx context.Context, actor, leagueID, claimID string, err error) error {
	var e *league.Error
	if !errors.As(err, &e) || e.Slug != "not-found" {
		return err
	}
	l, lerr := s.repo.Get(ctx, leagueID)
	if lerr != nil || l == nil {
		return err
	}
	t, ok := l.TeamOf(actor)
	if !ok {
		return err
	}
	all, lerr := s.repo.Claims(ctx, leagueID, t.ID)
	if lerr != nil {
		return lerr
	}
	if slices.ContainsFunc(all, func(c league.Claim) bool { return c.ID == claimID && c.Status != league.ClaimPending }) {
		return league.Errorf(league.KindConflict, "claim-not-pending", "Claim decided", "That claim was already processed or cancelled.")
	}
	return err
}

// UpdateClaim changes the player a pending claim drops.
func (s *Service) UpdateClaim(ctx context.Context, actor, leagueID, claimID, dropID string) (*View, league.Claim, error) {
	var out league.Claim
	v, err := s.change(ctx, leagueID, func(st *league.State, w *league.World, now time.Time, _ *[]league.Change) error {
		t, err := inSeason(st, actor)
		if err != nil {
			return err
		}
		c, err := pendingOf(st, t.ID, claimID)
		if err != nil {
			return err
		}
		if frozen(w, *c, now) {
			return errProcessing
		}
		if err := checkSwap(st, w, t.ID, c.Gameweek, c.AddPlayerID, dropID); err != nil {
			return err
		}
		if lk := st.Lock(dropID, c.Gameweek); lk != nil && lk.Reason == league.LockAdded {
			return lockedErr(w.Players[dropID].DisplayName)
		}
		if st.Game.PendingTrade(dropID, c.Gameweek) {
			return inTradeErr(w.Players[dropID].DisplayName)
		}
		c.DropPlayerID = dropID
		out = *c
		return nil
	})
	return v, out, s.decided(ctx, actor, leagueID, claimID, err)
}

// CancelClaim cancels a pending claim and closes the gap in the order.
func (s *Service) CancelClaim(ctx context.Context, actor, leagueID, claimID string) error {
	_, err := s.change(ctx, leagueID, func(st *league.State, w *league.World, now time.Time, _ *[]league.Change) error {
		t, err := inSeason(st, actor)
		if err != nil {
			return err
		}
		c, err := pendingOf(st, t.ID, claimID)
		if err != nil {
			return err
		}
		if frozen(w, *c, now) {
			return errProcessing
		}
		c.Status = league.ClaimCancelled
		renumber(st, t.ID)
		return nil
	})
	return s.decided(ctx, actor, leagueID, claimID, err)
}

// renumber gives a team's pending claims orders 1..n in their current order.
func renumber(st *league.State, team string) {
	var mine []*league.Claim
	for i := range st.Claims {
		if c := &st.Claims[i]; c.TeamID == team && c.Status == league.ClaimPending {
			mine = append(mine, c)
		}
	}
	slices.SortStableFunc(mine, func(a, b *league.Claim) int {
		return cmp.Or(cmp.Compare(a.Order, b.Order), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	for i, c := range mine {
		c.Order = i + 1
	}
}

// Reorder sets the order of the caller's pending claims: ids must be every
// pending claim once.
func (s *Service) Reorder(ctx context.Context, actor, leagueID string, claimIDs []string) (*View, []league.Claim, error) {
	var out []league.Claim
	v, err := s.change(ctx, leagueID, func(st *league.State, w *league.World, now time.Time, _ *[]league.Change) error {
		t, err := inSeason(st, actor)
		if err != nil {
			return err
		}
		var pending []string
		for _, c := range st.Claims {
			if c.TeamID == t.ID && c.Status == league.ClaimPending {
				if frozen(w, c, now) {
					return errProcessing
				}
				pending = append(pending, c.ID)
			}
		}
		sorted, want := slices.Clone(claimIDs), slices.Clone(pending)
		slices.Sort(sorted)
		slices.Sort(want)
		if !slices.Equal(sorted, want) {
			return league.Errorf(league.KindInvalid, "claim-order", "Invalid claim order",
				"Send the ids of every pending claim, once each, in order.",
				league.FieldError{Code: "claim_order", Message: "Send every pending claim once.", Field: "/claimIds"})
		}
		for i, id := range claimIDs {
			st.Claim(id).Order = i + 1
		}
		for _, c := range st.Claims {
			if c.TeamID == t.ID && c.Status == league.ClaimPending {
				out = append(out, c)
			}
		}
		sortPending(out)
		return nil
	})
	return v, out, err
}

// move swaps drop for add on team from gameweek gw, carrying the lineup.
func move(st *league.State, team, addID, dropID string, gw int, via string, now time.Time) {
	st.Game.Release(team, dropID, gw)
	st.Game.Acquire(team, addID, gw, via)
	st.Game.Swap(team, dropID, addID, gw, now)
}

// AddFreeAgent adds a free agent for a player of the same position, at
// once, for the upcoming gameweek.
func (s *Service) AddFreeAgent(ctx context.Context, actor, leagueID, addID, dropID string) (*View, league.Txn, error) {
	var txn league.Txn
	v, err := s.change(ctx, leagueID, func(st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		t, err := inSeason(st, actor)
		if err != nil {
			return err
		}
		c := CycleAt(st, w, now)
		if c.Upcoming > 38 {
			return league.ErrSeasonOver
		}
		if c.Phase.Phase == PhaseLocked {
			return transactionsLocked(c.Phase)
		}
		gw := c.Upcoming
		if err := checkSwap(st, w, t.ID, gw, addID, dropID); err != nil {
			return err
		}
		add, drop := w.Players[addID], w.Players[dropID]
		status, clears, _ := PlayerStatus(st, w, c, st.Game.OwnerAt(gw), addID)
		switch status {
		case Rostered:
			return league.Errorf(league.KindConflict, "player-taken", "Player taken", add.DisplayName+" is on a team.")
		case OnWaivers:
			detail := add.DisplayName + " is on waivers; put in a claim."
			if clears != nil {
				detail = add.DisplayName + " is on waivers until " + clears.Format("Mon 2 Jan 15:04 MST") + "; put in a claim."
			}
			return league.Errorf(league.KindConflict, "player-on-waivers", "On waivers", detail)
		}
		if lk := st.Lock(dropID, c.RunGW); lk != nil && lk.Reason == league.LockAdded {
			return lockedErr(drop.DisplayName)
		}
		if st.InTradeReview(dropID) {
			return inReviewErr(drop.DisplayName)
		}
		if st.Game.PendingTrade(dropID, gw) {
			return inTradeErr(drop.DisplayName)
		}
		move(st, t.ID, addID, dropID, gw, "free_agent", now)
		if !squadsLegal(st.Game, w, t.ID, gw) {
			return league.Errorf(league.KindConflict, "roster-invalid", "Squad not allowed",
				"That move would leave your squad without 2 goalkeepers, 5 defenders, 5 midfielders and 3 forwards.")
		}
		st.SetLock(addID, c.RunGW, league.LockAdded)
		st.SetLock(dropID, c.RunGW, league.LockDropped)
		txn = league.Txn{
			ID: ids.New("txn"), Kind: league.TxnFreeAgent, Gameweek: gw, OccurredAt: now,
			TeamID: t.ID, PlayerIn: addID, PlayerOut: dropID, Approved: true,
		}
		st.NewTxns = append(st.NewTxns, txn)
		return nil
	})
	return v, txn, err
}

// RunDue processes the league's waiver run for the upcoming gameweek once
// its waiver deadline has passed (the jobs). It also catches up a run that
// was missed altogether (workers down past a gameweek's deadline, ADR
// 0010): see overdue. It reports whether it ran.
func (s *Service) RunDue(ctx context.Context, leagueID string) (bool, error) {
	st, w, err := s.read(ctx, leagueID)
	if err != nil {
		return false, err
	}
	if st.League.Status != league.StatusInSeason {
		return false, nil
	}
	if gw, ok := due(st, w, s.now()); ok {
		return s.Run(ctx, leagueID, gw)
	}
	return false, nil
}

// due is the gameweek whose waiver run should happen now: the upcoming
// one once its waiver deadline passed, else an overdue one.
func due(st *league.State, w *league.World, now time.Time) (int, bool) {
	gw := w.UpcomingGW(now)
	if g, ok := w.Gameweeks[gw]; ok && !now.Before(g.WaiverDeadlineAt) {
		if _, ran := st.Ran(league.RunWaivers, gw); !ran {
			return gw, true
		}
	}
	return overdue(st, w, now)
}

// overdue finds a missed waiver run: the last gameweek whose deadline has
// passed has no run marker and claims for it (or earlier) still wait. Its
// claims are then decided late, with moves counting from the first
// gameweek still open (see Run).
func overdue(st *league.State, w *league.World, now time.Time) (int, bool) {
	gw := min(w.UpcomingGW(now)-1, 38)
	if gw < 1 {
		return 0, false
	}
	if _, ran := st.Ran(league.RunWaivers, gw); ran {
		return 0, false
	}
	for _, c := range st.Claims {
		if c.Status == league.ClaimPending && c.Gameweek <= gw {
			return gw, true
		}
	}
	return 0, false
}

// Run processes the waiver claims of gameweek gw (and any older pending
// ones). It is idempotent: a second run of the same gameweek does nothing.
// A run made after gw's deadline (caught up) moves players from the first
// gameweek still open; after the last deadline nothing can move and the
// claims lose (roster_locked).
func (s *Service) Run(ctx context.Context, leagueID string, gw int) (bool, error) {
	ran := false
	_, err := s.change(ctx, leagueID, func(st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		if _, done := st.Ran(league.RunWaivers, gw); done || st.League.Status != league.StatusInSeason {
			return nil
		}
		ran = true
		process(st, w, gw, max(gw, w.UpcomingGW(now)), now)
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeWaivers, Gameweek: gw})
		return nil
	})
	return ran && err == nil, err
}

// addedLock reports whether the player was added since the last run and
// so cannot be dropped at gameweek gw's run.
func addedLock(st *league.State, playerID string, gw int) bool {
	lk := st.Lock(playerID, gw)
	return lk != nil && lk.Reason == league.LockAdded
}

// Process runs gameweek gw's claims on st: deterministic for the same
// state. Teams go in the order the league's rule gives for the run
// (league.WaiverOrder); each gets its first claim that can still succeed,
// and a team that wins moves to the back of the order for the rest of the
// run and the round restarts. Claims that cannot succeed lose with a
// reason. The order the run ends with is stored: the next run starts from
// it when the league's order is rolling.
func Process(st *league.State, w *league.World, gw int, now time.Time) { process(st, w, gw, gw, now) }

// process is gameweek runGW's run with the moves counting from effGW
// (later than runGW when the run is caught up after runGW's deadline; past
// 38 nothing can move and every claim loses, roster_locked).
func process(st *league.State, w *league.World, runGW, effGW int, now time.Time) {
	gw := effGW
	// The league's rule gives the order the run starts from (reverse
	// standings, or the rolling order the last run left).
	priority := league.WaiverOrder(st, w, runGW)
	owner := st.Game.OwnerAt(gw)
	var queue []*league.Claim
	for i := range st.Claims {
		if c := &st.Claims[i]; c.Status == league.ClaimPending && c.Gameweek <= runGW {
			queue = append(queue, c)
		}
	}
	slices.SortStableFunc(queue, func(a, b *league.Claim) int {
		return cmp.Or(cmp.Compare(a.TeamID, b.TeamID), cmp.Compare(a.Order, b.Order), a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
	})
	decide := func(c *league.Claim, rank int, won bool, reason string) {
		// The gameweek the move counts from; the last one when none is left.
		at, cgw := now, min(gw, 38)
		c.ProcessedAt, c.Gameweek = &at, cgw
		txn := league.Txn{ID: ids.New("txn"), Kind: league.TxnWaiver, Gameweek: cgw, OccurredAt: now,
			TeamID: c.TeamID, PlayerIn: c.AddPlayerID, PlayerOut: c.DropPlayerID, Priority: rank}
		if won {
			c.Status, txn.Approved = league.ClaimWon, true
		} else {
			c.Status, c.FailureReason, txn.Reason = league.ClaimLost, reason, reason
		}
		st.NewTxns = append(st.NewTxns, txn)
	}
	var dropped []string
	for _, c := range queue {
		if gw > 38 {
			decide(c, slices.Index(priority, c.TeamID)+1, false, league.ClaimRosterLocked)
		}
	}
	for {
		progressed := false
		for rank, team := range priority {
			for _, c := range queue {
				if c.TeamID != team || c.Status != league.ClaimPending {
					continue
				}
				switch {
				case owner[c.AddPlayerID] != "":
					decide(c, rank+1, false, league.ClaimInUnavailable)
					continue
				case owner[c.DropPlayerID] != team:
					decide(c, rank+1, false, league.ClaimOutUnavailable)
					continue
				case st.InTradeReview(c.AddPlayerID) || st.InTradeReview(c.DropPlayerID),
					st.Game.PendingTrade(c.DropPlayerID, gw), addedLock(st, c.DropPlayerID, runGW):
					decide(c, rank+1, false, league.ClaimRosterLocked)
					continue
				}
				rosters, lineups := slices.Clone(st.Game.Rosters), slices.Clone(st.Game.Lineups)
				move(st, team, c.AddPlayerID, c.DropPlayerID, gw, "waiver", now)
				if !squadsLegal(st.Game, w, team, gw) {
					st.Game.Rosters, st.Game.Lineups = rosters, lineups
					decide(c, rank+1, false, league.ClaimRosterLocked)
					continue
				}
				delete(owner, c.DropPlayerID)
				owner[c.AddPlayerID] = team
				dropped = append(dropped, c.DropPlayerID)
				decide(c, rank+1, true, "")
				priority = append(slices.Delete(priority, rank, rank+1), team)
				progressed = true
				break
			}
			if progressed {
				break
			}
		}
		if !progressed {
			break
		}
	}
	st.Priority = priority
	// Survivors keep their relative order but close the gaps, so claims
	// added later queue behind them.
	for _, t := range st.League.Teams {
		renumber(st, t.ID)
	}
	st.PruneLocks(runGW + 1)
	for _, p := range dropped {
		st.SetLock(p, runGW+1, league.LockDropped)
	}
	st.Runs[league.RunKey{Kind: league.RunWaivers, Gameweek: runGW}] = now
}
