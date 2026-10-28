package waivers

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Gameweek 8 of 2026-27 as FPL Draft has it: waivers Fri 16 Oct 10:00 UTC,
// deadline Sat 17 Oct 10:00 UTC. Gameweek 7's deadline was Fri 2 Oct 17:30;
// gameweek 9's waivers Thu 22 Oct 17:30, deadline Fri 23 Oct 17:30.
func TestPhaseAtGameweek8(t *testing.T) {
	utc := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, time.UTC) }
	cal := func(gw7Done, gw8Done bool) map[int]stats.Gameweek {
		return map[int]stats.Gameweek{
			7: {Number: 7, DeadlineAt: utc(2, 17, 30), WaiverDeadlineAt: utc(1, 17, 30), Finished: gw7Done},
			8: {Number: 8, DeadlineAt: utc(17, 10, 0), WaiverDeadlineAt: utc(16, 10, 0), Finished: gw8Done},
			9: {Number: 9, DeadlineAt: utc(23, 17, 30), WaiverDeadlineAt: utc(22, 17, 30)},
		}
	}
	ran := func(gws ...int) func(int) bool {
		return func(n int) bool {
			for _, g := range gws {
				if g == n {
					return true
				}
			}
			return false
		}
	}
	for _, c := range []struct {
		name           string
		now            time.Time
		gw7Done, gw8Do bool
		ran            func(int) bool
		phase          string
		gw             int
		until          time.Time
	}{
		// Gameweek 7 still being played: locked, signings reopen at GW8's run.
		{"GW7 live", utc(12, 21, 0), false, false, ran(7), PhaseLocked, 8, utc(16, 10, 0)},
		// Gameweek 7 finished: claims for GW8 until Fri 10:00.
		{"GW7 finished", utc(14, 12, 0), true, false, ran(7), PhaseWaivers, 8, utc(16, 10, 0)},
		{"a second before the run", utc(16, 9, 59), true, false, ran(7), PhaseWaivers, 8, utc(16, 10, 0)},
		// Run time reached, run not done yet: still waivers (the run is due).
		{"run due", utc(16, 10, 0), true, false, ran(7), PhaseWaivers, 8, utc(16, 10, 0)},
		// After the run: free agency until Sat 10:00.
		{"after the run", utc(16, 10, 1), true, false, ran(7, 8), PhaseFreeAgency, 8, utc(17, 10, 0)},
		{"a second before the deadline", utc(17, 9, 59), true, false, ran(7, 8), PhaseFreeAgency, 8, utc(17, 10, 0)},
		// From GW8's deadline until GW8 is finished: locked, reopening after
		// GW9's run Thu 22 Oct 17:30. Saturday's matches over is not enough.
		{"at the deadline", utc(17, 10, 0), true, false, ran(7, 8), PhaseLocked, 9, utc(22, 17, 30)},
		{"Saturday evening", utc(17, 19, 0), true, false, ran(7, 8), PhaseLocked, 9, utc(22, 17, 30)},
		// GW8 finished: claims for GW9 until its run.
		{"GW8 finished", utc(20, 12, 0), true, true, ran(7, 8), PhaseWaivers, 9, utc(22, 17, 30)},
		// GW8's finished flag late past GW9's run time: the run still happens.
		{"finished flag late", utc(22, 17, 30), true, false, ran(7, 8), PhaseWaivers, 9, utc(22, 17, 30)},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := PhaseAt(c.now, cal(c.gw7Done, c.gw8Do), c.ran)
			if got.Phase != c.phase || got.Gameweek != c.gw || got.Until == nil || !got.Until.Equal(c.until) {
				t.Errorf("PhaseAt = %s GW%d until %v, want %s GW%d until %v", got.Phase, got.Gameweek, got.Until, c.phase, c.gw, c.until)
			}
		})
	}
}

// Without FPL's waivers_time the run is 24 hours before the deadline.
func TestWaiverTimeFallback(t *testing.T) {
	g := stats.Gameweek{DeadlineAt: time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC)}
	if got, want := WaiverTime(g), time.Date(2026, 10, 16, 10, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("WaiverTime = %v, want %v", got, want)
	}
}
