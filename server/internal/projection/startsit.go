package projection

import (
	"cmp"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Scored is a squad player with his actual points in a gameweek.
type Scored struct {
	PlayerID string
	Position scoring.Position
	Points   int
}

// OptimalXI is the best legal starting XI from a squad by actual points:
// rules.Starters players within rules.MinStarters and MaxStarters per
// position (1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD in the standard rules). It
// tries every legal formation with each position's highest scorers. ok is
// false when the squad cannot field a legal XI.
func OptimalXI(squad []Scored, rules scoring.LineupRules) (xi []string, points int, ok bool) {
	byPos := map[scoring.Position][]Scored{}
	for _, p := range squad {
		byPos[p.Position] = append(byPos[p.Position], p)
	}
	positions := []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD}
	for _, pos := range positions {
		slices.SortFunc(byPos[pos], func(a, b Scored) int {
			return cmp.Or(cmp.Compare(b.Points, a.Points), cmp.Compare(a.PlayerID, b.PlayerID))
		})
	}
	best := []int(nil)
	var try func(i, left int, counts []int)
	try = func(i, left int, counts []int) {
		if i == len(positions) {
			if left != 0 {
				return
			}
			sum := 0
			for j, pos := range positions {
				for _, p := range byPos[pos][:counts[j]] {
					sum += p.Points
				}
			}
			if best == nil || sum > points {
				best, points = slices.Clone(counts), sum
			}
			return
		}
		pos := positions[i]
		for c := rules.MinStarters[pos]; c <= min(rules.MaxStarters[pos], len(byPos[pos]), left); c++ {
			try(i+1, left-c, append(counts, c))
		}
	}
	try(0, rules.Starters, nil)
	if best == nil {
		return nil, 0, false
	}
	for j, pos := range positions {
		for _, p := range byPos[pos][:best[j]] {
			xi = append(xi, p.PlayerID)
		}
	}
	return xi, points, true
}

// Accuracy is start/sit accuracy for one gameweek: the points the team
// scored as a share of its optimal XI's points. ok is false when the
// optimal XI scored 0 or less (there is nothing to be a share of).
func Accuracy(actual, optimal int) (float64, bool) {
	if optimal <= 0 {
		return 0, false
	}
	return float64(actual) / float64(optimal), true
}

// SeasonAccuracy is the mean of the gameweeks' accuracies; ok is false
// with none.
func SeasonAccuracy(weeks []float64) (float64, bool) {
	if len(weeks) == 0 {
		return 0, false
	}
	sum := 0.0
	for _, a := range weeks {
		sum += a
	}
	return sum / float64(len(weeks)), true
}
