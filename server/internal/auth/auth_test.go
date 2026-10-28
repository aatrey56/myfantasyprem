package auth

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var ctx = context.Background()

// impls runs a test against every Service implementation: Memory always,
// Postgres when TEST_DATABASE_URL is set (see storetest).
func impls(t *testing.T, test func(t *testing.T, s Service, c *clock)) {
	t.Run("memory", func(t *testing.T) {
		c := &clock{t: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
		test(t, NewMemory(c.now), c)
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		c := &clock{t: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
		test(t, NewPostgres(pool, c.now), c)
	})
}

// must fails the test on err and returns v.
func must[T any](t *testing.T) func(v T, err error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

func startEmail(t *testing.T, s Service, email string) (string, time.Time) {
	t.Helper()
	in, err := s.StartEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	return in.Token, in.ExpiresAt
}

func startCode(t *testing.T, s Service, email string) EmailSignIn {
	t.Helper()
	in, err := s.StartEmail(ctx, email)
	if err != nil {
		t.Fatal(err)
	}
	return in
}

// wrongCode is a 6-digit code other than code.
func wrongCode(code string, n int) string {
	for i := 0; ; i++ {
		c := fmt.Sprintf("%06d", (n*7919+i)%1_000_000)
		if c != code {
			return c
		}
	}
}

func TestSignInCodeIsSixDigitsAndSignsIn(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		in := startCode(t, s, "Sam@Example.com ")
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(in.Code) || !in.ExpiresAt.Equal(c.t.Add(MagicLinkTTL)) {
			t.Fatalf("started = %+v", in)
		}
		// The address is matched as typed in the request, any case.
		tokens, u, created, err := s.VerifyEmailCode(ctx, " SAM@example.com", in.Code)
		if err != nil || !created || u.Email != "sam@example.com" {
			t.Fatalf("verify = %+v, %v, %v", u, created, err)
		}
		if uid, _, err := s.Authenticate(ctx, tokens.AccessToken); err != nil || uid != u.ID {
			t.Errorf("authenticate = %q, %v", uid, err)
		}
		// Single use: the code and the link are both spent.
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("code reused: err = %v", err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, in.Token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("link after the code: err = %v", err)
		}
		// Codes differ between sign-ins (one in a million collide).
		a, b := startCode(t, s, "a@example.com"), startCode(t, s, "a@example.com")
		if a.Code == b.Code && startCode(t, s, "a@example.com").Code == a.Code {
			t.Errorf("codes repeat: %s", a.Code)
		}
	})
}

func TestSignInCodeWrongCodeAndOtherAddress(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		in := startCode(t, s, "sam@example.com")
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", wrongCode(in.Code, 1)); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("wrong code: err = %v", err)
		}
		// The right code for another address does not work.
		if _, _, _, err := s.VerifyEmailCode(ctx, "kim@example.com", in.Code); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("other address: err = %v", err)
		}
		// One wrong guess does not end the sign-in.
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); err != nil {
			t.Fatalf("right code after a wrong one: %v", err)
		}
	})
}

func TestSignInCodeExpires(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		in := startCode(t, s, "sam@example.com")
		live := startCode(t, s, "kim@example.com")
		c.t = c.t.Add(MagicLinkTTL - time.Second)
		if _, _, _, err := s.VerifyEmailCode(ctx, "kim@example.com", live.Code); err != nil {
			t.Fatalf("code a second before expiry: %v", err)
		}
		c.t = c.t.Add(time.Second)
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("expired code: err = %v", err)
		}
	})
}

// The link and the code are one sign-in: whichever is used first spends both.
func TestSignInCodeAfterLinkUsed(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		in := startCode(t, s, "sam@example.com")
		if _, _, _, err := s.VerifyEmail(ctx, in.Token); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("code after the link: err = %v", err)
		}
		// A new email's code works again.
		next := startCode(t, s, "sam@example.com")
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", next.Code); err != nil {
			t.Fatalf("new code: %v", err)
		}
	})
}

// Only the latest email's code works: an older sign-in's code is refused
// (its link still works).
func TestSignInCodeLatestOnly(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		old := startCode(t, s, "sam@example.com")
		c.t = c.t.Add(time.Second)
		latest := startCode(t, s, "sam@example.com")
		if old.Code != latest.Code {
			if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", old.Code); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("older code: err = %v", err)
			}
		}
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", latest.Code); err != nil {
			t.Fatalf("latest code: %v", err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, old.Token); err != nil {
			t.Fatalf("older link: %v", err)
		}
	})
}

// CodeAttempts wrong codes kill the sign-in: the right code and the link
// stop working.
func TestSignInCodeLockoutAfterFiveWrong(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		in := startCode(t, s, "sam@example.com")
		for i := range CodeAttempts {
			if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", wrongCode(in.Code, i)); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("wrong code %d: err = %v", i+1, err)
			}
		}
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("right code after %d wrong: err = %v", CodeAttempts, err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, in.Token); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("link of a dead sign-in: err = %v", err)
		}
		// Four wrong then right still signs in.
		in = startCode(t, s, "kim@example.com")
		for i := range CodeAttempts - 1 {
			_, _, _, _ = s.VerifyEmailCode(ctx, "kim@example.com", wrongCode(in.Code, i))
		}
		if _, _, _, err := s.VerifyEmailCode(ctx, "kim@example.com", in.Code); err != nil {
			t.Fatalf("right code after %d wrong: %v", CodeAttempts-1, err)
		}
	})
}

// An address takes CodeFailuresPerEmail wrong codes per CodeFailureWindow
// across sign-ins; then even the right code is refused (not compared)
// until the oldest failure leaves the window. Links keep working.
func TestSignInCodePerEmailLimit(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		start := c.t
		failures := 0
		for failures < CodeFailuresPerEmail {
			in := startCode(t, s, "sam@example.com")
			for i := 0; i < CodeAttempts && failures < CodeFailuresPerEmail; i++ {
				if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", wrongCode(in.Code, failures)); !errors.Is(err, ErrInvalidToken) {
					t.Fatalf("wrong code %d: err = %v", failures+1, err)
				}
				failures++
				c.t = c.t.Add(time.Minute)
			}
		}
		in := startCode(t, s, "sam@example.com")
		_, _, _, err := s.VerifyEmailCode(ctx, "Sam@Example.com", in.Code)
		var limited *CodeLimitError
		if !errors.Is(err, ErrTooManyCodes) || !errors.As(err, &limited) {
			t.Fatalf("over the limit: err = %v", err)
		}
		// The first failure was at start; it leaves the window then.
		if want := start.Add(CodeFailureWindow).Sub(c.t); limited.RetryAfter != want {
			t.Errorf("retry after %v, want %v", limited.RetryAfter, want)
		}
		// Other addresses are not affected; the link still works.
		other := startCode(t, s, "kim@example.com")
		if _, _, _, err := s.VerifyEmailCode(ctx, "kim@example.com", other.Code); err != nil {
			t.Fatalf("another address: %v", err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, in.Token); err != nil {
			t.Fatalf("link while codes are locked: %v", err)
		}
		// Once the oldest failure is a window old, one more guess is allowed.
		c.t = start.Add(CodeFailureWindow)
		in = startCode(t, s, "sam@example.com")
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); err != nil {
			t.Fatalf("after the window: %v", err)
		}
	})
}

// Prune keeps failures inside the window (they still count) and drops older ones.
func TestPruneCodeFailures(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		in := startCode(t, s, "sam@example.com")
		for i := range CodeFailuresPerEmail {
			if i%CodeAttempts == 0 {
				in = startCode(t, s, "sam@example.com")
			}
			_, _, _, _ = s.VerifyEmailCode(ctx, "sam@example.com", wrongCode(in.Code, i))
		}
		if _, err := s.Prune(ctx); err != nil {
			t.Fatal(err)
		}
		in = startCode(t, s, "sam@example.com")
		if _, _, _, err := s.VerifyEmailCode(ctx, "sam@example.com", in.Code); !errors.Is(err, ErrTooManyCodes) {
			t.Fatalf("prune forgot live failures: err = %v", err)
		}
		c.t = c.t.Add(CodeFailureWindow)
		if n, err := s.Prune(ctx); err != nil || n < CodeFailuresPerEmail {
			t.Fatalf("pruned %d, %v; want the %d failures at least", n, err, CodeFailuresPerEmail)
		}
	})
}

func TestMagicLinkSignInIsSingleUseAndExpires(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		token, exp := startEmail(t, s, "Sam@Example.com ")
		if !exp.Equal(c.t.Add(MagicLinkTTL)) {
			t.Errorf("expiresAt = %v", exp)
		}
		tokens, u, created, err := s.VerifyEmail(ctx, token)
		if err != nil || !created || u.Email != "sam@example.com" || u.DisplayName != "sam" {
			t.Fatalf("verify = %+v, %v, %v", u, created, err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, token); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("second use: err = %v", err)
		}
		if uid, _, err := s.Authenticate(ctx, tokens.AccessToken); err != nil || uid != u.ID {
			t.Errorf("authenticate = %q, %v", uid, err)
		}

		// The same address signs in to the same account.
		token2, _ := startEmail(t, s, "sam@example.com")
		_, u2, created2, _ := s.VerifyEmail(ctx, token2)
		if created2 || u2.ID != u.ID {
			t.Errorf("second sign-in created a new user")
		}

		token3, _ := startEmail(t, s, "late@example.com")
		c.t = c.t.Add(MagicLinkTTL)
		if _, _, _, err := s.VerifyEmail(ctx, token3); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("expired link: err = %v", err)
		}
	})
}

func TestAccessTokenExpires(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		u := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		tok := must[Tokens](t)(s.Issue(ctx, u.ID))
		c.t = c.t.Add(AccessTTL - time.Second)
		if _, _, err := s.Authenticate(ctx, tok.AccessToken); err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(time.Second)
		if _, _, err := s.Authenticate(ctx, tok.AccessToken); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("err = %v", err)
		}
		if _, _, err := s.Authenticate(ctx, "nonsense"); !errors.Is(err, ErrInvalidToken) {
			t.Errorf("unknown token: err = %v", err)
		}
	})
}

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	impls(t, func(t *testing.T, s Service, _ *clock) {
		u := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		first := must[Tokens](t)(s.Issue(ctx, u.ID))
		second, _, err := s.Refresh(ctx, first.RefreshToken)
		if err != nil {
			t.Fatal(err)
		}
		if second.AccessToken == first.AccessToken || second.RefreshToken == first.RefreshToken {
			t.Fatal("tokens not rotated")
		}
		if _, _, err := s.Authenticate(ctx, first.AccessToken); err == nil {
			t.Error("old access token still works")
		}
		if _, _, err := s.Authenticate(ctx, second.AccessToken); err != nil {
			t.Errorf("new access token: %v", err)
		}
		// Reusing the old refresh token revokes the whole session.
		_, _, err = s.Refresh(ctx, first.RefreshToken)
		var revoked *RevokedError
		if !errors.Is(err, ErrInvalidToken) || !errors.As(err, &revoked) || revoked.SessionID != first.SessionID {
			t.Errorf("reuse: err = %v, want a RevokedError for %s", err, first.SessionID)
		}
		if _, _, err := s.Authenticate(ctx, second.AccessToken); err == nil {
			t.Error("session survived refresh-token reuse")
		}
		if _, _, err := s.Refresh(ctx, second.RefreshToken); err == nil {
			t.Error("refresh after revocation worked")
		}
	})
}

func TestRevokeAndDeleteUser(t *testing.T) {
	impls(t, func(t *testing.T, s Service, _ *clock) {
		u := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		a := must[Tokens](t)(s.Issue(ctx, u.ID))
		b := must[Tokens](t)(s.Issue(ctx, u.ID))
		_ = s.Revoke(ctx, a.SessionID)
		if _, _, err := s.Authenticate(ctx, a.AccessToken); err == nil {
			t.Error("revoked session works")
		}
		if _, _, err := s.Authenticate(ctx, b.AccessToken); err != nil {
			t.Errorf("other session: %v", err)
		}
		if ok, err := s.SessionActive(ctx, a.SessionID); ok || err != nil {
			t.Errorf("revoked session active = %v, %v", ok, err)
		}
		if ok, err := s.SessionActive(ctx, b.SessionID); !ok || err != nil {
			t.Errorf("live session active = %v, %v", ok, err)
		}
		_ = s.DeleteUser(ctx, u.ID)
		if ok, err := s.SessionActive(ctx, b.SessionID); ok || err != nil {
			t.Errorf("deleted user's session active = %v, %v", ok, err)
		}
		if _, _, err := s.Authenticate(ctx, b.AccessToken); err == nil {
			t.Error("deleted user's session works")
		}
		if _, err := s.User(ctx, u.ID); !errors.Is(err, ErrNoUser) {
			t.Errorf("err = %v", err)
		}
		if _, err := s.Issue(ctx, u.ID); !errors.Is(err, ErrNoUser) {
			t.Errorf("issue for deleted user: %v", err)
		}
	})
}

// Deleting an account also deletes the sign-in links sent to its email:
// an unused one must not bring the account back.
func TestDeleteUserPurgesMagicLinks(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		u := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		pending, _ := startEmail(t, s, "Sam@Example.com")
		other, _ := startEmail(t, s, "priya@example.com")
		if err := s.DeleteUser(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, pending); !errors.Is(err, ErrInvalidToken) {
			t.Fatalf("a link sent before deletion still signs in: %v", err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, other); err != nil {
			t.Fatalf("another address's link: %v", err)
		}
	})
}

// An Apple sign-in keeps the (sealed) refresh token on the identity, the
// newest one wins, and it goes with the account.
func TestAppleRefreshTokens(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		id := Identity{Provider: "apple", Subject: "001.apple", Email: "sam@privaterelay.example", EmailVerified: true, AppleRefreshToken: "sealed-1"}
		_, u, _, err := s.SignInWithIdentity(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got, err := s.AppleRefreshTokens(ctx, u.ID); err != nil || len(got) != 1 || got[0] != "sealed-1" {
			t.Fatalf("tokens = %v %v", got, err)
		}
		// A later sign-in without a token keeps the one stored; with one,
		// replaces it.
		id.AppleRefreshToken = ""
		if _, _, _, err := s.SignInWithIdentity(ctx, id); err != nil {
			t.Fatal(err)
		}
		id.AppleRefreshToken = "sealed-2"
		if _, _, _, err := s.SignInWithIdentity(ctx, id); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.AppleRefreshTokens(ctx, u.ID); len(got) != 1 || got[0] != "sealed-2" {
			t.Fatalf("tokens after re-sign-in = %v", got)
		}
		if err := s.DeleteUser(ctx, u.ID); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.AppleRefreshTokens(ctx, u.ID); len(got) != 0 {
			t.Fatalf("tokens after deletion = %v", got)
		}
	})
}

// Prune drops expired or used magic links and sessions past their refresh
// expiry, and keeps live ones.
func TestPrune(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		u := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		startEmail(t, s, "old@example.com")
		used, _ := startEmail(t, s, "used@example.com")
		must[Tokens](t)(s.Issue(ctx, u.ID))
		if _, _, _, err := s.VerifyEmail(ctx, used); err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(RefreshTTL + time.Minute)
		fresh := must[Tokens](t)(s.Issue(ctx, u.ID))
		live, _ := startEmail(t, s, "live@example.com")
		n, err := s.Prune(ctx)
		// Two links (expired, used) and two sessions (Sam's first one and
		// the one the used link opened; both past refresh expiry).
		if err != nil || n != 4 {
			t.Fatalf("pruned %d, %v; want 4", n, err)
		}
		if _, _, err := s.Authenticate(ctx, fresh.AccessToken); err != nil {
			t.Errorf("live session pruned: %v", err)
		}
		if _, _, _, err := s.VerifyEmail(ctx, live); err != nil {
			t.Errorf("live link pruned: %v", err)
		}
		// Now only the link just used goes.
		if n, _ := s.Prune(ctx); n != 1 {
			t.Errorf("second prune = %d, want 1", n)
		}
	})
}

func TestUpdateUser(t *testing.T) {
	impls(t, func(t *testing.T, s Service, _ *clock) {
		u := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		name, color := "Sam P.", "#112233"
		got, err := s.UpdateUser(ctx, u.ID, &name, &color)
		if err != nil || got.DisplayName != name || got.AvatarColor != color {
			t.Fatalf("got %+v, %v", got, err)
		}
		got, _ = s.UpdateUser(ctx, u.ID, nil, nil)
		if got.DisplayName != name {
			t.Errorf("nil changed the name")
		}
	})
}

func TestDisplayNamesAndProviders(t *testing.T) {
	impls(t, func(t *testing.T, s Service, _ *clock) {
		a := must[*User](t)(s.CreateUser(ctx, "Sam", "sam@example.com"))
		b := must[*User](t)(s.CreateUser(ctx, "Priya", "priya@example.com"))
		names := must[map[string]string](t)(s.DisplayNames(ctx, []string{a.ID, b.ID, "usr_nobody"}))
		if len(names) != 2 || names[a.ID] != "Sam" || names[b.ID] != "Priya" {
			t.Fatalf("names = %v", names)
		}
		if len(a.Providers) != 1 || a.Providers[0] != "email" {
			t.Fatalf("providers = %v", a.Providers)
		}
	})
}

// An account from a claim link has only a display name, and its session
// works like any other (docs/adr/0013).
func TestCreateAccountWithOnlyAName(t *testing.T) {
	impls(t, func(t *testing.T, s Service, c *clock) {
		tokens, u, err := s.CreateAccount(ctx, "  Jordan K.  ")
		if err != nil {
			t.Fatal(err)
		}
		if u.DisplayName != "Jordan K." || u.Email != "" || len(u.Providers) != 0 || !u.CreatedAt.Equal(c.t) {
			t.Errorf("user = %+v", u)
		}
		uid, _, err := s.Authenticate(ctx, tokens.AccessToken)
		if err != nil || uid != u.ID {
			t.Fatalf("authenticate = %q %v", uid, err)
		}
		if _, ru, err := s.Refresh(ctx, tokens.RefreshToken); err != nil || ru.ID != u.ID {
			t.Fatalf("refresh = %+v %v", ru, err)
		}
		// Two accounts with the same name are two people.
		_, other, err := s.CreateAccount(ctx, "Jordan K.")
		if err != nil || other.ID == u.ID {
			t.Fatalf("second account = %+v %v", other, err)
		}
	})
}
