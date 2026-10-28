package draft

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"pgregory.net/rapid"
)

// Tests in this file close gaps found by mutation testing (docs/testing.md).
// Expected values come from the rules (RESEARCH.md §7 "Draft", ADR 0008, the
// DraftUpdate contract), worked out by hand, not from the code.

// RESEARCH.md §7: the draft is a snake. For any team count and round count,
// every team picks exactly once per round, odd rounds run in draft order and
// even rounds in reverse.
func TestSlotSnakeProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(2, 20).Draw(rt, "teams")
		rounds := rapid.IntRange(1, 15).Draw(rt, "rounds")
		order := make([]string, n)
		for i := range order {
			order[i] = string(rune('A' + i))
		}
		for r := 1; r <= rounds; r++ {
			var got []string
			for k := 1; k <= n; k++ {
				team, round, in := Slot(order, (r-1)*n+k)
				if round != r || in != k {
					rt.Fatalf("pick %d: round %d pick %d, want %d/%d", (r-1)*n+k, round, in, r, k)
				}
				got = append(got, team)
			}
			want := slices.Clone(order)
			if r%2 == 0 {
				slices.Reverse(want)
			}
			if !slices.Equal(got, want) {
				rt.Fatalf("round %d = %v, want %v", r, got, want)
			}
		}
	})
}

// Overall pick numbers start at 1; anything else has no team.
func TestSlotOutOfRange(t *testing.T) {
	for _, n := range []int{0, -1} {
		if team, round, in := Slot([]string{"a", "b", "c"}, n); team != "" || round != 0 || in != 0 {
			t.Errorf("Slot(%d) = %q %d %d, want empty", n, team, round, in)
		}
	}
	if team, _, _ := Slot(nil, 1); team != "" {
		t.Errorf("Slot with no teams = %q", team)
	}
}

// ClockAt: the clock for pick n starts when pick n−1 was made (the draft
// start for pick 1) and runs for the pick timer. 3 teams × 2 rounds = 6
// picks; 2 made, at start+40s and start+100s, timer 60s.
func TestClockAt(t *testing.T) {
	start := time.Date(2026, 8, 1, 18, 0, 0, 0, time.UTC)
	p1, p2 := start.Add(40*time.Second), start.Add(100*time.Second)
	d := &league.Draft{
		Status: league.DraftInProgress, PickTimerS: 60, ClockStartedAt: &p2,
		Order: []string{"a", "b", "c"},
		Picks: []league.Pick{{Overall: 1, TeamID: "a", PickedAt: p1}, {Overall: 2, TeamID: "b", PickedAt: p2}},
	}
	// Pick 2's clock started at pick 1 (start+40s): deadline start+100s.
	c := ClockAt(d, 2, 2)
	if c == nil || c.TeamID != "b" || !c.StartedAt.Equal(p1) || !c.DeadlineAt.Equal(start.Add(100*time.Second)) {
		t.Fatalf("pick 2 clock = %+v", c)
	}
	// Pick 3 is on the clock now: started at pick 2, deadline start+160s.
	c = ClockAt(d, 2, 3)
	if c == nil || c.TeamID != "c" || c.Round != 1 || !c.DeadlineAt.Equal(start.Add(160*time.Second)) {
		t.Fatalf("pick 3 clock = %+v", c)
	}
	// No clock before pick 1, past the made picks + 1, or past the last pick.
	for _, n := range []int{0, 4, 7} {
		if c := ClockAt(d, 2, n); c != nil {
			t.Errorf("ClockAt(%d) = %+v, want nil", n, c)
		}
	}
	// A paused draft shows the pick on the clock without a deadline.
	paused := *d
	paused.Status = league.DraftPaused
	if c := ClockAt(&paused, 2, 3); c == nil || c.DeadlineAt != nil {
		t.Fatalf("paused clock = %+v", c)
	}
	// A complete draft has no clock for the next pick; a draft not started
	// has none at all.
	done := *d
	done.Status = league.DraftComplete
	if c := ClockAt(&done, 2, 3); c != nil {
		t.Fatalf("complete draft clock = %+v", c)
	}
	notStarted := *d
	notStarted.ClockStartedAt = nil
	if c := ClockAt(&notStarted, 2, 3); c != nil {
		t.Fatalf("not started clock = %+v", c)
	}
}

// DraftUpdate.pickTimerSeconds in the contract: 15 to 86400, inclusive.
func TestPickTimerRange(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 4, league.SettingsInput{})
		for _, tt := range []struct {
			secs int
			ok   bool
		}{{14, false}, {15, true}, {86400, true}, {86401, false}} {
			secs := tt.secs
			_, err := s.UpdateDraft(e.Ctx, users[0], l.ID, Update{PickTimer: &secs})
			if (err == nil) != tt.ok {
				t.Errorf("pick timer %d: err %v, want ok=%v", secs, err, tt.ok)
			}
		}
	})
}

// "order must list every team once": a repeated or unknown team is invalid
// even when the length is right.
func TestOrderMustListEveryTeamOnce(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 4, league.SettingsInput{})
		ids := []string{l.Teams[0].ID, l.Teams[1].ID, l.Teams[2].ID, l.Teams[3].ID}
		for name, order := range map[string][]string{
			"repeated": {ids[0], ids[0], ids[1], ids[2]},
			"unknown":  {ids[0], ids[1], ids[2], "tm_01k7m2xq8v0000000000000000"},
		} {
			if _, err := s.UpdateDraft(e.Ctx, users[0], l.ID, Update{Order: order}); slug(err) != "invalid-request" {
				t.Errorf("%s order: %v", name, err)
			}
		}
	})
}

// A queue holds at most 100 players (contract): 100 is fine, 101 is not.
func TestQueueLimit(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 4, league.SettingsInput{})
		w := e.World(t)
		var all []string
		for id := range w.Players {
			all = append(all, id)
		}
		slices.Sort(all)
		if _, err := s.SetQueue(e.Ctx, users[1], l.ID, all[:100]); err != nil {
			t.Fatalf("100 players: %v", err)
		}
		if _, err := s.SetQueue(e.Ctx, users[1], l.ID, all[:101]); slug(err) != "invalid-request" {
			t.Fatalf("101 players: %v", err)
		}
	})
}

// started is a 4-team league with the draft started in team order 0..3,
// every manager present, timer 60s.
func started(t *testing.T, e *leaguetest.Env) (*leaguetest.Env, *Service, *league.League, []string, *jobs) {
	t.Helper()
	sched := &jobs{}
	e.Repo.(scheduling).SetScheduler(sched)
	s := New(e.Repo, e.Stats, e.Clock.Now, nil)
	l, users := e.League(t, 4, league.SettingsInput{})
	timer := 60
	order := []string{l.Teams[0].ID, l.Teams[1].ID, l.Teams[2].ID, l.Teams[3].ID}
	if _, err := s.UpdateDraft(e.Ctx, users[0], l.ID, Update{Order: order, PickTimer: &timer}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(e.Ctx, users[0], l.ID); err != nil {
		t.Fatal(err)
	}
	return e, s, l, users, sched
}

// byRank lists players at pos best draft rank first (ties by id), skipping
// taken ones: the order RESEARCH.md §7 "highest draft_rank" asks for.
func byRank(w *league.World, pos scoring.Position, taken map[string]bool) []string {
	var out []string
	for id, p := range w.Players {
		if p.Position == pos && !taken[id] {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(w.Players[a].DraftRank, w.Players[b].DraftRank), cmp.Compare(a, b))
	})
	return out
}

// RESEARCH.md §7 "Missed pick": the top queued player at a needed position,
// otherwise the highest draft_rank. Team 0 fills both goalkeeper places by
// hand; its queue then holds a third keeper (not needed) and a drafted
// player (taken). In round 2 its clock runs out: neither queued player may be
// picked, so it takes the best-ranked player overall at a needed position.
func TestAutopickSkipsFullPositionsAndTakenPlayers(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		_, s, l, users, _ := started(t, e)
		w := e.World(t)
		gks := byRank(w, scoring.GKP, nil)
		// Round 1: team 0 takes gks[0]; teams 1-3 pick defenders.
		if _, _, err := s.Pick(e.Ctx, users[0], l.ID, gks[0]); err != nil {
			t.Fatal(err)
		}
		defs := byRank(w, scoring.DEF, nil)
		for i := 1; i <= 3; i++ {
			if _, _, err := s.Pick(e.Ctx, users[i], l.ID, defs[i]); err != nil {
				t.Fatal(err)
			}
		}
		// Round 2 runs 3,2,1,0: team 3..1 pick, team 0 takes its second keeper.
		for i := 3; i >= 1; i-- {
			if _, _, err := s.Pick(e.Ctx, users[i], l.ID, defs[4+i]); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := s.Pick(e.Ctx, users[0], l.ID, gks[1]); err != nil {
			t.Fatal(err)
		}
		// Round 3 starts with team 0 again. Its queue: a third keeper (no GKP
		// place left) and defs[1] (drafted by team 1).
		if _, err := s.SetQueue(e.Ctx, users[0], l.ID, []string{gks[2], defs[1]}); err != nil {
			t.Fatal(err)
		}
		// SetQueue silently drops drafted players, so put defs[1] back the way
		// a stale queue would hold it.
		if err := e.Repo.Tx(e.Ctx, func(tx league.Tx) error {
			st, err := tx.LoadState(e.Ctx, l.ID)
			if err != nil {
				return err
			}
			st.Draft.Queues[l.Teams[0].ID] = []string{gks[2], defs[1]}
			return tx.SaveState(e.Ctx, st)
		}); err != nil {
			t.Fatal(err)
		}
		e.Clock.Advance(61 * time.Second)
		if changed, err := s.Expire(e.Ctx, l.ID); err != nil || !changed {
			t.Fatalf("expire: %v %v", changed, err)
		}
		v, _ := s.Get(e.Ctx, users[0], l.ID)
		got := v.Draft.Picks[8]
		taken := map[string]bool{}
		for _, p := range v.Draft.Picks[:8] {
			taken[p.PlayerID] = true
		}
		// Best available overall among DEF/MID/FWD (GKP is full).
		var best string
		for _, pos := range []scoring.Position{scoring.DEF, scoring.MID, scoring.FWD} {
			c := byRank(w, pos, taken)[0]
			if best == "" || w.Players[c].DraftRank < w.Players[best].DraftRank ||
				(w.Players[c].DraftRank == w.Players[best].DraftRank && c < best) {
				best = c
			}
		}
		if got.Method != league.PickRank || got.PlayerID != best {
			t.Fatalf("autopick = %+v (%s), want rank pick of %s", got, w.Players[got.PlayerID].Position, best)
		}
	})
}

// A pick removes that player from every queue and nothing else.
func TestPickDropsPlayerFromEveryQueue(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		_, s, l, users, _ := started(t, e)
		w := e.World(t)
		mids := byRank(w, scoring.MID, nil)
		for _, u := range users[1:3] {
			if _, err := s.SetQueue(e.Ctx, u, l.ID, []string{mids[0], mids[1], mids[2]}); err != nil {
				t.Fatal(err)
			}
		}
		if _, _, err := s.Pick(e.Ctx, users[0], l.ID, mids[1]); err != nil {
			t.Fatal(err)
		}
		for _, u := range users[1:3] {
			q, err := s.GetQueue(e.Ctx, u, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			if want := []string{mids[0], mids[2]}; !slices.Equal(q.Players, want) {
				t.Fatalf("queue = %v, want %v", q.Players, want)
			}
		}
	})
}

// Expire does nothing while the clock runs; once it runs out it makes the
// one missed pick. The next clock job is for the next overall pick.
func TestExpireOnlyAfterDeadline(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		_, s, l, users, sched := started(t, e)
		e.Clock.Advance(59 * time.Second)
		if changed, err := s.Expire(e.Ctx, l.ID); err != nil || changed {
			t.Fatalf("expire before the deadline: %v %v", changed, err)
		}
		e.Clock.Advance(time.Second) // exactly the deadline: the clock has run out
		if changed, err := s.Expire(e.Ctx, l.ID); err != nil || !changed {
			t.Fatalf("expire at the deadline: %v %v", changed, err)
		}
		v, _ := s.Get(e.Ctx, users[0], l.ID)
		if len(v.Draft.Picks) != 1 || v.Draft.Picks[0].TeamID != l.Teams[0].ID {
			t.Fatalf("picks = %+v", v.Draft.Picks)
		}
		last := sched.args[len(sched.args)-1].(ClockArgs)
		if last.Overall != 2 || !last.Deadline.Equal(e.Clock.Now().Add(60*time.Second)) {
			t.Fatalf("next clock job = %+v", last)
		}
	})
}

// SetAutopick returns the caller's queue with the flag as set.
func TestSetAutopickReturnsQueue(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		_, s, l, users, _ := started(t, e)
		q, err := s.SetAutopick(e.Ctx, users[2], l.ID, true)
		if err != nil || q == nil || !q.Autopick || q.TeamID != l.Teams[2].ID {
			t.Fatalf("SetAutopick = %+v, %v", q, err)
		}
		q, err = s.SetAutopick(e.Ctx, users[2], l.ID, false)
		if err != nil || q == nil || q.Autopick {
			t.Fatalf("SetAutopick off = %+v, %v", q, err)
		}
	})
}

// StartIfDue starts nothing without a start time or before the league is
// full, even after the time.
func TestStartIfDueNeedsTimeAndFullLeague(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, _ := e.League(t, 4, league.SettingsInput{})
		e.Clock.Advance(24 * time.Hour)
		if ok, err := s.StartIfDue(e.Ctx, l.ID); ok || err != nil {
			t.Fatalf("no start time: %v %v", ok, err)
		}
		at := e.Clock.Now().Add(-time.Minute)
		u := e.User(t)
		half, err := e.Leagues.Create(e.Ctx, u, "Half", 4, "Mine", league.SettingsInput{DraftStartsAt: &at})
		if err != nil {
			t.Fatal(err)
		}
		if ok, err := s.StartIfDue(e.Ctx, half.ID); ok || err != nil {
			t.Fatalf("not full: %v %v", ok, err)
		}
		if ok, err := s.StartIfDue(e.Ctx, "lg_01k7m2xq8v0000000000000000"); ok || err != nil {
			t.Fatalf("unknown league: %v %v", ok, err)
		}
	})
}
