package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/tags"
	"github.com/aatrey56/myfantasyprem/server/internal/watchlist"
)

const testOrigin = "http://localhost:8081"

func testOptions() Options {
	return Options{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version:        "test",
		AllowedOrigins: []string{testOrigin},
	}
}

// fakeClock is a manually advanced clock.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// testToken is the bearer token of the user seeded by the most recent
// newTestServer. `do` sends it; `doAs` sends another; `doAnon` sends none.
// Tests are not parallel, so one package variable is enough.
var testToken string

// newTestServer returns a Server whose clock is the returned fakeClock, with
// one signed-in user whose token is testToken.
func newTestServer() (*Server, *fakeClock) { return newTestServerOn(nil) }

// newTestServerOn is newTestServer on Postgres stores when pool is set.
func newTestServerOn(pool *pgxpool.Pool) (*Server, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	srv := NewServer("test")
	srv.now = clock.Now
	if pool != nil {
		now := func() time.Time { return srv.now() }
		srv.auth = auth.NewPostgres(pool, now)
		srv.stats = stats.NewPostgres(pool)
		srv.leagues = league.NewService(league.NewPostgres(pool), now).WithStats(srv.stats)
		srv.watchlist = watchlist.NewPostgres(pool, now)
		srv.tags = tags.NewPostgres(pool, now)
		srv.history = history.NewPostgres(pool)
	}
	srv.sender = auth.DisabledSender{Log: testOptions().Logger}
	// The FPL import reads the real league recorded in provider/fpl's
	// testdata, never FPL.
	var store mirror.Store = mirror.NewMemoryStore()
	if pool != nil {
		store = mirror.NewPostgresStore(pool)
	}
	srv.mirror = &mirror.Service{
		Source: fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata/real-gw6")},
			Logger: testOptions().Logger, MinInterval: -1}),
		Leagues: srv.leagues.Repo(), Stats: srv.stats, Store: store, Now: srv.now, Log: testOptions().Logger,
	}
	testToken = signIn(srv, "Sam P.", "sam@example.com")
	return srv, clock
}

// signIn creates a user and returns a fresh access token for them.
func signIn(srv *Server, name, email string) string {
	u, err := srv.auth.CreateUser(context.Background(), name, email)
	if err != nil {
		panic(err)
	}
	tok, err := srv.auth.Issue(context.Background(), u.ID)
	if err != nil {
		panic(err)
	}
	return tok.AccessToken
}

func newTestHandler() http.Handler {
	srv, _ := newTestServer()
	return newHandler(testOptions(), newRoutes(testOptions(), srv))
}

func do(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	return doAs(h, testToken, method, target, body)
}

func doAnon(h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	return doAs(h, "", method, target, body)
}

func doAs(h http.Handler, token, method, target, body string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decodeProblem checks that rec is a problem+json response with the wanted
// status and returns the body. Every problem must carry the request id that is
// also in the X-Request-ID header.
func decodeProblem(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int) Problem {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, wantStatus, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/problem+json" {
		t.Fatalf("content-type = %q, want application/problem+json", ct)
	}
	var p Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("bad problem body %s: %v", rec.Body, err)
	}
	if p.Status != wantStatus || p.Title == "" {
		t.Fatalf("problem = %+v", p)
	}
	if p.RequestId == nil || *p.RequestId == "" || *p.RequestId != rec.Header().Get("X-Request-ID") {
		t.Fatalf("problem requestId = %v, header = %q", p.RequestId, rec.Header().Get("X-Request-ID"))
	}
	return p
}

func problemType(p Problem) string {
	if p.Type == nil {
		return ""
	}
	return *p.Type
}

func TestHealthz(t *testing.T) {
	rec := do(newTestHandler(), http.MethodGet, "/healthz", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var h Health
	if err := json.Unmarshal(rec.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.Status != HealthStatusOk || h.Version == nil || *h.Version != "test" {
		t.Fatalf("unexpected body %s", rec.Body)
	}
}

// With a health check (cmd/api pings the database), /healthz answers 503
// while it fails and gives up after healthTimeout.
func TestHealthzChecksDatabase(t *testing.T) {
	srv, clock := newTestServer()
	var down error
	pings := 0
	srv.healthCheck = func(ctx context.Context) error {
		pings++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("health check without a deadline")
		}
		return down
	}
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	if rec := doAnon(h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
		t.Fatalf("up: status = %d", rec.Code)
	}
	down = errors.New("connection refused")
	// Within the cache window the ping is not repeated.
	for range 20 {
		if rec := doAnon(h, http.MethodGet, "/healthz", ""); rec.Code != http.StatusOK {
			t.Fatalf("cached: status = %d", rec.Code)
		}
	}
	if pings != 1 {
		t.Fatalf("pings = %d within the cache window, want 1", pings)
	}
	clock.Advance(healthCacheTTL + time.Second)
	rec := doAnon(h, http.MethodGet, "/healthz", "")
	var body Health
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusServiceUnavailable ||
		body.Status != HealthStatusUnavailable {
		t.Fatalf("down: status = %d, body %s", rec.Code, rec.Body)
	}
}

// FPL_RESYNC_DISABLED_LEAGUES turns re-sync off for the listed leagues only,
// and only tells the league's commissioner: authorization comes first.
func TestResyncDisabledLeague(t *testing.T) {
	srv, _ := newTestServer()
	srv.resyncDisabled = []string{tLeague}
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	// A league the caller has no part in (here: none) is a 404, not a 409.
	decodeProblem(t, do(h, http.MethodPost, "/v1/leagues/"+tLeague+"/sync", `{"confirm":true}`), http.StatusNotFound)

	// Someone outside a real league is refused before the switch is revealed.
	l, err := srv.leagues.Create(context.Background(), userOf(t, srv, testToken), "Disabled", 10, "Team", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	srv.resyncDisabled = []string{l.ID}
	outsider := signIn(srv, "Olu A.", "olu@example.com")
	decodeProblem(t, doAs(h, outsider, http.MethodPost, "/v1/leagues/"+l.ID+"/sync", `{"confirm":true}`), http.StatusForbidden)
}

// Without a mirror service the import operations answer 503.
func TestImportsWithoutMirrorAre503(t *testing.T) {
	srv, _ := newTestServer()
	srv.mirror = nil
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	for _, c := range []struct{ method, target, body string }{
		{http.MethodGet, "/v1/imports/preview?fplLeagueId=1", ""},
		{http.MethodPost, "/v1/imports", `{"fplLeagueId":1,"seat":1}`},
		{http.MethodGet, "/v1/imports/" + tImportNA, ""},
		{http.MethodGet, "/v1/leagues/" + tLeague + "/sync", ""},
	} {
		p := decodeProblem(t, do(h, c.method, c.target, c.body), http.StatusServiceUnavailable)
		if problemType(p) != problemTypeBase+"imports-unavailable" {
			t.Errorf("%s %s: type = %q", c.method, c.target, problemType(p))
		}
	}
}

// The league reads answer 404 for a well-formed id that does not exist.
func TestUnknownLeagueReadsAre404(t *testing.T) {
	targets := []string{
		"/v1/leagues/" + tLeague,
		"/v1/leagues/" + tLeague + "/gameweeks/7/matchups",
		"/v1/leagues/" + tLeague + "/gameweeks/7/matchups/" + tMatchup,
		"/v1/leagues/" + tLeague + "/teams/" + tTeam,
		"/v1/leagues/" + tLeague + "/teams/" + tTeam + "/lineup",
		"/v1/leagues/" + tLeague + "/players?available=true&position=MID",
		"/v1/leagues/" + tLeague + "/players/" + tPlayer,
		"/v1/leagues/" + tLeague + "/transactions",
		"/v1/leagues/" + tLeague + "/standings",
		"/v1/leagues/" + tLeague + "/schedule",
		"/v1/leagues/" + tLeague + "/draft",
		"/v1/leagues/" + tLeague + "/trades",
	}
	h := newTestHandler()
	for _, target := range targets {
		decodeProblem(t, do(h, http.MethodGet, target, ""), http.StatusNotFound)
	}
}

// oapi-codegen does not enforce `pattern`, so the server checks path ids.
func TestMalformedIDIsProblem400(t *testing.T) {
	targets := []string{
		"/v1/leagues/lg_1",
		"/v1/leagues/12345",
		"/v1/leagues/" + tTeam, // wrong prefix
		"/v1/leagues/LG_01K7M2XQ8RFV3C9A4T6WBN5HDY",
		"/v1/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdi", // i is not in the alphabet
		"/v1/leagues/lg_1/gameweeks/7/matchups",
		"/v1/leagues/" + tLeague + "/gameweeks/7/matchups/mu_1",
		"/v1/leagues/" + tLeague + "/teams/tm_1",
		"/v1/leagues/lg_1/players",
		"/v1/leagues/" + tLeague + "/players/223094",
		"/v1/leagues/lg_1/transactions",
		"/v1/imports/imp_1",
		"/v1/imports/" + tLeague,
	}
	h := newTestHandler()
	for _, target := range targets {
		p := decodeProblem(t, do(h, http.MethodGet, target, ""), http.StatusBadRequest)
		if problemType(p) != problemTypeBase+"bad-request" || p.Detail == nil || *p.Detail == "" {
			t.Errorf("%s: problem = %+v", target, p)
		}
	}
}

func TestBadGameweekIsProblem400(t *testing.T) {
	h := newTestHandler()
	for _, gw := range []string{"notanumber", "0", "39", "-1", "1.5"} {
		rec := do(h, http.MethodGet, "/v1/leagues/"+tLeague+"/gameweeks/"+gw+"/matchups", "")
		p := decodeProblem(t, rec, http.StatusBadRequest)
		// The detail is ours; it never echoes the parser error or the value.
		if p.Detail == nil || strings.Contains(*p.Detail, "strconv") || strings.Contains(*p.Detail, gw+".") {
			t.Errorf("gw=%s: detail = %v", gw, p.Detail)
		}
	}
	for _, gw := range []string{"1", "38"} {
		rec := do(h, http.MethodGet, "/v1/leagues/"+tLeague+"/gameweeks/"+gw+"/matchups", "")
		decodeProblem(t, rec, http.StatusNotFound) // valid gameweek, unknown league
	}
}

func TestUnknownPathIsProblem404(t *testing.T) {
	h := newTestHandler()
	for _, target := range []string{"/", "/nope", "/v1", "/v1/players/223094", "/v1/me/nope", "/v1/leagues/lg_01k7m2xq8rfv3c9a4t6wbn5hdy/nope"} {
		p := decodeProblem(t, do(h, http.MethodGet, target, ""), http.StatusNotFound)
		if problemType(p) != problemTypeBase+"not-found" {
			t.Errorf("%s: type = %q", target, problemType(p))
		}
	}
}

func TestWrongMethodIsProblem405WithAllow(t *testing.T) {
	h := newTestHandler()
	tests := []struct{ method, target, allow string }{
		{http.MethodPost, "/healthz", "GET"},
		{http.MethodDelete, "/v1/imports", "POST"},
		{http.MethodGet, "/v1/imports", "POST"},
		{http.MethodPost, "/v1/imports/preview", "GET"},
		{http.MethodPut, "/v1/leagues/" + tLeague, "GET"},
		{http.MethodPost, "/v1/gameweeks/current", "GET"},
		{http.MethodGet, "/v1/leagues", "POST"},
		{http.MethodPut, "/v1/me", "PATCH"},
	}
	for _, tt := range tests {
		rec := do(h, tt.method, tt.target, "")
		p := decodeProblem(t, rec, http.StatusMethodNotAllowed)
		if problemType(p) != problemTypeBase+"method-not-allowed" {
			t.Errorf("%s %s: type = %q", tt.method, tt.target, problemType(p))
		}
		if allow := rec.Header().Get("Allow"); !strings.Contains(allow, tt.allow) {
			t.Errorf("%s %s: Allow = %q, want it to contain %q", tt.method, tt.target, allow, tt.allow)
		}
	}
}

func TestMalformedJSONBodyIsProblem400(t *testing.T) {
	h := newTestHandler()
	for _, body := range []string{`{`, `not json`, `{"fplLeagueId":`, `{"draftLeagueUrl":"x"`} {
		p := decodeProblem(t, do(h, http.MethodPost, "/v1/imports", body), http.StatusBadRequest)
		if p.Detail == nil || strings.Contains(*p.Detail, "unexpected") || strings.Contains(*p.Detail, "invalid character") {
			t.Errorf("body %q: detail leaks the decoder error: %v", body, p.Detail)
		}
	}
	// No body at all.
	decodeProblem(t, do(h, http.MethodPost, "/v1/imports", ""), http.StatusBadRequest)
}

func TestImportRequestShapeIsProblem400(t *testing.T) {
	h := newTestHandler()
	bodies := []string{
		`{}`,
		`[]`,
		`null`,
		`"text"`,
		`{"fplLeagueId":1,"draftLeagueUrl":"https://draft.premierleague.com/league/1"}`,
		`{"fplLeagueId":1,"extra":true}`,
		`{"fplLeagueId":0}`,
		`{"fplLeagueId":-3}`,
		`{"fplLeagueId":"12345"}`,
		`{"fplLeagueId":1.5}`,
		`{"fplLeagueId":null}`,
		`{"draftLeagueUrl":5}`,
		`{"draftLeagueUrl":""}`,
		`{"draftLeagueUrl":"https://example.com/league/1"}`,
		`{"draftLeagueUrl":"https://draft.premierleague.com/league/abc"}`,
		`{"draftLeagueUrl":"https://draft.premierleague.com/"}`,
	}
	for _, body := range bodies {
		decodeProblem(t, do(h, http.MethodPost, "/v1/imports", body), http.StatusBadRequest)
	}
}

func TestOversizedBodyIsProblem413(t *testing.T) {
	h := newTestHandler()
	body := `{"draftLeagueUrl":"https://draft.premierleague.com/league/1/` + strings.Repeat("a", maxBodyBytes) + `"}`
	decodeProblem(t, do(h, http.MethodPost, "/v1/imports", body), http.StatusRequestEntityTooLarge)

	// A body under the limit is still accepted.
	ok := `{"draftLeagueUrl":"https://draft.premierleague.com/league/90001/` + strings.Repeat("a", 4096) + `","seat":1}`
	if rec := do(h, http.MethodPost, "/v1/imports", ok); rec.Code != http.StatusAccepted {
		t.Fatalf("small body status = %d: %s", rec.Code, rec.Body)
	}
}

func TestClassicLeagueURLIs422(t *testing.T) {
	h := newTestHandler()
	classic := "https://fantasy.premierleague.com/leagues/12345/standings/c"
	cases := []struct{ name, method, target, body string }{
		{"create", http.MethodPost, "/v1/imports", `{"draftLeagueUrl":"` + classic + `"}`},
		{"create www", http.MethodPost, "/v1/imports", `{"draftLeagueUrl":"https://www.fantasy.premierleague.com/leagues/1/standings/c"}`},
		{"preview", http.MethodGet, "/v1/imports/preview?draftLeagueUrl=" + strings.NewReplacer(":", "%3A", "/", "%2F").Replace(classic), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := decodeProblem(t, do(h, c.method, c.target, c.body), http.StatusUnprocessableEntity)
			if problemType(p) != problemTypeBase+"classic-league-url" {
				t.Errorf("type = %q", problemType(p))
			}
			if p.Title != "Classic FPL league link" {
				t.Errorf("title = %q", p.Title)
			}
			if p.Detail == nil || !strings.Contains(*p.Detail, "draft.premierleague.com") || !strings.Contains(*p.Detail, "overlap") {
				t.Errorf("detail = %v", p.Detail)
			}
		})
	}
}

func TestPreviewImportValidation(t *testing.T) {
	h := newTestHandler()
	for _, target := range []string{
		"/v1/imports/preview",
		"/v1/imports/preview?fplLeagueId=1&draftLeagueUrl=https%3A%2F%2Fdraft.premierleague.com%2Fleague%2F1",
		"/v1/imports/preview?fplLeagueId=0",
		"/v1/imports/preview?fplLeagueId=abc",
		"/v1/imports/preview?draftLeagueUrl=https%3A%2F%2Fexample.com%2Fleague%2F1",
	} {
		decodeProblem(t, do(h, http.MethodGet, target, ""), http.StatusBadRequest)
	}
}

// panicRoutes panics on /panic and serves 200 elsewhere.
var panicRoutes = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/panic" {
		panic("secret internal detail: db password hunter2")
	}
	w.WriteHeader(http.StatusOK)
})

func TestPanicRecoveryIsGenericProblem500(t *testing.T) {
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	h := newHandler(opts, panicRoutes)

	req := httptest.NewRequest(http.MethodGet, "/panic", nil)
	req.Header.Set("Origin", testOrigin)
	req.Header.Set("X-Request-ID", "client-req-1")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	p := decodeProblem(t, rec, http.StatusInternalServerError)
	if *p.RequestId != "client-req-1" {
		t.Errorf("requestId = %q, want the client supplied id", *p.RequestId)
	}
	if problemType(p) != problemTypeBase+"internal" || p.Detail == nil || *p.Detail != "Something went wrong on our side." {
		t.Errorf("problem = %+v", p)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Errorf("response leaks the panic value: %s", rec.Body)
	}
	// The panic value and the request id go to the server log.
	if l := logs.String(); !strings.Contains(l, "hunter2") || !strings.Contains(l, "client-req-1") || !strings.Contains(l, "stack=") {
		t.Errorf("log lacks panic details: %s", l)
	}
	// The recoverer is outermost, so middleware inside it already ran.
	if rec.Header().Get("Access-Control-Allow-Origin") != testOrigin || rec.Header().Get("Vary") != "Origin" {
		t.Errorf("headers set by inner middleware were lost: %v", rec.Header())
	}

	// The server keeps serving after a panic.
	if rec := do(h, http.MethodGet, "/ok", ""); rec.Code != http.StatusOK {
		t.Fatalf("after panic status = %d", rec.Code)
	}
}

// If the panic happens before requestID ran, the recoverer mints its own id.
func TestRecovererMintsRequestIDWhenNoneSet(t *testing.T) {
	h := recoverer(testOptions().Logger, panicRoutes)
	rec := do(h, http.MethodGet, "/panic", "")
	p := decodeProblem(t, rec, http.StatusInternalServerError)
	if !validRequestID(*p.RequestId) {
		t.Errorf("requestId = %q", *p.RequestId)
	}
}

func TestRecovererRepanicsOnAbortHandler(t *testing.T) {
	h := recoverer(testOptions().Logger, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))
	defer func() {
		if v := recover(); v != http.ErrAbortHandler {
			t.Fatalf("recovered %v, want http.ErrAbortHandler", v)
		}
	}()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
}

// failingServer makes one operation return an internal error.
type failingServer struct{ *Server }

func (failingServer) GetCurrentGameweek(context.Context, GetCurrentGameweekRequestObject) (GetCurrentGameweekResponseObject, error) {
	return nil, errors.New("pq: connection refused to 10.0.0.5")
}

func TestHandlerErrorIsGenericProblem500(t *testing.T) {
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	h := newHandler(opts, newRoutes(opts, failingServer{func() *Server { s, _ := newTestServer(); return s }()}))

	rec := do(h, http.MethodGet, "/v1/gameweeks/current", "")
	p := decodeProblem(t, rec, http.StatusInternalServerError)
	if p.Detail == nil || *p.Detail != "Something went wrong on our side." {
		t.Errorf("detail = %v", p.Detail)
	}
	if strings.Contains(rec.Body.String(), "10.0.0.5") {
		t.Errorf("response leaks the error: %s", rec.Body)
	}
	if l := logs.String(); !strings.Contains(l, "10.0.0.5") || !strings.Contains(l, *p.RequestId) {
		t.Errorf("log lacks the error or request id: %s", l)
	}
}

func TestRequestID(t *testing.T) {
	h := newTestHandler()
	get := func(header string, set bool) string {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		if set {
			req.Header.Set("X-Request-ID", header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Header().Get("X-Request-ID")
	}

	for _, ok := range []string{"abc", "Req.1_2-3", strings.Repeat("a", 64)} {
		if got := get(ok, true); got != ok {
			t.Errorf("valid id %q was replaced by %q", ok, got)
		}
	}
	for _, bad := range []string{"", strings.Repeat("a", 65), "has space", "semi;colon", "new\tline", "ünï"} {
		got := get(bad, bad != "")
		if got == bad || !ids.Valid("x", "x_"+got) {
			t.Errorf("invalid id %q gave %q, want a generated ULID", bad, got)
		}
	}
	if a, b := get("", false), get("", false); a == b {
		t.Errorf("generated ids repeat: %q", a)
	}

	// The id also reaches handlers through the context.
	var seen string
	hh := newHandler(testOptions(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	rec := do(hh, http.MethodGet, "/", "")
	if seen == "" || seen != rec.Header().Get("X-Request-ID") {
		t.Errorf("context id = %q, header = %q", seen, rec.Header().Get("X-Request-ID"))
	}
	if RequestIDFrom(context.Background()) != "" {
		t.Error("RequestIDFrom should be empty outside a request")
	}
}

func TestAccessLogCarriesRequestID(t *testing.T) {
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "trace-me")
	NewHandler(opts).ServeHTTP(httptest.NewRecorder(), req)
	l := logs.String()
	for _, want := range []string{"request_id=trace-me", "status=200", "path=/healthz", "bytes="} {
		if !strings.Contains(l, want) {
			t.Errorf("access log %q lacks %q", l, want)
		}
	}
}

func TestStatusRecorderUnwrap(t *testing.T) {
	inner := httptest.NewRecorder()
	rec := &statusRecorder{ResponseWriter: inner, status: http.StatusOK}
	// ResponseController finds Flush on the wrapped writer through Unwrap.
	if err := http.NewResponseController(rec).Flush(); err != nil {
		t.Fatalf("Flush through statusRecorder: %v", err)
	}
	rec.WriteHeader(http.StatusTeapot)
	rec.WriteHeader(http.StatusOK) // ignored by the recorder, like net/http
	_, _ = rec.Write([]byte("hello"))
	if rec.status != http.StatusTeapot || rec.bytes != 5 {
		t.Errorf("status = %d, bytes = %d", rec.status, rec.bytes)
	}
}

func TestCORS(t *testing.T) {
	h := newTestHandler()
	const exposed = "Location, Retry-After, X-Request-ID, WWW-Authenticate, Idempotent-Replayed"
	vary := func(rec *httptest.ResponseRecorder) string { return strings.Join(rec.Header().Values("Vary"), ", ") }

	t.Run("simple request from allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", testOrigin)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Header().Get("Access-Control-Allow-Origin") != testOrigin {
			t.Errorf("headers = %v", rec.Header())
		}
		if got := rec.Header().Get("Access-Control-Expose-Headers"); got != exposed {
			t.Errorf("Expose-Headers = %q", got)
		}
		if vary(rec) != "Origin" {
			t.Errorf("Vary = %q", vary(rec))
		}
	})

	t.Run("simple request from disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
		req.Header.Set("Origin", "https://evil.example")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("status = %d", rec.Code)
		}
		if rec.Header().Get("Access-Control-Allow-Origin") != "" || rec.Header().Get("Access-Control-Expose-Headers") != "" {
			t.Errorf("disallowed origin got CORS headers: %v", rec.Header())
		}
		if vary(rec) != "Origin" {
			t.Errorf("Vary = %q, want Origin even for a disallowed origin", vary(rec))
		}
	})

	t.Run("no origin header", func(t *testing.T) {
		rec := do(h, http.MethodGet, "/healthz", "")
		if vary(rec) != "Origin" || rec.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Errorf("headers = %v", rec.Header())
		}
	})

	t.Run("preflight from allowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/imports", nil)
		req.Header.Set("Origin", testOrigin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		req.Header.Set("Access-Control-Request-Headers", "content-type, x-request-id")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("status = %d", rec.Code)
		}
		want := map[string]string{
			"Access-Control-Allow-Origin":   testOrigin,
			"Access-Control-Allow-Methods":  "GET, POST, PUT, PATCH, DELETE, OPTIONS",
			"Access-Control-Allow-Headers":  "Authorization, Content-Type, Idempotency-Key, X-Request-ID",
			"Access-Control-Expose-Headers": exposed,
			"Access-Control-Max-Age":        "600",
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s = %q, want %q", k, got, v)
			}
		}
		if got := vary(rec); got != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" {
			t.Errorf("Vary = %q", got)
		}
	})

	t.Run("preflight from disallowed origin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/v1/imports", nil)
		req.Header.Set("Origin", "https://evil.example")
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		for k := range rec.Header() {
			if strings.HasPrefix(k, "Access-Control-") {
				t.Errorf("disallowed preflight got %s", k)
			}
		}
		// It falls through to the routes, which do not serve OPTIONS.
		decodeProblem(t, rec, http.StatusMethodNotAllowed)
		if got := vary(rec); got != "Origin, Access-Control-Request-Method, Access-Control-Request-Headers" {
			t.Errorf("Vary = %q", got)
		}
	})
}

func TestImportSurvivesConcurrentCreates(t *testing.T) {
	srv, _ := newTestServer()
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	const n = 30
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- do(h, http.MethodPost, "/v1/imports", `{"fplLeagueId":90001,"seat":1}`).Code
		}()
	}
	wg.Wait()
	close(codes)
	created := 0
	for c := range codes {
		if c == http.StatusAccepted {
			created++
		} else if c != http.StatusOK && c != http.StatusTooManyRequests {
			t.Errorf("status = %d", c)
		}
	}
	if created != 1 {
		t.Fatalf("%d imports were created for one league, want 1", created)
	}
}
