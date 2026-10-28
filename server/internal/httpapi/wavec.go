package httpapi

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/statuspage"
	"github.com/aatrey56/myfantasyprem/server/internal/tags"
	"github.com/aatrey56/myfantasyprem/server/internal/tradeblock"
)

// Wave C: the trade block (package tradeblock), player news and personal
// tags (package tags).

func (s *Server) tradeBlockService() *tradeblock.Service {
	return tradeblock.New(s.leagues.Repo(), s.now, s.notifier())
}

// tradeBlockEntry renders one entry; the team is looked up in l.
func tradeBlockEntry(l *league.League, w *league.World, n names, e league.BlockEntry) TradeBlockEntry {
	t, _ := l.Team(e.TeamID)
	out := TradeBlockEntry{Player: playerRef(w, e.PlayerID), Team: teamRef(t, n), Note: e.Note,
		Wanted: []Position{}, AddedAt: e.AddedAt}
	for _, p := range e.Wanted {
		out.Wanted = append(out.Wanted, Position(p))
	}
	return out
}

func (s *Server) ListTradeBlock(ctx context.Context, r ListTradeBlockRequestObject) (ListTradeBlockResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return ListTradeBlock400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[ListTradeBlockdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	out := []TradeBlockEntry{}
	for _, e := range v.st.Block {
		if _, ok := v.w.Players[e.PlayerID]; ok {
			out = append(out, tradeBlockEntry(v.l, v.w, v.names, e))
		}
	}
	return ListTradeBlock200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) SetTradeBlockEntry(ctx context.Context, r SetTradeBlockEntryRequestObject) (SetTradeBlockEntryResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badID("playerId", "plr", r.PlayerId)); d != "" {
		return SetTradeBlockEntry400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	note, wanted := "", []string{}
	if b := r.Body; b != nil {
		if b.Note != nil {
			note = *b.Note
		}
		if b.Wanted != nil {
			for _, p := range *b.Wanted {
				wanted = append(wanted, string(p))
			}
		}
	}
	w, err := s.world(ctx)
	if errors.Is(err, errNoSeasonData) {
		return fail[SetTradeBlockEntrydefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	if err != nil {
		return nil, err
	}
	st, e, err := s.tradeBlockService().Set(ctx, userFrom(ctx), r.LeagueId, r.PlayerId, note, wanted)
	if err != nil {
		return failLeague[SetTradeBlockEntrydefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, st.League)
	if err != nil {
		return nil, err
	}
	return SetTradeBlockEntry200JSONResponse{Data: tradeBlockEntry(st.League, w, n, e), Meta: metaAt(s.now())}, nil
}

func (s *Server) RemoveTradeBlockEntry(ctx context.Context, r RemoveTradeBlockEntryRequestObject) (RemoveTradeBlockEntryResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badID("playerId", "plr", r.PlayerId)); d != "" {
		return RemoveTradeBlockEntry400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if err := s.tradeBlockService().Remove(ctx, userFrom(ctx), r.LeagueId, r.PlayerId); err != nil {
		return failLeague[RemoveTradeBlockEntrydefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	return RemoveTradeBlockEntry204Response{}, nil
}

// chanceNext is a player's chance of playing next round: the source's
// figure when it gives one for a player not fully available, else what
// his news says (statuspage.ChanceOfPlaying).
func chanceNext(p stats.Player) *int {
	if p.ChanceNext != nil && p.Availability != statuspage.AvailabilityAvailable {
		c := *p.ChanceNext
		return &c
	}
	return statuspage.ChanceOfPlaying(p.Availability, p.News)
}

// newsOrder orders news newest first; notes without a time last, by name.
func newsOrder(a, b stats.Player) int {
	switch {
	case a.NewsAt != nil && b.NewsAt != nil:
		if c := b.NewsAt.Compare(*a.NewsAt); c != 0 {
			return c
		}
	case a.NewsAt != nil:
		return -1
	case b.NewsAt != nil:
		return 1
	}
	return cmp.Or(cmp.Compare(strings.ToLower(a.DisplayName), strings.ToLower(b.DisplayName)), cmp.Compare(a.ID, b.ID))
}

func (s *Server) ListPlayerNews(ctx context.Context, r ListPlayerNewsRequestObject) (ListPlayerNewsResponseObject, error) {
	limit, limitErr := limitOf(r.Params.Limit)
	if d := cmp.Or(lg(r.LeagueId), optTeam(r.Params.TeamId), limitErr); d != "" {
		return ListPlayerNews400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[ListPlayerNewsdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	if id := r.Params.TeamId; id != nil {
		if _, ok := v.l.Team(*id); !ok {
			return fail[ListPlayerNewsdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Team not found.")), nil
		}
	}
	own := owners(v)
	var ps []stats.Player
	for id, pl := range v.w.Players {
		if pl.News == "" || (r.Params.TeamId != nil && own[id] != *r.Params.TeamId) {
			continue
		}
		ps = append(ps, pl)
	}
	slices.SortFunc(ps, newsOrder)
	out := []PlayerNewsItem{}
	for _, pl := range ps[:min(limit, len(ps))] {
		item := PlayerNewsItem{Player: playerRef(v.w, pl.ID), News: pl.News, NewsAt: pl.NewsAt, ChanceOfPlaying: chanceNext(pl)}
		if t := own[pl.ID]; t != "" {
			item.Owner = ptr(v.team(t))
		}
		out = append(out, item)
	}
	return ListPlayerNews200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) ListPlayerTags(ctx context.Context, r ListPlayerTagsRequestObject) (ListPlayerTagsResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return ListPlayerTags400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.watchlistWorld(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[ListPlayerTagsdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	entries, err := s.tags.List(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return nil, err
	}
	out := []PlayerTagEntry{}
	for _, e := range entries {
		// A player no longer in the season's data drops out of the list.
		if _, ok := v.w.Players[e.PlayerID]; ok {
			out = append(out, PlayerTagEntry{Player: playerRef(v.w, e.PlayerID), Tag: PlayerTag(e.Tag), UpdatedAt: e.UpdatedAt})
		}
	}
	return ListPlayerTags200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) SetPlayerTag(ctx context.Context, r SetPlayerTagRequestObject) (SetPlayerTagResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badID("playerId", "plr", r.PlayerId)); d != "" {
		return SetPlayerTag400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil || !tags.Valid(string(r.Body.Tag)) {
		return fail[SetPlayerTagdefaultApplicationProblemPlusJSONResponse](invalid(ctx,
			"tag must be one of hot, cold, target, sell and avoid.")), nil
	}
	v, p, err := s.watchlistWorld(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[SetPlayerTagdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	if _, ok := v.w.Players[r.PlayerId]; !ok {
		return fail[SetPlayerTagdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Player not found.")), nil
	}
	e, err := s.tags.Set(ctx, userFrom(ctx), r.LeagueId, r.PlayerId, string(r.Body.Tag))
	if err != nil {
		return nil, err
	}
	return SetPlayerTag200JSONResponse{
		Data: PlayerTagEntry{Player: playerRef(v.w, e.PlayerID), Tag: PlayerTag(e.Tag), UpdatedAt: e.UpdatedAt},
		Meta: metaAt(s.now()),
	}, nil
}

func (s *Server) RemovePlayerTag(ctx context.Context, r RemovePlayerTagRequestObject) (RemovePlayerTagResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badID("playerId", "plr", r.PlayerId)); d != "" {
		return RemovePlayerTag400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if _, err := s.leagues.Get(ctx, userFrom(ctx), r.LeagueId); err != nil {
		return failLeague[RemovePlayerTagdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	if err := s.tags.Remove(ctx, userFrom(ctx), r.LeagueId, r.PlayerId); err != nil {
		return nil, err
	}
	return RemovePlayerTag204Response{}, nil
}
