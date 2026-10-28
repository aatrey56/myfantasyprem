package httpapi

import (
	"cmp"
	"context"
	"errors"

	"github.com/aatrey56/myfantasyprem/server/internal/watchlist"
)

// watchlistWorld checks the caller is a member of the league and loads the
// season's players. A problem is the response to send.
func (s *Server) watchlistWorld(ctx context.Context, leagueID string) (*view, *Problem, error) {
	l, err := s.leagues.Get(ctx, userFrom(ctx), leagueID)
	if err != nil {
		p, err := leagueProblem(ctx, err)
		if err != nil {
			return nil, nil, err
		}
		return nil, &p, nil
	}
	w, err := s.world(ctx)
	if errors.Is(err, errNoSeasonData) {
		p := unavailable(ctx)
		return nil, &p, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return &view{l: l, w: w}, nil, nil
}

func (s *Server) GetWatchlist(ctx context.Context, r GetWatchlistRequestObject) (GetWatchlistResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return GetWatchlist400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.watchlistWorld(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[GetWatchlistdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	entries, err := s.watchlist.List(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return nil, err
	}
	out := []WatchlistEntry{}
	for _, e := range entries {
		// A player no longer in the season's data drops out of the list.
		if _, ok := v.w.Players[e.PlayerID]; ok {
			out = append(out, WatchlistEntry{Player: playerRef(v.w, e.PlayerID), AddedAt: e.AddedAt})
		}
	}
	return GetWatchlist200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) WatchPlayer(ctx context.Context, r WatchPlayerRequestObject) (WatchPlayerResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badID("playerId", "plr", r.PlayerId)); d != "" {
		return WatchPlayer400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.watchlistWorld(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[WatchPlayerdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	if _, ok := v.w.Players[r.PlayerId]; !ok {
		return fail[WatchPlayerdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Player not found.")), nil
	}
	e, err := s.watchlist.Add(ctx, userFrom(ctx), r.LeagueId, r.PlayerId)
	if errors.Is(err, watchlist.ErrFull) {
		return fail[WatchPlayerdefaultApplicationProblemPlusJSONResponse](newProblem(409, "watchlist-full", "Watchlist full",
			"A watchlist holds 200 players. Remove one to add another.", RequestIDFrom(ctx))), nil
	}
	if err != nil {
		return nil, err
	}
	return WatchPlayer200JSONResponse{
		Data: WatchlistEntry{Player: playerRef(v.w, e.PlayerID), AddedAt: e.AddedAt},
		Meta: metaAt(s.now()),
	}, nil
}

func (s *Server) UnwatchPlayer(ctx context.Context, r UnwatchPlayerRequestObject) (UnwatchPlayerResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badID("playerId", "plr", r.PlayerId)); d != "" {
		return UnwatchPlayer400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if _, err := s.leagues.Get(ctx, userFrom(ctx), r.LeagueId); err != nil {
		return failLeague[UnwatchPlayerdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	if err := s.watchlist.Remove(ctx, userFrom(ctx), r.LeagueId, r.PlayerId); err != nil {
		return nil, err
	}
	return UnwatchPlayer204Response{}, nil
}
