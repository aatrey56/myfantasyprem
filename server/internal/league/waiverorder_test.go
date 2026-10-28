package league

import (
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Waiver order (ADR 0008 rule 12, amended 2026-10-15), worked by hand.
//
// Teams joined A, B, C, D (in that order). The draft's first round went
// C, A, D, B, so before any standings exist the order is its reverse:
// B, D, A, C.
//
//	gw  match     result        gw  match     result
//	1   A 50-40 B A W, B L      2   A 30-40 C C W, A L
//	1   C 45-45 D draw          2   B 60-50 D B W, D L
//	3   A 0-90 B  (not finished: never counts)
//
//	after gw1: A 3 pts PF 50, C 1 PF 45, D 1 PF 45, B 0 PF 40
//	  C and D tie on league points and points for: the earlier-joined
//	  team (C) takes the earlier pick. Worst first: B, C, D, A.
//	after gw2: C 4 pts PF 85, B 3 PF 100, A 3 PF 80, D 1 PF 95
//	  A and B tie on league points: fewer points for picks first.
//	  Worst first: D, A, B, C.
func orderState() (*State, *World) {
	st := &State{
		League: &League{ID: "lg_x", Status: StatusInSeason, Settings: Settings{WaiverOrder: WaiverReverseStandings},
			Teams: []Team{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}}},
		Game:     &Game{},
		Draft:    &Draft{Status: DraftComplete, Order: []string{"C", "A", "D", "B"}},
		Priority: []string{"B", "D", "A", "C"},
		Runs:     map[RunKey]time.Time{},
	}
	w := &World{Gameweeks: map[int]stats.Gameweek{}}
	for gw := 1; gw <= 38; gw++ {
		w.Gameweeks[gw] = stats.Gameweek{Number: gw}
	}
	return st, w
}

func finish(w *World, gws ...int) {
	for _, gw := range gws {
		g := w.Gameweeks[gw]
		g.Finished = true
		w.Gameweeks[gw] = g
	}
}

func play(st *State) {
	st.Game.Matchups = []Matchup{
		{Gameweek: 3, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 0, AwayPoints: 90},
		{Gameweek: 2, HomeTeamID: "A", AwayTeamID: "C", HomePoints: 30, AwayPoints: 40},
		{Gameweek: 2, HomeTeamID: "B", AwayTeamID: "D", HomePoints: 60, AwayPoints: 50},
		{Gameweek: 1, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 50, AwayPoints: 40},
		{Gameweek: 1, HomeTeamID: "C", AwayTeamID: "D", HomePoints: 45, AwayPoints: 45},
	}
}

func TestReverseStandingsByHand(t *testing.T) {
	st, w := orderState()
	play(st)
	finish(w, 1, 2)
	cases := []struct {
		gw   int
		want []string
	}{
		{2, []string{"B", "C", "D", "A"}}, // standings after gw1, C before D on join order
		{3, []string{"D", "A", "B", "C"}}, // after gw2; A before B on points for
		{4, []string{"D", "A", "B", "C"}}, // gw3 is not finished: still after gw2
	}
	for _, c := range cases {
		if got := WaiverOrder(st, w, c.gw); !slices.Equal(got, c.want) {
			t.Errorf("gw%d run: order %v, want %v", c.gw, got, c.want)
		}
	}
	// The rolling order a league kept is ignored.
	st.Priority = []string{"A", "B", "C", "D"}
	if got := WaiverOrder(st, w, 3); !slices.Equal(got, []string{"D", "A", "B", "C"}) {
		t.Errorf("order %v follows the stored priority", got)
	}
}

// The full tie is not decided like the standings table (which ranks C
// above D on team id, so its reverse would put D first) nor by the draft
// (D drafted after C, so its reverse would put D first too).
func TestReverseStandingsFullTieOnJoinOrder(t *testing.T) {
	st, w := orderState()
	play(st)
	finish(w, 1)
	st.League.Teams = []Team{{ID: "A"}, {ID: "B"}, {ID: "D"}, {ID: "C"}} // D joined before C
	if got := WaiverOrder(st, w, 2); !slices.Equal(got, []string{"B", "D", "C", "A"}) {
		t.Errorf("order %v, want B, D, C, A", got)
	}
}

// Before the league's first gameweek is finished: the reverse of the
// draft's first round, whatever has been played so far.
func TestReverseStandingsBeforeAnyResult(t *testing.T) {
	st, w := orderState()
	want := []string{"B", "D", "A", "C"}
	if got := WaiverOrder(st, w, 1); !slices.Equal(got, want) {
		t.Errorf("gw1 run: order %v, want %v", got, want)
	}
	play(st) // gw1 scores exist but gw1 is not finished
	if got := WaiverOrder(st, w, 2); !slices.Equal(got, want) {
		t.Errorf("gw2 run before gw1 finished: order %v, want %v", got, want)
	}
	// A team missing from the draft order (none in practice) goes last,
	// in join order.
	st.League.Teams = append(st.League.Teams, Team{ID: "E"})
	if got := WaiverOrder(st, w, 1); !slices.Equal(got, []string{"B", "D", "A", "C", "E"}) {
		t.Errorf("with E: order %v", got)
	}
}

// A rolling league starts each run from its stored order; leagues saved
// before the setting existed have none and are rolling.
func TestRollingOrderIsTheStoredOne(t *testing.T) {
	st, w := orderState()
	play(st)
	finish(w, 1, 2)
	st.League.Settings.WaiverOrder = ""
	if st.League.Settings.WaiverRule() != WaiverRolling {
		t.Fatalf("rule %q, want rolling", st.League.Settings.WaiverRule())
	}
	st.Priority = []string{"C", "A"}
	if got := WaiverOrder(st, w, 3); !slices.Equal(got, []string{"C", "A", "B", "D"}) {
		t.Errorf("order %v, want C, A, then the rest in join order", got)
	}
}

func TestDefaultSettingsAreReverseStandings(t *testing.T) {
	if got := DefaultSettings(8).WaiverOrder; got != WaiverReverseStandings {
		t.Errorf("default waiver order %q", got)
	}
}
