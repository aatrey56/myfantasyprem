package httpapi

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// ClaimSeatSignIn claims a seat with its claim link for a signed-out
// person and signs them in (docs/adr/0013): a new account with only a
// display name, the seat and a session, in one transaction on Postgres.
// The claim code is a credential, so it comes in the body (the access log
// records paths) and is never logged.
func (s *Server) ClaimSeatSignIn(ctx context.Context, req ClaimSeatSignInRequestObject) (ClaimSeatSignInResponseObject, error) {
	type resp = ClaimSeatSignIndefaultApplicationProblemPlusJSONResponse
	if !s.claimSignIn {
		return fail[resp](newProblem(401, "claim-sign-in-off", "Sign in to claim",
			"Sign in to claim this team.", RequestIDFrom(ctx))), nil
	}
	if req.Body == nil {
		return fail[resp](invalid(ctx, "code and displayName are required.")), nil
	}
	if d := badCode(req.Body.Code); d != "" {
		return fail[resp](Problem(badRequest(ctx, d))), nil
	}
	name := strings.TrimSpace(req.Body.DisplayName)
	if c := utf8.RuneCountInString(name); c < 1 || c > 32 {
		return fail[resp](invalid(ctx, "The display name is not valid.",
			ProblemError{Code: "invalid_length", Message: "Your name must be 1 to 32 characters.", Field: ptr("/displayName")})), nil
	}
	l, t, tokens, err := s.claimWithNewAccount(ctx, req.Body.Code, name)
	if errors.Is(err, league.ErrNotClaimLink) {
		return fail[resp](newProblem(401, "sign-in-required", "Sign in to join",
			"Sign in to join this league.", RequestIDFrom(ctx))), nil
	}
	if err != nil {
		return failLeague[resp](ctx, err)
	}
	u, err := s.auth.User(ctx, t.OwnerID)
	if err != nil {
		return nil, err
	}
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, err
	}
	s.leagueChanged(ctx, l.ID)
	return ClaimSeatSignIn200JSONResponse{Data: ClaimSignIn{
		Session:    sessionBody(tokens, u, true),
		Membership: Membership{League: leagueSummary(l), Team: teamRef(t, n), Role: roleOf(l, u.ID)},
	}, Meta: metaAt(s.now())}, nil
}

// claimWithNewAccount creates the account only once the link is known to
// be a usable claim link, then gives it the seat: in one transaction on
// Postgres, so a claim that fails leaves no account behind. The memory
// stores have no shared transaction; the account is created inside the
// league's (which is what serialises claims) and so only for a claim that
// goes through.
func (s *Server) claimWithNewAccount(ctx context.Context, code, name string) (*league.League, league.Team, auth.Tokens, error) {
	var (
		l      *league.League
		t      league.Team
		tokens auth.Tokens
	)
	pgLeagues, okL := s.leagues.Repo().(*league.Postgres)
	_, okA := s.auth.(*auth.Postgres)
	if okL && okA {
		err := pgLeagues.TxWith(ctx, func(tx league.Tx, db store.DB) error {
			var err error
			l, t, err = league.ClaimSeatIn(ctx, tx, code, s.now(), func() (string, error) {
				var uid string
				var err error
				uid, tokens, err = auth.CreateAccountIn(ctx, db, name, s.now())
				return uid, err
			})
			return err
		})
		return l, t, tokens, err
	}
	l, t, err := s.leagues.ClaimSeat(ctx, code, func() (string, error) {
		var (
			u   *auth.User
			err error
		)
		tokens, u, err = s.auth.CreateAccount(ctx, name)
		if err != nil {
			return "", err
		}
		return u.ID, nil
	})
	return l, t, tokens, err
}
