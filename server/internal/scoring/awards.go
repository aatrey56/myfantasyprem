package scoring

import (
	"cmp"
	"slices"
	"strconv"
)

// Player of the Week and Team of the Week. The same rules as
// apps/app/src/lib/awards.ts, which the MSW mocks use; testdata/awards_cases.json
// holds the cases both test suites run, so the two cannot drift.
//
// The pool is every Premier League player in the game, owned or not.

// AwardStats are a player's gameweek numbers, as in the contract's
// AwardStats.
type AwardStats struct {
	Points                int `json:"points"`
	Bonus                 int `json:"bonus"`
	BPS                   int `json:"bps"`
	Goals                 int `json:"goals"`
	Assists               int `json:"assists"`
	DefensiveContribution int `json:"defensiveContribution"`
	CleanSheets           int `json:"cleanSheets"`
	Minutes               int `json:"minutes"`
}

// AwardCandidate is one player in the award pool.
type AwardCandidate struct {
	// ID is the last resort when every rule ties, so results never flicker.
	ID string `json:"id"`
	// Position is an open enum: an unknown one can win Player of the Week
	// but not a Team of the Week place.
	Position Position   `json:"position"`
	Stats    AwardStats `json:"stats"`
}

// CompareForAward is the award order: negative when a ranks ahead of b.
// Points, then goals, assists, bonus, BPS, defensive contribution and clean
// sheets (more is better), then fewer minutes. Ties after that fall back to
// the id.
func CompareForAward(a, b AwardCandidate) int {
	x, y := a.Stats, b.Stats
	return cmp.Or(
		cmp.Compare(y.Points, x.Points),
		cmp.Compare(y.Goals, x.Goals),
		cmp.Compare(y.Assists, x.Assists),
		cmp.Compare(y.Bonus, x.Bonus),
		cmp.Compare(y.BPS, x.BPS),
		cmp.Compare(y.DefensiveContribution, x.DefensiveContribution),
		cmp.Compare(y.CleanSheets, x.CleanSheets),
		cmp.Compare(x.Minutes, y.Minutes),
		cmp.Compare(a.ID, b.ID),
	)
}

// PlayerOfTheWeek is the gameweek's best player; ok is false for an empty
// pool.
func PlayerOfTheWeek(pool []AwardCandidate) (best AwardCandidate, ok bool) {
	for _, c := range pool {
		if !ok || CompareForAward(c, best) < 0 {
			best, ok = c, true
		}
	}
	return best, ok
}

// TOTWLimit is how many players of a position a Team of the Week holds.
type TOTWLimit struct{ Min, Max int }

// TOTWLimits is the Team of the Week shape: exactly one keeper, outfield
// lines between min and max.
var TOTWLimits = map[Position]TOTWLimit{GKP: {1, 1}, DEF: {3, 5}, MID: {2, 5}, FWD: {1, 3}}

// TOTWSize is the number of players in a full Team of the Week.
const TOTWSize = 11

// TOTWOrder is the order positions are listed in.
var TOTWOrder = []Position{GKP, DEF, MID, FWD}

// TeamOfTheWeek is the best keeper, three defenders, two midfielders and
// one forward, then the best four remaining outfield players that keep
// every line within its cap (DEF 5, MID 5, FWD 3). It is returned keeper
// first, then defenders, midfielders and forwards, best first within each.
//
// A pool too thin to fill a line yields fewer than 11 rather than breaking
// the caps.
func TeamOfTheWeek(pool []AwardCandidate) []AwardCandidate {
	var ranked []AwardCandidate
	for _, c := range pool {
		if _, ok := TOTWLimits[c.Position]; ok {
			ranked = append(ranked, c)
		}
	}
	slices.SortStableFunc(ranked, CompareForAward)
	picked := make([]bool, len(ranked))
	size := 0
	count := map[Position]int{}
	take := func(i int) {
		picked[i] = true
		size++
		count[ranked[i].Position]++
	}
	// The minimum of each line first.
	for _, pos := range TOTWOrder {
		for i, c := range ranked {
			if count[pos] >= TOTWLimits[pos].Min {
				break
			}
			if c.Position == pos {
				take(i)
			}
		}
	}
	// Then the best of the rest, within the caps.
	for i, c := range ranked {
		if size >= TOTWSize {
			break
		}
		if picked[i] || c.Position == GKP || count[c.Position] >= TOTWLimits[c.Position].Max {
			continue
		}
		take(i)
	}
	out := []AwardCandidate{}
	for _, pos := range TOTWOrder {
		for i, c := range ranked {
			if picked[i] && c.Position == pos {
				out = append(out, c)
			}
		}
	}
	return out
}

// TOTWFormation is the "4-4-2" style shape of a Team of the Week.
func TOTWFormation(team []AwardCandidate) string {
	n := map[Position]int{}
	for _, c := range team {
		n[c.Position]++
	}
	return strconv.Itoa(n[DEF]) + "-" + strconv.Itoa(n[MID]) + "-" + strconv.Itoa(n[FWD])
}
