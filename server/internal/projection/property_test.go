package projection

import (
	"math"
	"slices"
	"strconv"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"pgregory.net/rapid"
)

// Property tests. The optimal-XI oracle is written from the formation rule
// (1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD, 11 players) and shares no code with
// the package: it tries every 11 of the 15.

func legalXI(c map[scoring.Position]int) bool {
	return c[scoring.GKP] == 1 && c[scoring.DEF] >= 3 && c[scoring.DEF] <= 5 && c[scoring.MID] >= 2 && c[scoring.MID] <= 5 &&
		c[scoring.FWD] >= 1 && c[scoring.FWD] <= 3 && c[scoring.GKP]+c[scoring.DEF]+c[scoring.MID]+c[scoring.FWD] == 11
}

func drawSquad(t *rapid.T) []Scored {
	var squad []Scored
	for _, pc := range []struct {
		pos scoring.Position
		n   int
	}{{scoring.GKP, 2}, {scoring.DEF, 5}, {scoring.MID, 5}, {scoring.FWD, 3}} {
		for i := range pc.n {
			id := string(pc.pos) + strconv.Itoa(i)
			squad = append(squad, Scored{id, pc.pos, rapid.IntRange(-4, 24).Draw(t, id)})
		}
	}
	return squad
}

// bruteBest is the best total of any legal XI, trying all 2^15 subsets.
func bruteBest(squad []Scored) int {
	best := math.MinInt
	for mask := range 1 << len(squad) {
		c := map[scoring.Position]int{}
		sum := 0
		for i, p := range squad {
			if mask&(1<<i) != 0 {
				c[p.Position]++
				sum += p.Points
			}
		}
		if legalXI(c) && sum > best {
			best = sum
		}
	}
	return best
}

func TestOptimalXIIsLegalAndBest(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		squad := drawSquad(t)
		xi, pts, ok := OptimalXI(squad, scoring.StandardLineup())
		if !ok {
			t.Fatal("a 2/5/5/3 squad always has a legal XI")
		}
		byID := map[string]Scored{}
		for _, p := range squad {
			byID[p.PlayerID] = p
		}
		c := map[scoring.Position]int{}
		seen := map[string]bool{}
		sum := 0
		for _, id := range xi {
			p, in := byID[id]
			if !in || seen[id] {
				t.Fatalf("xi %v: %s not in the squad or twice", xi, id)
			}
			seen[id] = true
			c[p.Position]++
			sum += p.Points
		}
		if !legalXI(c) {
			t.Fatalf("xi %v is not a legal formation: %v", xi, c)
		}
		if sum != pts {
			t.Fatalf("xi sums to %d, reported %d", sum, pts)
		}
		// >= every legal XI: equal to the best of all of them.
		if want := bruteBest(squad); pts != want {
			t.Fatalf("optimal %d, best legal XI %d", pts, want)
		}
	})
}

func drawTeam(t *rapid.T, id string) Team {
	team := Team{ID: id}
	for i := range rapid.IntRange(0, 11).Draw(t, id+"-n") {
		label := id + "-" + strconv.Itoa(i)
		team.Starters = append(team.Starters, Starter{
			Actual:    rapid.IntRange(-3, 20).Draw(t, label+"-actual"),
			Remaining: rapid.IntRange(0, 2).Draw(t, label+"-remaining"),
			Live:      rapid.SliceOfN(rapid.IntRange(0, 100), 0, 2).Draw(t, label+"-live"),
			History:   rapid.SliceOfN(rapid.IntRange(-3, 20), 0, 6).Draw(t, label+"-history"),
		})
	}
	return team
}

func TestWinOddsProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		var teams []Team
		for i := range rapid.IntRange(2, 6).Draw(t, "teams") {
			teams = append(teams, drawTeam(t, "tm"+strconv.Itoa(i)))
		}
		home := teams[0].ID
		away := teams[1].ID
		if rapid.Bool().Draw(t, "average") {
			away = ""
		}
		seed := rapid.Uint64().Draw(t, "seed")
		n := rapid.IntRange(1, 300).Draw(t, "n")
		o := WinOdds(teams, home, away, seed, n)
		for _, p := range []float64{o.Home, o.Draw, o.Away} {
			if p < 0 || p > 1 {
				t.Fatalf("odds %+v out of [0, 1]", o)
			}
		}
		if math.Abs(o.Home+o.Draw+o.Away-1) > 1e-9 {
			t.Fatalf("odds %+v do not sum to 1", o)
		}
		// Symmetric: swapping the sides swaps the odds exactly.
		s := WinOdds(teams, away, home, seed, n)
		if s.Home != o.Away || s.Away != o.Home || s.Draw != o.Draw {
			t.Fatalf("home/away %+v, swapped %+v", o, s)
		}
		// A team against itself always draws.
		if self := WinOdds(teams, home, home, seed, n); self != (Odds{Draw: 1}) {
			t.Fatalf("team against itself: %+v", self)
		}
		// A match at minute 0 is the same draw as one not kicked off.
		kicked := make([]Team, len(teams))
		for i, tm := range teams {
			kicked[i] = Team{ID: tm.ID}
			for _, st := range tm.Starters {
				st.Live = append(make([]int, st.Remaining), st.Live...)
				st.Remaining = 0
				kicked[i].Starters = append(kicked[i].Starters, st)
			}
		}
		if k := WinOdds(kicked, home, away, seed, n); k != o {
			t.Fatalf("pre-kickoff %+v, at minute 0 %+v", o, k)
		}
		// A side whose worst case beats the other's best wins for certain.
		if away != "" {
			hLo, hHi := bounds(teams[0])
			aLo, aHi := bounds(teams[1])
			if hLo > aHi && o != (Odds{Home: 1}) || aLo > hHi && o != (Odds{Away: 1}) ||
				hLo == hHi && aLo == aHi && o != oracleFinal(hLo, aLo) {
				t.Fatalf("home %d..%d, away %d..%d: %+v", hLo, hHi, aLo, aHi, o)
			}
		}
	})
}

// bounds is a team's lowest and highest possible score, from the rule:
// Actual, plus History's worst/best per match to come, plus per match in
// progress that draw × (90 − minute)/90 rounded half away from zero.
func bounds(tm Team) (lo, hi int) {
	for _, st := range tm.Starters {
		lo += st.Actual
		hi += st.Actual
		if len(st.History) == 0 {
			continue
		}
		mn, mx := slices.Min(st.History), slices.Max(st.History)
		lo += st.Remaining * mn
		hi += st.Remaining * mx
		for _, m := range st.Live {
			share := float64(max(0, 90-m)) / 90
			lo += int(math.Round(float64(mn) * share))
			hi += int(math.Round(float64(mx) * share))
		}
	}
	return lo, hi
}

func oracleFinal(h, a int) Odds {
	if h > a {
		return Odds{Home: 1}
	}
	if h < a {
		return Odds{Away: 1}
	}
	return Odds{Draw: 1}
}

func TestExpectedProperties(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		form := float64(rapid.IntRange(0, 200).Draw(t, "form10")) / 10
		rank := rapid.IntRange(1, 20).Draw(t, "rank")
		e := Expected(form, []int{rank})
		// Within 0.8x..1.2x of form, to the rounding.
		if e < 0.8*form-0.05-1e-9 || e > 1.2*form+0.05+1e-9 {
			t.Fatalf("Expected(%v, %d) = %v outside 0.8..1.2 x form", form, rank, e)
		}
		// An easier opponent never lowers it.
		if rank < 20 && Expected(form, []int{rank + 1}) < e {
			t.Fatalf("rank %d -> %d lowered Expected", rank, rank+1)
		}
		// One decimal.
		if math.Abs(e*10-math.Round(e*10)) > 1e-6 {
			t.Fatalf("Expected %v has more than one decimal", e)
		}
	})
}
