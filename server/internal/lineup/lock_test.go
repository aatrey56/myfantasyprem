package lineup

import (
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
)

// RESEARCH.md §7: lineups lock at the gameweek deadline (first kickoff − 90
// minutes). LockDue announces each passed deadline once (ADR 0010). At the
// test start, gameweek 7 is being played: its deadline has passed, 8's has
// not.
func TestLockDue(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		notes := &leaguetest.Notes{}
		s := New(e.Repo, e.Stats, e.Clock.Now, notes)
		w := e.World(t)

		pre, _ := e.League(t, 4, league.SettingsInput{})
		if ok, err := s.LockDue(e.Ctx, pre.ID); ok || err != nil {
			t.Fatalf("league before the draft: %v %v", ok, err)
		}

		l, _ := e.InSeason(t, 4, league.SettingsInput{})
		if ok, err := s.LockDue(e.Ctx, l.ID); !ok || err != nil {
			t.Fatalf("first call: %v %v", ok, err)
		}
		st := e.State(t, l.ID)
		if _, ran := st.Ran(league.RunLineupLock, 7); !ran {
			t.Fatalf("runs = %v, want gameweek 7's lock", st.Runs)
		}
		if _, ran := st.Ran(league.RunLineupLock, 8); ran {
			t.Fatal("gameweek 8 locked before its deadline")
		}
		if ok, err := s.LockDue(e.Ctx, l.ID); ok || err != nil {
			t.Fatalf("second call: %v %v", ok, err)
		}
		got := notes.Kinds()
		if !slices.Equal(got, []string{league.ChangeLineupLock}) {
			t.Fatalf("announced %v, want one %s", got, league.ChangeLineupLock)
		}

		// One second before gameweek 8's deadline nothing new; at it, 8 locks.
		e.Clock.Set(w.Gameweeks[8].DeadlineAt.Add(-time.Second))
		if ok, _ := s.LockDue(e.Ctx, l.ID); ok {
			t.Fatal("gameweek 8 announced before its deadline")
		}
		e.Clock.Set(w.Gameweeks[8].DeadlineAt)
		if ok, err := s.LockDue(e.Ctx, l.ID); !ok || err != nil {
			t.Fatalf("at gameweek 8's deadline: %v %v", ok, err)
		}
		if _, ran := e.State(t, l.ID).Ran(league.RunLineupLock, 8); !ran {
			t.Fatal("gameweek 8 lock not recorded")
		}

		// After the last deadline, gameweek 38 is the last one announced.
		e.Clock.Set(w.Gameweeks[38].DeadlineAt.Add(time.Hour))
		if ok, err := s.LockDue(e.Ctx, l.ID); !ok || err != nil {
			t.Fatalf("after gameweek 38's deadline: %v %v", ok, err)
		}
		if _, ran := e.State(t, l.ID).Ran(league.RunLineupLock, 38); !ran {
			t.Fatal("gameweek 38 lock not recorded")
		}
	})
}

// Before gameweek 1's deadline there is nothing to lock.
func TestLockDueBeforeTheSeason(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, _ := e.InSeason(t, 4, league.SettingsInput{})
		w := e.World(t)
		e.Clock.Set(w.Gameweeks[1].DeadlineAt.Add(-time.Minute))
		if ok, err := s.LockDue(e.Ctx, l.ID); ok || err != nil {
			t.Fatalf("before gameweek 1: %v %v", ok, err)
		}
	})
}
