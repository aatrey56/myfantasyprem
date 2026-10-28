package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// signInLinkCodeOf is the code at the end of a sign-in link's URL.
func signInLinkCodeOf(t *testing.T, url string) string {
	t.Helper()
	_, code, ok := strings.Cut(url, "/signin-link/")
	if !ok || !signInLinkCode.MatchString(code) {
		t.Fatalf("sign-in link url = %q", url)
	}
	return code
}

// A manager who joined by claim link (no email) and signed out gets back
// in with a commissioner-issued sign-in link (docs/adr/0013): it signs
// them in as the seat's owner once, a newer link replaces it, only the
// commissioner issues one, and only for an account with no other sign-in.
func TestSeatSignInLink(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		t.Run(store, func(t *testing.T) {
			var pool *pgxpool.Pool
			if store == "postgres" {
				pool = storetest.New(t)
			}
			srv, _ := newTestServerOn(pool)
			srv.claimSignIn = true
			var logs bytes.Buffer
			opts := testOptions()
			opts.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			srv.log = opts.Logger
			h := newHandler(opts, newRoutes(opts, srv))
			commish := userOf(t, srv, testToken)
			memberTok := signIn(srv, "Priya N.", "priya@example.com")
			l := importedLeague(t, srv, commish, userOf(t, srv, memberTok), true)
			seat := l.Teams[2]

			// Jordan claims the seat signed out, then signs out.
			rec := doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(claimCode(t, srv, l.ID, seat.ID), "Jordan K."))
			var claimed ClaimSignInResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &claimed); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("claim = %d %s", rec.Code, rec.Body)
			}
			jordan := claimed.Data.Session.User.Id
			if rec := doAs(h, claimed.Data.Session.AccessToken, http.MethodPost, "/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
				t.Fatalf("logout = %d %s", rec.Code, rec.Body)
			}

			path := "/v1/leagues/" + l.ID + "/seats/" + seat.ID + "/signin-link"
			issue := func() SeatSignInLink {
				t.Helper()
				rec := do(h, http.MethodPost, path, "")
				var out SeatSignInLinkResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusCreated {
					t.Fatalf("issue = %d %s", rec.Code, rec.Body)
				}
				return out.Data
			}
			first := issue()
			if first.Owner.Id != jordan || first.Team.Id != seat.ID || !first.ExpiresAt.Equal(srv.now().Add(auth.SignInLinkTTL)) {
				t.Errorf("link = %+v", first)
			}
			second := issue()
			oldCode, code := signInLinkCodeOf(t, first.Url), signInLinkCodeOf(t, second.Url)

			body := func(c string) string { return `{"code":"` + c + `"}` }
			// The older link was replaced.
			decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/signin-link", body(oldCode)), http.StatusUnauthorized)
			rec = doAnon(h, http.MethodPost, "/v1/auth/signin-link", body(code))
			var got SignInLinkSessionResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || rec.Code != http.StatusOK {
				t.Fatalf("sign in = %d %s", rec.Code, rec.Body)
			}
			if got.Data.Session.User.Id != jordan || got.Data.LeagueId != l.ID || got.Data.Session.IsNewUser {
				t.Errorf("signed in = %+v", got.Data)
			}
			var mine MyLeagueListResponse
			rec = doAs(h, got.Data.Session.AccessToken, http.MethodGet, "/v1/me/leagues", "")
			if err := json.Unmarshal(rec.Body.Bytes(), &mine); err != nil || len(mine.Data) != 1 || mine.Data[0].Team.Id != seat.ID {
				t.Errorf("my leagues = %d %s", rec.Code, rec.Body)
			}
			// Once only.
			decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/signin-link", body(code)), http.StatusUnauthorized)

			// Commissioner only; not for unclaimed seats or managers who can sign in.
			if p := decodeProblem(t, doAs(h, memberTok, http.MethodPost, path, ""), http.StatusForbidden); p.Status != 403 {
				t.Errorf("member = %+v", p)
			}
			p := decodeProblem(t, do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/seats/"+l.Teams[3].ID+"/signin-link", ""), http.StatusConflict)
			if problemType(p) != problemTypeBase+"seat-unclaimed" {
				t.Errorf("unclaimed = %+v", p)
			}
			for _, tm := range l.Teams[:2] { // the commissioner's and Priya's: both have an email
				p := decodeProblem(t, do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/seats/"+tm.ID+"/signin-link", ""), http.StatusConflict)
				if problemType(p) != problemTypeBase+"has-sign-in" {
					t.Errorf("%s = %+v", tm.Name, p)
				}
			}

			// Limited per commissioner: the burst, then 429.
			n := 2
			for ; n < 50; n++ {
				if rec := do(h, http.MethodPost, path, ""); rec.Code == http.StatusTooManyRequests {
					if rec.Header().Get("Retry-After") == "" {
						t.Error("429 without Retry-After")
					}
					break
				}
			}
			if want := DefaultRateLimits().SignInLinkUser.Burst; n != want {
				t.Errorf("%d links issued before 429, want %d", n, want)
			}

			// The code is a credential: never in the log.
			if !strings.Contains(logs.String(), "/v1/auth/signin-link") {
				t.Fatalf("the requests were not logged")
			}
			for _, c := range []string{oldCode, code} {
				if strings.Contains(logs.String(), c) {
					t.Errorf("the log contains sign-in code %s", c)
				}
			}
		})
	}
}

// claimedSeat imports a league for the test user (commissioner) and has
// Jordan claim its third seat by claim link, signed out (no email).
func claimedSeat(t *testing.T, srv *Server, h http.Handler) (l *league.League, seat league.Team, jordanTok string) {
	t.Helper()
	srv.claimSignIn = true
	commish := userOf(t, srv, testToken)
	memberTok := signIn(srv, "Priya N.", "priya@example.com")
	l = importedLeague(t, srv, commish, userOf(t, srv, memberTok), true)
	seat = l.Teams[2]
	rec := doAnon(h, http.MethodPost, "/v1/auth/claim", claimBody(claimCode(t, srv, l.ID, seat.ID), "Jordan K."))
	var claimed ClaimSignInResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &claimed); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("claim = %d %s", rec.Code, rec.Body)
	}
	return l, seat, claimed.Data.Session.AccessToken
}

// The sign-in link is a credential: an Idempotency-Key on its issue must
// not keep the response (and so the code) in the idempotency store, and a
// retry issues a fresh link instead of replaying the first.
func TestSeatSignInLinkNotKeptByIdempotency(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		t.Run(store, func(t *testing.T) {
			var pool *pgxpool.Pool
			opts := testOptions()
			mem := newIdempotencyStore()
			opts.Idempotency = mem
			if store == "postgres" {
				pool = storetest.New(t)
				opts.Idempotency = NewPostgresIdempotency(pool)
			}
			srv, _ := newTestServerOn(pool)
			h := newHandler(opts, newRoutes(opts, srv))
			l, seat, _ := claimedSeat(t, srv, h)
			path := "/v1/leagues/" + l.ID + "/seats/" + seat.ID + "/signin-link"
			var urls []string
			for range 2 {
				rec := doAs(h, testToken, http.MethodPost, path, "", "Idempotency-Key", "signin-key-0001")
				var out SeatSignInLinkResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusCreated {
					t.Fatalf("issue = %d %s", rec.Code, rec.Body)
				}
				if rec.Header().Get("Idempotent-Replayed") != "" {
					t.Error("the sign-in link was replayed from the idempotency store")
				}
				urls = append(urls, out.Data.Url)
			}
			if urls[0] == urls[1] {
				t.Error("the retry replayed the first link")
			}
			if pool != nil {
				var n int
				if err := pool.QueryRow(context.Background(),
					`SELECT count(*) FROM idempotency_keys WHERE position(convert_to('/signin-link/', 'UTF8') in response_body) > 0`).Scan(&n); err != nil {
					t.Fatal(err)
				}
				if n != 0 {
					t.Errorf("%d idempotency rows hold a sign-in link", n)
				}
				return
			}
			mem.mu.Lock()
			defer mem.mu.Unlock()
			for _, e := range mem.entries {
				if bytes.Contains(e.body, []byte("/signin-link/")) {
					t.Error("the idempotency store holds a sign-in link")
				}
			}
		})
	}
}

// A commissioner can reissue access only to an account whose every role is
// under them: not to the commissioner of any league, nor to a member of a
// league the caller does not commission (409 target-has-role-elsewhere).
func TestSeatSignInLinkRefusesRolesElsewhere(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		t.Run(store, func(t *testing.T) {
			var pool *pgxpool.Pool
			if store == "postgres" {
				pool = storetest.New(t)
			}
			for _, tc := range []struct {
				name      string
				commishOf func(jordan, priya string) string // league B's commissioner
			}{
				{"commissioner elsewhere", func(jordan, _ string) string { return jordan }},
				{"member of another commissioner's league", func(_, priya string) string { return priya }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv, _ := newTestServerOn(pool)
					opts := testOptions()
					h := newHandler(opts, newRoutes(opts, srv))
					l, seat, jordanTok := claimedSeat(t, srv, h)
					jordan, priya := userOf(t, srv, jordanTok), l.Teams[1].OwnerID
					other := tc.commishOf(jordan, priya)
					member := jordan
					if other == jordan {
						member = priya
					}
					importedLeague(t, srv, other, member, true)
					p := decodeProblem(t, do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/seats/"+seat.ID+"/signin-link", ""), http.StatusConflict)
					if problemType(p) != problemTypeBase+"target-has-role-elsewhere" {
						t.Errorf("issue = %+v", p)
					}
				})
			}
		})
	}
}

// A sign-in link holds only while its league still lists the manager and
// its issuer still commissions that league: a commissioner hand-over, the
// manager leaving or being removed, or the issuer's account going deletes
// the league's links, and use re-checks both (for changes made elsewhere).
func TestSeatSignInLinkEndsWithTheRole(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		t.Run(store, func(t *testing.T) {
			var pool *pgxpool.Pool
			if store == "postgres" {
				pool = storetest.New(t)
			}
			for _, tc := range []struct {
				name    string
				revokes bool // the change deletes the link (else use refuses it)
				change  func(t *testing.T, srv *Server, h http.Handler, l *league.League, jordan, jordanTok string)
			}{
				{"commissioner hand-over", true, func(t *testing.T, _ *Server, h http.Handler, l *league.League, _, _ string) {
					rec := do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/commissioner", `{"userId":"`+l.Teams[1].OwnerID+`"}`)
					if rec.Code != http.StatusOK {
						t.Fatalf("transfer = %d %s", rec.Code, rec.Body)
					}
				}},
				{"manager removed", true, func(t *testing.T, _ *Server, h http.Handler, l *league.League, jordan, _ string) {
					if rec := do(h, http.MethodDelete, "/v1/leagues/"+l.ID+"/members/"+jordan, ""); rec.Code != http.StatusNoContent {
						t.Fatalf("remove = %d %s", rec.Code, rec.Body)
					}
				}},
				{"manager left", true, func(t *testing.T, _ *Server, h http.Handler, l *league.League, _, jordanTok string) {
					if rec := doAs(h, jordanTok, http.MethodPost, "/v1/leagues/"+l.ID+"/leave", ""); rec.Code != http.StatusNoContent {
						t.Fatalf("leave = %d %s", rec.Code, rec.Body)
					}
				}},
				{"commissioner changed elsewhere", false, func(t *testing.T, srv *Server, _ http.Handler, l *league.League, _, _ string) {
					editLeague(t, srv, l.ID, func(l *league.League) { l.CommissionerID = l.Teams[1].OwnerID })
				}},
				{"manager gone elsewhere", false, func(t *testing.T, srv *Server, _ http.Handler, l *league.League, _, _ string) {
					editLeague(t, srv, l.ID, func(l *league.League) { l.Teams[2].OwnerID = "" })
				}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					srv, _ := newTestServerOn(pool)
					opts := testOptions()
					h := newHandler(opts, newRoutes(opts, srv))
					l, seat, jordanTok := claimedSeat(t, srv, h)
					jordan := userOf(t, srv, jordanTok)
					rec := do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/seats/"+seat.ID+"/signin-link", "")
					var out SeatSignInLinkResponse
					if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusCreated {
						t.Fatalf("issue = %d %s", rec.Code, rec.Body)
					}
					tc.change(t, srv, h, l, jordan, jordanTok)
					if n := countSignInLinks(t, srv, pool, jordan); tc.revokes && n != 0 {
						t.Errorf("%d sign-in links left after the change", n)
					}
					code := signInLinkCodeOf(t, out.Data.Url)
					decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/signin-link", `{"code":"`+code+`"}`), http.StatusUnauthorized)
				})
			}
		})
	}
}

// editLeague changes a league in its store directly, as another path
// (an FPL sync, an admin command) would.
func editLeague(t *testing.T, srv *Server, leagueID string, fn func(l *league.League)) {
	t.Helper()
	ctx := context.Background()
	err := srv.leagues.Repo().Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		fn(st.League)
		return tx.Save(ctx, st.League)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// countSignInLinks counts userID's stored sign-in links.
func countSignInLinks(t *testing.T, srv *Server, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	if pool == nil {
		return srv.auth.(*auth.Memory).SignInLinkCount(userID)
	}
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM signin_links WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
