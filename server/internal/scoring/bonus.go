package scoring

import "sort"

// BPS is one player's bonus points system score in one fixture.
type BPS struct {
	PlayerID string
	Score    int
	// Unknown: there is no BPS for the player in this fixture (missing
	// per-fixture data), as opposed to a known score of 0 or below.
	Unknown bool
}

// BonusFromBPS awards bonus in one fixture with FPL's tie rules: the best
// BPS gets 3, the next 2, the next 1, ranked by standard competition ranking
// (ties share a rank and the next rank is skipped):
//
//	tie for 1st: 3, 3, 1
//	tie for 2nd: 3, 2, 2
//	tie for 3rd: 3, 2, 1, 1
//	three-way tie for 1st: 3, 3, 3
//
// Only players who played should be passed in. FPL ranks "the three best
// performing players in each match" whatever the sign of their BPS, so a
// known score of 0 or below ranks like any other. A player whose BPS is
// Unknown (missing per-fixture data) is left out: he is not ranked and
// takes no place. Players with no bonus are left out of the result.
func BonusFromBPS(scores []BPS) map[string]int {
	var sorted []BPS
	for _, s := range scores {
		if !s.Unknown {
			sorted = append(sorted, s)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Score > sorted[j].Score })
	out := map[string]int{}
	rank := 0
	for i, s := range sorted {
		if i == 0 || s.Score != sorted[i-1].Score {
			rank = i + 1
		}
		if rank > 3 {
			break
		}
		out[s.PlayerID] = 4 - rank
	}
	return out
}
