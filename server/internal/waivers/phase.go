package waivers

import (
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Transaction phases (TransactionPhase.phase in the contract).
const (
	PhaseWaivers     = "waivers"
	PhaseFreeAgency  = "free_agency"
	PhaseLocked      = "locked"
	Locked           = "locked" // RosterStatus: unowned while the gameweek is locked
	waiverLeadBefore = 24 * time.Hour
)

// ClaimsWhileLocked is whether waiver claims for the next run may be
// placed during the locked phase. ASSUMPTION (the owner to confirm): FPL
// Draft lets managers queue claims for the next waiver run once the
// deadline has passed.
const ClaimsWhileLocked = true

// Phase is where the transaction window stands at a moment.
type Phase struct {
	Phase string
	// Gameweek is the gameweek moves made now are for (the first whose
	// deadline has not passed; 39 after the last deadline).
	Gameweek int
	// Until is when the phase ends: the waiver run for waivers, the
	// deadline for free agency, and for locked the next waiver run, when
	// free-agent signings reopen. Nil when unknown or after the season.
	Until *time.Time
}

// WaiverTime is FPL's waiver time for a gameweek: its waivers_time, else
// 24 hours before its deadline.
func WaiverTime(g stats.Gameweek) time.Time {
	if !g.WaiverDeadlineAt.IsZero() {
		return g.WaiverDeadlineAt.UTC()
	}
	return g.DeadlineAt.Add(-waiverLeadBefore).UTC()
}

// PhaseAt is the FPL Draft transaction window at now. For each gameweek N:
// claims until N's waiver time; the waiver run; free agency until N's
// deadline; then locked (no free-agent signings) until gameweek N is
// finished. ranN reports whether gameweek n's waiver run is done. The lock
// also ends once the next waiver run is due or done, so a gameweek whose
// finished flag comes late never blocks the run.
func PhaseAt(now time.Time, gws map[int]stats.Gameweek, ran func(n int) bool) Phase {
	cur := 0
	for n := 1; n <= 38; n++ {
		if g, ok := gws[n]; ok && !g.DeadlineAt.IsZero() && !now.Before(g.DeadlineAt) {
			cur = n
		}
	}
	up := cur + 1
	if up > 38 {
		return Phase{Phase: PhaseFreeAgency, Gameweek: 39}
	}
	next, ok := gws[up]
	if !ok {
		return Phase{Phase: PhaseFreeAgency, Gameweek: up}
	}
	at := WaiverTime(next)
	runDone := ran(up)
	if runDone {
		d := next.DeadlineAt.UTC()
		return Phase{Phase: PhaseFreeAgency, Gameweek: up, Until: &d}
	}
	if now.Before(at) && cur >= 1 && !gws[cur].Finished {
		return Phase{Phase: PhaseLocked, Gameweek: up, Until: &at}
	}
	// Claims open, or the run is due and not done yet.
	return Phase{Phase: PhaseWaivers, Gameweek: up, Until: &at}
}
