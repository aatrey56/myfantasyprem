package scoring

import "sort"

// TeamTotal returns a team's gameweek points with the given auto-subs
// applied, and the points left on the bench. Starters count unless subbed
// out; bench players count only when subbed in.
func TeamTotal(starters, bench []LineupPlayer, subs []Sub) (total, benchPoints int) {
	out := map[int]bool{}
	in := map[int]bool{}
	for _, s := range subs {
		out[s.OutSlot] = true
		in[s.InSlot] = true
	}
	for i, p := range starters {
		if !out[i+1] {
			total += p.Points
		}
	}
	for j, p := range bench {
		if in[len(starters)+j+1] {
			total += p.Points
		} else {
			benchPoints += p.Points
		}
	}
	return total, benchPoints
}

// AveragePoints is the league-average opponent's score: the floor of the
// mean gameweek points of every real team, including the one it plays.
// Zero teams score 0.
func AveragePoints(teamTotals []int) int {
	if len(teamTotals) == 0 {
		return 0
	}
	sum := 0
	for _, t := range teamTotals {
		sum += t
	}
	// Integer division floors for non-negative sums; adjust for negatives.
	q := sum / len(teamTotals)
	if sum%len(teamTotals) != 0 && sum < 0 {
		q--
	}
	return q
}

// Result is a head-to-head outcome.
type Result string

const (
	Win  Result = "W"
	Draw Result = "D"
	Loss Result = "L"
)

// H2H returns the result for a side scoring `mine` against `theirs`.
func H2H(mine, theirs int) Result {
	switch {
	case mine > theirs:
		return Win
	case mine < theirs:
		return Loss
	}
	return Draw
}

// LeaguePoints: win 3, draw 1, loss 0.
func LeaguePoints(r Result) int {
	switch r {
	case Win:
		return 3
	case Draw:
		return 1
	}
	return 0
}

// Standing is one real team's row. The league-average opponent never has one.
type Standing struct {
	TeamID        string
	Wins          int
	Draws         int
	Losses        int
	PointsFor     int
	PointsAgainst int
}

// LeaguePoints of the row.
func (s Standing) LeaguePoints() int { return s.Wins*3 + s.Draws }

// SortStandings orders rows by league points, then points for, then team id
// (so the order is stable and deterministic).
func SortStandings(rows []Standing) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.LeaguePoints() != b.LeaguePoints() {
			return a.LeaguePoints() > b.LeaguePoints()
		}
		if a.PointsFor != b.PointsFor {
			return a.PointsFor > b.PointsFor
		}
		return a.TeamID < b.TeamID
	})
}

// ScoreState is how settled a gameweek's scores are. Values match the
// ScoreState enum in api/openapi.yaml.
type ScoreState string

const (
	StateUpcoming    ScoreState = "upcoming"
	StateLive        ScoreState = "live"
	StateProvisional ScoreState = "provisional"
	StateOfficial    ScoreState = "official"
	StateFinal       ScoreState = "final"
)

// FixtureProgress is one fixture's progress for StateOf.
type FixtureProgress struct {
	Started  bool
	Finished bool
}

// StateOf derives the gameweek score state: upcoming before any kickoff,
// live while any fixture is unfinished, provisional once all are finished
// but bonus is not added, official once bonus is added, final once the data
// is checked. A gameweek with no fixtures is upcoming.
func StateOf(fixtures []FixtureProgress, bonusAdded, dataChecked bool) ScoreState {
	started, allDone := false, len(fixtures) > 0
	for _, f := range fixtures {
		started = started || f.Started || f.Finished
		allDone = allDone && f.Finished
	}
	switch {
	case !started:
		return StateUpcoming
	case !allDone:
		return StateLive
	case dataChecked:
		return StateFinal
	case bonusAdded:
		return StateOfficial
	}
	return StateProvisional
}
