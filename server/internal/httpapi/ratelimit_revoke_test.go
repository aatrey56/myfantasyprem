package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// eachAuthStore runs test on the memory auth store and, when
// TEST_DATABASE_URL is set, the Postgres one.
func eachAuthStore(t *testing.T, test func(t *testing.T, pool *pgxpool.Pool)) {
	t.Run("memory", func(t *testing.T) { test(t, nil) })
	t.Run("postgres", func(t *testing.T) { test(t, storetest.New(t)) })
}

// issueSession signs up a new user and starts a session for them.
func issueSession(t *testing.T, srv *Server, email string) auth.Tokens {
	t.Helper()
	ctx := context.Background()
	u, err := srv.auth.CreateUser(ctx, "Kim", email)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := srv.auth.Issue(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

// drainAuthBucket spends the /v1/auth/* bucket of ip with bogus requests,
// as someone sharing the victim's public address could.
func drainAuthBucket(t *testing.T, h http.Handler, ip string) {
	t.Helper()
	hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, ip, "", "", "POST", "/v1/auth/refresh", `{"refreshToken":"bogus`+strconv.Itoa(i)+`"}`)
	})
}

// Signing out is an authenticated revocation: other people's traffic from
// the same public IP must never block it (it once shared the per-IP
// /v1/auth/* bucket, so a drained bucket left the session live).
func TestLogoutNotBlockedBySharedIP(t *testing.T) {
	eachAuthStore(t, func(t *testing.T, pool *pgxpool.Pool) {
		srv, _ := newTestServerOn(pool)
		h := newHandler(testOptions(), newRoutes(testOptions(), srv))
		victim := issueSession(t, srv, "kim@example.com")
		const ip = "203.0.113.7"
		drainAuthBucket(t, h, ip)

		if rec := from(h, ip, victim.AccessToken, "", "POST", "/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("logout after the IP's auth bucket drained = %d %s", rec.Code, rec.Body)
		}
		// The session is gone: its refresh token no longer works (asked from
		// another address, clear of the drained bucket).
		body := `{"refreshToken":"` + victim.RefreshToken + `"}`
		if rec := from(h, "198.51.100.9", "", "", "POST", "/v1/auth/refresh", body); rec.Code != http.StatusUnauthorized {
			t.Fatalf("refresh after logout = %d %s", rec.Code, rec.Body)
		}
		if rec := from(h, "198.51.100.9", victim.AccessToken, "", "GET", "/v1/me", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("access token after logout = %d", rec.Code)
		}
		// Unauthenticated auth traffic from that IP is still limited, and so
		// is a logout with a token that does not authenticate.
		if rec := from(h, ip, "", "", "POST", "/v1/auth/email", emailBody(1)); rec.Code != http.StatusTooManyRequests {
			t.Errorf("sign-in from the drained IP = %d", rec.Code)
		}
		if rec := from(h, ip, "not-a-token", "", "POST", "/v1/auth/logout", ""); rec.Code != http.StatusTooManyRequests {
			t.Errorf("bogus logout from the drained IP = %d", rec.Code)
		}
	})
}

// Account deletion (which revokes every session) and logout must also get
// through when the shared IP's global bucket is spent.
func TestRevocationNotBlockedByGlobalIPLimit(t *testing.T) {
	eachAuthStore(t, func(t *testing.T, pool *pgxpool.Pool) {
		srv, _ := newTestServerOn(pool)
		opts := testOptions()
		l := DefaultRateLimits()
		l.Global = Limit{PerMinute: 60, Burst: 3}
		opts.RateLimits = &l
		h := newHandler(opts, newRoutes(opts, srv))
		const ip = "203.0.113.7"
		hitUntil429(t, 10, func(int) *httptest.ResponseRecorder {
			return from(h, ip, "", "", "GET", "/v1/clubs", "")
		})

		victim := issueSession(t, srv, "kim@example.com")
		if rec := from(h, ip, victim.AccessToken, "", "POST", "/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("logout after the IP's global bucket drained = %d %s", rec.Code, rec.Body)
		}
		leaver := issueSession(t, srv, "lee@example.com")
		if rec := from(h, ip, leaver.AccessToken, "", "DELETE", "/v1/me", ""); rec.Code != http.StatusNoContent {
			t.Fatalf("account deletion after the IP's global bucket drained = %d %s", rec.Code, rec.Body)
		}
		body := `{"refreshToken":"` + leaver.RefreshToken + `"}`
		if rec := from(h, "198.51.100.9", "", "", "POST", "/v1/auth/refresh", body); rec.Code != http.StatusUnauthorized {
			t.Fatalf("refresh after deletion = %d", rec.Code)
		}
		// Ordinary authenticated routes from that IP stay limited.
		if rec := from(h, ip, testToken, "", "GET", "/v1/me", ""); rec.Code != http.StatusTooManyRequests {
			t.Errorf("GET /v1/me from the drained IP = %d", rec.Code)
		}
	})
}

// Revocations have their own limit per session: one session spending it
// does not touch another's, and the default is generous.
func TestRevocationLimitedPerSession(t *testing.T) {
	rl := newRateLimiter(RateLimits{Revoke: Limit{PerMinute: 1, Burst: 2}}, nil, func() time.Time {
		return time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	})
	mux := http.NewServeMux()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Handle("POST /v1/auth/logout", ok)
	mux.Handle("DELETE /v1/me", ok)
	h := rl.limitByUser(mux, mux)
	send := func(sessionID, method, target string) int {
		req := httptest.NewRequest(method, target, nil)
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, principal{userID: "usr_1", sessionID: sessionID}))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if a, b := send("ses_a", "POST", "/v1/auth/logout"), send("ses_a", "DELETE", "/v1/me"); a != 204 || b != 204 {
		t.Fatalf("within the burst = %d, %d", a, b)
	}
	if c := send("ses_a", "POST", "/v1/auth/logout"); c != http.StatusTooManyRequests {
		t.Errorf("over the session's limit = %d", c)
	}
	if c := send("ses_b", "POST", "/v1/auth/logout"); c != 204 {
		t.Errorf("another session of the same user = %d", c)
	}
	if d := DefaultRateLimits().Revoke; d.Burst < 10 || d.PerMinute < 30 {
		t.Errorf("default revoke limit %+v is not generous", d)
	}
}
