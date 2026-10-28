package httpapi

import (
	"context"
	"errors"
	"strconv"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Player and Team of the Week (GET /v1/gameweeks/{gw}/awards and GET
// /v1/awards/players-of-the-week): the rules are scoring.PlayerOfTheWeek
// and scoring.TeamOfTheWeek, over every player in the game.

// awardWorld is the world with every gameweek's per-player lines.
type awardWorld struct {
	w *league.World
	// lines are upstream's gameweek lines by gameweek, then player.
	lines map[int]map[string]stats.PlayerGameweek
}

func (s *Server) awardWorld(ctx context.Context, gw int) (*awardWorld, error) {
	w, err := s.world(ctx)
	if err != nil {
		return nil, err
	}
	ls, err := s.stats.PlayerGameweeks(ctx, gw)
	if err != nil {
		return nil, err
	}
	a := &awardWorld{w: w, lines: map[int]map[string]stats.PlayerGameweek{}}
	for _, l := range ls {
		if a.lines[l.Gameweek] == nil {
			a.lines[l.Gameweek] = map[string]stats.PlayerGameweek{}
		}
		a.lines[l.Gameweek][l.PlayerID] = l
	}
	return a, nil
}

// awardStats is a player's gameweek: points, bonus and minutes as scored
// (the rows), BPS, goals, assists, defensive contribution and clean sheets
// from upstream's gameweek line (FPL's own totals, which a double
// gameweek's breakdown cannot split), or from the rows when there is no
// line.
func (a *awardWorld) awardStats(playerID string, gw int) scoring.AwardStats {
	var st scoring.AwardStats
	pos := a.w.Players[playerID].Position
	for _, r := range a.w.Rows[gw][playerID] {
		st.Points += r.Points
		st.Bonus += r.Stats.Bonus
		st.Minutes += r.Stats.Minutes
		if !r.BPSUnknown {
			st.BPS += r.BPS
		}
		st.Goals += r.Stats.Goals
		st.Assists += r.Stats.Assists
		st.DefensiveContribution += r.Stats.ClearancesBlocksInterceptions + r.Stats.Tackles
		if pos != scoring.DEF {
			st.DefensiveContribution += r.Stats.Recoveries
		}
		if r.Stats.CleanSheet && r.Stats.Minutes >= 60 {
			st.CleanSheets++
		}
	}
	if l, ok := a.lines[gw][playerID]; ok {
		st.BPS, st.Goals, st.Assists = l.BPS, l.GoalsScored, l.Assists
		st.DefensiveContribution, st.CleanSheets = l.DefensiveContribution, l.CleanSheets
	}
	return st
}

// awards ranks every player in the game for one gameweek.
func (a *awardWorld) awards(gw int) (potw *scoring.AwardCandidate, totw []scoring.AwardCandidate) {
	pool := make([]scoring.AwardCandidate, 0, len(a.w.Players))
	for id, p := range a.w.Players {
		pool = append(pool, scoring.AwardCandidate{ID: id, Position: p.Position, Stats: a.awardStats(id, gw)})
	}
	if best, ok := scoring.PlayerOfTheWeek(pool); ok {
		potw = &best
	}
	return potw, scoring.TeamOfTheWeek(pool)
}

func (a *awardWorld) awardPlayer(c scoring.AwardCandidate) AwardPlayer {
	st := c.Stats
	return AwardPlayer{Player: playerRef(a.w, c.ID), Stats: AwardStats{
		Points: st.Points, Bonus: st.Bonus, Bps: st.BPS, Goals: st.Goals, Assists: st.Assists,
		DefensiveContribution: st.DefensiveContribution, CleanSheets: st.CleanSheets, Minutes: st.Minutes,
	}}
}

func (s *Server) GetGameweekAwards(ctx context.Context, r GetGameweekAwardsRequestObject) (GetGameweekAwardsResponseObject, error) {
	if d := badGameweek(r.Gw); d != "" {
		return GetGameweekAwards400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	a, err := s.awardWorld(ctx, r.Gw)
	if errors.Is(err, errNoSeasonData) {
		return fail[GetGameweekAwardsdefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	if err != nil {
		return nil, err
	}
	if !a.w.Started(r.Gw) {
		return fail[GetGameweekAwardsdefaultApplicationProblemPlusJSONResponse](
			notFound(ctx, "Gameweek "+strconv.Itoa(r.Gw)+" has not started.")), nil
	}
	state := a.w.State(r.Gw)
	body := GameweekAwards{Gameweek: r.Gw, State: ScoreState(state), TeamOfTheWeek: []AwardPlayer{}}
	potw, totw := a.awards(r.Gw)
	if potw != nil {
		body.PlayerOfTheWeek = ptr(a.awardPlayer(*potw))
	}
	for _, c := range totw {
		body.TeamOfTheWeek = append(body.TeamOfTheWeek, a.awardPlayer(c))
	}
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return GetGameweekAwards200JSONResponse{Data: body, Meta: meta}, nil
}

func (s *Server) ListPlayersOfTheWeek(ctx context.Context, _ ListPlayersOfTheWeekRequestObject) (ListPlayersOfTheWeekResponseObject, error) {
	a, err := s.awardWorld(ctx, 0)
	if errors.Is(err, errNoSeasonData) {
		return fail[ListPlayersOfTheWeekdefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	if err != nil {
		return nil, err
	}
	out := []PlayerOfTheWeek{}
	for gw := 1; gw <= 38; gw++ {
		if !a.w.Started(gw) {
			continue
		}
		if potw, _ := a.awards(gw); potw != nil {
			out = append(out, PlayerOfTheWeek{Gameweek: gw, State: ScoreState(a.w.State(gw)), Award: a.awardPlayer(*potw)})
		}
	}
	state := a.w.State(a.w.Status.Current)
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return ListPlayersOfTheWeek200JSONResponse{Data: out, Meta: meta}, nil
}
