package waivers

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Tests in this file close gaps found by mutation testing (docs/testing.md).
// Expected values come from the rules (RESEARCH.md §7 "Waivers and free
// agency", ADR 0008 rule 12: rolling priority, winner to the back), worked
// out by hand.

// pureState is a 3-team in-season state with the given rosters (gameweek 1
// on) and priority A, B, C.
func pureState(rosters map[string][]string) *league.State {
	st := &league.State{
		League: &league.League{ID: "lg_x", Status: league.StatusInSeason,
			Teams: []league.Team{{ID: "A", OwnerID: "ua"}, {ID: "B", OwnerID: "ub"}, {ID: "C", OwnerID: "uc"}}},
		Game: &league.Game{}, Priority: []string{"A", "B", "C"}, Runs: map[league.RunKey]time.Time{},
	}
	for team, ids := range rosters {
		for _, id := range ids {
			st.Game.Acquire(team, id, 1, "draft")
		}
	}
	return st
}

// squadWorld makes every claim in st a legal same-position swap: it pads
// each team's roster to a 2/5/5/3 squad with filler players and returns a
// world where the named players and the free ones are midfielders. ADR
// 0008: claims drop a player of the same position, squads always stay
// 2/5/5/3; ADR 0010: every roster move checks it, so a claim that would
// leave a short squad loses (roster_locked).
func squadWorld(st *league.State, free ...string) *league.World {
	w := &league.World{Players: map[string]stats.Player{}}
	add := func(id string, pos scoring.Position) {
		w.Players[id] = stats.Player{ID: id, Position: pos}
	}
	for _, id := range free {
		add(id, scoring.MID)
	}
	for _, tm := range st.League.Teams {
		named := st.Game.RosterAt(tm.ID, 1)
		if len(named) == 0 {
			continue
		}
		for _, id := range named {
			add(id, scoring.MID)
		}
		need := scoring.StandardLineup().Squad
		need[scoring.MID] -= len(named)
		for _, pos := range []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD} {
			for i := range need[pos] {
				id := fmt.Sprintf("%s-%s%d", tm.ID, pos, i)
				add(id, pos)
				st.Game.Acquire(tm.ID, id, 1, "draft")
			}
		}
	}
	return w
}

func claim(id, team, add, drop string, order, gw int) league.Claim {
	return league.Claim{ID: id, TeamID: team, AddPlayerID: add, DropPlayerID: drop, Order: order, Gameweek: gw, Status: league.ClaimPending}
}

// ADR 0008 rule 12 worked by hand. Priority A, B, C.
//
//	pass 1: A's first claim (p1 for a1) wins       -> priority B, C, A
//	pass 2: B wants p1, now owned: lost; B has no other claim;
//	        C's claim (p2 for c1) wins              -> priority B, A, C
//	pass 3: B nothing; A's second claim wants p2, now owned: lost
//
// Result: p1 to A, p2 to C, priority B, A, C.
func TestProcessRollingPriority(t *testing.T) {
	st := pureState(map[string][]string{"A": {"a1", "a2"}, "B": {"b1"}, "C": {"c1"}})
	st.Claims = []league.Claim{
		claim("w1", "A", "p1", "a1", 1, 8),
		claim("w2", "A", "p2", "a2", 2, 8),
		claim("w3", "B", "p1", "b1", 1, 8),
		claim("w4", "C", "p2", "c1", 1, 8),
	}
	Process(st, squadWorld(st, "p1", "p2"), 8, time.Unix(0, 0))
	want := map[string]string{"w1": league.ClaimWon, "w2": league.ClaimLost, "w3": league.ClaimLost, "w4": league.ClaimWon}
	for _, c := range st.Claims {
		if c.Status != want[c.ID] {
			t.Errorf("claim %s = %s, want %s", c.ID, c.Status, want[c.ID])
		}
	}
	if want := []string{"B", "A", "C"}; !slices.Equal(st.Priority, want) {
		t.Fatalf("priority = %v, want %v", st.Priority, want)
	}
	own := st.Game.OwnerAt(8)
	if own["p1"] != "A" || own["p2"] != "C" || own["a1"] != "" || own["c1"] != "" || own["a2"] != "A" {
		t.Fatalf("owners = %v", own)
	}
}

// A run for gameweek 8 handles claims for 8 and older, never claims made
// for a later run.
func TestProcessLeavesLaterClaims(t *testing.T) {
	st := pureState(map[string][]string{"A": {"a1", "a2"}})
	st.Claims = []league.Claim{claim("w1", "A", "p1", "a1", 1, 7), claim("w2", "A", "p2", "a2", 2, 9)}
	Process(st, squadWorld(st, "p1", "p2"), 8, time.Unix(0, 0))
	if st.Claims[0].Status != league.ClaimWon || st.Claims[1].Status != league.ClaimPending {
		t.Fatalf("claims = %+v", st.Claims)
	}
}

// RESEARCH.md §7 Locks: a lock lasts until the next waiver run. After
// gameweek 8's run, locks until run 7 or 8 are over; a lock until 9 stays;
// the player dropped at the run is locked until run 9.
func TestProcessEndsLocksAtTheRun(t *testing.T) {
	st := pureState(map[string][]string{"A": {"a1"}})
	st.Locks = []league.WaiverLock{
		{PlayerID: "x7", UntilGW: 7, Reason: league.LockDropped},
		{PlayerID: "x8", UntilGW: 8, Reason: league.LockAdded},
		{PlayerID: "x9", UntilGW: 9, Reason: league.LockAdded},
	}
	st.Claims = []league.Claim{claim("w1", "A", "p1", "a1", 1, 8)}
	Process(st, squadWorld(st, "p1"), 8, time.Unix(0, 0))
	got := map[string]int{}
	for _, l := range st.Locks {
		got[l.PlayerID] = l.UntilGW
	}
	if want := map[string]int{"x9": 9, "a1": 9}; !mapsEqual(got, want) {
		t.Fatalf("locks = %v, want %v", got, want)
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

// LastRunAt is the newest waiver run; other run kinds do not count.
func TestCycleLastRunAt(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		w := e.World(t)
		t6, t7 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
		st := pureState(nil)
		st.Runs[league.RunKey{Kind: league.RunWaivers, Gameweek: 7}] = t7
		st.Runs[league.RunKey{Kind: league.RunWaivers, Gameweek: 6}] = t6
		st.Runs[league.RunKey{Kind: league.RunLineupLock, Gameweek: 7}] = t7.Add(time.Hour)
		for range 5 { // map order varies; any order must give the newest
			if c := CycleAt(st, w, leaguetest.Start); c.LastRunAt == nil || !c.LastRunAt.Equal(t7) {
				t.Fatalf("LastRunAt = %v, want %v", c.LastRunAt, t7)
			}
		}
	})
}

// PlayerStatus: unowned players are free agents outside the season, and on
// waivers through gameweek 38's waiver period.
func TestPlayerStatusSeasonEdges(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		w := e.World(t)
		st := pureState(nil)
		if s, _, _ := PlayerStatus(st, w, Cycle{Upcoming: 38, RunGW: 38}, nil, "p"); s != OnWaivers {
			t.Errorf("gameweek 38 waiver period: %s, want %s", s, OnWaivers)
		}
		if s, _, _ := PlayerStatus(st, w, Cycle{Upcoming: 39, RunGW: 39}, nil, "p"); s != FreeAgent {
			t.Errorf("after the season: %s, want %s", s, FreeAgent)
		}
		st.League.Status = league.StatusPreDraft
		if s, _, _ := PlayerStatus(st, w, Cycle{Upcoming: 8, RunGW: 8}, nil, "p"); s != FreeAgent {
			t.Errorf("before the draft: %s, want %s", s, FreeAgent)
		}
	})
}

// Gameweek 38 still has a waiver run and free agency (RESEARCH.md §7: free
// agency runs after waivers until the deadline); after its deadline the
// season is over for roster moves.
func TestGameweek38(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		g38 := f.w.Gameweeks[38]
		e.Clock.Set(g38.WaiverDeadlineAt.Add(-time.Hour))
		add := f.free(t, scoring.FWD, 2)
		drop := f.rostered(t, 0, scoring.FWD, 38)
		if _, c, err := f.s.CreateClaim(e.Ctx, f.users[0], f.l.ID, add[0], drop[0]); err != nil || c.Gameweek != 38 {
			t.Fatalf("claim for gameweek 38 = %+v, %v", c, err)
		}
		e.Clock.Set(g38.WaiverDeadlineAt)
		if ran, err := f.s.RunDue(e.Ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("gameweek 38 run: %v %v", ran, err)
		}
		v, err := f.s.Get(e.Ctx, f.users[1], f.l.ID)
		if err != nil || !v.Cycle.FreeAgency || v.Cycle.Upcoming != 38 {
			t.Fatalf("cycle after the run = %+v, %v", v.Cycle, err)
		}
		if _, _, err := f.s.AddFreeAgent(e.Ctx, f.users[1], f.l.ID, add[1], f.rostered(t, 1, scoring.FWD, 38)[0]); err != nil {
			t.Fatalf("free agent in gameweek 38: %v", err)
		}
		e.Clock.Set(g38.DeadlineAt)
		if _, _, err := f.s.CreateClaim(e.Ctx, f.users[2], f.l.ID, add[1], f.rostered(t, 2, scoring.FWD, 38)[0]); slug(err) != "season-over" {
			t.Fatalf("claim after the last deadline: %v", err)
		}
		if _, _, err := f.s.AddFreeAgent(e.Ctx, f.users[2], f.l.ID, add[1], f.rostered(t, 2, scoring.FWD, 38)[0]); slug(err) != "season-over" {
			t.Fatalf("free agent after the last deadline: %v", err)
		}
	})
}

// RESEARCH.md §7 Locks: a free-agent add is locked until the next waiver
// run, so a claim may not drop him either.
func TestClaimCannotDropAJustAddedPlayer(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		// Team 2 claims x for d1 and wins, so d1 is on waivers after the run.
		defs := f.free(t, scoring.DEF, 2)
		d1 := f.rostered(t, 1, scoring.DEF, 8)[0]
		if _, _, err := f.s.CreateClaim(e.Ctx, f.users[1], f.l.ID, defs[0], d1); err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if _, err := f.s.Run(e.Ctx, f.l.ID, 8); err != nil {
			t.Fatal(err)
		}
		// Team 1 adds free agent y, then claims d1 dropping y: y is locked.
		y := defs[1]
		if _, _, err := f.s.AddFreeAgent(e.Ctx, f.users[0], f.l.ID, y, f.rostered(t, 0, scoring.DEF, 8)[0]); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.CreateClaim(e.Ctx, f.users[0], f.l.ID, d1, y); slug(err) != "player-locked" {
			t.Fatalf("claim dropping a just-added player: %v", err)
		}
	})
}

// A run happens once per gameweek, however it is called.
func TestRunIsIdempotent(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if ran, err := f.s.Run(e.Ctx, f.l.ID, 8); !ran || err != nil {
			t.Fatalf("first run: %v %v", ran, err)
		}
		before := e.State(t, f.l.ID).Priority
		if ran, err := f.s.Run(e.Ctx, f.l.ID, 8); ran || err != nil {
			t.Fatalf("second run: %v %v", ran, err)
		}
		if after := e.State(t, f.l.ID).Priority; !slices.Equal(before, after) {
			t.Fatalf("priority changed on a second run: %v -> %v", before, after)
		}
	})
}

// Claim orders are per team: each team's list starts at 1, cancelling
// closes the gap in that team's list only, and a reorder sees only the
// caller's pending claims.
func TestClaimOrdersArePerTeam(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		adds := f.free(t, scoring.DEF, 3)
		d0 := f.rostered(t, 0, scoring.DEF, 8)[0]
		d1 := f.rostered(t, 1, scoring.DEF, 8)[0]
		var mine []league.Claim
		for _, a := range adds {
			_, c, err := f.s.CreateClaim(e.Ctx, f.users[0], f.l.ID, a, d0)
			if err != nil {
				t.Fatal(err)
			}
			mine = append(mine, c)
		}
		_, b1, err := f.s.CreateClaim(e.Ctx, f.users[1], f.l.ID, adds[0], d1)
		if err != nil || b1.Order != 1 {
			t.Fatalf("team 2's first claim = %+v, %v (order must be 1)", b1, err)
		}
		_, b2, _ := f.s.CreateClaim(e.Ctx, f.users[1], f.l.ID, adds[1], d1)

		// Cancel team 1's first claim: its others become 1 and 2; team 2's
		// stay 1 and 2.
		if err := f.s.CancelClaim(e.Ctx, f.users[0], f.l.ID, mine[0].ID); err != nil {
			t.Fatal(err)
		}
		orders := map[string]int{}
		for _, c := range e.State(t, f.l.ID).Claims {
			if c.Status == league.ClaimPending {
				orders[c.ID] = c.Order
			}
		}
		want := map[string]int{mine[1].ID: 1, mine[2].ID: 2, b1.ID: 1, b2.ID: 2}
		if !mapsEqual(orders, want) {
			t.Fatalf("orders = %v, want %v", orders, want)
		}

		// Reorder team 1's two claims; team 2's are not part of it.
		_, got, err := f.s.Reorder(e.Ctx, f.users[0], f.l.ID, []string{mine[2].ID, mine[1].ID})
		if err != nil || len(got) != 2 || got[0].ID != mine[2].ID {
			t.Fatalf("reorder = %+v, %v", got, err)
		}

		// An unknown claim id is not found, even though team 1 has a
		// cancelled claim.
		if _, _, err := f.s.UpdateClaim(e.Ctx, f.users[0], f.l.ID, "wv_01k7m2xq8v0000000000000000", d0); slug(err) != "not-found" {
			t.Fatalf("unknown claim: %v", err)
		}
	})
}
