package league

import (
	"fmt"
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"pgregory.net/rapid"
)

// Tests in this file close gaps found by mutation testing (docs/testing.md).
// Expected values are worked out by hand from the rules: RESEARCH.md §7
// "H2H: win 3, draw 1, loss 0. Ties are broken on points for. With an odd
// number of teams, an 'average' opponent is added."

// Three teams and the league average over six finished gameweeks, plus an
// unfinished gameweek 7 that must not count. Matchups are given newest
// first so the table must order them itself. Team D never plays.
//
//	gw  A v B     result      C v avg   result
//	1   50 v 40   A W, B L    30 v 45   C L
//	2   40 v 40   D, D        60 v 50   C W
//	3   20 v 70   A L, B W    45 v 45   C D
//	4   55 v 50   A W, B L    10 v 30   C L
//	5   61 v 60   A W, B L    70 v 20   C W
//	6   33 v 33   D, D        40 v 41   C L
//	7   0 v 100   (unfinished: ignored)
//
//	A: W D L W W D = 3W 2D 1L = 11 pts, PF 50+40+20+55+61+33 = 259, PA 293
//	B: L D W L L D = 1W 2D 3L =  5 pts, PF 293, PA 259
//	C: L W D L W L = 2W 1D 3L =  7 pts, PF 30+60+45+10+70+40 = 255,
//	   PA 45+50+45+30+20+41 = 231
//	D: no games, 0 pts
//
// Table: A 11, C 7, B 5, D 0. A's form, last five oldest first: D L W W D.
func TestStandingsByHand(t *testing.T) {
	l := &League{Teams: []Team{{ID: "A"}, {ID: "B"}, {ID: "C"}, {ID: "D"}}}
	w := &World{Gameweeks: map[int]stats.Gameweek{}}
	for gw := 1; gw <= 7; gw++ {
		w.Gameweeks[gw] = stats.Gameweek{Number: gw, Finished: gw <= 6}
	}
	ab := [][2]int{{50, 40}, {40, 40}, {20, 70}, {55, 50}, {61, 60}, {33, 33}, {0, 100}}
	cAvg := [][2]int{{30, 45}, {60, 50}, {45, 45}, {10, 30}, {70, 20}, {40, 41}, {0, 0}}
	g := &Game{}
	for gw := 7; gw >= 1; gw-- {
		home, away := "A", "B"
		hp, ap := ab[gw-1][0], ab[gw-1][1]
		if gw == 3 || gw == 6 { // B at home
			home, away, hp, ap = "B", "A", ap, hp
		}
		g.Matchups = append(g.Matchups,
			Matchup{Gameweek: gw, HomeTeamID: home, AwayTeamID: away, HomePoints: hp, AwayPoints: ap},
			Matchup{Gameweek: gw, HomeTeamID: "C", AwayTeamID: "", HomePoints: cAvg[gw-1][0], AwayPoints: cAvg[gw-1][1]},
		)
	}
	rows := Standings(l, g, w)
	type row struct {
		id                 string
		rank, w, d, l, pld int
		pf, pa, pts        int
	}
	want := []row{
		{"A", 1, 3, 2, 1, 6, 259, 293, 11},
		{"C", 2, 2, 1, 3, 6, 255, 231, 7},
		{"B", 3, 1, 2, 3, 6, 293, 259, 5},
		{"D", 4, 0, 0, 0, 0, 0, 0, 0},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d rows, want %d", len(rows), len(want))
	}
	for i, wr := range want {
		r := rows[i]
		got := row{r.TeamID, r.Rank, r.Wins, r.Draws, r.Losses, r.Played, r.PointsFor, r.PointsAgainst, r.LeaguePoints()}
		if got != wr {
			t.Errorf("row %d = %+v, want %+v", i, got, wr)
		}
	}
	D, L, W := scoring.Draw, scoring.Loss, scoring.Win
	if want := []scoring.Result{D, L, W, W, D}; !slices.Equal(rows[0].Form, want) {
		t.Errorf("A form = %v, want %v", rows[0].Form, want)
	}
	if want := []scoring.Result{W, D, L, W, L}; !slices.Equal(rows[1].Form, want) {
		t.Errorf("C form = %v, want %v", rows[1].Form, want)
	}
	if rows[3].Form == nil || len(rows[3].Form) != 0 {
		t.Errorf("D form = %#v, want empty (not null)", rows[3].Form)
	}
}

// RoundRobin for any team count and gameweek range (ADR 0008; the code's
// stated contract): every real team plays exactly once per gameweek, the
// average side (odd counts) is always away, every pair meets exactly once
// in each full cycle of n−1 gameweeks (n made even), and a pairing's home
// and away swap from one cycle to the next.
func TestRoundRobinProperty(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(2, 20).Draw(rt, "teams")
		start := rapid.IntRange(1, 38).Draw(rt, "start")
		end := rapid.IntRange(start, 38).Draw(rt, "end")
		var teams []string
		for i := range n {
			teams = append(teams, fmt.Sprintf("t%02d", i))
		}
		ms := RoundRobin(teams, start, end)
		sides := n + n%2
		cycleLen := sides - 1
		byGW := map[int][]Matchup{}
		for _, m := range ms {
			if m.Gameweek < start || m.Gameweek > end {
				rt.Fatalf("matchup in gameweek %d outside %d..%d", m.Gameweek, start, end)
			}
			if m.HomeTeamID == "" {
				rt.Fatalf("average side at home: %+v", m)
			}
			byGW[m.Gameweek] = append(byGW[m.Gameweek], m)
		}
		// home[cycle][pair] = home team.
		home := map[int]map[[2]string]string{}
		for gw := start; gw <= end; gw++ {
			seen := map[string]int{}
			for _, m := range byGW[gw] {
				seen[m.HomeTeamID]++
				seen[m.AwayTeamID]++
				c := (gw - start) / cycleLen
				if home[c] == nil {
					home[c] = map[[2]string]string{}
				}
				pair := [2]string{min(m.HomeTeamID, m.AwayTeamID), max(m.HomeTeamID, m.AwayTeamID)}
				if _, dup := home[c][pair]; dup {
					rt.Fatalf("cycle %d: %v meet twice", c, pair)
				}
				home[c][pair] = m.HomeTeamID
			}
			for _, id := range teams {
				if seen[id] != 1 {
					rt.Fatalf("gameweek %d: %s plays %d times", gw, id, seen[id])
				}
			}
			if seen[""] != n%2 {
				rt.Fatalf("gameweek %d: %d average sides, want %d", gw, seen[""], n%2)
			}
		}
		for c := 0; (c+1)*cycleLen <= end-start+1; c++ {
			if got, want := len(home[c]), sides*(sides-1)/2; got != want {
				rt.Fatalf("full cycle %d has %d pairings, want %d", c, got, want)
			}
			if next, ok := home[c+1]; ok {
				for pair, h := range home[c] {
					if h2, ok := next[pair]; ok && pair[0] != "" && h2 == h {
						rt.Fatalf("%v: %s at home in cycles %d and %d", pair, h, c, c+1)
					}
				}
			}
		}
	})
}

// Only teams with players score (and count towards the average).
func TestTeamsWithRosters(t *testing.T) {
	l := &League{Teams: []Team{{ID: "A"}, {ID: "B"}, {ID: "C"}}}
	g := &Game{Rosters: []RosterSlot{
		{TeamID: "A", PlayerID: "p1", FromGW: 1},
		{TeamID: "C", PlayerID: "p2", FromGW: 9}, // joins later
	}}
	if got := TeamsWithRosters(l, g, 8); !slices.Equal(got, []string{"A"}) {
		t.Fatalf("gameweek 8 = %v, want [A]", got)
	}
	if got := TeamsWithRosters(l, g, 9); !slices.Equal(got, []string{"A", "C"}) {
		t.Fatalf("gameweek 9 = %v, want [A C]", got)
	}
}
