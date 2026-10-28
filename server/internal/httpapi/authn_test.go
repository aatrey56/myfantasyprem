package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

func TestBearerTokenRequired(t *testing.T) {
	h := newTestHandler()
	cases := []struct{ name, auth string }{
		{"none", ""},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"unknown token", "Bearer not-a-real-token"},
		{"empty bearer", "Bearer "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := doAs(h, "", http.MethodGet, "/v1/me", "", "Authorization", c.auth)
			p := decodeProblem(t, rec, http.StatusUnauthorized)
			if problemType(p) != problemTypeBase+"unauthorized" || !strings.HasPrefix(rec.Header().Get("WWW-Authenticate"), "Bearer") {
				t.Errorf("problem = %+v, WWW-Authenticate = %q", p, rec.Header().Get("WWW-Authenticate"))
			}
		})
	}
	// Scheme is case-insensitive.
	if rec := doAs(h, "", http.MethodGet, "/v1/me", "", "Authorization", "bearer "+testToken); rec.Code != http.StatusOK {
		t.Errorf("lowercase scheme: %d %s", rec.Code, rec.Body)
	}
	// Public routes do not need a token, and unknown paths still 404.
	if rec := doAnon(h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Errorf("healthz = %d", rec.Code)
	}
	decodeProblem(t, doAnon(h, http.MethodGet, "/v1/nope", ""), http.StatusNotFound)
}

func TestEmailSignInFlow(t *testing.T) {
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	srv, clock := newTestServer()
	srv.log, srv.sender = opts.Logger, auth.LogSender{Log: opts.Logger, AppURL: testOrigin}
	h := newHandler(opts, newRoutes(opts, srv))

	rec := doAnon(h, http.MethodPost, "/v1/auth/email", `{"email":"new@example.com"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	m := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`).FindStringSubmatch(logs.String())
	if m == nil {
		t.Fatalf("no dev magic link logged: %s", logs.String())
	}
	if strings.Contains(logs.String(), "new@example.com") {
		t.Error("the email address was logged")
	}

	rec = doAnon(h, http.MethodPost, "/v1/auth/email/verify", `{"token":"`+m[1]+`"}`)
	var session SessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("verify = %d %s", rec.Code, rec.Body)
	}
	s := session.Data
	if !s.IsNewUser || s.TokenType != Bearer || s.User.DisplayName != "new" || !s.ExpiresAt.Equal(clock.Now().Add(time.Hour)) {
		t.Errorf("session = %+v", s)
	}

	if rec := doAs(h, s.AccessToken, http.MethodGet, "/v1/me", ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"email":"new@example.com"`) {
		t.Errorf("me = %d %s", rec.Code, rec.Body)
	}

	rec = doAnon(h, http.MethodPost, "/v1/auth/refresh", `{"refreshToken":"`+s.RefreshToken+`"}`)
	var refreshed SessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &refreshed)
	if rec.Code != http.StatusOK || refreshed.Data.AccessToken == s.AccessToken || refreshed.Data.IsNewUser {
		t.Fatalf("refresh = %d %s", rec.Code, rec.Body)
	}
	decodeProblem(t, doAs(h, s.AccessToken, http.MethodGet, "/v1/me", ""), http.StatusUnauthorized)

	if rec := doAs(h, refreshed.Data.AccessToken, http.MethodPost, "/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", rec.Code)
	}
	decodeProblem(t, doAs(h, refreshed.Data.AccessToken, http.MethodGet, "/v1/me", ""), http.StatusUnauthorized)
}

// codeServer is a handler whose dev sender logs into logs.
func codeServer(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	srv, _ := newTestServer()
	srv.log, srv.sender = opts.Logger, auth.LogSender{Log: opts.Logger, AppURL: testOrigin}
	return newHandler(opts, newRoutes(opts, srv)), &logs
}

// lastLogged is the last value of key=... in the logs.
func lastLogged(t *testing.T, logs *bytes.Buffer, key string) string {
	t.Helper()
	all := regexp.MustCompile(`\b`+key+`=(\S+)`).FindAllStringSubmatch(logs.String(), -1)
	if len(all) == 0 {
		t.Fatalf("no %s logged: %s", key, logs.String())
	}
	return strings.Trim(all[len(all)-1][1], `"`)
}

// The email carries a 6-digit code too: it signs in once, like the link,
// and spends the link.
func TestEmailCodeSignInFlow(t *testing.T) {
	h, logs := codeServer(t)
	rec := doAnon(h, http.MethodPost, "/v1/auth/email", `{"email":"new@example.com","next":"/join/K7QM2XRP4D"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	code, token := lastLogged(t, logs, "code"), lastLogged(t, logs, "token")
	if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(code) {
		t.Fatalf("code = %q", code)
	}
	// The link brings the user back to where they started signing in.
	if link := lastLogged(t, logs, "link"); !strings.HasSuffix(link, "&next=%2Fjoin%2FK7QM2XRP4D") {
		t.Errorf("link = %s", link)
	}

	wrong := wrongCode(code)
	p := decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/email/code", `{"email":"new@example.com","code":"`+wrong+`"}`), http.StatusUnauthorized)
	if p.Detail == nil || !strings.Contains(*p.Detail, "code") {
		t.Errorf("wrong code problem = %+v", p)
	}
	rec = doAnon(h, http.MethodPost, "/v1/auth/email/code", `{"email":"New@Example.com","code":"`+code+`"}`)
	var session SessionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("code = %d %s", rec.Code, rec.Body)
	}
	if !session.Data.IsNewUser || session.Data.User.Email == nil || *session.Data.User.Email != "new@example.com" {
		t.Errorf("session = %+v", session.Data)
	}
	decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/email/code", `{"email":"new@example.com","code":"`+code+`"}`), http.StatusUnauthorized)
	decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/email/verify", `{"token":"`+token+`"}`), http.StatusUnauthorized)
}

// Past the per-address limit of wrong codes the endpoint answers 429 with
// Retry-After, before looking at the code.
func TestEmailCodePerAddressLimit(t *testing.T) {
	h, logs := codeServer(t)
	failures := 0
	for failures < auth.CodeFailuresPerEmail {
		// From different IPs: the per-IP auth limit is not what stops this.
		ip := "203.0.113." + strconv.Itoa(failures+1)
		if rec := from(h, ip, "", "", "POST", "/v1/auth/email", `{"email":"sam@example.com"}`); rec.Code != http.StatusAccepted {
			// The per-address link limit allows a burst of 5; wait it out.
			t.Fatalf("start = %d %s", rec.Code, rec.Body)
		}
		code := lastLogged(t, logs, "code")
		for i := 0; i < auth.CodeAttempts && failures < auth.CodeFailuresPerEmail; i++ {
			body := `{"email":"sam@example.com","code":"` + fmt.Sprintf("%06d", (mustAtoi(code)+1+i)%1_000_000) + `"}`
			if rec := from(h, ip, "", "", "POST", "/v1/auth/email/code", body); rec.Code != http.StatusUnauthorized {
				t.Fatalf("wrong code %d = %d %s", failures+1, rec.Code, rec.Body)
			}
			failures++
		}
	}
	if rec := from(h, "203.0.113.99", "", "", "POST", "/v1/auth/email", `{"email":"sam@example.com"}`); rec.Code != http.StatusAccepted {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	code := lastLogged(t, logs, "code")
	rec := from(h, "203.0.113.99", "", "", "POST", "/v1/auth/email/code", `{"email":"sam@example.com","code":"`+code+`"}`)
	check429(t, rec, httptest.NewRequest("POST", "/v1/auth/email/code", strings.NewReader(`{"email":"sam@example.com","code":"`+code+`"}`)))
	if secs, _ := strconv.Atoi(rec.Header().Get("Retry-After")); secs < 3600 {
		t.Errorf("Retry-After = %d, want hours", secs)
	}
	// The link of that email still signs in.
	if rec := doAnon(h, http.MethodPost, "/v1/auth/email/verify", `{"token":"`+lastLogged(t, logs, "token")+`"}`); rec.Code != http.StatusOK {
		t.Errorf("link = %d %s", rec.Code, rec.Body)
	}
}

func mustAtoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		panic(err)
	}
	return n
}

func TestMagicLinkNotLoggedByDefault(t *testing.T) {
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	h := NewHandler(opts)
	doAnon(h, http.MethodPost, "/v1/auth/email", `{"email":"new@example.com"}`)
	if strings.Contains(logs.String(), "token=") {
		t.Errorf("token logged without DevMagicLinks: %s", logs.String())
	}
}

func TestIdempotencyKey(t *testing.T) {
	h := newTestHandler()
	body := `{"name":"Office League","teamCount":10,"teamName":"Heroes"}`
	first := do2(h, body, "league-key-0001")
	if first.Code != http.StatusCreated {
		t.Fatalf("first = %d %s", first.Code, first.Body)
	}
	again := do2(h, body, "league-key-0001")
	if again.Code != http.StatusCreated || again.Body.String() != first.Body.String() ||
		again.Header().Get("Idempotent-Replayed") != "true" || again.Header().Get("Location") != first.Header().Get("Location") {
		t.Errorf("replay = %d %v %s", again.Code, again.Header(), again.Body)
	}
	other := do2(h, body, "league-key-0002")
	if other.Code != http.StatusCreated || other.Body.String() == first.Body.String() {
		t.Errorf("a new key made no new league: %s", other.Body)
	}
	p := decodeProblem(t, do2(h, `{"name":"Another","teamCount":8,"teamName":"Heroes"}`, "league-key-0001"), http.StatusUnprocessableEntity)
	if problemType(p) != problemTypeBase+"idempotency-key-reused" {
		t.Errorf("type = %q", problemType(p))
	}
	decodeProblem(t, do2(h, body, "short"), http.StatusBadRequest)

	// The replay did not create a second league.
	if rec := doAs(h, testToken, http.MethodGet, "/v1/me/leagues", ""); !strings.Contains(rec.Body.String(), "Office League") {
		t.Errorf("leagues = %s", rec.Body)
	}
	var leagues MyLeagueListResponse
	_ = json.Unmarshal(doAs(h, testToken, http.MethodGet, "/v1/me/leagues", "").Body.Bytes(), &leagues)
	if len(leagues.Data) != 2 {
		t.Errorf("leagues = %d, want 2 (replay must not create one)", len(leagues.Data))
	}
}

func do2(h http.Handler, body, key string) *httptest.ResponseRecorder {
	return doAs(h, testToken, http.MethodPost, "/v1/leagues", body, "Idempotency-Key", key)
}

// New invite codes are 10 characters; the 8-character ones made before
// still parse. Anything else is a 400.
func TestInviteCodeShapes(t *testing.T) {
	h := newTestHandler()
	for code, want := range map[string]int{
		"ZZZZZZZZZZ": http.StatusNotFound, "zzzzzzzzzz": http.StatusNotFound, "ZZZZZZZZ": http.StatusNotFound,
		"ZZZZZZZZZ": http.StatusBadRequest, "ZZZZZZZZZZZ": http.StatusBadRequest, "ZZZZZZZZZO": http.StatusBadRequest,
	} {
		if rec := doAnon(h, http.MethodGet, "/v1/invites/"+code, ""); rec.Code != want {
			t.Errorf("GET /v1/invites/%s = %d, want %d", code, rec.Code, want)
		}
	}
	rec := do(h, http.MethodPost, "/v1/leagues", `{"name":"Codes","teamCount":4,"teamName":"Heroes"}`)
	var created LeagueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	var inv InviteResponse
	_ = json.Unmarshal(do(h, http.MethodPost, "/v1/leagues/"+created.Data.League.Id+"/invites", "").Body.Bytes(), &inv)
	if len(inv.Data.Code) != 10 || inv.Data.ExpiresAt == nil {
		t.Errorf("invite = %+v", inv.Data)
	}
}

func TestLeagueSettingsOutOfRangeIs422(t *testing.T) {
	h := newTestHandler()
	rec := do(h, http.MethodPost, "/v1/leagues", `{"name":"Bounds","teamCount":4,"teamName":"Heroes",
		"settings":{"draft":{"pickTimerSeconds":-1},"trades":{"review":"commissioner","reviewHours":9999999,"vetoThreshold":7}}}`)
	p := decodeProblem(t, rec, http.StatusUnprocessableEntity)
	if p.Errors == nil || len(*p.Errors) != 3 {
		t.Fatalf("errors = %+v", p.Errors)
	}
}

func TestInviteAndJoinOverHTTP(t *testing.T) {
	srv, _ := newTestServer()
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	rec := do(h, http.MethodPost, "/v1/leagues", `{"name":"Office League","teamCount":4,"teamName":"Heroes"}`)
	var created LeagueResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	lid := created.Data.League.Id
	if created.Data.Viewer.Role != LeagueRoleCommissioner || created.Data.Settings.Roster != (PositionCounts{GKP: 2, DEF: 5, MID: 5, FWD: 3}) ||
		created.Data.Settings.Lineup.MinStarters != (PositionCounts{GKP: 1, DEF: 3, MID: 2, FWD: 1}) || created.Data.League.Status != "pre_draft" {
		t.Errorf("created = %+v", created.Data)
	}

	rec = do(h, http.MethodPost, "/v1/leagues/"+lid+"/invites", "")
	var inv InviteResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &inv)
	if rec.Code != http.StatusCreated || !strings.HasSuffix(inv.Data.Url, "/join/"+inv.Data.Code) {
		t.Fatalf("invite = %d %s", rec.Code, rec.Body)
	}
	if rec := doAnon(h, http.MethodGet, "/v1/invites/"+strings.ToLower(inv.Data.Code), ""); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"joinable":true`) ||
		!strings.Contains(rec.Body.String(), `"leagueId":"`+lid+`"`) {
		t.Errorf("preview = %d %s", rec.Code, rec.Body)
	}
	friend := signIn(srv, "Priya N.", "priya@example.com")
	if rec := doAs(h, friend, http.MethodGet, "/v1/leagues/"+lid+"/members", ""); rec.Code != http.StatusForbidden {
		t.Errorf("non-member members = %d", rec.Code)
	}
	if rec := doAs(h, friend, http.MethodPost, "/v1/invites/"+inv.Data.Code+"/accept", `{"teamName":"Gaffer's Army"}`); rec.Code != http.StatusCreated {
		t.Fatalf("accept = %d %s", rec.Code, rec.Body)
	}
	// Joining again names the league, so the app can open it.
	again := decodeProblem(t, doAs(h, friend, http.MethodPost, "/v1/invites/"+inv.Data.Code+"/accept", `{"teamName":"Again"}`), http.StatusConflict)
	if again.LeagueId == nil || *again.LeagueId != lid || !strings.HasSuffix(problemType(again), "/already-member") {
		t.Errorf("already member = %+v", again)
	}
	var members MemberListResponse
	_ = json.Unmarshal(doAs(h, friend, http.MethodGet, "/v1/leagues/"+lid+"/members", "").Body.Bytes(), &members)
	if len(members.Data) != 2 || members.Data[0].Role != LeagueRoleCommissioner || members.Data[1].Team.ManagerName != "Priya N." {
		t.Errorf("members = %+v", members.Data)
	}
	// The draft answers 403 to outsiders, and 503 to members while no
	// season data (players) has been loaded.
	stranger := signIn(srv, "Olu A.", "olu@example.com")
	decodeProblem(t, doAs(h, stranger, http.MethodGet, "/v1/leagues/"+lid+"/draft", ""), http.StatusForbidden)
	decodeProblem(t, doAs(h, friend, http.MethodGet, "/v1/leagues/"+lid+"/draft", ""), http.StatusServiceUnavailable)

	// Deleting the commissioner's account hands the league to the friend.
	if rec := do(h, http.MethodDelete, "/v1/me", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	var l LeagueResponse
	_ = json.Unmarshal(doAs(h, friend, http.MethodPatch, "/v1/leagues/"+lid, `{"name":"Priya's League"}`).Body.Bytes(), &l)
	if l.Data.League.Name != "Priya's League" || l.Data.Viewer.Role != LeagueRoleCommissioner || l.Data.League.MemberCount != 1 {
		t.Errorf("after delete = %+v", l.Data)
	}
}

// A replay answers with the CORS headers of the retry's origin, not those
// stored with the first response (memory and Postgres stores).
func TestIdempotentReplayKeepsRequestCORS(t *testing.T) {
	const other = "https://app.example"
	for _, pg := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "postgres"}[pg], func(t *testing.T) {
			opts := testOptions()
			opts.AllowedOrigins = []string{testOrigin, other}
			var pool *pgxpool.Pool
			if pg {
				pool = storetest.New(t)
				opts.Idempotency = NewPostgresIdempotency(pool)
			}
			srv, _ := newTestServerOn(pool)
			h := newHandler(opts, newRoutes(opts, srv))
			body := `{"name":"Office League","teamCount":10,"teamName":"Heroes"}`
			first := doAs(h, testToken, http.MethodPost, "/v1/leagues", body, "Idempotency-Key", "cors-key-0001", "Origin", testOrigin)
			if first.Code != http.StatusCreated || first.Header().Get("Access-Control-Allow-Origin") != testOrigin {
				t.Fatalf("first = %d %v", first.Code, first.Header())
			}
			again := doAs(h, testToken, http.MethodPost, "/v1/leagues", body, "Idempotency-Key", "cors-key-0001", "Origin", other)
			if again.Header().Get("Idempotent-Replayed") != "true" {
				t.Fatalf("not a replay: %d %v", again.Code, again.Header())
			}
			if got := again.Header().Values("Access-Control-Allow-Origin"); len(got) != 1 || got[0] != other {
				t.Errorf("replayed Access-Control-Allow-Origin = %v, want [%s]", got, other)
			}
			if v := again.Header().Values("Vary"); len(v) != 1 {
				t.Errorf("Vary = %v", v)
			}
		})
	}
}
