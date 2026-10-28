package league

import "time"

// CurrentGW is the season's current gameweek at now: the latest gameweek
// whose deadline has passed (FPL's own is_current agrees). It is 0 before
// the first deadline or when no deadline is known.
func (w *World) CurrentGW(now time.Time) int {
	cur := 0
	for gw := 1; gw <= 38; gw++ {
		if g, ok := w.Gameweeks[gw]; ok && !g.DeadlineAt.IsZero() && !now.Before(g.DeadlineAt) {
			cur = gw
		}
	}
	return cur
}

// SyncCalendar sets Status.Current and Status.Next from the gameweek
// calendar at now. The stored status (the poller's last read of FPL) stays
// only when no deadline has passed yet or none is known.
func (w *World) SyncCalendar(now time.Time) {
	cur := w.CurrentGW(now)
	if cur == 0 {
		return
	}
	w.Status.Current = cur
	w.Status.Next = 0
	if cur < 38 {
		w.Status.Next = cur + 1
	}
}
