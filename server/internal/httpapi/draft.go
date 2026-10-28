package httpapi

import (
	"cmp"
	"context"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// The draft room (package draft). Handlers check ids, call the service and
// render; the service owns every rule.

func (s *Server) notifier() league.Notifier {
	if s.notify == nil {
		return league.NopNotifier{}
	}
	return s.notify
}

// drafts is the draft service on the server's current stores and clock.
func (s *Server) drafts() *draft.Service {
	return draft.New(s.leagues.Repo(), s.stats, s.now, s.notifier())
}

func pickBody(v *draft.View, names names, p league.Pick) DraftPick {
	t, _ := v.League.Team(p.TeamID)
	_, round, in := draft.Slot(v.Draft.Order, p.Overall)
	method := DraftPickMethod(p.Method)
	if !method.Valid() {
		method = DraftPickMethodRank
	}
	return DraftPick{
		Id: p.ID, Overall: p.Overall, Round: round, PickInRound: in, Team: teamRef(t, names),
		Player: playerRef(v.World, p.PlayerID), Method: method, PickedAt: p.PickedAt, MissedClock: p.MissedClock,
	}
}

func clockBody(v *draft.View, names names, c *draft.Clock) *DraftClock {
	if c == nil {
		return nil
	}
	t, _ := v.League.Team(c.TeamID)
	return &DraftClock{
		Overall: c.Overall, Round: c.Round, PickInRound: c.InRound, Team: teamRef(t, names),
		StartedAt: c.StartedAt, DeadlineAt: c.DeadlineAt,
	}
}

func draftBody(v *draft.View, names names) Draft {
	d := v.Draft
	body := Draft{
		Id: d.ID, Status: DraftStatus(d.Status), Type: DraftTypeSnake, Rounds: v.Rounds,
		PickTimerSeconds: d.PickTimerS, StartsAt: v.League.Settings.DraftStartsAt,
		Order: []DraftSlot{}, Picks: []DraftPick{}, CompletedAt: d.CompletedAt,
		CurrentPick: clockBody(v, names, v.Current()),
	}
	if d.Status == league.DraftScheduled {
		body.PickTimerSeconds = v.League.Settings.PickTimerSeconds
	}
	for i, id := range d.Order {
		t, _ := v.League.Team(id)
		body.Order = append(body.Order, DraftSlot{Slot: i + 1, Team: teamRef(t, names), Autopick: d.Autopick[id] || t.OwnerID == ""})
	}
	for _, p := range d.Picks {
		body.Picks = append(body.Picks, pickBody(v, names, p))
	}
	return body
}

// draftResponse renders a view with the names of its league.
func (s *Server) draftResponse(ctx context.Context, v *draft.View) (DraftResponse, error) {
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return DraftResponse{}, err
	}
	return DraftResponse{Data: draftBody(v, n), Meta: metaAt(s.now())}, nil
}

func (s *Server) GetDraft(ctx context.Context, r GetDraftRequestObject) (GetDraftResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return GetDraft400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, err := s.drafts().Get(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[GetDraftdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.draftResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return GetDraft200JSONResponse(body), nil
}

func (s *Server) UpdateDraft(ctx context.Context, r UpdateDraftRequestObject) (UpdateDraftResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return UpdateDraft400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	b := r.Body
	// startsAt: null unschedules the draft; omitted keeps the time.
	clear := b != nil && b.StartsAt == nil && explicitNull(ctx, "startsAt")
	if b == nil || (b.Order == nil && b.RandomizeOrder == nil && b.PickTimerSeconds == nil && b.StartsAt == nil && !clear) {
		return fail[UpdateDraftdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "Send startsAt, pickTimerSeconds, order or randomizeOrder.")), nil
	}
	in := draft.Update{StartsAt: b.StartsAt, ClearStartsAt: clear, PickTimer: b.PickTimerSeconds, Randomize: b.RandomizeOrder != nil && *b.RandomizeOrder}
	if b.Order != nil {
		for _, id := range *b.Order {
			if d := tm(id); d != "" {
				return UpdateDraft400ApplicationProblemPlusJSONResponse{badRequest(ctx, "order: "+d)}, nil
			}
		}
		in.Order = append([]string{}, *b.Order...)
	}
	v, err := s.drafts().UpdateDraft(ctx, userFrom(ctx), r.LeagueId, in)
	if err != nil {
		return failLeague[UpdateDraftdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.draftResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return UpdateDraft200JSONResponse(body), nil
}

func (s *Server) StartDraft(ctx context.Context, r StartDraftRequestObject) (StartDraftResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return StartDraft400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, err := s.drafts().Start(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[StartDraftdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.draftResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return StartDraft200JSONResponse(body), nil
}

func (s *Server) PauseDraft(ctx context.Context, r PauseDraftRequestObject) (PauseDraftResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return PauseDraft400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, err := s.drafts().Pause(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[PauseDraftdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.draftResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return PauseDraft200JSONResponse(body), nil
}

func (s *Server) ResumeDraft(ctx context.Context, r ResumeDraftRequestObject) (ResumeDraftResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return ResumeDraft400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, err := s.drafts().Resume(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[ResumeDraftdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.draftResponse(ctx, v)
	if err != nil {
		return nil, err
	}
	return ResumeDraft200JSONResponse(body), nil
}

func (s *Server) MakeDraftPick(ctx context.Context, r MakeDraftPickRequestObject) (MakeDraftPickResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Body != nil {
		d = badID("playerId", "plr", r.Body.PlayerId)
	}
	if d != "" {
		return MakeDraftPick400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[MakeDraftPickdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "playerId is required.")), nil
	}
	v, pk, err := s.drafts().Pick(ctx, userFrom(ctx), r.LeagueId, r.Body.PlayerId)
	if err != nil {
		return failLeague[MakeDraftPickdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, v.League)
	if err != nil {
		return nil, err
	}
	return MakeDraftPick201JSONResponse{
		Body:    DraftPickResponse{Data: pickBody(v, n, pk), Meta: metaAt(s.now())},
		Headers: MakeDraftPick201ResponseHeaders{Location: "/v1/leagues/" + r.LeagueId + "/draft"},
	}, nil
}

func (s *Server) queueResponse(ctx context.Context, q *draft.Queue) (DraftQueueResponse, error) {
	n, err := s.namesFor(ctx, q.League)
	if err != nil {
		return DraftQueueResponse{}, err
	}
	t, _ := q.League.Team(q.TeamID)
	body := DraftQueue{Team: teamRef(t, n), Autopick: q.Autopick, AutopickOnTimeout: q.OnTimeout, Players: []DraftQueueItem{}}
	for _, id := range q.Players {
		body.Players = append(body.Players, DraftQueueItem{Player: playerRef(q.World, id), DraftRank: q.World.Players[id].DraftRank})
	}
	return DraftQueueResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) GetDraftQueue(ctx context.Context, r GetDraftQueueRequestObject) (GetDraftQueueResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return GetDraftQueue400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	q, err := s.drafts().GetQueue(ctx, userFrom(ctx), r.LeagueId)
	if err != nil {
		return failLeague[GetDraftQueuedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.queueResponse(ctx, q)
	if err != nil {
		return nil, err
	}
	return GetDraftQueue200JSONResponse(body), nil
}

func (s *Server) SetDraftQueue(ctx context.Context, r SetDraftQueueRequestObject) (SetDraftQueueResponseObject, error) {
	d := lg(r.LeagueId)
	if d == "" && r.Body != nil {
		for _, id := range r.Body.PlayerIds {
			d = cmp.Or(d, badID("playerIds", "plr", id))
		}
	}
	if d != "" {
		return SetDraftQueue400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil {
		return fail[SetDraftQueuedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "playerIds is required.")), nil
	}
	q, err := s.drafts().SetQueue(ctx, userFrom(ctx), r.LeagueId, r.Body.PlayerIds)
	if err != nil {
		return failLeague[SetDraftQueuedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.queueResponse(ctx, q)
	if err != nil {
		return nil, err
	}
	return SetDraftQueue200JSONResponse(body), nil
}

func (s *Server) SetDraftAutopick(ctx context.Context, r SetDraftAutopickRequestObject) (SetDraftAutopickResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return SetDraftAutopick400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if r.Body == nil || (r.Body.Enabled == nil && r.Body.OnTimeout == nil) {
		return fail[SetDraftAutopickdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "Send enabled, onTimeout or both.")), nil
	}
	var (
		q   *draft.Queue
		err error
	)
	// The switch first, so a team turning both on picks with it set.
	if r.Body.OnTimeout != nil {
		q, err = s.drafts().SetAutopickOnTimeout(ctx, userFrom(ctx), r.LeagueId, *r.Body.OnTimeout)
	}
	if err == nil && r.Body.Enabled != nil {
		q, err = s.drafts().SetAutopick(ctx, userFrom(ctx), r.LeagueId, *r.Body.Enabled)
	}
	if err != nil {
		return failLeague[SetDraftAutopickdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.queueResponse(ctx, q)
	if err != nil {
		return nil, err
	}
	return SetDraftAutopick200JSONResponse(body), nil
}
