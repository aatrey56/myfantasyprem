package httpapi

import (
	"cmp"
	"context"
	"errors"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// problemResp is every operation's `default` response: a problem with any status.
type problemResp interface {
	~struct {
		Body       Problem
		StatusCode int
	}
}

// fail renders p as an operation's default problem response, using p.Status.
func fail[T problemResp](p Problem) T {
	return T{Body: p, StatusCode: p.Status}
}

// metaAt is the envelope meta for league state, which is always current.
func metaAt(now time.Time) Meta {
	return Meta{DataAsOf: now, ServerTime: now, Stale: false}
}

// leagueProblem maps a league.Error to a problem; anything else is a bug.
func leagueProblem(ctx context.Context, err error) (Problem, error) {
	var e *league.Error
	if !errors.As(err, &e) {
		return Problem{}, err
	}
	status := map[league.Kind]int{
		league.KindInvalid:   422,
		league.KindForbidden: 403,
		league.KindNotFound:  404,
		league.KindConflict:  409,
		league.KindGone:      410,
	}[e.Kind]
	p := newProblem(status, e.Slug, e.Title, e.Detail, RequestIDFrom(ctx))
	if e.LeagueID != "" {
		id := e.LeagueID
		p.LeagueId = &id
	}
	if e.ReopensAt != nil {
		at := e.ReopensAt.UTC()
		p.ReopensAt = &at
	}
	if len(e.Errors) > 0 {
		errs := make([]ProblemError, 0, len(e.Errors))
		for _, fe := range e.Errors {
			pe := ProblemError{Code: fe.Code, Message: fe.Message}
			if fe.Field != "" {
				pe.Field = ptr(fe.Field)
			}
			if fe.PlayerID != "" {
				pe.PlayerId = ptr(fe.PlayerID)
			}
			errs = append(errs, pe)
		}
		p.Errors = &errs
	}
	return p, nil
}

// leagueChanged reports league.updated (settings, members or names).
func (s *Server) leagueChanged(ctx context.Context, leagueID string) {
	s.notifier().Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeLeague})
}

// errorsIs503 reports an error that answers 503 (no season data yet).
func errorsIs503(err error) bool { return errors.Is(err, league.ErrNoSeasonData) }

// failLeague turns a league error into op's problem response (503 when
// there is no season data yet), or returns the error for a 500.
func failLeague[T problemResp](ctx context.Context, err error) (T, error) {
	if errors.Is(err, league.ErrNoSeasonData) {
		return fail[T](unavailable(ctx)), nil
	}
	p, err := leagueProblem(ctx, err)
	if err != nil {
		var zero T
		return zero, err
	}
	return fail[T](p), nil
}

// ---------------------------------------------------------------- mapping

// names maps user id to display name for one or more leagues' members.
// A missing entry renders as "Former manager".
type names map[string]string

func (n names) of(userID string) string {
	if name, ok := n[userID]; ok && userID != "" {
		return name
	}
	return "Former manager"
}

// namesFor loads the display names of every manager and commissioner in ls.
func (s *Server) namesFor(ctx context.Context, ls ...*league.League) (names, error) {
	var userIDs []string
	for _, l := range ls {
		if l.CommissionerID != "" {
			userIDs = append(userIDs, l.CommissionerID)
		}
		for _, t := range l.Teams {
			if t.OwnerID != "" {
				userIDs = append(userIDs, t.OwnerID)
			}
		}
	}
	return s.auth.DisplayNames(ctx, userIDs)
}

// teamRef renders a team. A team without a manager is an "Open seat",
// except an imported seat nobody has claimed yet, which shows its FPL
// manager's name.
func teamRef(t league.Team, n names) TeamRef {
	manager := n.of(t.OwnerID)
	if t.OwnerID == "" {
		manager = cmp.Or(t.ManagerName, "Open seat")
	}
	return TeamRef{Id: t.ID, Name: t.Name, ManagerName: manager}
}

func leagueSummary(l *league.League) LeagueSummary {
	out := LeagueSummary{
		Id:          l.ID,
		Name:        l.Name,
		Season:      l.Season,
		Status:      LeagueStatus(l.Status),
		Source:      LeagueSource(league.SourceNative),
		TeamCount:   l.Settings.TeamCount,
		MemberCount: l.MemberCount(),
	}
	if l.Source == league.SourceFPL {
		out.Source = LeagueSource(league.SourceFPL)
		id := l.FPLLeagueID
		out.FplLeagueId = &id
	}
	return out
}

func leagueSettings(st league.Settings) LeagueSettings {
	rules := scoring.StandardLineup()
	counts := func(c scoring.Counts) PositionCounts {
		return PositionCounts{GKP: c[scoring.GKP], DEF: c[scoring.DEF], MID: c[scoring.MID], FWD: c[scoring.FWD]}
	}
	return LeagueSettings{
		TeamCount: st.TeamCount,
		Scoring:   FplStandard,
		Schedule:  LeagueSettingsScheduleH2h,
		Roster:    counts(rules.Squad),
		Lineup: LineupRules{
			Starters:             rules.Starters,
			MinStarters:          counts(rules.MinStarters),
			MaxStarters:          counts(rules.MaxStarters),
			BenchGoalkeeperFirst: rules.BenchGoalkeeperFirst,
		},
		Draft:   DraftSettings{Type: DraftSettingsTypeSnake, PickTimerSeconds: st.PickTimerSeconds, StartsAt: st.DraftStartsAt},
		Waivers: WaiverSettings{Type: WaiverSettingsTypeRollingPriority, Order: WaiverOrderRule(st.WaiverRule())},
		Trades: TradeSettings{
			Review:           TradeReview(st.TradeReview),
			ReviewHours:      st.ReviewHours,
			VetoThreshold:    float32(st.VetoThreshold),
			DeadlineGameweek: st.TradeDeadlineGW,
		},
		StartGameweek: st.StartGameweek,
		EndGameweek:   st.EndGameweek,
	}
}

func roleOf(l *league.League, userID string) LeagueRole {
	if l.CommissionerID == userID {
		return LeagueRoleCommissioner
	}
	return LeagueRoleMember
}

func leagueViewer(l *league.League, viewer string, n names) LeagueViewer {
	v := LeagueViewer{Role: roleOf(l, viewer)}
	if t, ok := l.TeamOf(viewer); ok {
		ref := teamRef(t, n)
		v.Team = &ref
	}
	return v
}

func (s *Server) leagueBody(ctx context.Context, l *league.League, viewer string) (League, error) {
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return League{}, err
	}
	return League{
		League:       leagueSummary(l),
		Settings:     leagueSettings(l.Settings),
		Commissioner: UserRef{Id: l.CommissionerID, DisplayName: n.of(l.CommissionerID)},
		Viewer:       leagueViewer(l, viewer, n),
		CreatedAt:    l.CreatedAt,
	}, nil
}

// settingsInput maps the settings of a request body. An explicit null
// (path is where the settings object sits in the body) clears a nullable
// setting; omitted leaves it.
func settingsInput(ctx context.Context, in *LeagueSettingsInput, path ...string) league.SettingsInput {
	var out league.SettingsInput
	if in == nil {
		return out
	}
	at := func(p ...string) []string { return append(append([]string{}, path...), p...) }
	out.ClearDraftStartsAt = explicitNull(ctx, at("draft", "startsAt")...)
	out.ClearTradeDeadline = explicitNull(ctx, at("trades", "deadlineGameweek")...)
	out.TeamCount = in.TeamCount
	if in.StartGameweek != nil {
		g := *in.StartGameweek
		out.StartGameweek = &g
	}
	if d := in.Draft; d != nil {
		out.PickTimerSeconds = d.PickTimerSeconds
		out.DraftStartsAt = d.StartsAt
	}
	if t := in.Trades; t != nil {
		if t.Review != nil {
			r := string(*t.Review)
			out.TradeReview = &r
		}
		out.ReviewHours = t.ReviewHours
		if t.VetoThreshold != nil {
			v := float64(*t.VetoThreshold)
			out.VetoThreshold = &v
		}
		out.TradeDeadlineGW = t.DeadlineGameweek
	}
	if wv := in.Waivers; wv != nil && wv.Order != nil {
		o := string(*wv.Order)
		out.WaiverOrder = &o
	}
	return out
}

// inviteBody renders an invite of l; its link opens the web app's join
// route. A claim link names its seat.
func (s *Server) inviteBody(inv league.Invite, l *league.League, n names) Invite {
	out := Invite{
		Id:        inv.ID,
		Code:      inv.Code,
		Url:       s.appURL + "/join/" + inv.Code,
		CreatedAt: inv.CreatedAt,
		ExpiresAt: inv.ExpiresAt,
		MaxUses:   inv.MaxUses,
		Uses:      inv.Uses,
		Revoked:   inv.Revoked,
	}
	if t, ok := l.Team(inv.TeamID); ok && inv.TeamID != "" {
		ref := teamRef(t, n)
		out.ClaimTeam = &ref
	}
	return out
}

// ---------------------------------------------------------------- handlers

func (s *Server) CreateLeague(ctx context.Context, req CreateLeagueRequestObject) (CreateLeagueResponseObject, error) {
	if req.Body == nil {
		return fail[CreateLeaguedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "A JSON body is required.")), nil
	}
	uid := userFrom(ctx)
	l, err := s.leagues.Create(ctx, uid, req.Body.Name, req.Body.TeamCount, req.Body.TeamName, settingsInput(ctx, req.Body.Settings, "settings"))
	if err != nil {
		return failLeague[CreateLeaguedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.leagueBody(ctx, l, uid)
	if err != nil {
		return nil, err
	}
	return CreateLeague201JSONResponse{
		Body:    LeagueResponse{Data: body, Meta: metaAt(s.now())},
		Headers: CreateLeague201ResponseHeaders{Location: "/v1/leagues/" + l.ID},
	}, nil
}

func (s *Server) UpdateLeague(ctx context.Context, req UpdateLeagueRequestObject) (UpdateLeagueResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[UpdateLeaguedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if req.Body == nil || (req.Body.Name == nil && req.Body.Settings == nil) {
		return fail[UpdateLeaguedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "Send name or settings.")), nil
	}
	uid := userFrom(ctx)
	l, err := s.leagues.Update(ctx, uid, req.LeagueId, req.Body.Name, settingsInput(ctx, req.Body.Settings, "settings"))
	if err != nil {
		return failLeague[UpdateLeaguedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	body, err := s.leagueBody(ctx, l, uid)
	if err != nil {
		return nil, err
	}
	s.leagueChanged(ctx, req.LeagueId)
	return UpdateLeague200JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) ListMyLeagues(ctx context.Context, _ ListMyLeaguesRequestObject) (ListMyLeaguesResponseObject, error) {
	uid := userFrom(ctx)
	ls, err := s.leagues.ForUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	n, err := s.namesFor(ctx, ls...)
	if err != nil {
		return nil, err
	}
	// In-season leagues also show the record and this gameweek's matchup,
	// scored from the shared stats (loaded once for every league).
	var w *league.World
	for _, l := range ls {
		if l.Status == league.StatusInSeason && w == nil {
			if w, err = s.world(ctx); errors.Is(err, errNoSeasonData) {
				break
			} else if err != nil {
				return nil, err
			}
		}
	}
	out := []MyLeague{}
	for _, l := range ls {
		t, _ := l.TeamOf(uid)
		ml := MyLeague{
			League:        leagueSummary(l),
			Team:          teamRef(t, n),
			Role:          roleOf(l, uid),
			DraftStartsAt: l.Settings.DraftStartsAt,
		}
		if l.Status == league.StatusInSeason && w != nil {
			g, err := s.leagues.Repo().Game(ctx, l.ID)
			if err != nil {
				return nil, err
			}
			v := &view{l: l, g: g, w: w, names: n, now: s.now()}
			rec := recordOf(v.standings(), t.ID)
			ml.Record = &rec
			ms, err := v.summaries(v.leagueGW())
			if err != nil {
				return nil, err
			}
			for i, m := range g.MatchupsIn(v.leagueGW()) {
				if m.HomeTeamID == t.ID || m.AwayTeamID == t.ID {
					ml.CurrentMatchup = &ms[i]
				}
			}
		}
		out = append(out, ml)
	}
	meta := metaAt(s.now())
	if w != nil {
		// Records and current matchups are stats-derived: report the
		// poller's freshness, not the request time.
		if meta, err = s.statsMeta(ctx, nil); err != nil {
			return nil, err
		}
	}
	return ListMyLeagues200JSONResponse{Data: out, Meta: meta}, nil
}

func (s *Server) ListMembers(ctx context.Context, req ListMembersRequestObject) (ListMembersResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[ListMembersdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	l, err := s.leagues.Get(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failLeague[ListMembersdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	out := []Member{}
	for _, t := range l.Teams {
		if t.OwnerID == "" {
			continue
		}
		m := Member{
			User:     UserRef{Id: t.OwnerID, DisplayName: n.of(t.OwnerID)},
			Team:     teamRef(t, n),
			Role:     roleOf(l, t.OwnerID),
			JoinedAt: t.JoinedAt,
		}
		if m.Role == LeagueRoleCommissioner {
			out = append([]Member{m}, out...)
		} else {
			out = append(out, m)
		}
	}
	return ListMembers200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) RemoveMember(ctx context.Context, req RemoveMemberRequestObject) (RemoveMemberResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("userId", "usr", req.UserId)); d != "" {
		return fail[RemoveMemberdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	changes, err := s.leagues.RemoveMember(ctx, userFrom(ctx), req.LeagueId, req.UserId)
	if err != nil {
		return failLeague[RemoveMemberdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	s.notifier().Notify(ctx, changes...)
	s.revokeRealtime(ctx, realtime.Revocation{UserID: req.UserId, LeagueID: req.LeagueId})
	s.revokeSignInLinks(ctx, req.LeagueId, req.UserId)
	s.leagueChanged(ctx, req.LeagueId)
	return RemoveMember204Response{}, nil
}

func (s *Server) LeaveLeague(ctx context.Context, req LeaveLeagueRequestObject) (LeaveLeagueResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[LeaveLeaguedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	changes, err := s.leagues.Leave(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failLeague[LeaveLeaguedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	s.notifier().Notify(ctx, changes...)
	s.revokeRealtime(ctx, realtime.Revocation{UserID: userFrom(ctx), LeagueID: req.LeagueId})
	s.revokeSignInLinks(ctx, req.LeagueId, userFrom(ctx))
	s.leagueChanged(ctx, req.LeagueId)
	return LeaveLeague204Response{}, nil
}

func (s *Server) TransferCommissioner(ctx context.Context, req TransferCommissionerRequestObject) (TransferCommissionerResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[TransferCommissionerdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if req.Body == nil {
		return fail[TransferCommissionerdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "userId is required.")), nil
	}
	if d := badID("userId", "usr", req.Body.UserId); d != "" {
		return fail[TransferCommissionerdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	uid := userFrom(ctx)
	l, err := s.leagues.TransferCommissioner(ctx, uid, req.LeagueId, req.Body.UserId)
	if err != nil {
		return failLeague[TransferCommissionerdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	if l.CommissionerID != uid {
		s.revokeSignInLinks(ctx, l.ID, "")
	}
	body, err := s.leagueBody(ctx, l, uid)
	if err != nil {
		return nil, err
	}
	s.leagueChanged(ctx, req.LeagueId)
	return TransferCommissioner200JSONResponse{Data: body, Meta: metaAt(s.now())}, nil
}

func (s *Server) UpdateTeam(ctx context.Context, req UpdateTeamRequestObject) (UpdateTeamResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("teamId", "tm", req.TeamId)); d != "" {
		return fail[UpdateTeamdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if req.Body == nil {
		return fail[UpdateTeamdefaultApplicationProblemPlusJSONResponse](invalid(ctx, "name is required.")), nil
	}
	t, err := s.leagues.RenameTeam(ctx, userFrom(ctx), req.LeagueId, req.TeamId, req.Body.Name)
	if err != nil {
		return failLeague[UpdateTeamdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.auth.DisplayNames(ctx, []string{t.OwnerID})
	if err != nil {
		return nil, err
	}
	s.leagueChanged(ctx, req.LeagueId)
	return UpdateTeam200JSONResponse{Data: teamRef(t, n), Meta: metaAt(s.now())}, nil
}

func (s *Server) CreateInvite(ctx context.Context, req CreateInviteRequestObject) (CreateInviteResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[CreateInvitedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	var maxUses *int
	var expiresIn *time.Duration
	if b := req.Body; b != nil {
		if b.MaxUses != nil && (*b.MaxUses < 1 || *b.MaxUses > 20) {
			return fail[CreateInvitedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "maxUses must be 1 to 20.")), nil
		}
		if b.ExpiresInHours != nil && (*b.ExpiresInHours < 1 || *b.ExpiresInHours > 720) {
			return fail[CreateInvitedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "expiresInHours must be 1 to 720.")), nil
		}
		maxUses = b.MaxUses
		if b.ExpiresInHours != nil {
			d := time.Duration(*b.ExpiresInHours) * time.Hour
			expiresIn = &d
		}
	}
	inv, err := s.leagues.CreateInvite(ctx, userFrom(ctx), req.LeagueId, maxUses, expiresIn)
	if err != nil {
		return failLeague[CreateInvitedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	return CreateInvite201JSONResponse{
		Body:    InviteResponse{Data: s.inviteBody(inv, &league.League{}, nil), Meta: metaAt(s.now())},
		Headers: CreateInvite201ResponseHeaders{Location: "/v1/leagues/" + req.LeagueId + "/invites/" + inv.ID},
	}, nil
}

func (s *Server) ListInvites(ctx context.Context, req ListInvitesRequestObject) (ListInvitesResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return fail[ListInvitesdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	invs, err := s.leagues.ListInvites(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failLeague[ListInvitesdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	l, err := s.leagues.Get(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failLeague[ListInvitesdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	out := []Invite{}
	for _, inv := range invs {
		out = append(out, s.inviteBody(inv, l, n))
	}
	return ListInvites200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}

func (s *Server) RevokeInvite(ctx context.Context, req RevokeInviteRequestObject) (RevokeInviteResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("inviteId", "inv", req.InviteId)); d != "" {
		return fail[RevokeInvitedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	if err := s.leagues.RevokeInvite(ctx, userFrom(ctx), req.LeagueId, req.InviteId); err != nil {
		return failLeague[RevokeInvitedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	return RevokeInvite204Response{}, nil
}

// badCode returns a 400 detail unless code looks like an invite code:
// league.CodeLength characters, or 8 for codes made before codes grew.
func badCode(code string) string {
	const detail = "code must be an invite code (10 letters and numbers)."
	if len(code) != league.CodeLength && len(code) != 8 {
		return detail
	}
	for _, c := range code {
		switch {
		case c >= '2' && c <= '9', c >= 'A' && c <= 'Z' && c != 'I' && c != 'L' && c != 'O' && c != 'U',
			c >= 'a' && c <= 'z' && c != 'i' && c != 'l' && c != 'o' && c != 'u':
		default:
			return detail
		}
	}
	return ""
}

func (s *Server) GetInvite(ctx context.Context, req GetInviteRequestObject) (GetInviteResponseObject, error) {
	if d := badCode(req.Code); d != "" {
		return fail[GetInvitedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	l, inv, err := s.leagues.PreviewInvite(ctx, req.Code)
	if err != nil {
		return failLeague[GetInvitedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	return GetInvite200JSONResponse{Data: InvitePreview{
		Code:             inv.Code,
		LeagueId:         l.ID,
		LeagueName:       l.Name,
		Season:           l.Season,
		Status:           LeagueStatus(l.Status),
		TeamCount:        l.Settings.TeamCount,
		MemberCount:      l.MemberCount(),
		CommissionerName: n.of(l.CommissionerID),
		ExpiresAt:        inv.ExpiresAt,
		Joinable:         inv.TeamID != "" || l.HasPlace(),
		ClaimTeam:        claimTeam(l, inv),
		ClaimSignIn:      s.claimSignIn && inv.TeamID != "",
	}, Meta: metaAt(s.now())}, nil
}

// claimTeam is a claim link's seat for the preview.
func claimTeam(l *league.League, inv league.Invite) *InviteClaimTeam {
	t, ok := l.Team(inv.TeamID)
	if inv.TeamID == "" || !ok {
		return nil
	}
	return &InviteClaimTeam{TeamName: t.Name, FplManagerName: t.ManagerName}
}

func (s *Server) AcceptInvite(ctx context.Context, req AcceptInviteRequestObject) (AcceptInviteResponseObject, error) {
	if d := badCode(req.Code); d != "" {
		return fail[AcceptInvitedefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	teamName := ""
	if req.Body != nil && req.Body.TeamName != nil {
		teamName = *req.Body.TeamName
	}
	uid := userFrom(ctx)
	l, t, err := s.leagues.AcceptInvite(ctx, uid, req.Code, teamName)
	if err != nil {
		return failLeague[AcceptInvitedefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	s.leagueChanged(ctx, l.ID)
	return AcceptInvite201JSONResponse{
		Body: MembershipResponse{Data: Membership{
			League: leagueSummary(l), Team: teamRef(t, n), Role: roleOf(l, uid),
		}, Meta: metaAt(s.now())},
		Headers: AcceptInvite201ResponseHeaders{Location: "/v1/leagues/" + l.ID},
	}, nil
}
