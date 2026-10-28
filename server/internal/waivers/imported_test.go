package waivers

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// An imported league follows FPL's calendar: gameweek 8's waivers run
// Fri 16 Oct 10:00 UTC (FPL's waivers_time) and its deadline is Sat 17 Oct
// 10:00 UTC. Between the two every unowned player is a free agent, even if
// our jobs never stamped the run (server down at 10:00, a re-sync after).
func TestImportedLeagueFollowsFPLWaiverTime(t *testing.T) {
	utc := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	w := &league.World{Gameweeks: map[int]stats.Gameweek{
		7: {Number: 7, DeadlineAt: utc(3, 10), WaiverDeadlineAt: utc(2, 10), Finished: true},
		8: {Number: 8, DeadlineAt: utc(17, 10), WaiverDeadlineAt: utc(16, 10)},
		9: {Number: 9, DeadlineAt: utc(24, 10), WaiverDeadlineAt: utc(23, 10)},
	}}
	state := func(source string) *league.State {
		return &league.State{League: &league.League{Source: source, Status: league.StatusInSeason},
			Game: &league.Game{}, Runs: map[league.RunKey]time.Time{}}
	}
	status := func(st *league.State, now time.Time) (string, *time.Time) {
		s, clears, _ := PlayerStatus(st, w, CycleAt(st, w, now), map[string]string{}, "plr_x")
		return s, clears
	}

	fpl := state(league.SourceFPL)
	// Thu 15 Oct 12:00: before FPL's run, on waivers until Fri 10:00.
	if s, clears := status(fpl, utc(15, 12)); s != OnWaivers || !clears.Equal(utc(16, 10)) {
		t.Fatalf("Thu: %s %v, want waivers until Fri 10:00", s, clears)
	}
	// Sat 17 Oct 08:00, no run marker: FPL ran Friday, so a free agent.
	c := CycleAt(fpl, w, utc(17, 8))
	if !c.FreeAgency || c.Upcoming != 8 || c.RunGW != 9 || !c.RunAt.Equal(utc(23, 10)) {
		t.Fatalf("Sat cycle = %+v, want free agency for 8, next run GW9 Fri 23 Oct", c)
	}
	if s, _ := status(fpl, utc(17, 8)); s != FreeAgent {
		t.Fatalf("Sat: %s, want free_agent", s)
	}
	// Sat 17 Oct 10:00 exactly: gameweek 8 is locked until it is finished;
	// free agency reopens after gameweek 9's run, Fri 23 Oct 10:00.
	if s, clears := status(fpl, utc(17, 10)); s != Locked || !clears.Equal(utc(23, 10)) {
		t.Fatalf("after the deadline: %s %v, want locked until Fri 23 Oct", s, clears)
	}
	// A claim made here still waits for our run: no free agency until it
	// is decided.
	fpl.Claims = []league.Claim{{ID: "clm_1", Gameweek: 8, Status: league.ClaimPending}}
	if c := CycleAt(fpl, w, utc(17, 8)); c.FreeAgency {
		t.Fatalf("pending claim: %+v, want the run still due", c)
	}

	// A native league keeps its own run: waivers until our jobs stamp it.
	native := state(league.SourceNative)
	if c := CycleAt(native, w, utc(17, 8)); c.FreeAgency {
		t.Fatalf("native without a run: %+v", c)
	}
	native.Runs[league.RunKey{Kind: league.RunWaivers, Gameweek: 8}] = utc(16, 10)
	if s, _ := status(native, utc(17, 8)); s != FreeAgent {
		t.Fatalf("native after its run: %s", s)
	}
}
