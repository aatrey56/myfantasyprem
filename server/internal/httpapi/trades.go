package httpapi

import (
	"cmp"
	"context"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
)

// Trades (package trades).

func (s *Server) tradeService() *trades.Service {
	return trades.New(s.leagues.Repo(), s.stats, s.now, s.notifier())
}

// tradeBody renders a trade for one member (userID, owner of teamID):
// viewerActions and review.myVote are theirs.
func tradeBody(l *league.League, w *league.World, n names, t league.Trade, userID, teamID string) Trade {
	from, _ := l.Team(t.OfferingTeamID)
	to, _ := l.Team(t.ReceivingTeamID)
	status := TradeStatus(t.Status)
	if !status.Valid() {
		status = TradeStatusUnknown
	}
	body := Trade{
		Id: t.ID, Status: status, OfferingTeam: teamRef(from, n), ReceivingTeam: teamRef(to, n),
		Offered: playerRefs(w, t.Offered), Requested: playerRefs(w, t.Requested),
		CreatedAt: t.CreatedAt, ExpiresAt: t.ExpiresAt, RespondedAt: t.RespondedAt, ProcessedAt: t.ProcessedAt,
		ViewerActions: []TradeAction{},
	}
	if t.Message != "" {
		body.Message = ptr(t.Message)
	}
	if t.EffectiveGW > 0 {
		body.EffectiveGameweek = ptr(t.EffectiveGW)
	}
	if t.FailureReason != "" {
		f := TradeFailure(t.FailureReason)
		body.FailureReason = &f
	}
	if t.ReviewMode != "" {
		veto, allow := trades.Tally(t)
		r := TradeReviewState{Mode: TradeReview(t.ReviewMode), EndsAt: t.ReviewEndsAt, VetoVotes: veto, AllowVotes: allow}
		if t.ReviewMode == league.ReviewLeagueVote {
			r.VotesNeeded = ptr(trades.VotesNeeded(l, t))
		}
		if v, ok := t.Votes[teamID]; ok && teamID != "" {
			tv := TradeVote(v)
			r.MyVote = &tv
		}
		if t.DecidedBy != "" {
			r.DecidedBy = &UserRef{Id: t.DecidedBy, DisplayName: n.of(t.DecidedBy)}
		}
		body.Review = &r
	}
	for _, a := range trades.Actions(t, l, userID, teamID) {
		body.ViewerActions = append(body.ViewerActions, TradeAction(a))
	}
	return body
}

// tradeResponse renders the one trade of a view for its viewer.
func (s *Server) tradeResponse(ctx context.Context, v *trades.View, t league.Trade) (TradeResponse, error) {
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return TradeResponse{}, err
	}
	return TradeResponse{Data: tradeBody(v.League, v.World, n, t, v.UserID, v.TeamID), Meta: metaAt(s.now())}, nil
}

func (s *Server) ListTrades(ctx context.Context, r ListTradesRequestObject) (ListTradesResponseObject, error) {
	q := r.Params
	limit, limitErr := limitOf(q.Limit)
	offset, cursorOK := decodeCursor(q.Cursor)
	d := cmp.Or(lg(r.LeagueId), optTeam(q.TeamId), limitErr)
	switch {
	case d != "":
	case q.Status != nil && !q.Status.Valid():
		d = "status must be open or a trade status."
	case !cursorOK:
		d = "cursor is not valid; pass meta.nextCursor back unchanged."
	}
	if d != "" {
		return ListTrades400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, err := s.tradeService().List(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[ListTradesdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	var items []league.Trade
	for _, t := range v.Trades {
		if q.Status != nil {
			if *q.Status == ListTradesParamsStatusOpen {
				if !league.TradeOpen(t.Status) {
					continue
				}
			} else if string(*q.Status) != t.Status {
				continue
			}
		}
		if q.TeamId != nil && t.OfferingTeamID != *q.TeamId && t.ReceivingTeamID != *q.TeamId {
			continue
		}
		items = append(items, t)
	}
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return nil, err
	}
	page := []Trade{}
	if offset < len(items) {
		for _, t := range items[offset:min(offset+limit, len(items))] {
			page = append(page, tradeBody(v.League, v.World, n, t, v.UserID, v.TeamID))
		}
	}
	var next *string
	if offset+limit < len(items) {
		c := encodeCursor(offset + limit)
		next = &c
	}
	return ListTrades200JSONResponse{Data: page, Meta: pageMeta(metaAt(s.now()), next)}, nil
}

func (s *Server) ProposeTrade(ctx context.Context, r ProposeTradeRequestObject) (ProposeTradeResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Body != nil {
		d = tm(r.Body.ReceivingTeamId)
		for _, id := range slices.Concat(r.Body.OfferedPlayerIds, r.Body.RequestedPlayerIds) {
			d = cmp.Or(d, badID("playerId", "plr", id))
		}
	}
	if d != "" {
		return ProposeTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[ProposeTradedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "receivingTeamId, offeredPlayerIds and requestedPlayerIds are required.")), nil
	}
	b := r.Body
	msg := ""
	if b.Message != nil {
		msg = *b.Message
	}
	v, t, err := s.tradeService().Propose(ctx, userFrom(ctx), r.LeagueId, b.ReceivingTeamId, b.OfferedPlayerIds, b.RequestedPlayerIds, msg)
	if err != nil {
		return failLeague[ProposeTradedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.tradeResponse(ctx, v, t)
	if err != nil {
		return nil, err
	}
	return ProposeTrade201JSONResponse{Body: body, Headers: ProposeTrade201ResponseHeaders{Location: "/v1/leagues/" + r.LeagueId + "/trades/" + t.ID}}, nil
}

func (s *Server) GetTrade(ctx context.Context, r GetTradeRequestObject) (GetTradeResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), trd(r.TradeId)); d != "" {
		return GetTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, t, err := s.tradeService().Get(ctx, userFrom(ctx), r.LeagueId, r.TradeId)
	if err != nil {
		return failLeague[GetTradedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.tradeResponse(ctx, v, t)
	if err != nil {
		return nil, err
	}
	return GetTrade200JSONResponse(body), nil
}

// tradeOp is a trade action's result as a TradeResponse or a problem.
func (s *Server) tradeOp(ctx context.Context, v *trades.View, t league.Trade, err error) (TradeResponse, *Problem, error) {
	if err != nil {
		if errorsIs503(err) {
			p := unavailable(ctx)
			return TradeResponse{}, &p, nil
		}
		p, err := leagueProblem(ctx, err)
		if err != nil {
			return TradeResponse{}, nil, err
		}
		return TradeResponse{}, &p, nil
	}
	body, err := s.tradeResponse(ctx, v, t)
	return body, nil, err
}

func (s *Server) AcceptTrade(ctx context.Context, r AcceptTradeRequestObject) (AcceptTradeResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), trd(r.TradeId)); d != "" {
		return AcceptTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, t, err := s.tradeService().Accept(ctx, userFrom(ctx), r.LeagueId, r.TradeId)
	body, p, err := s.tradeOp(ctx, v, t, err)
	if p != nil || err != nil {
		return fail[AcceptTradedefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return AcceptTrade200JSONResponse(body), nil
}

func (s *Server) DeclineTrade(ctx context.Context, r DeclineTradeRequestObject) (DeclineTradeResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), trd(r.TradeId)); d != "" {
		return DeclineTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, t, err := s.tradeService().Decline(ctx, userFrom(ctx), r.LeagueId, r.TradeId)
	body, p, err := s.tradeOp(ctx, v, t, err)
	if p != nil || err != nil {
		return fail[DeclineTradedefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return DeclineTrade200JSONResponse(body), nil
}

func (s *Server) WithdrawTrade(ctx context.Context, r WithdrawTradeRequestObject) (WithdrawTradeResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), trd(r.TradeId)); d != "" {
		return WithdrawTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, t, err := s.tradeService().Withdraw(ctx, userFrom(ctx), r.LeagueId, r.TradeId)
	body, p, err := s.tradeOp(ctx, v, t, err)
	if p != nil || err != nil {
		return fail[WithdrawTradedefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return WithdrawTrade200JSONResponse(body), nil
}

func (s *Server) VoteOnTrade(ctx context.Context, r VoteOnTradeRequestObject) (VoteOnTradeResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), trd(r.TradeId)); d != "" {
		return VoteOnTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[VoteOnTradedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "vote is required.")), nil
	}
	v, t, err := s.tradeService().Vote(ctx, userFrom(ctx), r.LeagueId, r.TradeId, string(r.Body.Vote))
	body, p, err := s.tradeOp(ctx, v, t, err)
	if p != nil || err != nil {
		return fail[VoteOnTradedefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return VoteOnTrade200JSONResponse(body), nil
}

func (s *Server) ReviewTrade(ctx context.Context, r ReviewTradeRequestObject) (ReviewTradeResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), trd(r.TradeId)); d != "" {
		return ReviewTrade400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[ReviewTradedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "decision is required.")), nil
	}
	reason := ""
	if r.Body.Reason != nil {
		reason = *r.Body.Reason
	}
	v, t, err := s.tradeService().Review(ctx, userFrom(ctx), r.LeagueId, r.TradeId, string(r.Body.Decision), reason)
	body, p, err := s.tradeOp(ctx, v, t, err)
	if p != nil || err != nil {
		return fail[ReviewTradedefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return ReviewTrade200JSONResponse(body), nil
}
