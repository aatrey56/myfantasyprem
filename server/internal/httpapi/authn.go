package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// publicRoutes are the operations with `security: []` in the contract. Every
// other route needs a bearer token. TestContract checks this set against the
// spec in both directions.
var publicRoutes = map[string]bool{
	"GET /healthz":                    true,
	"POST /v1/auth/apple":             true,
	"POST /v1/auth/google":            true,
	"POST /v1/auth/email":             true,
	"POST /v1/auth/email/verify":      true,
	"POST /v1/auth/email/code":        true,
	"POST /v1/auth/claim":             true,
	"POST /v1/auth/signin-link":       true,
	"POST /v1/auth/refresh":           true,
	"GET /v1/clubs":                   true,
	"GET /v1/gameweeks/current":       true,
	"GET /v1/gameweeks/{gw}/fixtures": true,
	"GET /v1/invites/{code}":          true,
	"GET /v1/config":                  true,
	// Images load through <img> and native image views, which send no
	// bearer token; they are public data.
	"GET /v1/assets/players/{playerId}/photo": true,
	"GET /v1/assets/clubs/{clubCode}/crest":   true,
	"GET /v1/assets/clubs/{clubCode}/shirt":   true,
}

// authenticator resolves a bearer token. auth.Service implements it.
type authenticator interface {
	Authenticate(ctx context.Context, accessToken string) (userID, sessionID string, err error)
}

type principalKey struct{}

type principal struct{ userID, sessionID string }

// userFrom returns the signed-in user id, or "" on a public route.
func userFrom(ctx context.Context) string {
	p, _ := ctx.Value(principalKey{}).(principal)
	return p.userID
}

func sessionFrom(ctx context.Context) string {
	p, _ := ctx.Value(principalKey{}).(principal)
	return p.sessionID
}

// bearerToken extracts the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) string {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(token)
}

// authenticate requires a valid bearer token on every matched route that is
// not public, and puts the principal in the request context. Unmatched
// requests pass through so they still get their 404 or 405. On a failure,
// rejected (if set) may answer instead of the 401: it returns true when it
// has written the response.
func authenticate(mux *http.ServeMux, authn authenticator, rejected func(http.ResponseWriter, *http.Request) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, pattern := mux.Handler(r)
		if pattern == "" || publicRoutes[pattern] || ticketRoutes[pattern] {
			next.ServeHTTP(w, r)
			return
		}
		userID, sessionID, err := "", "", errors.New("no token")
		if token := bearerToken(r); token != "" {
			userID, sessionID, err = authn.Authenticate(r.Context(), token)
		}
		if err != nil {
			if rejected != nil && rejected(w, r) {
				return
			}
			w.Header().Set("WWW-Authenticate", `Bearer realm="myfantasyprem"`)
			writeProblem(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", "Sign in to continue.")
			return
		}
		ctx := context.WithValue(r.Context(), principalKey{}, principal{userID: userID, sessionID: sessionID})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ---------------------------------------------------------------- auth ops

func sessionBody(t auth.Tokens, u *auth.User, isNew bool) Session {
	return Session{
		AccessToken:      t.AccessToken,
		TokenType:        Bearer,
		ExpiresAt:        t.AccessExpiresAt,
		RefreshToken:     t.RefreshToken,
		RefreshExpiresAt: t.RefreshExpiresAt,
		User:             userBody(u),
		IsNewUser:        isNew,
	}
}

func userBody(u *auth.User) User {
	out := User{
		Id:            u.ID,
		DisplayName:   u.DisplayName,
		CreatedAt:     u.CreatedAt,
		AuthProviders: []AuthProvider{},
	}
	if u.Email != "" {
		e := openapi_types.Email(u.Email)
		out.Email = &e
	}
	if u.AvatarColor != "" {
		c := u.AvatarColor
		out.AvatarColor = &c
	}
	for _, p := range u.Providers {
		out.AuthProviders = append(out.AuthProviders, AuthProvider(p))
	}
	return out
}

func invalid(ctx context.Context, detail string, errs ...ProblemError) Problem {
	p := newProblem(422, "invalid-request", "Invalid request", detail, RequestIDFrom(ctx))
	if len(errs) > 0 {
		p.Errors = &errs
	}
	return p
}

func unauthorized(ctx context.Context, detail string) Problem {
	return newProblem(401, "unauthorized", "Unauthorized", detail, RequestIDFrom(ctx))
}

// providerSignIn verifies a provider's identity token and signs the user
// in. The returned problem is the response when sign-in fails.
func (s *Server) providerSignIn(ctx context.Context, v *idp.Verifier, provider, label, token string, nonce *string, name, appleCode string) (*Session, *Problem, error) {
	if !v.Configured() {
		p := newProblem(503, "sign-in-unavailable", "Sign-in unavailable", label+" sign-in is not configured on this server.", RequestIDFrom(ctx))
		return nil, &p, nil
	}
	claims, err := v.Verify(ctx, token)
	if errors.Is(err, idp.ErrInvalid) {
		s.log.InfoContext(ctx, "identity token refused", "provider", provider, "err", err)
		p := newProblem(401, "invalid-identity-token", "Sign-in failed", "The "+label+" sign-in could not be verified. Try again.", RequestIDFrom(ctx))
		return nil, &p, nil
	}
	if err != nil {
		s.log.WarnContext(ctx, "identity provider unreachable", "provider", provider, "err", err)
		p := newProblem(503, "sign-in-unavailable", "Sign-in unavailable", label+" could not be reached. Try again shortly.", RequestIDFrom(ctx))
		return nil, &p, nil
	}
	// Replay protection: the token must carry the client's nonce (always
	// sent for Apple; for Google when the client asked for one).
	if nonce != nil && !idp.NonceMatches(claims.Nonce, *nonce) {
		p := newProblem(401, "invalid-identity-token", "Sign-in failed", "The "+label+" sign-in nonce does not match.", RequestIDFrom(ctx))
		return nil, &p, nil
	}
	if name == "" {
		name = claims.Name
	}
	var appleRefresh string
	if appleCode != "" {
		appleRefresh = s.appleRefreshToken(ctx, v, claims, nonce, appleCode)
	}
	t, u, created, err := s.auth.SignInWithIdentity(ctx, auth.Identity{
		Provider: provider, Subject: claims.Subject, Email: claims.Email, EmailVerified: claims.EmailVerified, Name: name,
		AppleRefreshToken: appleRefresh,
	})
	if err != nil {
		return nil, nil, err
	}
	body := sessionBody(t, u, created)
	return &body, nil, nil
}

// appleRefreshToken exchanges a Sign in with Apple authorization code for
// a refresh token and returns it sealed for storage, so account deletion
// can revoke it. The code is sent separately from the identity token, so
// the exchange's id_token must be the signed-in identity (subject and
// audience, and the nonce when it carries one): another account's code
// stored here would have deletion revoke that account's authorization.
// Failures do not fail the sign-in; they are logged, as that account's
// Apple token then cannot be revoked.
func (s *Server) appleRefreshToken(ctx context.Context, v *idp.Verifier, signedIn idp.Claims, nonce *string, code string) string {
	if !s.appleTokens.Configured() {
		return "" // logged once at startup (cmd/api)
	}
	clientID := signedIn.Audience
	rt, idToken, err := s.appleTokens.Exchange(ctx, clientID, code)
	if err == nil {
		var got idp.Claims
		got, err = v.Verify(ctx, idToken)
		switch {
		case err != nil:
			err = fmt.Errorf("exchanged id_token: %w", err)
		case got.Subject != signedIn.Subject || got.Audience != clientID:
			err = errors.New("the authorization code belongs to another Apple identity than the identity token")
		case got.Nonce != "" && (nonce == nil || !idp.NonceMatches(got.Nonce, *nonce)):
			err = errors.New("the exchanged id_token's nonce does not match the sign-in")
		}
	}
	if err == nil {
		var sealed string
		if sealed, err = s.appleTokens.Seal(clientID, rt); err == nil {
			return sealed
		}
	}
	s.log.ErrorContext(ctx, "apple authorization code exchange failed: this account's Apple token cannot be revoked at deletion", "err", err)
	return ""
}

// revokeAppleTokens revokes a deleted account's Apple refresh tokens
// (sealed, read before the deletion). Failures are logged: the account
// is gone either way.
func (s *Server) revokeAppleTokens(ctx context.Context, uid string, sealed []string) {
	if len(sealed) == 0 {
		return
	}
	if !s.appleTokens.Configured() {
		s.log.ErrorContext(ctx, "apple token NOT revoked at account deletion: Apple token revocation is not configured (APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY, APPLE_TOKEN_KEY)", "user", uid)
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	for _, st := range sealed {
		clientID, rt, err := s.appleTokens.Open(st)
		if err == nil {
			err = s.appleTokens.Revoke(ctx, clientID, rt)
		}
		if err != nil {
			s.log.ErrorContext(ctx, "apple token revocation failed", "user", uid, "err", err)
		}
	}
}

func (s *Server) SignInWithApple(ctx context.Context, req SignInWithAppleRequestObject) (SignInWithAppleResponseObject, error) {
	if req.Body == nil || req.Body.IdentityToken == "" || req.Body.AuthorizationCode == "" || req.Body.Nonce == "" {
		return fail[SignInWithAppledefaultApplicationProblemPlusJSONResponse](invalid(ctx, "identityToken, authorizationCode and nonce are required.")), nil
	}
	b := req.Body
	// Apple sends the name only on the first sign-in, to the client.
	var name []string
	for _, part := range []*string{b.GivenName, b.FamilyName} {
		if part != nil && strings.TrimSpace(*part) != "" {
			name = append(name, strings.TrimSpace(*part))
		}
	}
	sess, p, err := s.providerSignIn(ctx, s.apple, "apple", "Apple", b.IdentityToken, &b.Nonce, strings.Join(name, " "), b.AuthorizationCode)
	if p != nil || err != nil {
		return fail[SignInWithAppledefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return SignInWithApple200JSONResponse{Data: *sess, Meta: metaAt(s.now())}, nil
}

func (s *Server) SignInWithGoogle(ctx context.Context, req SignInWithGoogleRequestObject) (SignInWithGoogleResponseObject, error) {
	if req.Body == nil || req.Body.IdToken == "" {
		return fail[SignInWithGoogledefaultApplicationProblemPlusJSONResponse](invalid(ctx, "idToken is required.")), nil
	}
	sess, p, err := s.providerSignIn(ctx, s.google, "google", "Google", req.Body.IdToken, req.Body.Nonce, "", "")
	if p != nil || err != nil {
		return fail[SignInWithGoogledefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	return SignInWithGoogle200JSONResponse{Data: *sess, Meta: metaAt(s.now())}, nil
}

// emailPattern is a deliberately loose check; the link itself proves the address.
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// nextPattern is a path inside the app (EmailSignInRequest.next): segments
// of URL-safe characters, none starting with a dot. No scheme, host, `//`,
// backslash, query or `..`, so a sign-in link cannot redirect off-site.
var nextPattern = regexp.MustCompile(`^(/[A-Za-z0-9_~-][A-Za-z0-9_.~-]*)+$`)

// codePattern is a sign-in code.
var codePattern = regexp.MustCompile(`^[0-9]{6}$`)

func (s *Server) StartEmailSignIn(ctx context.Context, req StartEmailSignInRequestObject) (StartEmailSignInResponseObject, error) {
	if req.Body == nil || len(req.Body.Email) > 254 || !emailPattern.MatchString(string(req.Body.Email)) {
		return fail[StartEmailSignIndefaultApplicationProblemPlusJSONResponse](invalid(ctx, "email must be an email address.",
			ProblemError{Code: "invalid_email", Message: "Enter a valid email address.", Field: ptr("/email")})), nil
	}
	var next string
	if n := req.Body.Next; n != nil {
		if len(*n) > 200 || !nextPattern.MatchString(*n) {
			return fail[StartEmailSignIndefaultApplicationProblemPlusJSONResponse](invalid(ctx, "next must be a path in the app.",
				ProblemError{Code: "invalid_next", Message: "next must be a path like /join/K7QM2XRP4D.", Field: ptr("/next")})), nil
		}
		next = *n
	}
	email := string(req.Body.Email)
	// Per address, on top of the per-IP auth limit: many IPs must not
	// flood one inbox. Every address is limited alike, so a 429 says
	// nothing about whether it has an account.
	if ok, wait := s.emailLimit.allow(strings.ToLower(strings.TrimSpace(email)), time.Now()); !ok {
		secs := retryAfter(wait)
		return StartEmailSignIn429ApplicationProblemPlusJSONResponse{TooManyRequestsApplicationProblemPlusJSONResponse{
			Body: newProblem(http.StatusTooManyRequests, "rate-limited", "Too many requests",
				"Too many sign-in links for this address. Try again in "+strconv.Itoa(secs)+" seconds.", RequestIDFrom(ctx)),
			Headers: TooManyRequestsResponseHeaders{RetryAfter: secs},
		}}, nil
	}
	in, err := s.auth.StartEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	// The sender mails the link; in development (AUTH_DEV_MAGIC_LINKS=true)
	// it logs it so a developer can finish the sign-in by hand.
	if err := s.sender.SendMagicLink(ctx, auth.MagicLink{
		Email: strings.ToLower(strings.TrimSpace(email)), Token: in.Token, Code: in.Code, Next: next, ExpiresAt: in.ExpiresAt,
	}); err != nil {
		return nil, err
	}
	now := s.now()
	return StartEmailSignIn202JSONResponse{Data: EmailSignInStarted{ExpiresAt: in.ExpiresAt}, Meta: metaAt(now)}, nil
}

// VerifyEmailSignInCode signs in with the code from the latest sign-in
// email. Wrong codes are limited per sign-in and per address in the store
// (auth.CodeAttempts, auth.CodeFailuresPerEmail), on top of the per-IP
// /v1/auth/* limit.
func (s *Server) VerifyEmailSignInCode(ctx context.Context, req VerifyEmailSignInCodeRequestObject) (VerifyEmailSignInCodeResponseObject, error) {
	if req.Body == nil || len(req.Body.Email) > 254 || !emailPattern.MatchString(string(req.Body.Email)) || !codePattern.MatchString(req.Body.Code) {
		return fail[VerifyEmailSignInCodedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "email and a 6-digit code are required.")), nil
	}
	t, u, created, err := s.auth.VerifyEmailCode(ctx, string(req.Body.Email), req.Body.Code)
	if limited := (*auth.CodeLimitError)(nil); errors.As(err, &limited) {
		// Every address is limited alike, so this says nothing about accounts.
		secs := retryAfter(limited.RetryAfter)
		return VerifyEmailSignInCode429ApplicationProblemPlusJSONResponse{TooManyRequestsApplicationProblemPlusJSONResponse{
			Body: newProblem(http.StatusTooManyRequests, "rate-limited", "Too many requests",
				"Too many wrong codes for this address. Use the link in the email, or try a code again later.", RequestIDFrom(ctx)),
			Headers: TooManyRequestsResponseHeaders{RetryAfter: secs},
		}}, nil
	}
	if err != nil && !errors.Is(err, auth.ErrInvalidToken) {
		return nil, err
	}
	if err != nil {
		return fail[VerifyEmailSignInCodedefaultApplicationProblemPlusJSONResponse](
			unauthorized(ctx, "That code is wrong or has expired. Check the latest email, or request a new one.")), nil
	}
	return VerifyEmailSignInCode200JSONResponse{Data: sessionBody(t, u, created), Meta: metaAt(s.now())}, nil
}

func (s *Server) VerifyEmailSignIn(ctx context.Context, req VerifyEmailSignInRequestObject) (VerifyEmailSignInResponseObject, error) {
	if req.Body == nil || req.Body.Token == "" {
		return fail[VerifyEmailSignIndefaultApplicationProblemPlusJSONResponse](invalid(ctx, "token is required.")), nil
	}
	t, u, created, err := s.auth.VerifyEmail(ctx, req.Body.Token)
	if err != nil && !errors.Is(err, auth.ErrInvalidToken) {
		return nil, err
	}
	if err != nil {
		return fail[VerifyEmailSignIndefaultApplicationProblemPlusJSONResponse](
			unauthorized(ctx, "This sign-in link has expired or was already used. Request a new one.")), nil
	}
	return VerifyEmailSignIn200JSONResponse{Data: sessionBody(t, u, created), Meta: metaAt(s.now())}, nil
}

func (s *Server) RefreshSession(ctx context.Context, req RefreshSessionRequestObject) (RefreshSessionResponseObject, error) {
	if req.Body == nil || req.Body.RefreshToken == "" {
		return fail[RefreshSessiondefaultApplicationProblemPlusJSONResponse](invalid(ctx, "refreshToken is required.")), nil
	}
	t, u, err := s.auth.Refresh(ctx, req.Body.RefreshToken)
	if err != nil && !errors.Is(err, auth.ErrInvalidToken) {
		return nil, err
	}
	if revoked := (*auth.RevokedError)(nil); errors.As(err, &revoked) {
		// A reused (copied) or expired refresh token ended the session:
		// so do its sockets.
		s.revokeRealtime(ctx, realtime.Revocation{SessionID: revoked.SessionID})
	}
	if err != nil {
		return fail[RefreshSessiondefaultApplicationProblemPlusJSONResponse](unauthorized(ctx, "Sign in again.")), nil
	}
	return RefreshSession200JSONResponse{Data: sessionBody(t, u, false), Meta: metaAt(s.now())}, nil
}

func (s *Server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	if err := s.auth.Revoke(ctx, sessionFrom(ctx)); err != nil {
		return nil, err
	}
	s.revokeRealtime(ctx, realtime.Revocation{SessionID: sessionFrom(ctx)})
	return Logout204Response{}, nil
}

// ---------------------------------------------------------------- me

func (s *Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	u, err := s.auth.User(ctx, userFrom(ctx))
	if err != nil && !errors.Is(err, auth.ErrNoUser) {
		return nil, err
	}
	if err != nil {
		return fail[GetMedefaultApplicationProblemPlusJSONResponse](unauthorized(ctx, "Sign in to continue.")), nil
	}
	return GetMe200JSONResponse{Data: userBody(u), Meta: metaAt(s.now())}, nil
}

var avatarColorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (s *Server) UpdateMe(ctx context.Context, req UpdateMeRequestObject) (UpdateMeResponseObject, error) {
	// avatarColor: null goes back to the default colour ("" to the store).
	color := req.Body.AvatarColor
	if req.Body != nil && color == nil && explicitNull(ctx, "avatarColor") {
		color = ptr("")
	}
	if req.Body == nil || (req.Body.DisplayName == nil && color == nil) {
		return fail[UpdateMedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "Send displayName or avatarColor.")), nil
	}
	var errs []ProblemError
	var name *string
	if req.Body.DisplayName != nil {
		n := strings.TrimSpace(*req.Body.DisplayName)
		if c := utf8.RuneCountInString(n); c < 1 || c > 32 {
			errs = append(errs, ProblemError{Code: "invalid_length", Message: "Display name must be 1 to 32 characters.", Field: ptr("/displayName")})
		}
		name = &n
	}
	if c := req.Body.AvatarColor; c != nil && !avatarColorPattern.MatchString(*c) {
		errs = append(errs, ProblemError{Code: "invalid_color", Message: "avatarColor must look like #2f6fed.", Field: ptr("/avatarColor")})
	}
	if len(errs) > 0 {
		return fail[UpdateMedefaultApplicationProblemPlusJSONResponse](invalid(ctx, "The profile is not valid.", errs...)), nil
	}
	u, err := s.auth.UpdateUser(ctx, userFrom(ctx), name, color)
	if err != nil && !errors.Is(err, auth.ErrNoUser) {
		return nil, err
	}
	if err != nil {
		return fail[UpdateMedefaultApplicationProblemPlusJSONResponse](unauthorized(ctx, "Sign in to continue.")), nil
	}
	return UpdateMe200JSONResponse{Data: userBody(u), Meta: metaAt(s.now())}, nil
}

// DeleteMe deletes the account (docs/account-deletion.md): in one
// transaction, in each league the user's pending waiver claims are
// cancelled and open trade offers withdrawn or declined as the user leaves
// it (pre-draft
// teams go, drafted ones are orphaned and play on, on autopick during a
// draft), a commissioner's role passes to the longest-standing member,
// and the account, its sessions and identities are deleted.
func (s *Server) DeleteMe(ctx context.Context, _ DeleteMeRequestObject) (DeleteMeResponseObject, error) {
	uid := userFrom(ctx)
	ls, err := s.leagues.ForUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	apple, err := s.auth.AppleRefreshTokens(ctx, uid)
	if err != nil {
		return nil, err
	}
	changes, err := s.removeAccount(ctx, uid)
	if err != nil {
		return nil, err
	}
	s.notifier().Notify(ctx, changes...)
	s.revokeAppleTokens(ctx, uid, apple)
	s.revokeRealtime(ctx, realtime.Revocation{UserID: uid})
	for _, l := range ls {
		s.leagueChanged(ctx, l.ID)
	}
	return DeleteMe204Response{}, nil
}

// removeAccount withdraws the user's pending moves, takes the user out of
// every league and deletes the account, in one transaction on Postgres:
// either the user is gone or nothing changed, and a retry starts over. The
// memory stores have no shared transaction and run the two steps in order.
// It returns the league changes to report.
func (s *Server) removeAccount(ctx context.Context, uid string) ([]league.Change, error) {
	pgLeagues, okL := s.leagues.Repo().(*league.Postgres)
	_, okA := s.auth.(*auth.Postgres)
	if okL && okA {
		var changes []league.Change
		err := pgLeagues.TxWith(ctx, func(tx league.Tx, db store.DB) error {
			var err error
			if changes, err = league.RemoveUserEverywhereIn(ctx, tx, uid, s.now()); err != nil {
				return err
			}
			return auth.DeleteUserIn(ctx, db, uid)
		})
		return changes, err
	}
	changes, err := s.leagues.RemoveUserEverywhere(ctx, uid)
	if err != nil {
		return nil, err
	}
	return changes, s.auth.DeleteUser(ctx, uid)
}

func ptr[T any](v T) *T { return &v }
