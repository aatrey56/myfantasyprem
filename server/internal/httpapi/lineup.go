package httpapi

import (
	"cmp"
	"context"

	"github.com/aatrey56/myfantasyprem/server/internal/lineup"
)

func (s *Server) lineups() *lineup.Service {
	return lineup.New(s.leagues.Repo(), s.stats, s.now, s.notifier())
}

func (s *Server) SetLineup(ctx context.Context, r SetLineupRequestObject) (SetLineupResponseObject, error) {
	d := cmp.Or(lg(r.LeagueId), tm(r.TeamId))
	if d == "" && r.Body != nil {
		d = badGameweek(r.Body.Gameweek)
		for _, id := range append(append([]string{}, r.Body.Starters...), r.Body.Bench...) {
			d = cmp.Or(d, badID("playerId", "plr", id))
		}
	}
	if d != "" {
		return SetLineup400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[SetLineupdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "gameweek, starters and bench are required.")), nil
	}
	b := r.Body
	if err := s.lineups().Set(ctx, userFrom(ctx), r.LeagueId, r.TeamId, b.Gameweek, b.Starters, b.Bench); err != nil {
		return failLeague[SetLineupdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	gw := b.Gameweek
	resp, err := s.GetLineup(ctx, GetLineupRequestObject{LeagueId: r.LeagueId, TeamId: r.TeamId, Params: GetLineupParams{Gameweek: &gw}})
	if err != nil {
		return nil, err
	}
	switch v := resp.(type) {
	case GetLineup200JSONResponse:
		return SetLineup200JSONResponse(v), nil
	case GetLineupdefaultApplicationProblemPlusJSONResponse:
		return SetLineupdefaultApplicationProblemPlusJSONResponse(v), nil
	}
	return nil, errUnexpected
}
