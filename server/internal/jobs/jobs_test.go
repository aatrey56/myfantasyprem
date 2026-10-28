package jobs

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

func runner(e *leaguetest.Env, n league.Notifier) *Runner {
	return &Runner{Leagues: e.Repo, Stats: e.Stats, Now: e.Clock.Now, Notify: n, Log: e.Log}
}

func TestTick(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		notes := &leaguetest.Notes{}
		r := runner(e, notes)
		w := e.World(t)

		inSeason, _ := e.InSeason(t, 4, league.SettingsInput{})
		at := e.Clock.Now().Add(time.Hour)
		scheduled, _ := e.League(t, 4, league.SettingsInput{DraftStartsAt: &at})
		drafting, users := e.League(t, 4, league.SettingsInput{})
		if _, err := r.Draft().Start(e.Ctx, users[0], drafting.ID); err != nil {
			t.Fatal(err)
		}
		notes.Kinds() // forget the start

		// Monday: gameweek 7's lock is announced once; nothing else is due.
		res, err := r.Tick(e.Ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res != (TickResult{LineupLocks: 1}) {
			t.Fatalf("first tick = %+v", res)
		}
		if res, _ := r.Tick(e.Ctx); res != (TickResult{}) {
			t.Fatalf("second tick = %+v", res)
		}
		if k := notes.Kinds(); !slices.Equal(k, []string{league.ChangeLineupLock}) {
			t.Fatalf("changes = %v", k)
		}

		// An hour on: the scheduled draft starts and the first clock (60 s)
		// has run out.
		e.Clock.Advance(time.Hour)
		res, _ = r.Tick(e.Ctx)
		if res.DraftsStarted != 1 || res.ClocksSettled != 1 || res.Errors != 0 {
			t.Fatalf("draft tick = %+v", res)
		}
		if st := e.State(t, scheduled.ID); st.Draft.Status != league.DraftInProgress {
			t.Fatalf("scheduled draft = %s", st.Draft.Status)
		}
		if st := e.State(t, drafting.ID); len(st.Draft.Picks) == 0 || !st.Draft.Autopick[st.Draft.Picks[0].TeamID] {
			t.Fatalf("expired clock: %+v", st.Draft.Picks)
		}

		// Thursday 17:30: gameweek 8's waivers run, once.
		e.Clock.Set(w.Gameweeks[8].WaiverDeadlineAt)
		res, _ = r.Tick(e.Ctx)
		if res.WaiverRuns != 1 {
			t.Fatalf("waiver tick = %+v", res)
		}
		if _, ran := e.State(t, inSeason.ID).Ran(league.RunWaivers, 8); !ran {
			t.Fatal("waivers did not run")
		}
		if res, _ := r.Tick(e.Ctx); res.WaiverRuns != 0 {
			t.Fatal("waivers ran twice")
		}
		// Friday's deadline: gameweek 8's lock.
		e.Clock.Set(w.Gameweeks[8].DeadlineAt)
		if res, _ := r.Tick(e.Ctx); res.LineupLocks != 1 {
			t.Fatalf("deadline tick = %+v", res)
		}
	})
}

// Run dispatches each job kind.
func TestRunDispatch(t *testing.T) {
	e := leaguetest.New(t, nil)
	r := runner(e, nil)
	l, users := e.League(t, 4, league.SettingsInput{})
	if _, err := r.Draft().Start(e.Ctx, users[0], l.ID); err != nil {
		t.Fatal(err)
	}
	e.Clock.Advance(2 * time.Minute)
	if err := r.Run(e.Ctx, draft.ClockArgs{LeagueID: l.ID, Overall: 1}); err != nil {
		t.Fatal(err)
	}
	if st := e.State(t, l.ID); len(st.Draft.Picks) != 1 {
		t.Fatalf("picks = %d", len(st.Draft.Picks))
	}
	if err := r.Run(e.Ctx, TickArgs{}); err != nil {
		t.Fatal(err)
	}
	if err := r.Run(e.Ctx, PruneArgs{}); err != nil {
		t.Fatal(err)
	}
}

// The prune job also clears expired sign-in state: magic links, sessions
// and idempotency records (24 hours old).
func TestPruneClearsAuthAndIdempotency(t *testing.T) {
	e := leaguetest.New(t, nil)
	r := runner(e, nil)
	var authPruned bool
	var idemBefore time.Time
	r.PruneAuth = func(context.Context) (int, error) { authPruned = true; return 3, nil }
	r.PruneIdempotency = func(_ context.Context, before time.Time) (int, error) { idemBefore = before; return 2, nil }
	if err := r.Run(e.Ctx, PruneArgs{}); err != nil {
		t.Fatal(err)
	}
	if !authPruned || !idemBefore.Equal(e.Clock.Now().Add(-24*time.Hour)) {
		t.Fatalf("auth pruned %v, idempotency before %v", authPruned, idemBefore)
	}
}

// The memory scheduler runs a job at its time, outside the transaction
// that scheduled it.
func TestMemorySchedulerRunsClockJobs(t *testing.T) {
	e := leaguetest.New(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := runner(e, nil)
	m := NewMemory(ctx, r)
	e.Repo.(*league.Memory).SetScheduler(m)
	l, users := e.League(t, 4, league.SettingsInput{})
	if _, err := r.Draft().Start(e.Ctx, users[0], l.ID); err != nil {
		t.Fatal(err)
	}
	// Move the test clock past the deadline, then schedule a job for now:
	// it settles the clock.
	e.Clock.Advance(2 * time.Minute)
	if err := m.ScheduleTx(ctx, nil, draft.ClockArgs{LeagueID: l.ID, Overall: 1}, time.Now()); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(e.State(t, l.ID).Draft.Picks) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the clock job did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	m.stop()
}

// A draft whose final pick comes after the last gameweek's deadline (here
// a missed clock autopicked by the tick) still completes: rosters from the
// picks, no lineups or matchups, the league complete, and the next tick
// has nothing to do instead of failing forever.
func TestTickFinishesADraftAfterTheSeason(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		r := runner(e, nil)
		s := r.Draft()
		last := e.World(t).Gameweeks[38].DeadlineAt
		e.Clock.Set(last.Add(-time.Hour))
		l, users := e.League(t, 4, league.SettingsInput{})
		order := []string{l.Teams[0].ID, l.Teams[1].ID, l.Teams[2].ID, l.Teams[3].ID}
		if _, err := s.UpdateDraft(e.Ctx, users[0], l.ID, draft.Update{Order: order}); err != nil {
			t.Fatal(err)
		}
		for _, u := range users[:3] {
			if _, err := s.SetAutopick(e.Ctx, u, l.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Start(e.Ctx, users[0], l.ID); err != nil {
			t.Fatal(err)
		}
		// Team 4 picks by hand until only the final pick (its own) is left.
		limit := map[scoring.Position]int{scoring.GKP: 2, scoring.DEF: 5, scoring.MID: 5, scoring.FWD: 3}
		for {
			v, err := s.Get(e.Ctx, users[3], l.ID)
			if err != nil {
				t.Fatal(err)
			}
			total := v.Total()
			if len(v.Draft.Picks) == total-1 {
				break
			}
			if c := v.Current(); c == nil || c.TeamID != order[3] {
				t.Fatalf("on the clock: %+v after %d picks", c, len(v.Draft.Picks))
			}
			taken, have := map[string]bool{}, scoring.Counts{}
			for _, p := range v.Draft.Picks {
				taken[p.PlayerID] = true
				if p.TeamID == order[3] {
					have[v.World.Players[p.PlayerID].Position]++
				}
			}
			var ids []string
			for id, p := range v.World.Players {
				if !taken[id] && have[p.Position] < limit[p.Position] {
					ids = append(ids, id)
				}
			}
			slices.Sort(ids)
			if _, _, err := s.Pick(e.Ctx, users[3], l.ID, ids[0]); err != nil {
				t.Fatal(err)
			}
		}

		// The season's last deadline passes with the final pick's clock
		// running; the tick autopicks it and completes the draft.
		e.Clock.Set(last.Add(time.Minute))
		res, err := r.Tick(e.Ctx)
		if err != nil || res != (TickResult{ClocksSettled: 1}) {
			t.Fatalf("tick = %+v, %v", res, err)
		}
		st := e.State(t, l.ID)
		if st.Draft.Status != league.DraftComplete || st.League.Status != league.StatusComplete || len(st.Draft.Picks) != 60 {
			t.Fatalf("draft %s, league %s, %d picks", st.Draft.Status, st.League.Status, len(st.Draft.Picks))
		}
		if p := st.Draft.Picks[59]; p.TeamID != order[3] || p.Method == league.PickManual {
			t.Fatalf("final pick = %+v", p)
		}
		if len(st.Game.Rosters) != 60 {
			t.Fatalf("roster slots = %d", len(st.Game.Rosters))
		}
		for _, team := range order {
			if n := len(st.Game.RosterAt(team, 39)); n != 15 {
				t.Fatalf("team %s roster = %d", team, n)
			}
			if n := len(st.Game.RosterAt(team, 38)); n != 0 {
				t.Fatalf("team %s rostered in a played gameweek: %d", team, n)
			}
		}
		if len(st.Game.Lineups) != 0 || len(st.Game.Matchups) != 0 {
			t.Fatalf("lineups %d, matchups %d", len(st.Game.Lineups), len(st.Game.Matchups))
		}
		for range 2 {
			if res, err := r.Tick(e.Ctx); err != nil || res != (TickResult{}) {
				t.Fatalf("idle tick = %+v, %v", res, err)
			}
		}
	})
}
