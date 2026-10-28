package httpapi

import (
	"context"

	"github.com/aatrey56/myfantasyprem/server/internal/history"
)

// previousSeasons is a player's finished seasons for the card, newest
// first. History is keyed by FPL code (the player's permanent external
// id); a player without one has none. Untracked stats stay null.
func (s *Server) previousSeasons(ctx context.Context, playerID string) ([]PlayerSeasonTotals, error) {
	out := []PlayerSeasonTotals{}
	codes, err := s.stats.ExternalIDs(ctx, s.historyProvider, "player")
	if err != nil {
		return nil, err
	}
	code := ""
	for c, id := range codes {
		if id == playerID {
			code = c
			break
		}
	}
	if code == "" {
		return out, nil
	}
	seasons, err := s.history.Seasons(ctx, code)
	if err != nil {
		return nil, err
	}
	for _, h := range seasons {
		out = append(out, pastSeason(h))
	}
	return out, nil
}

func pastSeason(h history.Season) PlayerSeasonTotals {
	return PlayerSeasonTotals{
		Season: h.Season, Points: h.TotalPoints, Minutes: h.Minutes, GoalsScored: h.GoalsScored,
		Assists: h.Assists, CleanSheets: h.CleanSheets, Bonus: h.Bonus, Starts: h.Starts,
		ExpectedGoals: f32(h.ExpectedGoals), ExpectedAssists: f32(h.ExpectedAssists),
		ExpectedGoalInvolvements: f32(h.ExpectedGoalInvolvements), IctIndex: f32(h.ICTIndex),
		DefensiveContribution: h.DefensiveContribution,
	}
}
