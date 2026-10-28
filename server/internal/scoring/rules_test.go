package scoring

import (
	"reflect"
	"testing"
)

// Tests in this file close gaps found by mutation testing (docs/testing.md).
// Every expected value is derived by hand from the rules, not from the code:
// docs/research/RESEARCH.md §7 and ADR 0008 "Scoring and rules".

// ADR 0008 rule 10: each bench player can come on once. Two forwards miss
// the gameweek and only one bench forward played. f3 replaces f1 (slot 10);
// f2 (slot 11) then has nobody left: g2 is a keeper, d5 and m5 did not play.
func TestAutoSubsBenchPlayerComesOnOnce(t *testing.T) {
	xi := xi442()
	xi[9] = p("f1", FWD, 0, true, 0)
	xi[10] = p("f2", FWD, 0, true, 0)
	b := bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 0, true, 0), p("m5", MID, 0, true, 0), p("f3", FWD, 90, true, 5))
	got := pairs(StandardLineup().AutoSubs(xi, b, true))
	if want := []subPair{{10, 15}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("subs = %v, want %v (f3 must not replace both forwards)", got, want)
	}
}

// A request with no bench at all is reported, not a crash: 11 starters and
// 0 bench is "wrong_bench_count" (15 − 11 = 4 expected) plus the 4 missing
// roster players, and there is no bench slot 12 to check for a keeper.
func TestValidateLineupEmptyBench(t *testing.T) {
	xi := []string{"g1", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "f1", "f2"}
	got := codes(StandardLineup().ValidateLineup(squad(), xi, nil))
	want := []string{"wrong_bench_count", "player_missing", "player_missing", "player_missing", "player_missing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("violations = %v, want %v", got, want)
	}
}

// AveragePoints is the floor of the mean (ADR 0008: league-average
// opponent). Hand-computed:
//
//	[-2, -2]    sum -4, mean -2.0  -> -2 (exact: no rounding down)
//	[-1, -2]    sum -3, mean -1.5  -> -2
//	[-4, 0, 1]  sum -3, mean -1.0  -> -1
//	[7, 8]      sum 15, mean  7.5  ->  7
func TestAveragePointsFloor(t *testing.T) {
	for _, tt := range []struct {
		in   []int
		want int
	}{
		{[]int{-2, -2}, -2},
		{[]int{-1, -2}, -2},
		{[]int{-4, 0, 1}, -1},
		{[]int{7, 8}, 7},
	} {
		if got := AveragePoints(tt.in); got != tt.want {
			t.Errorf("AveragePoints(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

// RESEARCH.md §7 H2H: win 3, draw 1, loss 0.
func TestStandingLeaguePoints(t *testing.T) {
	for _, tt := range []struct {
		s    Standing
		want int
	}{
		{Standing{Wins: 0, Draws: 1, Losses: 4}, 1},
		{Standing{Wins: 2, Draws: 1, Losses: 1}, 7}, // 2×3 + 1
		{Standing{Wins: 5, Draws: 0, Losses: 0}, 15},
		{Standing{Wins: 0, Draws: 0, Losses: 9}, 0},
	} {
		if got := tt.s.LeaguePoints(); got != tt.want {
			t.Errorf("%+v: %d, want %d", tt.s, got, tt.want)
		}
	}
}

// H2H at the one-point boundary, both sides of it.
func TestH2HBoundaries(t *testing.T) {
	for _, tt := range []struct {
		mine, theirs int
		want         Result
	}{
		{51, 50, Win}, {50, 51, Loss}, {50, 50, Draw},
		{0, -1, Win}, {-1, 0, Loss}, {-3, -3, Draw},
	} {
		if got := H2H(tt.mine, tt.theirs); got != tt.want {
			t.Errorf("H2H(%d, %d) = %s, want %s", tt.mine, tt.theirs, got, tt.want)
		}
	}
}

// RESEARCH.md §7: "Ties are broken on points for". Equal league points
// (a: 3W = 9; b: 2W 3D = 9) so points for decides: b 101 > a 100.
func TestSortStandingsTieOnPointsFor(t *testing.T) {
	rows := []Standing{
		{TeamID: "a", Wins: 3, PointsFor: 100},
		{TeamID: "b", Wins: 2, Draws: 3, PointsFor: 101},
	}
	SortStandings(rows)
	if rows[0].TeamID != "b" {
		t.Fatalf("order = %s, %s; want b first on points for", rows[0].TeamID, rows[1].TeamID)
	}
}

// RESEARCH.md §7 Scoring table, one boundary per row. Each want is the
// table arithmetic written out.
func TestPointsBoundaries(t *testing.T) {
	r := StandardRules()
	for _, tt := range []struct {
		name string
		pos  Position
		s    FixtureStats
		want int
	}{
		{"1 minute is an appearance", MID, FixtureStats{Minutes: 1}, 1},
		{"60 minutes is the full appearance", MID, FixtureStats{Minutes: 60}, 2},
		{"clean sheet at exactly 60 minutes", GKP, FixtureStats{Minutes: 60, CleanSheet: true}, 2 + 4},
		{"clean sheet at 59 minutes does not count", GKP, FixtureStats{Minutes: 59, CleanSheet: true}, 1},
		{"3 conceded is one deduction", GKP, FixtureStats{Minutes: 90, GoalsConceded: 3}, 2 - 1},
		{"4 conceded is two deductions", DEF, FixtureStats{Minutes: 90, GoalsConceded: 4}, 2 - 2},
		{"3 saves is a point", GKP, FixtureStats{Minutes: 90, Saves: 3}, 2 + 1},
		{"6 saves is two points", GKP, FixtureStats{Minutes: 90, Saves: 6}, 2 + 2},
		{"two yellows without a red still cost 1", MID, FixtureStats{Minutes: 90, YellowCards: 2}, 2 - 1},
		{"DEF 9 CBIT is not enough", DEF, FixtureStats{Minutes: 90, ClearancesBlocksInterceptions: 9}, 2},
		{"MID 12 CBIRT from tackles alone", MID, FixtureStats{Minutes: 90, Tackles: 12}, 2 + 2},
		{"FWD 11 CBIRT is not enough", FWD, FixtureStats{Minutes: 90, Tackles: 5, Recoveries: 6}, 2},
		{"two penalties saved", GKP, FixtureStats{Minutes: 90, PenaltiesSaved: 2}, 2 + 10},
		{"two own goals", DEF, FixtureStats{Minutes: 90, OwnGoals: 2}, 2 - 4},
		{"negative minutes score nothing", DEF, FixtureStats{Minutes: -1, Goals: 1}, 0},
	} {
		if got, _ := r.Points(tt.pos, tt.s); got != tt.want {
			t.Errorf("%s: %d, want %d", tt.name, got, tt.want)
		}
	}
}
