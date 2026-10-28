package mirror

import (
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Shadow check reasons (ShadowArgs.Reason).
const (
	ReasonAfterWaivers   = "after_waivers"
	ReasonBeforeDeadline = "before_deadline"
	ReasonAfterDeadline  = "after_deadline"
	ReasonKickoff        = "kickoff"
	ReasonHalfTime       = "half_time"
	ReasonFullTime       = "full_time"
	ReasonBonus          = "bonus"
	ReasonDataChecked    = "data_checked"
	ReasonManual         = "manual"
)

// Offsets from the FPL calendar (docs/adr/0016).
const (
	afterWaivers   = 10 * time.Minute  // FPL's waiver run takes a few minutes
	beforeDeadline = 15 * time.Minute  // lineups as they stand before the lock
	afterDeadline  = 10 * time.Minute  // FPL publishes picks soon after the lock
	halfTime       = 55 * time.Minute  // kickoff + 45 + a little stoppage
	fullTime       = 115 * time.Minute // kickoff + 90 + stoppage + half time
	bonus          = time.Hour         // after full time: bonus is added
)

// ShadowSlot is one scheduled shadow check.
type ShadowSlot struct {
	At     time.Time
	Reason string
}

// ShadowTimes lists the shadow checks the FPL calendar calls for in
// [from, to): after each waiver run, before and after each deadline, and
// at each fixture's kickoff, half time, full time and bonus. Slots at the
// same moment run once (the first reason wins, by the order above); the
// result is in time order. A postponed fixture (no kickoff) has none.
func ShadowTimes(gws []stats.Gameweek, fixtures []stats.Fixture, from, to time.Time) []ShadowSlot {
	var out []ShadowSlot
	add := func(at time.Time, reason string) {
		if at.IsZero() || at.Before(from) || !at.Before(to) {
			return
		}
		if slices.ContainsFunc(out, func(s ShadowSlot) bool { return s.At.Equal(at) }) {
			return
		}
		out = append(out, ShadowSlot{At: at.UTC(), Reason: reason})
	}
	for _, g := range gws {
		if !g.WaiverDeadlineAt.IsZero() {
			add(g.WaiverDeadlineAt.Add(afterWaivers), ReasonAfterWaivers)
		}
		if !g.DeadlineAt.IsZero() {
			add(g.DeadlineAt.Add(-beforeDeadline), ReasonBeforeDeadline)
			add(g.DeadlineAt.Add(afterDeadline), ReasonAfterDeadline)
		}
	}
	for _, f := range fixtures {
		if f.KickoffAt == nil || f.Gameweek == 0 {
			continue
		}
		k := *f.KickoffAt
		add(k, ReasonKickoff)
		add(k.Add(halfTime), ReasonHalfTime)
		add(k.Add(fullTime), ReasonFullTime)
		add(k.Add(fullTime+bonus), ReasonBonus)
	}
	slices.SortStableFunc(out, func(a, b ShadowSlot) int { return a.At.Compare(b.At) })
	return out
}

// DataCheckedSlot is the check for the latest gameweek FPL marked
// data_checked before now, at its last kickoff (a fixed moment, so the
// hourly planner schedules it once); ok is false when there is none or the
// next deadline has passed.
func DataCheckedSlot(gws []stats.Gameweek, fixtures []stats.Fixture, now time.Time) (ShadowSlot, bool) {
	var best *stats.Gameweek
	for i := range gws {
		g := &gws[i]
		if g.DataChecked && !g.DeadlineAt.After(now) && (best == nil || g.Number > best.Number) {
			best = g
		}
	}
	if best == nil {
		return ShadowSlot{}, false
	}
	for _, g := range gws {
		if g.Number == best.Number+1 && !g.DeadlineAt.IsZero() && !now.Before(g.DeadlineAt) {
			return ShadowSlot{}, false
		}
	}
	at := best.DeadlineAt
	for _, f := range fixtures {
		if f.Gameweek == best.Number && f.KickoffAt != nil && f.KickoffAt.After(at) {
			at = *f.KickoffAt
		}
	}
	return ShadowSlot{At: at.UTC(), Reason: ReasonDataChecked}, true
}
