package projection

import (
	"math"
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Expected values are worked out by hand from the rules in
// docs/adr/0014-projections.md; the arithmetic is in the comments.

func TestRecentGameweeks(t *testing.T) {
	done := func(finished ...int) func(int) bool {
		return func(gw int) bool { return slices.Contains(finished, gw) }
	}
	cases := []struct {
		name   string
		before int
		done   func(int) bool
		want   []int
	}{
		// Before GW 10 with 1-9 finished: the last six, 4..9.
		{"six before", 10, done(1, 2, 3, 4, 5, 6, 7, 8, 9), []int{4, 5, 6, 7, 8, 9}},
		// GW 9 live (not finished) and asked before 10: 3..8.
		{"skips unfinished", 10, done(1, 2, 3, 4, 5, 6, 7, 8), []int{3, 4, 5, 6, 7, 8}},
		// Early season: before GW 3, only 1 and 2.
		{"early", 3, done(1, 2), []int{1, 2}},
		{"none", 1, done(), nil},
	}
	for _, c := range cases {
		if got := RecentGameweeks(c.before, c.done); !slices.Equal(got, c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

// The hand-worked window: four clubs A, B, C, D and three finished
// matches. A and C play twice (a double), B and D once (a blank).
//
//	f1 A v B, f2 C v D, f3 A v C
//
// Points scored, by position, and so conceded by the other club:
//
//	MID: f1 A 6 (B concedes), B 2 (A); f2 C 2 (D), D 4+4 (C); f3 A 5 (C), C 1 (A)
//	     conceded: A 2+1 = 3 in 2 = 1.5 a match; B 6 in 1 = 6; C 8+5 = 13 in 2 = 6.5; D 2 in 1 = 2
//	     hardest first: A 1.5, D 2, B 6, C 6.5 -> A 1st, D 2nd, B 3rd, C 4th
//	     (by totals D's 2 would beat A's 3: per match is what counts)
//	FWD: f1 B 4 (A), A 0 (B); f3 C 2 (A); f2 C 3 (D)
//	     conceded: A 6 in 2 = 3; B 0; C 0; D 3 in 1 = 3
//	     B and C share 1st, A and D share 3rd
//	GKP: nothing scored: all share 1st. DEF: no lines: all share 1st.
//
// Ignored: a line for a fixture outside the window, and a line whose club
// does not play in its fixture.
func TestRankHandWorked(t *testing.T) {
	matches := []Match{{"f1", "A", "B"}, {"f2", "C", "D"}, {"f3", "A", "C"}}
	lines := []Line{
		{"f1", "A", scoring.MID, 6}, {"f1", "B", scoring.MID, 2},
		{"f2", "C", scoring.MID, 2}, {"f2", "D", scoring.MID, 4}, {"f2", "D", scoring.MID, 4},
		{"f3", "A", scoring.MID, 5}, {"f3", "C", scoring.MID, 1},
		{"f1", "B", scoring.FWD, 4}, {"f1", "A", scoring.FWD, 0}, {"f3", "C", scoring.FWD, 2}, {"f2", "C", scoring.FWD, 3},
		{"f9", "A", scoring.MID, 50}, // not in the window
		{"f1", "C", scoring.MID, 50}, // C does not play f1
	}
	d := Rank(matches, lines)
	want := map[scoring.Position]map[string]Ranking{
		scoring.MID: {
			"A": {"A", 1, 3, 2}, "D": {"D", 2, 2, 1}, "B": {"B", 3, 6, 1}, "C": {"C", 4, 13, 2},
		},
		scoring.FWD: {
			"B": {"B", 1, 0, 1}, "C": {"C", 1, 0, 2}, "A": {"A", 3, 6, 2}, "D": {"D", 3, 3, 1},
		},
		scoring.GKP: {"A": {"A", 1, 0, 2}, "B": {"B", 1, 0, 1}, "C": {"C", 1, 0, 2}, "D": {"D", 1, 0, 1}},
	}
	for pos, clubs := range want {
		for club, w := range clubs {
			if got := d[pos][club]; got != w {
				t.Errorf("%s %s: got %+v, want %+v", pos, club, got, w)
			}
		}
	}
	if got := d.Rank(scoring.MID, "E"); got != 0 {
		t.Errorf("a club with no match in the window: rank %d, want 0", got)
	}
	if len(d[scoring.DEF]) != 4 || d.Rank(scoring.DEF, "C") != 1 {
		t.Errorf("DEF with no lines: %+v, want all four 1st", d[scoring.DEF])
	}
}

func TestRankTwentyClubs(t *testing.T) {
	// Ten matches, club i (01..20) conceding i points to forwards in its
	// one match: 01 hardest (1st) ... 20 easiest (20th).
	var matches []Match
	var lines []Line
	club := func(i int) string { return string(rune('a'+i/10)) + string(rune('0'+i%10)) }
	for i := 1; i <= 20; i += 2 {
		f := "f" + club(i)
		matches = append(matches, Match{f, club(i), club(i + 1)})
		lines = append(lines, Line{f, club(i + 1), scoring.FWD, i}, Line{f, club(i), scoring.FWD, i + 1})
	}
	d := Rank(matches, lines)
	for i := 1; i <= 20; i++ {
		if got := d.Rank(scoring.FWD, club(i)); got != i {
			t.Errorf("club conceding %d: rank %d, want %d", i, got, i)
		}
	}
}

func TestFactor(t *testing.T) {
	cases := []struct {
		rank int
		want float64
	}{
		{1, 0.8},                  // hardest
		{20, 1.2},                 // easiest
		{0, 1},                    // unranked
		{10, 0.8 + 0.4*9.0/19.0},  // 0.98947...
		{11, 0.8 + 0.4*10.0/19.0}, // 1.01052...
		{25, 1.2},                 // clamped
	}
	for _, c := range cases {
		if got := Factor(c.rank); math.Abs(got-c.want) > 1e-12 {
			t.Errorf("Factor(%d) = %v, want %v", c.rank, got, c.want)
		}
	}
}

func TestExpected(t *testing.T) {
	cases := []struct {
		name  string
		form  float64
		ranks []int
		want  float64
	}{
		{"easiest", 6.0, []int{20}, 7.2},            // 6.0 x 1.2
		{"hardest", 6.0, []int{1}, 4.8},             // 6.0 x 0.8
		{"double", 6.0, []int{20, 1}, 12.0},         // 6.0 x (1.2 + 0.8)
		{"blank", 6.0, nil, 0},                      // no match
		{"10th", 4.5, []int{10}, 4.5},               // 4.5 x 0.98947 = 4.4526 -> 4.5
		{"11th", 5.0, []int{11}, 5.1},               // 5.0 x 1.01053 = 5.0526 -> 5.1
		{"rounds down", 5.9, []int{1}, 4.7},         // 5.9 x 0.8 = 4.72 -> 4.7
		{"half up", 4.5, []int{0}, 4.5},             // unranked factor 1
		{"half up 2", 2.5, []int{20, 20}, 6.0},      // 2.5 x 2.4 = 6.0
		{"negative form", -1.0, []int{20}, -1.2},    // -1.0 x 1.2
		{"tenths", 0.7, []int{1}, 0.6},              // 0.7 x 0.8 = 0.56 -> 0.6
		{"x.x5 binary", 4.35 / 1.2, []int{20}, 4.4}, // 4.35 exactly half: up
	}
	for _, c := range cases {
		if got := Expected(c.form, c.ranks); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("%s: Expected(%v, %v) = %v, want %v", c.name, c.form, c.ranks, got, c.want)
		}
	}
}

func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// Sampling tolerance: 10,000 plays give a standard error of at most
// sqrt(0.25/10000) = 0.005; 0.03 is six of them.
const tol = 0.03

func TestWinOdds(t *testing.T) {
	one := func(id string, s Starter) Team { return Team{ID: id, Starters: []Starter{s}} }
	cases := []struct {
		name       string
		teams      []Team
		home, away string
		want       Odds
		exact      bool
	}{
		{
			// Home's worst week (5) beats away's best (2).
			"dominant", []Team{one("h", Starter{Remaining: 1, History: []int{5, 6}}), one("a", Starter{Remaining: 1, History: []int{1, 2}})},
			"h", "a", Odds{Home: 1}, true,
		},
		{
			// Each side draws 2 or 4: (2,2) D, (2,4) A, (4,2) H, (4,4) D.
			"coin flips", []Team{one("h", Starter{Remaining: 1, History: []int{2, 4}}), one("a", Starter{Remaining: 1, History: []int{2, 4}})},
			"h", "a", Odds{Home: 0.25, Draw: 0.5, Away: 0.25}, false,
		},
		{
			// Live: home has played (7, nothing left); away draws 5 (loses) or 9 (wins).
			"live", []Team{one("h", Starter{Actual: 7}), one("a", Starter{Remaining: 1, History: []int{5, 9}})},
			"h", "a", Odds{Home: 0.5, Away: 0.5}, false,
		},
		{
			// A double: two draws of {0, 10}: 0, 10, 10, 20 vs a fixed 10 -> H 1/4, D 1/2, A 1/4.
			"double", []Team{one("h", Starter{Remaining: 2, History: []int{0, 10}}), one("a", Starter{Actual: 10})},
			"h", "a", Odds{Home: 0.25, Draw: 0.5, Away: 0.25}, false,
		},
		{
			// Every match kicked off, minute 5: each side adds 0 or
			// round(9 × 85/90 = 8.5) = 9. Home 1 or 10, away 0 or 9:
			// (1,0) H, (1,9) A, (10,0) H, (10,9) H -> H 3/4, A 1/4.
			"minute 5", []Team{one("h", Starter{Actual: 1, Live: []int{5}, History: []int{0, 9}}),
				one("a", Starter{Live: []int{5}, History: []int{0, 9}})},
			"h", "a", Odds{Home: 0.75, Away: 0.25}, false,
		},
		{
			// Half time: away has 2 and adds half of 4 or 12 (2 or 6):
			// 4 or 8 against home's finished 7 -> H 1/2, A 1/2.
			"half time", []Team{one("h", Starter{Actual: 7}), one("a", Starter{Actual: 2, Live: []int{45}, History: []int{4, 12}})},
			"h", "a", Odds{Home: 0.5, Away: 0.5}, false,
		},
		{
			// Minute 90 leaves nothing to add: 2 v 7.
			"minute 90", []Team{one("h", Starter{Actual: 7}), one("a", Starter{Actual: 2, Live: []int{90}, History: []int{4, 12}})},
			"h", "a", Odds{Home: 1}, true,
		},
		{
			// No history adds nothing: 3 v 3.
			"no history", []Team{one("h", Starter{Actual: 3, Remaining: 2}), one("a", Starter{Actual: 3})},
			"h", "a", Odds{Draw: 1}, true,
		},
		{
			// Average of 10, 4, 7 = floor(21/3) = 7: 10 beats it.
			"vs average", []Team{one("t1", Starter{Actual: 10}), one("t2", Starter{Actual: 4}), one("t3", Starter{Actual: 7})},
			"t1", "", Odds{Home: 1}, true,
		},
		{
			// 7 draws with the average 7.
			"draws average", []Team{one("t1", Starter{Actual: 10}), one("t2", Starter{Actual: 4}), one("t3", Starter{Actual: 7})},
			"t3", "", Odds{Draw: 1}, true,
		},
		{
			// The average at home (7) beats 4.
			"average at home", []Team{one("t1", Starter{Actual: 10}), one("t2", Starter{Actual: 4}), one("t3", Starter{Actual: 7})},
			"", "t2", Odds{Home: 1}, true,
		},
		{
			// Average of -3 and 0 = floor(-1.5) = -2; 0 beats it.
			"negative average floors", []Team{one("t1", Starter{Actual: -3}), one("t2", Starter{Actual: 0})},
			"t2", "", Odds{Home: 1}, true,
		},
	}
	for _, c := range cases {
		got := WinOdds(c.teams, c.home, c.away, Seed("mu_x", 5, 0), Samples)
		ok := got == c.want
		if !c.exact {
			ok = near(got.Home, c.want.Home, tol) && near(got.Draw, c.want.Draw, tol) && near(got.Away, c.want.Away, tol)
		}
		if !ok {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

// Remainder from the rule: pts × max(0, 90 − minute)/90, nearest point,
// halves away from zero.
func TestRemainder(t *testing.T) {
	for _, c := range []struct{ pts, minute, want int }{
		{10, 0, 10}, {6, 45, 3}, {3, 45, 2} /* 1.5 */, {-1, 45, -1}, /* -0.5 */
		{-1, 60, 0} /* -0.33 */, {2, 60, 1} /* 0.67 */, {9, 30, 6}, {1, 46, 0}, /* 0.49 */
		{10, 90, 0}, {10, 95, 0}, {0, 10, 0},
	} {
		if got := Remainder(c.pts, c.minute); got != c.want {
			t.Errorf("Remainder(%d, %d) = %d, want %d", c.pts, c.minute, got, c.want)
		}
	}
}

func TestWinOddsDeterministic(t *testing.T) {
	teams := []Team{
		{ID: "h", Starters: []Starter{{Remaining: 1, History: []int{1, 2, 3, 8, 12}}}},
		{ID: "a", Starters: []Starter{{Remaining: 1, History: []int{0, 4, 6, 9}}}},
	}
	a := WinOdds(teams, "h", "a", Seed("mu_1", 7, 0), 500)
	b := WinOdds(teams, "h", "a", Seed("mu_1", 7, 0), 500)
	if a != b {
		t.Errorf("same seed: %+v then %+v", a, b)
	}
	if Seed("mu_1", 7, 0) == Seed("mu_1", 7, 1) || Seed("mu_1", 7, 0) == Seed("mu_2", 7, 0) || Seed("mu_1", 7, 0) == Seed("mu_1", 8, 0) {
		t.Error("seeds of different (matchup, gameweek, bucket) collide")
	}
	if got := WinOdds(teams, "h", "a", 1, 0); got != (Odds{}) {
		t.Errorf("no plays: %+v", got)
	}
}

func TestFinal(t *testing.T) {
	if Final(50, 40) != (Odds{Home: 1}) || Final(40, 50) != (Odds{Away: 1}) || Final(45, 45) != (Odds{Draw: 1}) {
		t.Error("Final does not follow the result")
	}
}

// The hand-worked squad. Best XI: GKP 6; DEF 8, 7, 1; MID 10, 9, 3, 2
// (one of the 2s); FWD 12, 5, 4 -> 3-4-3:
// 6 + 16 + 24 + 21 = 67. 4-4-2 would swap FWD 4 for DEF 0 (63); 3-5-2
// swaps it for MID 2 (65).
func TestOptimalXIHandWorked(t *testing.T) {
	squad := []Scored{
		{"g1", scoring.GKP, 6}, {"g2", scoring.GKP, 2},
		{"d1", scoring.DEF, 8}, {"d2", scoring.DEF, 7}, {"d3", scoring.DEF, 1}, {"d4", scoring.DEF, 0}, {"d5", scoring.DEF, 0},
		{"m1", scoring.MID, 10}, {"m2", scoring.MID, 9}, {"m3", scoring.MID, 3}, {"m4", scoring.MID, 2}, {"m5", scoring.MID, 2},
		{"f1", scoring.FWD, 12}, {"f2", scoring.FWD, 5}, {"f3", scoring.FWD, 4},
	}
	xi, pts, ok := OptimalXI(squad, scoring.StandardLineup())
	if !ok || pts != 67 {
		t.Fatalf("got %d (ok %v), want 67", pts, ok)
	}
	want := []string{"g1", "d1", "d2", "d3", "m1", "m2", "m3", "m4", "f1", "f2", "f3"}
	if !slices.Equal(xi, want) {
		t.Errorf("xi %v, want %v", xi, want)
	}
}

func TestOptimalXINegativesStillFillTheMinimum(t *testing.T) {
	// Every forward scored -2: one must still start (1-3 FWD). 1 GKP 0,
	// 5 DEF 1 each, 5 MID 1 each would be 10 outfield; with the forward
	// the XI is GKP 0 + 5 DEF + 4 MID + FWD -2 = 0 + 5 + 4 - 2 = 7.
	squad := []Scored{{"g1", scoring.GKP, 0}, {"g2", scoring.GKP, 0}}
	for _, id := range []string{"d1", "d2", "d3", "d4", "d5"} {
		squad = append(squad, Scored{id, scoring.DEF, 1})
	}
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5"} {
		squad = append(squad, Scored{id, scoring.MID, 1})
	}
	for _, id := range []string{"f1", "f2", "f3"} {
		squad = append(squad, Scored{id, scoring.FWD, -2})
	}
	if _, pts, ok := OptimalXI(squad, scoring.StandardLineup()); !ok || pts != 7 {
		t.Errorf("got %d (ok %v), want 7", pts, ok)
	}
}

func TestOptimalXINoLegalXI(t *testing.T) {
	// No goalkeeper: no legal XI.
	squad := []Scored{{"d1", scoring.DEF, 5}}
	if _, _, ok := OptimalXI(squad, scoring.StandardLineup()); ok {
		t.Error("a squad without a goalkeeper has no legal XI")
	}
}

func TestAccuracy(t *testing.T) {
	// 55 of an optimal 67 = 0.82089...
	if a, ok := Accuracy(55, 67); !ok || math.Abs(a-55.0/67.0) > 1e-12 {
		t.Errorf("Accuracy(55, 67) = %v %v", a, ok)
	}
	if a, ok := Accuracy(67, 67); !ok || a != 1 {
		t.Errorf("Accuracy(67, 67) = %v %v, want 1", a, ok)
	}
	if _, ok := Accuracy(0, 0); ok {
		t.Error("an optimal of 0 has no accuracy")
	}
	if _, ok := Accuracy(-3, -1); ok {
		t.Error("a negative optimal has no accuracy")
	}
	// (1 + 0.5 + 0.75) / 3 = 0.75
	if s, ok := SeasonAccuracy([]float64{1, 0.5, 0.75}); !ok || s != 0.75 {
		t.Errorf("SeasonAccuracy = %v %v, want 0.75", s, ok)
	}
	if _, ok := SeasonAccuracy(nil); ok {
		t.Error("no gameweeks: no season accuracy")
	}
}
