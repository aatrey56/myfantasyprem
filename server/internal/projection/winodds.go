package projection

import (
	"hash/fnv"
	"math/rand/v2"
	"strconv"
)

// Samples is how many simulated gameweeks WinOdds plays.
const Samples = 10000

// Starter is one starter of a team in the gameweek being simulated.
type Starter struct {
	// Actual is the points he has already scored this gameweek (0 before
	// his first match).
	Actual int
	// Remaining is how many of his club's matches this gameweek have not
	// kicked off: each adds one draw from History.
	Remaining int
	// Live is the match minute of each of his club's matches in progress
	// (the fixture's clock, not his own minutes): each adds one draw from
	// History scaled by the share of the match left, Remainder.
	Live []int
	// History is his points in each of the recent completed gameweeks
	// (RecentGameweeks), 0 for a gameweek he did not play.
	History []int
}

// Team is a team's starting XI as named for the gameweek (auto-subs are
// not applied).
type Team struct {
	ID       string
	Starters []Starter
}

// Odds is a matchup's chances. Home + Draw + Away is 1.
type Odds struct {
	Home, Draw, Away float64
}

// Seed is the simulation seed for a matchup: the same matchup, gameweek
// and minute bucket always give the same odds, so they do not flicker on
// refresh.
func Seed(matchupID string, gw int, bucket int64) uint64 {
	h := fnv.New64a()
	h.Write([]byte(matchupID + "|" + strconv.Itoa(gw) + "|" + strconv.FormatInt(bucket, 10)))
	return h.Sum64()
}

// MatchMinutes is the length of a match for Remainder.
const MatchMinutes = 90

// Remainder is what is left to score of a sampled gameweek total pts in a
// match at minute: pts × max(0, 90 − minute) / 90, rounded to the nearest
// point with halves away from zero (6 at minute 45 → 3, 3 → 1.5 → 2,
// −1 → −0.5 → −1). Minute 0 leaves pts; minute 90 or later leaves 0.
func Remainder(pts, minute int) int {
	left := max(0, MatchMinutes-max(0, minute))
	x := pts * left
	if x < 0 {
		return -((-x + MatchMinutes/2) / MatchMinutes)
	}
	return (x + MatchMinutes/2) / MatchMinutes
}

// WinOdds plays the gameweek n times. In each play every starter scores
// his Actual points plus, for each Remaining match, one of his History
// gameweeks picked uniformly at random (a starter with no history adds 0),
// plus, for each Live match, another such draw cut to the Remainder of the
// match. A finished match is neither: its points are all in Actual.
// A side's score is its starters' total; homeID or awayID "" is the
// league-average opponent, which scores the floor of the mean of every
// team in teams (scoring.AveragePoints), so with it every team is played.
//
// Each team draws from its own random stream, keyed by the seed and its
// id, so the odds do not depend on which side is home: swapping the sides
// swaps Home and Away exactly.
func WinOdds(teams []Team, homeID, awayID string, seed uint64, n int) Odds {
	if n <= 0 {
		return Odds{}
	}
	playAll := homeID == "" || awayID == ""
	type stream struct {
		team Team
		rng  *rand.Rand
	}
	var play []stream
	for _, t := range teams {
		if playAll || t.ID == homeID || t.ID == awayID {
			h := fnv.New64a()
			h.Write([]byte(t.ID))
			play = append(play, stream{t, rand.New(rand.NewPCG(seed, h.Sum64()))})
		}
	}
	totals := map[string]int{}
	var home, draw, away int
	for range n {
		sum := 0
		for _, s := range play {
			total := 0
			for _, st := range s.team.Starters {
				total += st.Actual
				if len(st.History) == 0 {
					continue
				}
				for range st.Remaining {
					total += st.History[s.rng.IntN(len(st.History))]
				}
				for _, minute := range st.Live {
					total += Remainder(st.History[s.rng.IntN(len(st.History))], minute)
				}
			}
			totals[s.team.ID] = total
			sum += total
		}
		avg := floorDiv(sum, len(play))
		side := func(id string) int {
			if id == "" {
				return avg
			}
			return totals[id]
		}
		switch h, a := side(homeID), side(awayID); {
		case h > a:
			home++
		case h < a:
			away++
		default:
			draw++
		}
	}
	return Odds{Home: float64(home) / float64(n), Draw: float64(draw) / float64(n), Away: float64(away) / float64(n)}
}

// floorDiv is the floor of a/b (scoring.AveragePoints), 0 for no teams.
func floorDiv(a, b int) int {
	if b == 0 {
		return 0
	}
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// Final is the odds of a matchup whose gameweek is over: the result.
func Final(home, away int) Odds {
	switch {
	case home > away:
		return Odds{Home: 1}
	case home < away:
		return Odds{Away: 1}
	}
	return Odds{Draw: 1}
}
