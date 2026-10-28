package httpapi

import (
	"cmp"
	"context"
	"errors"
	"regexp"
	"strconv"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// signInLinkCode is the shape of a sign-in link's code (auth.randomToken).
var signInLinkCode = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

// CreateSeatSignInLink issues a sign-in link for a claimed seat's manager
// (docs/adr/0013, "Sign-in links"): commissioner only, for accounts with
// no other way to sign in, limited per caller. The code appears only in
// this response and is never logged.
func (s *Server) CreateSeatSignInLink(ctx context.Context, req CreateSeatSignInLinkRequestObject) (CreateSeatSignInLinkResponseObject, error) {
	type resp = CreateSeatSignInLinkdefaultApplicationProblemPlusJSONResponse
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("teamId", "tm", req.TeamId)); d != "" {
		return fail[resp](Problem(badRequest(ctx, d))), nil
	}
	uid := userFrom(ctx)
	l, err := s.leagues.Get(ctx, uid, req.LeagueId)
	if err != nil {
		return failLeague[resp](ctx, err)
	}
	if err := league.Commissioner(l, uid); err != nil {
		return failLeague[resp](ctx, err)
	}
	if l.Source != league.SourceFPL {
		return fail[resp](newProblem(409, "not-imported", "Not an imported league",
			"This league was created here, not imported from FPL Draft.", RequestIDFrom(ctx))), nil
	}
	t, ok := l.Team(req.TeamId)
	if !ok {
		return fail[resp](notFound(ctx, "No such team in this league.")), nil
	}
	if t.OwnerID == "" {
		return fail[resp](newProblem(409, "seat-unclaimed", "Nobody has claimed this team",
			"Nobody has claimed "+t.Name+" yet. Send its claim link instead.", RequestIDFrom(ctx))), nil
	}
	owner, err := s.auth.User(ctx, t.OwnerID)
	if errors.Is(err, auth.ErrNoUser) {
		return fail[resp](notFound(ctx, "This team's manager no longer has an account.")), nil
	}
	if err != nil {
		return nil, err
	}
	if owner.Email != "" || len(owner.Providers) > 0 {
		return fail[resp](newProblem(409, "has-sign-in", "Can sign in already",
			owner.DisplayName+" can sign in with their own email, Apple or Google account.", RequestIDFrom(ctx))), nil
	}
	// The caller must already hold every role the account has: never the
	// commissioner of any league, never a member of a league they do not
	// commission, so a link cannot reach past the caller's own leagues.
	theirs, err := s.leagues.ForUser(ctx, owner.ID)
	if err != nil {
		return nil, err
	}
	for _, o := range theirs {
		if o.CommissionerID == owner.ID || o.CommissionerID != uid {
			return fail[resp](newProblem(409, "target-has-role-elsewhere", "Has a role in another league",
				owner.DisplayName+" is in a league you do not run, so only they can sign themselves in.", RequestIDFrom(ctx))), nil
		}
	}
	if ok, wait := s.signInLinkLimit.allow(uid, s.now()); !ok {
		secs := retryAfter(wait)
		return CreateSeatSignInLink429ApplicationProblemPlusJSONResponse{TooManyRequestsApplicationProblemPlusJSONResponse{
			Body: newProblem(429, "rate-limited", "Too many requests",
				"Slow down and try again in "+strconv.Itoa(secs)+" seconds.", RequestIDFrom(ctx)),
			Headers: TooManyRequestsResponseHeaders{RetryAfter: secs},
		}}, nil
	}
	link, err := s.auth.IssueSignInLink(ctx, owner.ID, l.ID, uid)
	if err != nil {
		return nil, err
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	return CreateSeatSignInLink201JSONResponse{Data: SeatSignInLink{
		Team:      teamRef(t, n),
		Owner:     UserRef{Id: owner.ID, DisplayName: owner.DisplayName},
		Url:       s.appURL + "/signin-link/" + link.Code,
		ExpiresAt: link.ExpiresAt,
	}, Meta: metaAt(s.now())}, nil
}

// SignInWithLink consumes a sign-in link and signs its account in. The
// code comes in the body (the access log records paths).
func (s *Server) SignInWithLink(ctx context.Context, req SignInWithLinkRequestObject) (SignInWithLinkResponseObject, error) {
	type resp = SignInWithLinkdefaultApplicationProblemPlusJSONResponse
	if req.Body == nil {
		return fail[resp](invalid(ctx, "code is required.")), nil
	}
	if !signInLinkCode.MatchString(req.Body.Code) {
		return fail[resp](Problem(badRequest(ctx, "That is not a sign-in link code."))), nil
	}
	t, u, leagueID, err := s.auth.UseSignInLink(ctx, req.Body.Code, s.signInLinkHolds)
	if errors.Is(err, auth.ErrInvalidToken) {
		return fail[resp](unauthorized(ctx,
			"This sign-in link has expired, was already used or was replaced. Ask your commissioner for a new one.")), nil
	}
	if err != nil {
		return nil, err
	}
	return SignInWithLink200JSONResponse{Data: SignInLinkSession{
		Session: sessionBody(t, u, false), LeagueId: leagueID,
	}, Meta: metaAt(s.now())}, nil
}

// signInLinkHolds confirms a link's grant at use: its league still lists
// the manager and its issuer still commissions that league.
func (s *Server) signInLinkHolds(ctx context.Context, g auth.SignInLinkGrant) error {
	l, err := s.leagues.Repo().Get(ctx, g.LeagueID)
	if err != nil {
		return err
	}
	if l == nil {
		return auth.ErrInvalidToken
	}
	if _, ok := l.TeamOf(g.UserID); !ok || l.CommissionerID != g.IssuerID {
		return auth.ErrInvalidToken
	}
	return nil
}

// revokeSignInLinks deletes a league's unused sign-in links (userID's
// only, when set) after a change that ends them. A failure is logged: use
// re-checks the grant anyway.
func (s *Server) revokeSignInLinks(ctx context.Context, leagueID, userID string) {
	if err := s.auth.RevokeSignInLinks(ctx, leagueID, userID); err != nil {
		s.log.WarnContext(ctx, "revoke sign-in links", "league_id", leagueID, "request_id", RequestIDFrom(ctx), "err", err)
	}
}
