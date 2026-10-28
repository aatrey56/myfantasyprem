package waivers

import (
	"cmp"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

func slug(err error) string {
	var e *league.Error
	if errors.As(err, &e) {
		return e.Slug
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// fixture is an in-season league at the start of gameweek 8's waiver
// period (Monday; waivers run Thursday 17:30, the deadline is Friday).
type fixture struct {
	e     *leaguetest.Env
	s     *Service
	l     *league.League
	users []string
	teams []string
	w     *league.World
}

func newFixture(t *testing.T, e *leaguetest.Env) *fixture {
	l, users := e.InSeason(t, 4, league.SettingsInput{})
	f := &fixture{e: e, s: New(e.Repo, e.Stats, e.Clock.Now, nil), l: l, users: users, w: e.World(t)}
	for _, tm := range l.Teams {
		f.teams = append(f.teams, tm.ID)
	}
	return f
}

// free returns the n best-ranked unowned players at pos in gameweek 8.
func (f *fixture) free(t *testing.T, pos scoring.Position, n int) []string {
	t.Helper()
	st := f.e.State(t, f.l.ID)
	own := st.Game.OwnerAt(8)
	var out []string
	for id, p := range f.w.Players {
		if p.Position == pos && own[id] == "" && st.Lock(id, 0) == nil {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(f.w.Players[a].DraftRank, f.w.Players[b].DraftRank), cmp.Compare(a, b))
	})
	return out[:n]
}

// rostered returns team i's players at pos in gameweek gw.
func (f *fixture) rostered(t *testing.T, i int, pos scoring.Position, gw int) []string {
	t.Helper()
	var out []string
	for _, id := range f.e.State(t, f.l.ID).Game.RosterAt(f.teams[i], gw) {
		if f.w.Players[id].Position == pos {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func TestWaiverCycle(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx, s := e.Ctx, f.s
		defs := f.free(t, scoring.DEF, 3)
		x, y, z := defs[0], defs[1], defs[2]
		d0 := f.rostered(t, 0, scoring.DEF, 8)[0]
		d1 := f.rostered(t, 1, scoring.DEF, 8)[0]
		d3 := f.rostered(t, 3, scoring.DEF, 8)[0]
		mid := f.free(t, scoring.MID, 1)[0]

		// Gameweek 7 is still live on Monday, so free-agent moves are
		// locked; claims for gameweek 8's run can still be made
		// (ClaimsWhileLocked), only for unowned players.
		if _, _, err := s.AddFreeAgent(ctx, f.users[0], f.l.ID, x, d0); slug(err) != "transactions-locked" {
			t.Fatalf("free agent in the waiver period: %v", err)
		}
		if _, _, err := s.CreateClaim(ctx, f.users[0], f.l.ID, d1, d0); slug(err) != "player-not-on-waivers" {
			t.Fatalf("claim a rostered player: %v", err)
		}
		if _, _, err := s.CreateClaim(ctx, f.users[0], f.l.ID, mid, d0); slug(err) != "position-mismatch" {
			t.Fatalf("position mismatch: %v", err)
		}
		if _, _, err := s.CreateClaim(ctx, f.users[0], f.l.ID, x, d1); slug(err) != "drop-not-on-roster" {
			t.Fatalf("drop someone else's player: %v", err)
		}
		v, cx, err := s.CreateClaim(ctx, f.users[0], f.l.ID, x, d0)
		if err != nil {
			t.Fatal(err)
		}
		if cx.Order != 1 || cx.Gameweek != 8 || v.Cycle.RunGW != 8 || v.Cycle.FreeAgency {
			t.Fatalf("claim = %+v, cycle %+v", cx, v.Cycle)
		}
		if _, _, err := s.CreateClaim(ctx, f.users[0], f.l.ID, x, d0); slug(err) != "duplicate-claim" {
			t.Fatalf("duplicate: %v", err)
		}
		_, cy, err := s.CreateClaim(ctx, f.users[0], f.l.ID, y, d0)
		if err != nil || cy.Order != 2 {
			t.Fatalf("second claim = %+v, %v", cy, err)
		}
		// Reorder, then back.
		if _, _, err := s.Reorder(ctx, f.users[0], f.l.ID, []string{cy.ID}); slug(err) != "claim-order" {
			t.Fatalf("partial reorder: %v", err)
		}
		_, got, err := s.Reorder(ctx, f.users[0], f.l.ID, []string{cy.ID, cx.ID})
		if err != nil || got[0].ID != cy.ID || got[0].Order != 1 {
			t.Fatalf("reorder = %+v %v", got, err)
		}
		if _, _, err := s.Reorder(ctx, f.users[0], f.l.ID, []string{cx.ID, cy.ID}); err != nil {
			t.Fatal(err)
		}
		// Team 4 (first priority) wants x too; team 2 wants z.
		if _, _, err := s.CreateClaim(ctx, f.users[3], f.l.ID, x, d3); err != nil {
			t.Fatal(err)
		}
		_, cz, err := s.CreateClaim(ctx, f.users[1], f.l.ID, z, d1)
		if err != nil {
			t.Fatal(err)
		}
		// Change the drop of a claim, and cancel one.
		d1b := f.rostered(t, 1, scoring.DEF, 8)[1]
		if _, c, err := s.UpdateClaim(ctx, f.users[1], f.l.ID, cz.ID, d1b); err != nil || c.DropPlayerID != d1b {
			t.Fatalf("update = %+v %v", c, err)
		}
		_, extra, _ := s.CreateClaim(ctx, f.users[2], f.l.ID, y, f.rostered(t, 2, scoring.DEF, 8)[0])
		if err := s.CancelClaim(ctx, f.users[2], f.l.ID, extra.ID); err != nil {
			t.Fatal(err)
		}
		if err := s.CancelClaim(ctx, f.users[2], f.l.ID, extra.ID); slug(err) != "claim-not-pending" {
			t.Fatalf("cancel twice: %v", err)
		}
		if err := s.CancelClaim(ctx, f.users[1], f.l.ID, cx.ID); slug(err) != "not-found" {
			t.Fatalf("cancel another team's claim: %v", err)
		}
		_, pending, _ := s.Claims(ctx, f.users[0], f.l.ID, false)
		if len(pending) != 2 {
			t.Fatalf("pending = %+v", pending)
		}

		// Not before the waiver deadline; then once.
		if ran, err := s.RunDue(ctx, f.l.ID); ran || err != nil {
			t.Fatalf("early run: %v %v", ran, err)
		}
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if ran, err := s.RunDue(ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("run: %v %v", ran, err)
		}
		if ran, err := s.RunDue(ctx, f.l.ID); ran || err != nil {
			t.Fatalf("second run: %v %v", ran, err)
		}

		// Priority was [4 3 2 1]: team 4 wins x (back), team 2 wins z
		// (back), team 1 loses x and wins y (back).
		st := e.State(t, f.l.ID)
		if want := []string{f.teams[2], f.teams[3], f.teams[1], f.teams[0]}; !slices.Equal(st.Priority, want) {
			t.Fatalf("priority = %v, want %v", st.Priority, want)
		}
		own := st.Game.OwnerAt(8)
		if own[x] != f.teams[3] || own[y] != f.teams[0] || own[z] != f.teams[1] || own[d0] != "" || own[d3] != "" {
			t.Fatalf("owners: x %s y %s z %s d0 %q d3 %q", own[x], own[y], own[z], own[d0], own[d3])
		}
		if st.Game.OwnerAt(7)[d0] != "" {
			t.Fatal("the move changed gameweek 7")
		}
		if lu, _ := st.Game.LineupAt(f.teams[0], 8); !slices.Contains(lu.Slots, y) || slices.Contains(lu.Slots, d0) {
			t.Fatalf("team 1's lineup did not take y: %v", lu.Slots)
		}
		_, processed, _ := s.Claims(ctx, f.users[0], f.l.ID, true)
		if len(processed) != 2 {
			t.Fatalf("processed = %+v", processed)
		}
		for _, c := range processed {
			switch c.ID {
			case cx.ID:
				if c.Status != league.ClaimLost || c.FailureReason != league.ClaimInUnavailable {
					t.Errorf("claim x = %+v", c)
				}
			case cy.ID:
				if c.Status != league.ClaimWon || c.ProcessedAt == nil {
					t.Errorf("claim y = %+v", c)
				}
			}
		}
		txns, _ := e.Repo.Transactions(ctx, f.l.ID)
		if len(txns) != 4 {
			t.Fatalf("feed = %+v", txns)
		}
		for _, x := range txns {
			if x.Kind != league.TxnWaiver || x.Priority < 1 || x.Gameweek != 8 {
				t.Errorf("txn = %+v", x)
			}
		}

		// Free agency until the deadline: unowned players are free agents,
		// the players dropped at the run stay on waivers until the next.
		v, _ = s.Get(ctx, f.users[2], f.l.ID)
		if !v.Cycle.FreeAgency || v.Cycle.RunGW != 9 || v.Cycle.LastRunAt == nil {
			t.Fatalf("cycle after the run = %+v", v.Cycle)
		}
		d2 := f.rostered(t, 2, scoring.DEF, 8)[0]
		if _, _, err := s.AddFreeAgent(ctx, f.users[2], f.l.ID, d0, d2); slug(err) != "player-on-waivers" {
			t.Fatalf("add a player dropped at the run: %v", err)
		}
		fa := f.free(t, scoring.DEF, 1)[0]
		_, txn, err := s.AddFreeAgent(ctx, f.users[2], f.l.ID, fa, d2)
		if err != nil || !txn.Approved || txn.Gameweek != 8 {
			t.Fatalf("free agent = %+v %v", txn, err)
		}
		if _, _, err := s.AddFreeAgent(ctx, f.users[1], f.l.ID, fa, d1); slug(err) != "player-taken" {
			t.Fatalf("taken: %v", err)
		}
		other := f.free(t, scoring.DEF, 1)[0]
		if _, _, err := s.AddFreeAgent(ctx, f.users[2], f.l.ID, other, fa); slug(err) != "player-locked" {
			t.Fatalf("drop a player just added: %v", err)
		}
		// Claims now go to gameweek 9's run.
		_, c9, err := s.CreateClaim(ctx, f.users[1], f.l.ID, d2, d1)
		if err != nil || c9.Gameweek != 9 {
			t.Fatalf("claim for the next run = %+v %v", c9, err)
		}
		// The deadline closes free agency: gameweek 8 is locked until it
		// is finished.
		e.Clock.Set(f.w.Gameweeks[8].DeadlineAt)
		if _, _, err := s.AddFreeAgent(ctx, f.users[0], f.l.ID, other, f.rostered(t, 0, scoring.DEF, 9)[0]); slug(err) != "transactions-locked" {
			t.Fatalf("after the deadline: %v", err)
		}
	})
}

// The same claims processed twice from the same state decide the same way.
func TestProcessIsDeterministic(t *testing.T) {
	e := leaguetest.New(t, nil)
	f := newFixture(t, e)
	defs := f.free(t, scoring.DEF, 2)
	for i := range f.teams {
		drop := f.rostered(t, i, scoring.DEF, 8)[0]
		for _, add := range defs {
			if _, _, err := f.s.CreateClaim(e.Ctx, f.users[i], f.l.ID, add, drop); err != nil {
				t.Fatal(err)
			}
		}
	}
	at := f.w.Gameweeks[8].WaiverDeadlineAt
	decide := func() ([]string, []string) {
		st := e.State(t, f.l.ID)
		Process(st, f.w, 8, at)
		var out []string
		for _, c := range st.Claims {
			out = append(out, c.ID+":"+c.Status+":"+c.FailureReason)
		}
		slices.Sort(out)
		return out, st.Priority
	}
	a, pa := decide()
	b, pb := decide()
	if !slices.Equal(a, b) || !slices.Equal(pa, pb) {
		t.Fatalf("runs differ:\n%v %v\n%v %v", a, pa, b, pb)
	}
	// Two players for four teams: two claims win, the rest lose.
	won := 0
	for _, s := range a {
		if strings.Contains(s, ":"+league.ClaimWon+":") {
			won++
		}
	}
	if won != 2 || len(a) != 8 {
		t.Fatalf("won %d of %d", won, len(a))
	}
	// Which two, by ADR 0008 rule 12 (priority starts 4 3 2 1; every team
	// claims defs[0] then defs[1], dropping the same defender):
	//   team 4: defs[0] wins             -> priority 3 2 1 4
	//   team 3: defs[0] taken, defs[1] wins -> priority 2 1 4 3
	//   team 2, team 1: both taken; team 4's second claim: taken.
	st := e.State(t, f.l.ID)
	Process(st, f.w, 8, at)
	own := st.Game.OwnerAt(8)
	if own[defs[0]] != f.teams[3] || own[defs[1]] != f.teams[2] {
		t.Fatalf("defs[0] to %s, defs[1] to %s; want teams 4 and 3", own[defs[0]], own[defs[1]])
	}
	if want := []string{f.teams[1], f.teams[0], f.teams[3], f.teams[2]}; !slices.Equal(st.Priority, want) {
		t.Fatalf("priority = %v, want %v", st.Priority, want)
	}
}

// Two teams adding the same free agent at once: exactly one gets him.
func TestConcurrentFreeAgentAdds(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if _, err := f.s.Run(e.Ctx, f.l.ID, 8); err != nil {
			t.Fatal(err)
		}
		e.Clock.Advance(time.Minute)
		fa := f.free(t, scoring.MID, 1)[0]
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			wins int
		)
		for i := range f.teams {
			drop := f.rostered(t, i, scoring.MID, 8)[0]
			for range 3 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, _, err := f.s.AddFreeAgent(e.Ctx, f.users[i], f.l.ID, fa, drop)
					mu.Lock()
					defer mu.Unlock()
					switch slug(err) {
					case "":
						wins++
					case "player-taken", "drop-not-on-roster": // a team's second try
					default:
						t.Errorf("unexpected: %v", err)
					}
				}()
			}
		}
		wg.Wait()
		if wins != 1 {
			t.Fatalf("wins = %d, want 1", wins)
		}
		owners := 0
		for _, r := range e.State(t, f.l.ID).Game.Rosters {
			if r.PlayerID == fa && r.OnRosterAt(8) {
				owners++
			}
		}
		if owners != 1 {
			t.Fatalf("owners = %d", owners)
		}
	})
}

// The same claim sent many times at once is stored once.
func TestConcurrentDuplicateClaims(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		add := f.free(t, scoring.FWD, 1)[0]
		drop := f.rostered(t, 0, scoring.FWD, 8)[0]
		var wg sync.WaitGroup
		errs := make([]error, 8)
		for i := range errs {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, errs[i] = f.s.CreateClaim(e.Ctx, f.users[0], f.l.ID, add, drop)
			}()
		}
		wg.Wait()
		ok := 0
		for _, err := range errs {
			if err == nil {
				ok++
			} else if slug(err) != "duplicate-claim" {
				t.Fatalf("unexpected: %v", err)
			}
		}
		_, pending, _ := f.s.Claims(e.Ctx, f.users[0], f.l.ID, false)
		if ok != 1 || len(pending) != 1 {
			t.Fatalf("ok %d, pending %d", ok, len(pending))
		}
	})
}

func TestBeforeTheDraft(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		l, users := e.League(t, 4, league.SettingsInput{})
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		p := e.World(t)
		var a, b string
		for id := range p.Players {
			if a == "" {
				a = id
			} else if b == "" {
				b = id
			}
		}
		if _, _, err := s.CreateClaim(e.Ctx, users[0], l.ID, a, b); slug(err) != "season-not-started" {
			t.Fatalf("claim before the draft: %v", err)
		}
	})
}

// claimOne makes team 0's claim for the best free defender, dropping one
// of its defenders, and returns the claim.
func (f *fixture) claimOne(t *testing.T, gw int) league.Claim {
	t.Helper()
	add := f.free(t, scoring.DEF, 1)[0]
	drop := f.rostered(t, 0, scoring.DEF, gw)[0]
	_, c, err := f.s.CreateClaim(f.e.Ctx, f.users[0], f.l.ID, add, drop)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func claimByID(t *testing.T, e *leaguetest.Env, leagueID, teamID, id string) league.Claim {
	t.Helper()
	cs, err := e.Repo.Claims(e.Ctx, leagueID, teamID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("claim %s not found", id)
	return league.Claim{}
}

// Workers down from before gameweek 8's waiver run until after its
// deadline: the tick catches the missed run up (ADR 0010) instead of
// leaving the claims to the next run, and the moves count from the first
// gameweek still open, never a locked one.
func TestMissedRunCaughtUp(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		c := f.claimOne(t, 8)
		e.Clock.Set(f.w.Gameweeks[8].DeadlineAt.Add(time.Hour)) // gameweek 9 is next
		ran, err := f.s.RunDue(e.Ctx, f.l.ID)
		if err != nil || !ran {
			t.Fatalf("catch-up run: %v %v", ran, err)
		}
		got := claimByID(t, e, f.l.ID, f.teams[0], c.ID)
		if got.Status != league.ClaimWon {
			t.Fatalf("claim = %s %s", got.Status, got.FailureReason)
		}
		st := e.State(t, f.l.ID)
		if !slices.Contains(st.Game.RosterAt(f.teams[0], 9), c.AddPlayerID) ||
			slices.Contains(st.Game.RosterAt(f.teams[0], 8), c.AddPlayerID) {
			t.Fatal("the move does not count from gameweek 9 only")
		}
		if _, ok := st.Ran(league.RunWaivers, 8); !ok {
			t.Fatal("no run marker for gameweek 8")
		}
		if ran, _ := f.s.RunDue(e.Ctx, f.l.ID); ran {
			t.Fatal("ran twice")
		}
		// Gameweek 9's own run still happens at its waiver deadline.
		e.Clock.Set(f.w.Gameweeks[9].WaiverDeadlineAt.Add(time.Minute))
		if ran, err := f.s.RunDue(e.Ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("gameweek 9 run: %v %v", ran, err)
		}
	})
}

// After gameweek 38's deadline a missed run still settles its claims:
// nothing can move any more, so they lose (roster_locked) instead of
// staying pending forever.
func TestMissedLastRunSettlesClaims(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		e.Clock.Set(f.w.Gameweeks[38].WaiverDeadlineAt.Add(-time.Hour))
		c := f.claimOne(t, 38)
		if c.Gameweek != 38 {
			t.Fatalf("claim for gameweek %d", c.Gameweek)
		}
		e.Clock.Set(f.w.Gameweeks[38].DeadlineAt.Add(time.Hour))
		ran, err := f.s.RunDue(e.Ctx, f.l.ID)
		if err != nil || !ran {
			t.Fatalf("catch-up run: %v %v", ran, err)
		}
		got := claimByID(t, e, f.l.ID, f.teams[0], c.ID)
		if got.Status != league.ClaimLost || got.FailureReason != league.ClaimRosterLocked || got.Gameweek != 38 {
			t.Fatalf("claim = %s %s", got.Status, got.FailureReason)
		}
		if slices.Contains(e.State(t, f.l.ID).Game.RosterAt(f.teams[0], 38), c.AddPlayerID) {
			t.Fatal("a player moved into a played gameweek")
		}
		if ran, _ := f.s.RunDue(e.Ctx, f.l.ID); ran {
			t.Fatal("ran twice")
		}
	})
}
