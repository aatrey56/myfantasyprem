package waivers

import (
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Reverse standings (ADR 0008 rule 12 as amended 2026-10-15), worked by
// hand. Teams A, B, C joined in that order.
//
//	gw1 (finished): A 60-50 B; C 40 v the average 45
//	after gw1: A 3 pts PF 60, B 0 PF 50, C 0 PF 40 -> gw2 run order C, B, A
//
// Claims for the gw2 run:
//
//	C: 1. p1 for c1   2. p3 for c2
//	B: 1. p1 for b1   2. p2 for b1
//	A: 1. p3 for a1
//
//	C's p1 wins; C goes to the back for the rest of the run: B, A, C
//	B's p1 is gone (lost); B's p2 wins:                      A, C, B
//	A's p3 wins (before C's second claim, though C picked first)
//	C's p3 is gone (lost)
//
// The same claims under rolling priority A, B, C go another way: A takes
// p3, B takes p1 and C gets nothing.
func reverseRun(order string) (*league.State, *league.World) {
	st := pureState(map[string][]string{"A": {"a1"}, "B": {"b1"}, "C": {"c1", "c2"}})
	st.League.Settings.WaiverOrder = order
	st.Game.Matchups = []league.Matchup{
		{Gameweek: 1, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 60, AwayPoints: 50},
		{Gameweek: 1, HomeTeamID: "C", HomePoints: 40, AwayPoints: 45},
		{Gameweek: 2, HomeTeamID: "B", AwayTeamID: "A", HomePoints: 70, AwayPoints: 30},
		{Gameweek: 2, HomeTeamID: "C", HomePoints: 50, AwayPoints: 20},
	}
	st.Claims = []league.Claim{
		claim("c1", "C", "p1", "c1", 1, 2), claim("c2", "C", "p3", "c2", 2, 2),
		claim("b1", "B", "p1", "b1", 1, 2), claim("b2", "B", "p2", "b1", 2, 2),
		claim("a1", "A", "p3", "a1", 1, 2),
	}
	w := squadWorld(st, "p1", "p2", "p3")
	w.Gameweeks = map[int]stats.Gameweek{1: {Number: 1, Finished: true}, 2: {Number: 2}, 3: {Number: 3}}
	return st, w
}

func TestProcessReverseStandingsWinnerToTheBackForTheRun(t *testing.T) {
	st, w := reverseRun(league.WaiverReverseStandings)
	if got := league.WaiverOrder(st, w, 2); !slices.Equal(got, []string{"C", "B", "A"}) {
		t.Fatalf("gw2 order %v, want C, B, A", got)
	}
	Process(st, w, 2, time.Unix(0, 0))
	want := map[string]string{"c1": league.ClaimWon, "b1": league.ClaimLost, "b2": league.ClaimWon,
		"a1": league.ClaimWon, "c2": league.ClaimLost}
	for _, c := range st.Claims {
		if c.Status != want[c.ID] {
			t.Errorf("claim %s = %s, want %s", c.ID, c.Status, want[c.ID])
		}
	}
	// The feed records the processing order and each claim's place in it.
	var seq []string
	for _, x := range st.NewTxns {
		seq = append(seq, x.TeamID+":"+x.PlayerIn)
	}
	if want := []string{"C:p1", "B:p1", "B:p2", "A:p3", "C:p3"}; !slices.Equal(seq, want) {
		t.Errorf("processing order %v, want %v", seq, want)
	}
	own := st.Game.OwnerAt(2)
	if own["p1"] != "C" || own["p2"] != "B" || own["p3"] != "A" || own["c2"] != "C" {
		t.Errorf("owners = %v", own)
	}

	// The next run starts from the standings again, not from where this
	// run left the order (C, B, A). After gw2: A 3 PF 90, B 3 PF 120,
	// C 3 PF 90; A and C tie on both, A joined first: A, C, B.
	if !slices.Equal(st.Priority, []string{"C", "B", "A"}) {
		t.Errorf("stored order %v", st.Priority)
	}
	g := w.Gameweeks[2]
	g.Finished = true
	w.Gameweeks[2] = g
	if got := league.WaiverOrder(st, w, 3); !slices.Equal(got, []string{"A", "C", "B"}) {
		t.Errorf("gw3 order %v, want A, C, B", got)
	}
}

func TestProcessRollingIgnoresStandings(t *testing.T) {
	st, w := reverseRun(league.WaiverRolling)
	Process(st, w, 2, time.Unix(0, 0))
	want := map[string]string{"a1": league.ClaimWon, "b1": league.ClaimWon, "b2": league.ClaimLost,
		"c1": league.ClaimLost, "c2": league.ClaimLost}
	for _, c := range st.Claims {
		if c.Status != want[c.ID] {
			t.Errorf("claim %s = %s, want %s", c.ID, c.Status, want[c.ID])
		}
	}
	// A then B won and moved to the back; the next run starts from C.
	if !slices.Equal(st.Priority, []string{"C", "A", "B"}) {
		t.Errorf("priority %v, want C, A, B", st.Priority)
	}
}
