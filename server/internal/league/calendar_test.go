package league

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// The 2026-27 calendar around gameweek 8: GW7's deadline Fri 2 Oct 17:30,
// GW8's Sat 17 Oct 10:00, GW9's Fri 23 Oct 17:30 (UTC).
func calendarWorld(stored int) *World {
	at := func(m time.Month, d, h, min int) time.Time { return time.Date(2026, m, d, h, min, 0, 0, time.UTC) }
	return &World{
		Status: stats.Status{Current: stored, Next: stored + 1},
		Gameweeks: map[int]stats.Gameweek{
			7: {Number: 7, DeadlineAt: at(10, 2, 17, 30)},
			8: {Number: 8, DeadlineAt: at(10, 17, 10, 0)},
			9: {Number: 9, DeadlineAt: at(10, 23, 17, 30)},
		},
	}
}

func TestSyncCalendar(t *testing.T) {
	for _, c := range []struct {
		name      string
		now       time.Time
		stored    int
		cur, next int
	}{
		// The owner's case: the poller stored 7, GW8's deadline passed at 10:00.
		{"after GW8 deadline", time.Date(2026, 10, 17, 19, 0, 0, 0, time.UTC), 7, 8, 9},
		{"at GW8 deadline", time.Date(2026, 10, 17, 10, 0, 0, 0, time.UTC), 7, 8, 9},
		{"a second before", time.Date(2026, 10, 17, 9, 59, 59, 0, time.UTC), 7, 7, 8},
		// Before any known deadline the stored value stays.
		{"before the calendar", time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), 5, 5, 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := calendarWorld(c.stored)
			w.SyncCalendar(c.now)
			if w.Status.Current != c.cur || w.Status.Next != c.next {
				t.Errorf("current/next = %d/%d, want %d/%d", w.Status.Current, w.Status.Next, c.cur, c.next)
			}
		})
	}
	// After gameweek 38's deadline there is no next.
	w := &World{Gameweeks: map[int]stats.Gameweek{38: {Number: 38, DeadlineAt: time.Date(2027, 5, 23, 13, 30, 0, 0, time.UTC)}}}
	w.SyncCalendar(time.Date(2027, 5, 24, 0, 0, 0, 0, time.UTC))
	if w.Status.Current != 38 || w.Status.Next != 0 {
		t.Errorf("after GW38: %+v", w.Status)
	}
}
