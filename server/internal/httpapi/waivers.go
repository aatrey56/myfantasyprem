package httpapi

import (
	"cmp"
	"context"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// Waivers and free agents (package waivers).

func (s *Server) waiverService() *waivers.Service {
	return waivers.New(s.leagues.Repo(), s.stats, s.now, s.notifier())
}

func (s *Server) claimBody(v *waivers.View, n names, c league.Claim) WaiverClaim {
	t, _ := v.League.Team(c.TeamID)
	body := WaiverClaim{
		Id: c.ID, Team: teamRef(t, n), Add: playerRef(v.World, c.AddPlayerID), Drop: playerRef(v.World, c.DropPlayerID),
		Order: max(c.Order, 1), Status: WaiverClaimStatus(c.Status), Gameweek: c.Gameweek,
		ProcessedAt: c.ProcessedAt, CreatedAt: c.CreatedAt,
	}
	if g, ok := v.World.Gameweeks[c.Gameweek]; ok {
		body.ProcessAt = g.WaiverDeadlineAt.UTC()
	}
	if c.FailureReason != "" {
		f := WaiverClaimFailure(c.FailureReason)
		body.FailureReason = &f
	}
	return body
}

func (s *Server) GetWaivers(ctx context.Context, r GetWaiversRequestObject) (GetWaiversResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return GetWaivers400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, err := s.waiverService().Get(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[GetWaiversdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return nil, err
	}
	body := WaiverState{Type: WaiverStateTypeRollingPriority, Order: WaiverOrderRule(v.League.Settings.WaiverRule()),
		Priority: []WaiverPriorityRow{}, LastRunAt: v.Cycle.LastRunAt}
	// The order the next run starts from under the league's rule.
	var order []string
	switch {
	case v.League.Status != league.StatusPreDraft && v.League.Status != league.StatusDrafting:
		order = league.WaiverOrder(v.State, v.World, v.Cycle.RunGW)
	case v.State.Draft != nil && len(v.State.Draft.Order) > 0:
		order = league.PreStandingsOrder(v.State)
	default:
		// Before the draft order is set: the reverse of the join order.
		for i := len(v.League.Teams) - 1; i >= 0; i-- {
			order = append(order, v.League.Teams[i].ID)
		}
	}
	for i, id := range order {
		t, _ := v.League.Team(id)
		body.Priority = append(body.Priority, WaiverPriorityRow{Rank: i + 1, Team: teamRef(t, n)})
	}
	if v.League.Status == league.StatusInSeason && v.Cycle.RunGW <= 38 {
		at, gw := v.Cycle.RunAt, v.Cycle.RunGW
		body.NextRunAt, body.NextRunGameweek = &at, &gw
	}
	return GetWaivers200JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) claimList(ctx context.Context, v *waivers.View, claims []league.Claim) (WaiverClaimListResponse, error) {
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return WaiverClaimListResponse{}, err
	}
	out := []WaiverClaim{}
	for _, c := range claims {
		out = append(out, s.claimBody(v, n, c))
	}
	return WaiverClaimListResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) ListWaiverClaims(ctx context.Context, r ListWaiverClaimsRequestObject) (ListWaiverClaimsResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Params.Status != nil && !r.Params.Status.Valid() {
		d = "status must be pending or processed."
	}
	if d != "" {
		return ListWaiverClaims400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	processed := r.Params.Status != nil && *r.Params.Status == ListWaiverClaimsParamsStatusProcessed
	v, claims, err := s.waiverService().Claims(ctx, userFrom(ctx), r.LeagueId, processed)
	if err != nil {
		return failLeague[ListWaiverClaimsdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.claimList(ctx, v, claims)
	if err != nil {
		return nil, err
	}
	return ListWaiverClaims200JSONResponse(body), nil
}

func (s *Server) CreateWaiverClaim(ctx context.Context, r CreateWaiverClaimRequestObject) (CreateWaiverClaimResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Body != nil {
		d = cmp.Or(badID("addPlayerId", "plr", r.Body.AddPlayerId), badID("dropPlayerId", "plr", r.Body.DropPlayerId))
	}
	if d != "" {
		return CreateWaiverClaim400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[CreateWaiverClaimdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "addPlayerId and dropPlayerId are required.")), nil
	}
	v, c, err := s.waiverService().CreateClaim(ctx, userFrom(ctx), r.LeagueId, r.Body.AddPlayerId, r.Body.DropPlayerId)
	if err != nil {
		return failLeague[CreateWaiverClaimdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return nil, err
	}
	return CreateWaiverClaim201JSONResponse{
		Body:    WaiverClaimResponse{Data: s.claimBody(v, n, c), Meta: metaAt(s.now())},
		Headers: CreateWaiverClaim201ResponseHeaders{Location: "/v1/leagues/" + r.LeagueId + "/waivers/claims/" + c.ID},
	}, nil
}

func (s *Server) UpdateWaiverClaim(ctx context.Context, r UpdateWaiverClaimRequestObject) (UpdateWaiverClaimResponseObject, error) {
	d := cmp.Or(lg(r.LeagueId), wv(r.ClaimId))
	if d == "" && r.Body != nil {
		d = badID("dropPlayerId", "plr", r.Body.DropPlayerId)
	}
	if d != "" {
		return UpdateWaiverClaim400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[UpdateWaiverClaimdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "dropPlayerId is required.")), nil
	}
	v, c, err := s.waiverService().UpdateClaim(ctx, userFrom(ctx), r.LeagueId, r.ClaimId, r.Body.DropPlayerId)
	if err != nil {
		return failLeague[UpdateWaiverClaimdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return nil, err
	}
	return UpdateWaiverClaim200JSONResponse{Data: s.claimBody(v, n, c), Meta: metaAt(s.now())}, nil
}

func (s *Server) CancelWaiverClaim(ctx context.Context, r CancelWaiverClaimRequestObject) (CancelWaiverClaimResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), wv(r.ClaimId)); d != "" {
		return CancelWaiverClaim400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if err := s.waiverService().CancelClaim(ctx, userFrom(ctx), r.LeagueId, r.ClaimId); err != nil {
		return failLeague[CancelWaiverClaimdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	return CancelWaiverClaim204Response{}, nil
}

func (s *Server) ReorderWaiverClaims(ctx context.Context, r ReorderWaiverClaimsRequestObject) (ReorderWaiverClaimsResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Body != nil {
		for _, id := range r.Body.ClaimIds {
			d = cmp.Or(d, badID("claimIds", "wv", id))
		}
	}
	if d != "" {
		return ReorderWaiverClaims400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[ReorderWaiverClaimsdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "claimIds is required.")), nil
	}
	v, claims, err := s.waiverService().Reorder(ctx, userFrom(ctx), r.LeagueId, r.Body.ClaimIds)
	if err != nil {
		return failLeague[ReorderWaiverClaimsdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.claimList(ctx, v, claims)
	if err != nil {
		return nil, err
	}
	return ReorderWaiverClaims200JSONResponse(body), nil
}

func (s *Server) AddFreeAgent(ctx context.Context, r AddFreeAgentRequestObject) (AddFreeAgentResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Body != nil {
		d = cmp.Or(badID("addPlayerId", "plr", r.Body.AddPlayerId), badID("dropPlayerId", "plr", r.Body.DropPlayerId))
	}
	if d != "" {
		return AddFreeAgent400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[AddFreeAgentdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "addPlayerId and dropPlayerId are required.")), nil
	}
	v, txn, err := s.waiverService().AddFreeAgent(ctx, userFrom(ctx), r.LeagueId, r.Body.AddPlayerId, r.Body.DropPlayerId)
	if err != nil {
		return failLeague[AddFreeAgentdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return nil, err
	}
	body, err := freeAgentBody(v.League, v.World, n, txn)
	if err != nil {
		return nil, err
	}
	return AddFreeAgent201JSONResponse{
		Body:    FreeAgentMoveResponse{Data: body, Meta: metaAt(s.now())},
		Headers: AddFreeAgent201ResponseHeaders{Location: "/v1/leagues/" + r.LeagueId + "/transactions"},
	}, nil
}

// ---------------------------------------------------------------- feed

func claimResult(x league.Txn) (ClaimResult, error) {
	var r ClaimResult
	if x.Approved {
		return r, r.FromClaimApproved(ClaimApproved{Outcome: Approved})
	}
	reason := ClaimDeniedReason(x.Reason)
	if !reason.Valid() {
		reason = ClaimDeniedReasonPlayerInUnavailable
	}
	return r, r.FromClaimDenied(ClaimDenied{Outcome: Denied, Reason: reason})
}

func freeAgentBody(l *league.League, w *league.World, n names, x league.Txn) (FreeAgentTransaction, error) {
	t, _ := l.Team(x.TeamID)
	res, err := claimResult(x)
	return FreeAgentTransaction{
		Id: x.ID, Kind: FreeAgentTransactionKindFreeAgent, OccurredAt: x.OccurredAt, Gameweek: x.Gameweek,
		Team: teamRef(t, n), PlayerIn: playerRef(w, x.PlayerIn), PlayerOut: playerRef(w, x.PlayerOut), Result: res,
		SourceState: sourceState(x),
	}, err
}

// sourceState is an imported entry's raw FPL code (support only), or nil.
func sourceState(x league.Txn) *string {
	if x.SourceState == "" {
		return nil
	}
	s := x.SourceState
	return &s
}

func playerRefs(w *league.World, ids []string) []PlayerRef {
	out := []PlayerRef{}
	for _, id := range ids {
		out = append(out, playerRef(w, id))
	}
	return out
}

// transactionBody renders one feed entry.
func transactionBody(l *league.League, w *league.World, n names, x league.Txn) (Transaction, error) {
	var out Transaction
	switch x.Kind {
	case league.TxnWaiver:
		t, _ := l.Team(x.TeamID)
		res, err := claimResult(x)
		if err != nil {
			return out, err
		}
		return out, out.FromWaiverTransaction(WaiverTransaction{
			Id: x.ID, Kind: WaiverTransactionKindWaiver, OccurredAt: x.OccurredAt, Gameweek: x.Gameweek,
			Team: teamRef(t, n), PlayerIn: playerRef(w, x.PlayerIn), PlayerOut: playerRef(w, x.PlayerOut),
			Result: res, Priority: max(x.Priority, 1), SourceState: sourceState(x),
		})
	case league.TxnFreeAgent:
		body, err := freeAgentBody(l, w, n, x)
		if err != nil {
			return out, err
		}
		return out, out.FromFreeAgentTransaction(body)
	case league.TxnDraftAutopick:
		t, _ := l.Team(x.TeamID)
		method := DraftPickMethod(x.Method)
		if !method.Valid() {
			method = DraftPickMethodRank
		}
		return out, out.FromDraftAutopickTransaction(DraftAutopickTransaction{
			Id: x.ID, Kind: DraftAutopickTransactionKindDraftAutopick, OccurredAt: x.OccurredAt, Gameweek: max(x.Gameweek, 1),
			Team: teamRef(t, n), Player: playerRef(w, x.PlayerIn), Overall: max(x.Overall, 1),
			Round: (max(x.Overall, 1)-1)/max(len(l.Teams), 1) + 1, Method: method,
		})
	default:
		from, _ := l.Team(x.OfferingTeamID)
		to, _ := l.Team(x.ReceivingTeamID)
		status := TradeStatus(x.Status)
		if !status.Valid() {
			status = TradeStatusUnknown
		}
		return out, out.FromTradeTransaction(TradeTransaction{
			Id: x.ID, Kind: TradeTransactionKindTrade, OccurredAt: x.OccurredAt, Gameweek: x.Gameweek, Status: status,
			OfferingTeam: teamRef(from, n), ReceivingTeam: teamRef(to, n),
			Offered: playerRefs(w, x.Offered), Requested: playerRefs(w, x.Requested), SourceState: sourceState(x),
		})
	}
}
