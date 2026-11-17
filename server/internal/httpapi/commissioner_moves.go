package httpapi

import (
	"cmp"
	"context"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// Commissioner moves (docs/adr/0017): the log of what the commissioner did
// with powers a manager does not have, and each member's seen-up-to marker.

// moveBody renders an entry for a viewer: unseen is theirs.
func moveBody(l *league.League, n names, e league.AuditEntry, unseen bool) CommissionerMove {
	teams := []TeamRef{}
	for _, id := range e.TeamIDs {
		if t, ok := l.Team(id); ok {
			teams = append(teams, teamRef(t, n))
		}
	}
	// A commissioner whose account was deleted is no one: Postgres clears
	// actor_id, and the memory store keeps an id nobody has (no name).
	var actor *UserRef
	if name, ok := n[e.ActorID]; e.ActorID != "" && ok {
		actor = &UserRef{Id: e.ActorID, DisplayName: name}
	}
	m := CommissionerMove{
		Id: e.ID, Action: CommissionerMoveAction(e.Action), Summary: e.Summary, Actor: actor, Teams: teams,
		Unseen: unseen, CreatedAt: e.At,
	}
	if e.Before != nil || e.After != nil {
		d := CommissionerMoveDetails{}
		if e.Before != nil {
			d.Before = &e.Before
		}
		if e.After != nil {
			d.After = &e.After
		}
		m.Details = &d
	}
	return m
}

// moveNames loads the display names the entries need: the league's
// members, and commissioners who have since handed the role over.
func (s *Server) moveNames(ctx context.Context, l *league.League, es []league.AuditEntry) (names, error) {
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	var more []string
	for _, e := range es {
		if _, ok := n[e.ActorID]; !ok && e.ActorID != "" {
			more = append(more, e.ActorID)
		}
	}
	if len(more) == 0 {
		return n, nil
	}
	extra, err := s.auth.DisplayNames(ctx, more)
	if err != nil {
		return nil, err
	}
	for id, name := range extra {
		n[id] = name
	}
	return n, nil
}

func (s *Server) summaryResponse(ctx context.Context, sum league.AuditSummary, l *league.League) (CommissionerMovesSummaryResponse, error) {
	body := CommissionerMovesSummary{}
	body.UnseenCount = sum.Unseen
	if sum.Latest != nil {
		n, err := s.moveNames(ctx, l, []league.AuditEntry{*sum.Latest})
		if err != nil {
			return CommissionerMovesSummaryResponse{}, err
		}
		m := moveBody(l, n, *sum.Latest, true)
		body.Latest = &m
	}
	return CommissionerMovesSummaryResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) ListCommissionerMoves(ctx context.Context, r ListCommissionerMovesRequestObject) (ListCommissionerMovesResponseObject, error) {
	limit, limitErr := limitOf(r.Params.Limit)
	offset, cursorOK := decodeCursor(r.Params.Cursor)
	d := cmp.Or(lg(r.LeagueId), limitErr)
	if d == "" && !cursorOK {
		d = "cursor is not valid; pass meta.nextCursor back unchanged."
	}
	if d != "" {
		return ListCommissionerMoves400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	page, err := s.leagues.CommissionerMoves(ctx, userFrom(ctx), r.LeagueId, offset, limit)
	if err != nil {
		return failLeague[ListCommissionerMovesdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	var next *string
	if len(page.Entries) > limit {
		page.Entries = page.Entries[:limit]
		c := encodeCursor(offset + limit)
		next = &c
	}
	n, err := s.moveNames(ctx, page.League, page.Entries)
	if err != nil {
		return nil, err
	}
	out := []CommissionerMove{}
	for _, e := range page.Entries {
		out = append(out, moveBody(page.League, n, e, page.Unseen(e)))
	}
	return ListCommissionerMoves200JSONResponse{Data: out, Meta: pageMeta(metaAt(s.now()), next)}, nil
}

func (s *Server) GetCommissionerMovesSummary(ctx context.Context, r GetCommissionerMovesSummaryRequestObject) (GetCommissionerMovesSummaryResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return GetCommissionerMovesSummary400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	sum, l, err := s.leagues.CommissionerMovesSummary(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[GetCommissionerMovesSummarydefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.summaryResponse(ctx, sum, l)
	if err != nil {
		return nil, err
	}
	return GetCommissionerMovesSummary200JSONResponse(body), nil
}

func (s *Server) MarkCommissionerMovesSeen(ctx context.Context, r MarkCommissionerMovesSeenRequestObject) (MarkCommissionerMovesSeenResponseObject, error) {
	d := lg(r.LeagueId)
	through := ""
	if r.Body != nil && r.Body.ThroughId != nil {
		through = *r.Body.ThroughId
		if !ids.Valid("cmv", through) {
			d = cmp.Or(d, "throughId must be a commissioner move id.")
		}
	}
	if d != "" {
		return MarkCommissionerMovesSeen400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	sum, l, err := s.leagues.MarkCommissionerMovesSeen(ctx, userFrom(ctx), r.LeagueId, through)
	if err != nil {
		return failLeague[MarkCommissionerMovesSeendefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.summaryResponse(ctx, sum, l)
	if err != nil {
		return nil, err
	}
	return MarkCommissionerMovesSeen200JSONResponse(body), nil
}
