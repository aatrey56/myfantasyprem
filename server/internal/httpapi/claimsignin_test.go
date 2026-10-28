package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// claimBody is a POST /v1/auth/claim body.
func claimBody(code, name string) string {
	b, _ := json.Marshal(map[string]string{"code": code, "displayName": name})
	return string(b)
}

// claimCode is the current claim link of l's team.
func claimCode(t *testing.T, srv *Server, leagueID, teamID string) string {
	t.Helper()
	l, err := srv.leagues.Repo().Get(context.Background(), leagueID)
	if err != nil {
		t.Fatal(err)
	}
	inv, ok := l.ClaimLink(teamID)
	if !ok {
		t.Fatalf("team %s has no claim link", teamID)
	}
	return inv.Code
}

// countUsers counts the accounts on Postgres (-1 on memory).
func countUsers(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	if pool == nil {
		return -1
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM users`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A seat's claim link signs a signed-out person in (docs/adr/0013): with
// only a display name they get a new account, the seat and a session that
// works; the link is then used up, a regenerated link replaces the old
// one, ordinary invites still need sign-in, and with CLAIM_LINK_SIGNIN off
// claim links need sign-in too.
func TestClaimSignIn(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		t.Run(store, func(t *testing.T) {
			var pool *pgxpool.Pool
			if store == "postgres" {
				pool = storetest.New(t)
			}
			srv, _ := newTestServerOn(pool)
			srv.claimSignIn = true
			ctx := context.Background()
			h := newHandler(testOptions(), newRoutes(testOptions(), srv))
			commish := userOf(t, srv, testToken)
			member := userOf(t, srv, signIn(srv, "Priya N.", "priya@example.com"))
			l := importedLeague(t, srv, commish, member, true)
			seat, other := l.Teams[2], l.Teams[3]
			code := claimCode(t, srv, l.ID, seat.ID)

			var preview InvitePreviewResponse
			rec := doAnon(h, http.MethodGet, "/v1/invites/"+code, "")
			if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("preview = %d %s", rec.Code, rec.Body)
			}
			if !preview.Data.ClaimSignIn || preview.Data.ClaimTeam == nil || preview.Data.ClaimTeam.TeamName != seat.Name {
				t.Fatalf("preview = %+v", preview.Data)
			}

			// Signed out: a display name is all it takes.
			rec = doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(code, "  Jordan K.  "))
			if rec.Code != http.StatusOK {
				t.Fatalf("claim = %d %s", rec.Code, rec.Body)
			}
			var got ClaimSignInResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			sess, m := got.Data.Session, got.Data.Membership
			if !sess.IsNewUser || sess.User.DisplayName != "Jordan K." || sess.User.Email != nil || len(sess.User.AuthProviders) != 0 ||
				sess.AccessToken == "" || sess.RefreshToken == "" {
				t.Errorf("session = %+v", sess)
			}
			if m.League.Id != l.ID || m.Team.Id != seat.ID || m.Team.Name != seat.Name || m.Role != LeagueRoleMember {
				t.Errorf("membership = %+v", m)
			}
			// The session works: the account, the league with the seat as
			// its team, and a refresh.
			var me UserResponse
			rec = doAs(h, sess.AccessToken, http.MethodGet, "/v1/me", "")
			if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil || rec.Code != http.StatusOK || me.Data.Id != sess.User.Id {
				t.Fatalf("me = %d %s", rec.Code, rec.Body)
			}
			after, err := srv.leagues.Get(ctx, me.Data.Id, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mine, ok := after.TeamOf(me.Data.Id); !ok || mine.ID != seat.ID {
				t.Errorf("my team = %+v %v", mine, ok)
			}
			var mine MyLeagueListResponse
			rec = doAs(h, sess.AccessToken, http.MethodGet, "/v1/me/leagues", "")
			if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil || rec.Code != http.StatusOK ||
				len(mine.Data) != 1 || mine.Data[0].League.Id != l.ID || mine.Data[0].Team.Id != seat.ID {
				t.Errorf("my leagues = %d %s", rec.Code, rec.Body)
			}
			if rec := doAnon(h, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"`+sess.RefreshToken+`"}`); rec.Code != http.StatusOK {
				t.Errorf("refresh = %d %s", rec.Code, rec.Body)
			}

			// The link is used up: 409 in plain English, and no account made.
			users := countUsers(t, pool)
			p := decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(code, "Someone Else")), http.StatusConflict)
			if problemType(p) != problemTypeBase+"seat-claimed" || p.Detail == nil || !strings.Contains(*p.Detail, seat.Name+" has already been claimed") {
				t.Errorf("reuse = %+v", p)
			}
			if n := countUsers(t, pool); n != users {
				t.Errorf("a refused claim made an account: %d users, want %d", n, users)
			}

			// Regenerating the other seat's link: the old one is gone, the new one works.
			old := claimCode(t, srv, l.ID, other.ID)
			rec = do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/seats/"+other.ID+"/invite", "")
			if rec.Code != http.StatusCreated {
				t.Fatalf("resend = %d %s", rec.Code, rec.Body)
			}
			fresh := claimCode(t, srv, l.ID, other.ID)
			p = decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(old, "Alex")), http.StatusGone)
			if problemType(p) != problemTypeBase+"invite-expired" {
				t.Errorf("old link = %+v", p)
			}
			if n := countUsers(t, pool); n != users {
				t.Errorf("a revoked link made an account: %d users, want %d", n, users)
			}
			decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody("ZZZZZZZZZZ", "Alex")), http.StatusNotFound)
			decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(fresh, "   ")), http.StatusUnprocessableEntity)
			decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(fresh, strings.Repeat("x", 33))), http.StatusUnprocessableEntity)
			decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody("bad", "Alex")), http.StatusBadRequest)

			// An ordinary invite still needs sign-in.
			native, err := srv.leagues.Create(ctx, commish, "Native", 4, "Mine", league.SettingsInput{})
			if err != nil {
				t.Fatal(err)
			}
			inv, err := srv.leagues.CreateInvite(ctx, commish, native.ID, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			p = decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(inv.Code, "Alex")), http.StatusUnauthorized)
			if problemType(p) != problemTypeBase+"sign-in-required" {
				t.Errorf("ordinary invite = %+v", p)
			}
			rec = doAnon(h, http.MethodGet, "/v1/invites/"+inv.Code, "")
			if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || preview.Data.ClaimSignIn {
				t.Errorf("ordinary invite preview = %s", rec.Body)
			}

			// CLAIM_LINK_SIGNIN off: the claim link needs sign-in again.
			srv.claimSignIn = false
			rec = doAnon(h, http.MethodGet, "/v1/invites/"+fresh, "")
			if err := json.Unmarshal(rec.Body.Bytes(), &preview); err != nil || preview.Data.ClaimSignIn || preview.Data.ClaimTeam == nil {
				t.Errorf("preview with the setting off = %s", rec.Body)
			}
			p = decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(fresh, "Alex")), http.StatusUnauthorized)
			if problemType(p) != problemTypeBase+"claim-sign-in-off" {
				t.Errorf("setting off = %+v", p)
			}
			if n := countUsers(t, pool); n != users {
				t.Errorf("the setting off still made an account: %d users, want %d", n, users)
			}
			// A signed-in person still claims with their own account.
			friend := signIn(srv, "Alex", "alex@example.com")
			if rec := doAs(h, friend, http.MethodPost, "/v1/invites/"+fresh+"/accept", `{}`); rec.Code != http.StatusCreated {
				t.Errorf("signed-in claim = %d %s", rec.Code, rec.Body)
			}
		})
	}
}

// The claim-and-sign-in endpoint shares the per-IP /v1/auth/* limit.
func TestRateLimitClaimSignInPerIP(t *testing.T) {
	h, srv := limitedHandler(t, nil)
	srv.claimSignIn = true
	ok, rec := hitUntil429(t, 100, func(int) *httptest.ResponseRecorder {
		return from(h, "203.0.113.7", "", "", "POST", "/v1/auth/claim", claimBody("ZZZZZZZZZZ", "Guess"))
	})
	if want := DefaultRateLimits().Auth.Burst; ok != want {
		t.Errorf("%d claims went through, want the burst of %d", ok, want)
	}
	check429(t, rec, httptest.NewRequest("POST", "/v1/auth/claim", strings.NewReader(claimBody("ZZZZZZZZZZ", "Guess"))))
	if rec := from(h, "203.0.113.7", "", "", "POST", "/v1/auth/refresh", `{"refreshToken":"x"}`); rec.Code != 429 {
		t.Errorf("refresh from the same IP = %d", rec.Code)
	}
	if rec := from(h, "203.0.113.8", "", "", "POST", "/v1/auth/claim", claimBody("ZZZZZZZZZZ", "Guess")); rec.Code != http.StatusNotFound {
		t.Errorf("another IP = %d", rec.Code)
	}
}

// A claim code is a credential: neither the preview, the accept nor the
// claim sign-in writes it to the log.
func TestClaimCodeNotLogged(t *testing.T) {
	var logs bytes.Buffer
	srv, _ := newTestServer()
	srv.claimSignIn = true
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv.log = opts.Logger
	h := newHandler(opts, newRoutes(opts, srv))
	commish := userOf(t, srv, testToken)
	l := importedLeague(t, srv, commish, "", true)
	code := claimCode(t, srv, l.ID, l.Teams[2].ID)
	other := claimCode(t, srv, l.ID, l.Teams[3].ID)

	doAnon(h, http.MethodGet, "/v1/invites/"+code, "")
	doAnon(h, http.MethodGet, "/v1/invites/"+strings.ToLower(code), "")
	if rec := doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(code, "Jordan")); rec.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", rec.Code, rec.Body)
	}
	doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(code, "Again"))
	doAs(h, signIn(srv, "Alex", "alex@example.com"), http.MethodPost, "/v1/invites/"+other+"/accept", `{}`)
	out := strings.ToUpper(logs.String())
	if !strings.Contains(out, "/V1/INVITES/") || !strings.Contains(out, "/V1/AUTH/CLAIM") {
		t.Fatalf("the requests were not logged: %s", logs.String())
	}
	for _, c := range []string{code, other} {
		if strings.Contains(out, c) {
			t.Errorf("the log contains claim code %s: %s", c, logs.String())
		}
	}
}
