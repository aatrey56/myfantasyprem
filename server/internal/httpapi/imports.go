package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// The FPL Draft import, sync and switch-over (docs/adr/0012) run on
// mirror.Service; the claim links on league.Service.

// importRetryAfter is the polling hint for a new import, in seconds.
const importRetryAfter = 2

// upstreamProblem is the 502 for FPL failures.
func upstreamProblem(ctx context.Context) Problem {
	return newProblem(502, "upstream-error", "FPL unavailable",
		"FPL Draft could not be reached or sent something unexpected. Try again in a few minutes.", RequestIDFrom(ctx))
}

func importsUnavailable(ctx context.Context) Problem {
	return newProblem(503, "imports-unavailable", "Import unavailable", "Importing FPL Draft leagues is not set up on this server.",
		RequestIDFrom(ctx))
}

// failMirror maps a mirror error: league errors as usual, FPL failures to
// 502, no season data to 503; anything else is a 500.
func failMirror[T problemResp](ctx context.Context, err error) (T, error) {
	if errors.Is(err, provider.ErrUpstream) {
		return fail[T](upstreamProblem(ctx)), nil
	}
	return failLeague[T](ctx, err)
}

// tooManyImports is the 429 when a user previews or imports too often;
// zero when allowed.
func (s *Server) tooManyImports(ctx context.Context) (TooManyRequestsApplicationProblemPlusJSONResponse, bool) {
	ok, wait := s.importLimit.allow(userFrom(ctx), s.now())
	if ok {
		return TooManyRequestsApplicationProblemPlusJSONResponse{}, false
	}
	secs := retryAfter(wait)
	return TooManyRequestsApplicationProblemPlusJSONResponse{
		Body: newProblem(429, "rate-limited", "Too many requests",
			"Slow down and try again in "+strconv.Itoa(secs)+" seconds.", RequestIDFrom(ctx)),
		Headers: TooManyRequestsResponseHeaders{RetryAfter: secs},
	}, true
}

// decodeImportRequest reads the ImportRequest oneOf. The generated union only
// holds the raw JSON, so it checks the shape itself: a JSON object with
// exactly one of draftLeagueUrl and fplLeagueId, and seat (0 when missing).
func decodeImportRequest(body *ImportRequest) (draftLeagueURL *string, fplLeagueID *int64, seat int, err error) {
	if body == nil {
		return nil, nil, 0, errors.New("A JSON request body is required.")
	}
	raw, err := body.MarshalJSON()
	if err != nil {
		return nil, nil, 0, errors.New("The request body is not valid JSON.")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return nil, nil, 0, errors.New("The request body must be a JSON object.")
	}
	for name := range fields {
		if name != "draftLeagueUrl" && name != "fplLeagueId" && name != "seat" {
			return nil, nil, 0, errors.New("Unknown field " + strconv.Quote(name) + "; send draftLeagueUrl or fplLeagueId, and seat.")
		}
	}
	if v, ok := fields["draftLeagueUrl"]; ok {
		var u string
		if err := json.Unmarshal(v, &u); err != nil {
			return nil, nil, 0, errors.New("draftLeagueUrl must be a string.")
		}
		draftLeagueURL = &u
	}
	if v, ok := fields["fplLeagueId"]; ok {
		var n int64
		if err := json.Unmarshal(v, &n); err != nil {
			return nil, nil, 0, errors.New("fplLeagueId must be an integer.")
		}
		fplLeagueID = &n
	}
	v, ok := fields["seat"]
	if !ok {
		return draftLeagueURL, fplLeagueID, 0, nil
	}
	if err := json.Unmarshal(v, &seat); err != nil || seat < 1 || seat > league.MaxTeams {
		return nil, nil, 0, errors.New("seat must be an integer from 1 to 20.")
	}
	return draftLeagueURL, fplLeagueID, seat, nil
}

// classicLeagueURLProblem is the 422 for classic FPL links.
func classicLeagueURLProblem(ctx context.Context) Problem {
	return newProblem(422, "classic-league-url", "Classic FPL league link",
		"That link is for a classic FPL league (fantasy.premierleague.com). "+
			"Draft and classic league ids overlap, so importing it could mirror the wrong league. "+
			"Paste your league link from draft.premierleague.com instead.",
		RequestIDFrom(ctx))
}

// importProblemStatus is the status a failed import's problem shows.
var importProblemStatus = map[string]int{
	"upstream-error": 502, "unsupported-league": 422, "draft-not-complete": 422,
	"stats-not-ready": 503, "import-unavailable": 409,
}

func importBody(imp mirror.Import) Import {
	out := Import{
		Id: imp.ID, FplLeagueId: imp.FPLLeagueID, Status: ImportStatus(imp.Status),
		Progress:  ImportProgress{Done: imp.Done, Total: imp.Total},
		CreatedAt: imp.CreatedAt, UpdatedAt: imp.UpdatedAt,
	}
	if imp.Error != nil {
		p := newProblem(cmp.Or(importProblemStatus[imp.Error.Slug], 500), imp.Error.Slug, imp.Error.Title, imp.Error.Detail, "")
		out.Error = &p
	}
	if imp.Status == mirror.ImportSucceeded {
		id := imp.LeagueID
		out.LeagueId = &id
	}
	return out
}

func (s *Server) CreateImport(ctx context.Context, req CreateImportRequestObject) (CreateImportResponseObject, error) {
	draftURL, fplID, seat, err := decodeImportRequest(req.Body)
	var leagueID int64
	if err == nil {
		leagueID, err = resolveLeagueRef(draftURL, fplID)
	}
	if errors.Is(err, errClassicLeagueURL) {
		return CreateImport422ApplicationProblemPlusJSONResponse{UnprocessableApplicationProblemPlusJSONResponse(classicLeagueURLProblem(ctx))}, nil
	}
	if err == nil && seat == 0 {
		err = errors.New("seat is required: your team's number in the preview.")
	}
	if err != nil {
		return CreateImport400ApplicationProblemPlusJSONResponse{badRequest(ctx, err.Error())}, nil
	}
	if s.mirror == nil {
		return fail[CreateImportdefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	uid := userFrom(ctx)
	if prev, err := s.mirror.Store.LatestImport(ctx, uid, leagueID); err != nil {
		return nil, err
	} else if prev == nil || !(prev.Active() || prev.Status == mirror.ImportSucceeded) {
		if p, limited := s.tooManyImports(ctx); limited {
			return CreateImport429ApplicationProblemPlusJSONResponse{p}, nil
		}
	}
	imp, existing, err := s.mirror.StartImport(ctx, uid, leagueID, seat)
	if err != nil {
		return failMirror[CreateImportdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body := ImportResponse{Data: importBody(imp), Meta: metaAt(s.now())}
	if existing {
		return CreateImport200JSONResponse(body), nil
	}
	return CreateImport202JSONResponse{
		Body:    body,
		Headers: CreateImport202ResponseHeaders{Location: "/v1/imports/" + imp.ID, RetryAfter: importRetryAfter},
	}, nil
}

func (s *Server) GetImport(ctx context.Context, req GetImportRequestObject) (GetImportResponseObject, error) {
	if d := badID("importId", "imp", req.ImportId); d != "" {
		return GetImport400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if s.mirror == nil {
		return fail[GetImportdefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	imp, err := s.mirror.Import(ctx, userFrom(ctx), req.ImportId)
	if err != nil {
		return failMirror[GetImportdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	return GetImport200JSONResponse(ImportResponse{Data: importBody(imp), Meta: metaAt(s.now())}), nil
}

// previewTimeout bounds a preview's FPL lookup. The mirror's client is
// rate paced and shared with syncs, so the lookup can queue for a while;
// past this it answers 502 rather than holding the request open. The app
// waits a little longer (IMPORT_PREVIEW_TIMEOUT in apps/app), and the
// route's write deadline is extended to match (slowRouteWriteTimeout).
var previewTimeout = 35 * time.Second

// previewPath is the route whose write deadline outlasts previewTimeout.
const previewPath = "/v1/imports/preview"

// slowRouteWriteTimeout replaces the server's 15 s WriteTimeout for
// previewPath, so its bounded FPL lookup can still write the answer.
const slowRouteWriteTimeout = 40 * time.Second

// withSlowRouteDeadline extends the write deadline of a preview request.
func withSlowRouteDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == previewPath {
			_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(slowRouteWriteTimeout))
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) PreviewImport(ctx context.Context, req PreviewImportRequestObject) (PreviewImportResponseObject, error) {
	id, err := resolveLeagueRef(req.Params.DraftLeagueUrl, req.Params.FplLeagueId)
	if errors.Is(err, errClassicLeagueURL) {
		return PreviewImport422ApplicationProblemPlusJSONResponse{UnprocessableApplicationProblemPlusJSONResponse(classicLeagueURLProblem(ctx))}, nil
	}
	if err != nil {
		return PreviewImport400ApplicationProblemPlusJSONResponse{badRequest(ctx, err.Error())}, nil
	}
	if s.mirror == nil {
		return fail[PreviewImportdefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	if p, limited := s.tooManyImports(ctx); limited {
		return PreviewImport429ApplicationProblemPlusJSONResponse{p}, nil
	}
	lookup, cancel := context.WithTimeout(ctx, previewTimeout)
	defer cancel()
	p, err := s.mirror.Preview(lookup, userFrom(ctx), id)
	if err != nil && ctx.Err() == nil && errors.Is(lookup.Err(), context.DeadlineExceeded) {
		return fail[PreviewImportdefaultApplicationProblemPlusJSONResponse](upstreamProblem(ctx)), nil
	}
	if err != nil {
		return failMirror[PreviewImportdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	out := ImportPreview{
		FplLeagueId: p.FPLLeagueID, Name: p.Name, TeamCount: len(p.Seats), Seats: []ImportPreviewSeat{},
		CurrentGameweek: p.CurrentGameweek, Scoring: ImportLeagueScoringClassic, DraftComplete: p.DraftComplete,
		Importable: p.Problem == nil,
	}
	if p.H2H {
		out.Scoring = ImportLeagueScoringH2h
	}
	for _, seat := range p.Seats {
		out.Seats = append(out.Seats, ImportPreviewSeat{Seat: seat.Seat, TeamName: seat.TeamName, ManagerName: seat.ManagerName})
	}
	if p.Problem != nil {
		prob, _ := leagueProblem(ctx, p.Problem)
		prob.Status = cmp.Or(importProblemStatus[p.Problem.Slug], prob.Status)
		out.Problem = &prob
	}
	if p.ImportedLeagueID != "" {
		out.AlreadyImportedLeagueId = &p.ImportedLeagueID
	}
	return PreviewImport200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

// syncBody renders an imported league's FPL status.
func (s *Server) syncBody(ctx context.Context, v *mirror.View) (LeagueSync, error) {
	l := v.League
	out := LeagueSync{FplLeagueId: l.FPLLeagueID}
	if r := v.Record; r != nil {
		out.LastSyncedAt, out.LastAttemptAt = r.LastSyncedAt, r.LastAttemptAt
		if r.Pending != "" && r.RequestedAt != nil {
			out.Pending = &SyncPending{Kind: SyncJobKind(r.Pending), RequestedAt: *r.RequestedAt}
		}
		if r.LastError != "" {
			out.LastError = &r.LastError
		}
		if r.Note != "" {
			out.Note = &r.Note
		}
	}
	if v.Report == nil && v.Shadow == nil {
		return out, nil
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return out, err
	}
	syncItem := func(it mirror.Item) SyncItem {
		item := SyncItem{Key: it.Key, Kind: SyncItemKind(it.Kind), Status: SyncItemStatus(it.Status), Ours: it.Ours, Theirs: it.Theirs}
		if it.Gameweek >= 1 && it.Gameweek <= 38 {
			gw := it.Gameweek
			item.Gameweek = &gw
		}
		if t, ok := l.Team(it.TeamID); ok && it.TeamID != "" {
			ref := teamRef(t, n)
			item.Team = &ref
		}
		if it.Note != "" {
			note := it.Note
			item.Note = &note
		}
		return item
	}
	if r := v.Shadow; r != nil {
		sh := ShadowCheck{CheckedAt: r.CheckedAt, Reason: r.Reason, Status: SyncReportStatus(r.Status),
			Mismatches: []SyncItem{}, Pending: []SyncItem{}}
		if r.Gameweek >= 1 && r.Gameweek <= 38 {
			gw := r.Gameweek
			sh.Gameweek = &gw
		}
		for _, it := range r.Items {
			switch it.Status {
			case mirror.ItemOK:
				sh.Counts.Ok++
			case mirror.ItemMismatch:
				sh.Counts.Mismatch++
				sh.Mismatches = append(sh.Mismatches, syncItem(it))
			case mirror.ItemPending:
				sh.Counts.Pending++
				sh.Pending = append(sh.Pending, syncItem(it))
			default:
				sh.Counts.Unverified++
			}
		}
		out.Shadow = &sh
	}
	if v.Report == nil {
		return out, nil
	}
	var by []string
	for _, e := range v.Explanations {
		by = append(by, e.ExplainedBy)
	}
	who, err := s.auth.DisplayNames(ctx, by)
	if err != nil {
		return out, err
	}
	explained := map[string]mirror.Explanation{}
	for _, e := range v.Explanations {
		explained[e.Key] = e
	}
	ver := SyncVerification{Status: SyncReportStatus(v.Report.Status), CheckedAt: v.Report.CheckedAt, Failing: []SyncItem{},
		Unexplained: len(v.Unexplained), Pending: []SyncItem{}}
	for _, it := range v.Report.Items {
		switch it.Status {
		case mirror.ItemOK:
			ver.Counts.Ok++
			continue
		case mirror.ItemPending:
			ver.Counts.Pending++
		case mirror.ItemMismatch:
			ver.Counts.Mismatch++
		default:
			ver.Counts.Unverified++
		}
		item := syncItem(it)
		if it.Status == mirror.ItemPending {
			ver.Pending = append(ver.Pending, item)
			continue
		}
		if e, ok := explained[it.Key]; ok && e.Ours == it.Ours && e.Theirs == it.Theirs {
			item.Explanation = &SyncExplanation{Note: e.Note, ExplainedBy: names(who).of(e.ExplainedBy), ExplainedAt: e.ExplainedAt}
		}
		ver.Failing = append(ver.Failing, item)
	}
	out.Verification = &ver
	return out, nil
}

func (s *Server) GetLeagueSync(ctx context.Context, req GetLeagueSyncRequestObject) (GetLeagueSyncResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[GetLeagueSyncdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if s.mirror == nil {
		return fail[GetLeagueSyncdefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	v, err := s.mirror.View(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failMirror[GetLeagueSyncdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.syncBody(ctx, v)
	if err != nil {
		return nil, err
	}
	return GetLeagueSync200JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) ResyncLeague(ctx context.Context, req ResyncLeagueRequestObject) (ResyncLeagueResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[ResyncLeaguedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if req.Body == nil || !req.Body.Confirm {
		return fail[ResyncLeaguedefaultApplicationProblemPlusJSONResponse](invalid(ctx,
			"A re-sync replaces every move made here since the last one: send confirm: true.",
			ProblemError{Code: "confirm_required", Message: "Confirm the re-sync.", Field: ptr("/confirm")})), nil
	}
	if s.mirror == nil {
		return fail[ResyncLeaguedefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	// Authorize first: the switch is only revealed to the league's commissioner.
	if slices.Contains(s.resyncDisabled, req.LeagueId) {
		if err := s.mirror.AuthorizeCommissioner(ctx, userFrom(ctx), req.LeagueId); err != nil {
			return failMirror[ResyncLeaguedefaultApplicationProblemPlusJSONResponse](ctx, err)
		}
		return fail[ResyncLeaguedefaultApplicationProblemPlusJSONResponse](newProblem(409, "resync-disabled", "Re-sync is turned off",
			"Re-syncing this league from FPL is turned off on the server.", RequestIDFrom(ctx))), nil
	}
	v, err := s.mirror.RequestResync(ctx, userFrom(ctx), req.LeagueId, bool(req.Body.Confirm))
	if err != nil {
		return failMirror[ResyncLeaguedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.syncBody(ctx, v)
	if err != nil {
		return nil, err
	}
	return ResyncLeague202JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) CheckLeagueSync(ctx context.Context, req CheckLeagueSyncRequestObject) (CheckLeagueSyncResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[CheckLeagueSyncdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if s.mirror == nil {
		return fail[CheckLeagueSyncdefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	v, err := s.mirror.RequestCheck(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failMirror[CheckLeagueSyncdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.syncBody(ctx, v)
	if err != nil {
		return nil, err
	}
	return CheckLeagueSync202JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) ExplainSyncItem(ctx context.Context, req ExplainSyncItemRequestObject) (ExplainSyncItemResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[ExplainSyncItemdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if req.Body == nil {
		return fail[ExplainSyncItemdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "key and note are required.")), nil
	}
	if s.mirror == nil {
		return fail[ExplainSyncItemdefaultApplicationProblemPlusJSONResponse](importsUnavailable(ctx)), nil
	}
	v, err := s.mirror.Explain(ctx, userFrom(ctx), req.LeagueId, req.Body.Key, req.Body.Note)
	if err != nil {
		return failMirror[ExplainSyncItemdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.syncBody(ctx, v)
	if err != nil {
		return nil, err
	}
	return ExplainSyncItem200JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) ListSeats(ctx context.Context, req ListSeatsRequestObject) (ListSeatsResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[ListSeatsdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	uid := userFrom(ctx)
	l, err := s.leagues.Get(ctx, uid, req.LeagueId)
	if err != nil {
		return failLeague[ListSeatsdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	if err := league.Commissioner(l, uid); err != nil {
		return failLeague[ListSeatsdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	if l.Source != league.SourceFPL {
		return fail[ListSeatsdefaultApplicationProblemPlusJSONResponse](newProblem(409, "not-imported", "Not an imported league",
			"This league was created here, not imported from FPL Draft.", RequestIDFrom(ctx))), nil
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	out := []Seat{}
	for _, t := range l.Teams {
		seat := Seat{Team: teamRef(t, n), FplManagerName: t.ManagerName, Claimed: t.OwnerID != ""}
		if t.OwnerID != "" {
			seat.Owner = &UserRef{Id: t.OwnerID, DisplayName: n.of(t.OwnerID)}
		} else if inv, ok := l.ClaimLink(t.ID); ok {
			body := s.inviteBody(inv, l, n)
			seat.Invite = &body
		}
		out = append(out, seat)
	}
	return ListSeats200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) ResendSeatInvite(ctx context.Context, req ResendSeatInviteRequestObject) (ResendSeatInviteResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("teamId", "tm", req.TeamId)); d != "" {
		return fail[ResendSeatInvitedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	uid := userFrom(ctx)
	inv, err := s.leagues.ClaimInvite(ctx, uid, req.LeagueId, req.TeamId)
	if err != nil {
		return failLeague[ResendSeatInvitedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	l, err := s.leagues.Get(ctx, uid, req.LeagueId)
	if err != nil {
		return failLeague[ResendSeatInvitedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	s.leagueChanged(ctx, l.ID)
	return ResendSeatInvite201JSONResponse{
		Body:    InviteResponse{Data: s.inviteBody(inv, l, n), Meta: metaAt(s.now())},
		Headers: ResendSeatInvite201ResponseHeaders{Location: "/v1/leagues/" + l.ID + "/invites/" + inv.ID},
	}, nil
}
