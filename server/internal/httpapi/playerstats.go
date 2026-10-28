package httpapi

import "github.com/aatrey56/myfantasyprem/server/internal/stats"

// gameStats is a player's PlayerGameStats for one gameweek. Before any of
// his club's fixtures has kicked off (or in a blank) every field is null,
// as in the app's mocks. Otherwise it is upstream's gameweek line (FPL's
// own totals, summed over a double gameweek), except bonus and defensive
// contribution points, which come from the points breakdown so they match
// the points shown (bonus can be our projection until FPL confirms it).
// The ICT index and expected stats stay null when the provider does not
// publish them. Without a line (stats stored before lines were kept) the
// breakdown fills what it can.
//
// FPL publishes ICT and expected stats only once matches are processed:
// during a gameweek its live feed can send 0.0 for a player who played the
// whole match. So until the gameweek is settled (finished or data checked),
// an all-zero ICT group or expected-stats group for a player with minutes
// is null ("not published yet"), not a real zero.
func gameStats(fixtures []PlayerFixture, line *stats.PlayerGameweek, settled bool) PlayerGameStats {
	var started []PlayerFixture
	for _, f := range fixtures {
		switch f.Status {
		case FixtureStatusLive, FixtureStatusFinishedProvisional, FixtureStatusFinished:
			started = append(started, f)
		}
	}
	s := gameStatsFromBreakdown(started)
	if line == nil || len(started) == 0 {
		return s
	}
	l := *line
	s.GoalsScored, s.Assists, s.OwnGoals = ptr(l.GoalsScored), ptr(l.Assists), ptr(l.OwnGoals)
	s.CleanSheets, s.GoalsConceded, s.Saves = ptr(l.CleanSheets), ptr(l.GoalsConceded), ptr(l.Saves)
	s.PenaltiesSaved, s.PenaltiesMissed = ptr(l.PenaltiesSaved), ptr(l.PenaltiesMissed)
	s.YellowCards, s.RedCards = ptr(l.YellowCards), ptr(l.RedCards)
	s.Bps = ptr(l.BPS)
	s.Cbit = ptr(l.ClearancesBlocksInterceptions + l.Tackles)
	s.Recoveries, s.Tackles = ptr(l.Recoveries), ptr(l.Tackles)
	s.Influence, s.Creativity, s.Threat, s.IctIndex = f32(l.Influence), f32(l.Creativity), f32(l.Threat), f32(l.ICTIndex)
	s.ExpectedGoals, s.ExpectedAssists = f32(l.ExpectedGoals), f32(l.ExpectedAssists)
	s.ExpectedGoalInvolvements, s.ExpectedGoalsConceded = f32(l.ExpectedGoalInvolvements), f32(l.ExpectedGoalsConceded)
	if l.Minutes > 0 && !settled {
		if allZero(l.Influence, l.Creativity, l.Threat, l.ICTIndex) {
			s.Influence, s.Creativity, s.Threat, s.IctIndex = nil, nil, nil, nil
		}
		if allZero(l.ExpectedGoals, l.ExpectedAssists, l.ExpectedGoalInvolvements, l.ExpectedGoalsConceded) {
			s.ExpectedGoals, s.ExpectedAssists, s.ExpectedGoalInvolvements, s.ExpectedGoalsConceded = nil, nil, nil, nil
		}
	}
	return s
}

// allZero reports whether every value is absent or zero.
func allZero(vs ...*float64) bool {
	for _, v := range vs {
		if v != nil && *v != 0 {
			return false
		}
	}
	return true
}

// f32 narrows an optional decimal stat to the generated type.
func f32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	return ptr(float32(*v))
}

// pointsOnlyStats are the stats that always score when non-zero, so a missing
// `explain` line means zero. Others (clean sheets for forwards, one or two
// saves, a goal conceded) can be non-zero without a line.
var pointsOnlyStats = map[PointsStat]func(*PlayerGameStats) **int{
	"goals_scored":           func(s *PlayerGameStats) **int { return &s.GoalsScored },
	"assists":                func(s *PlayerGameStats) **int { return &s.Assists },
	"own_goals":              func(s *PlayerGameStats) **int { return &s.OwnGoals },
	"penalties_saved":        func(s *PlayerGameStats) **int { return &s.PenaltiesSaved },
	"penalties_missed":       func(s *PlayerGameStats) **int { return &s.PenaltiesMissed },
	"yellow_cards":           func(s *PlayerGameStats) **int { return &s.YellowCards },
	"red_cards":              func(s *PlayerGameStats) **int { return &s.RedCards },
	"defensive_contribution": func(s *PlayerGameStats) **int { return &s.DefensiveContributionPoints },
	"bonus":                  func(s *PlayerGameStats) **int { return &s.Bonus },
}

// gameStatsFromBreakdown is what the points breakdown can tell about a
// gameweek's stats. With no fixtures (a blank gameweek, or none started)
// every field is null.
func gameStatsFromBreakdown(fixtures []PlayerFixture) PlayerGameStats {
	var s PlayerGameStats
	if len(fixtures) == 0 {
		return s
	}
	for _, field := range pointsOnlyStats {
		zero := 0
		*field(&s) = &zero
	}
	for _, f := range fixtures {
		for _, l := range f.Breakdown {
			if field, ok := pointsOnlyStats[l.Stat]; ok {
				**field(&s) += pointsValue(l)
			}
		}
	}
	return s
}

// pointsValue is the count a line stands for; DC is reported as its points.
func pointsValue(l PointsLine) int {
	if l.Stat == "defensive_contribution" {
		return l.Points
	}
	return l.Value
}
