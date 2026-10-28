package projection

import (
	"cmp"
	"math"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// RecentWeeks is how many completed gameweeks fixture difficulty and win
// probability look back over.
const RecentWeeks = 6

// RecentGameweeks returns the RecentWeeks most recent gameweeks before
// before that completed reports as finished, oldest first. Early in the
// season it returns fewer; before any gameweek has finished, none.
func RecentGameweeks(before int, completed func(gw int) bool) []int {
	var out []int
	for gw := before - 1; gw >= 1 && len(out) < RecentWeeks; gw-- {
		if completed(gw) {
			out = append(out, gw)
		}
	}
	slices.Reverse(out)
	return out
}

// Match is a finished fixture in the difficulty window.
type Match struct {
	FixtureID  string
	HomeClubID string
	AwayClubID string
}

// Line is one player's points in one match of the window. ClubID is the
// club he played for; the points count as conceded by the other club.
type Line struct {
	FixtureID string
	ClubID    string
	Position  scoring.Position
	Points    int
}

// Ranking is one club's difficulty for one position.
type Ranking struct {
	ClubID string
	// Rank is 1 (concedes least: hardest) to 20 (concedes most: easiest).
	// Clubs conceding the same per match share the better rank (1, 2, 2, 4).
	Rank int
	// Conceded is the points the position scored against the club over
	// the window, Matches how many window matches the club played.
	Conceded int
	Matches  int
}

// Difficulty is every club's ranking for each position. A club with no
// match in the window has no ranking.
type Difficulty map[scoring.Position]map[string]Ranking

// Rank is the club's rank for a position, or 0 when it has none.
func (d Difficulty) Rank(pos scoring.Position, clubID string) int {
	return d[pos][clubID].Rank
}

// Rank computes Difficulty from the window's matches and lines. Lines of
// a fixture not in matches, or of a club not playing in it, are ignored.
func Rank(matches []Match, lines []Line) Difficulty {
	type acc struct{ conceded, matches int }
	byFixture := map[string]Match{}
	played := map[string]int{}
	for _, m := range matches {
		if _, dup := byFixture[m.FixtureID]; dup {
			continue
		}
		byFixture[m.FixtureID] = m
		played[m.HomeClubID]++
		played[m.AwayClubID]++
	}
	conceded := map[scoring.Position]map[string]int{}
	for _, l := range lines {
		m, ok := byFixture[l.FixtureID]
		if !ok {
			continue
		}
		var against string
		switch l.ClubID {
		case m.HomeClubID:
			against = m.AwayClubID
		case m.AwayClubID:
			against = m.HomeClubID
		default:
			continue
		}
		if conceded[l.Position] == nil {
			conceded[l.Position] = map[string]int{}
		}
		conceded[l.Position][against] += l.Points
	}
	out := Difficulty{}
	for _, pos := range []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD} {
		rows := make([]Ranking, 0, len(played))
		for club, n := range played {
			rows = append(rows, Ranking{ClubID: club, Conceded: conceded[pos][club], Matches: n})
		}
		// Per match, compared without division: a/m < b/n <=> a*n < b*m.
		perMatch := func(a, b Ranking) int { return cmp.Compare(a.Conceded*b.Matches, b.Conceded*a.Matches) }
		slices.SortFunc(rows, func(a, b Ranking) int { return cmp.Or(perMatch(a, b), cmp.Compare(a.ClubID, b.ClubID)) })
		out[pos] = map[string]Ranking{}
		for i, r := range rows {
			r.Rank = i + 1
			if i > 0 && perMatch(rows[i-1], r) == 0 {
				r.Rank = out[pos][rows[i-1].ClubID].Rank
			}
			out[pos][r.ClubID] = r
		}
	}
	return out
}

// Factor is the fixture factor for a difficulty rank: 0.80 against the
// hardest opponent (1st) rising in equal steps to 1.20 against the easiest
// (20th), 0.8 + 0.4 × (rank − 1) / 19. An unranked opponent (0) is 1.
func Factor(rank int) float64 {
	if rank < 1 {
		return 1
	}
	return 0.8 + 0.4*float64(min(rank, 20)-1)/19
}

// Expected is a player's Expected points for a gameweek: form times the
// sum of the fixture factors of his matches in it (ranks, one per match:
// none in a blank gameweek, two in a double), rounded to one decimal,
// halves up, as form is.
func Expected(form float64, ranks []int) float64 {
	sum := 0.0
	for _, r := range ranks {
		sum += Factor(r)
	}
	return round1(form * sum)
}

// round1 rounds to one decimal, halves up (statuspage.Form's rounding).
// The 1e-9 absorbs binary error such as 4.35 stored as 4.3499999.
func round1(x float64) float64 { return math.Floor(x*10+0.5+1e-9) / 10 }
