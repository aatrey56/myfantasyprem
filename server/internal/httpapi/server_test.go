package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

func decodeImport(t *testing.T, rec *httptest.ResponseRecorder) ImportResponse {
	t.Helper()
	var r ImportResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &r); err != nil {
		t.Fatalf("bad import body %s: %v", rec.Body, err)
	}
	return r
}

// realStats ingests the real gameweek 1 and 6 stats recorded in
// provider/fpl's testdata, as the poller does, and sets the clock to
// Wednesday 14 October 2026 (gameweek 6 finished, 7 next).
func realStats(t *testing.T, srv *Server, clock *fakeClock) {
	t.Helper()
	clock.Set(time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC))
	src := fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata/real-gw6")},
		Logger: testOptions().Logger, MinInterval: -1})
	p := poller.New(src, stats.NewIngester(srv.stats, "fpl", srv.now), srv.stats, noRescore{}, poller.Config{}, testOptions().Logger)
	p.Now = srv.now
	p.Sleep = func(context.Context, time.Duration) error { return nil }
	for range 3 {
		p.Tick(context.Background())
	}
}

type noRescore struct{}

func (noRescore) Recompute(context.Context, int, []string, bool) (league.RecomputeResult, error) {
	return league.RecomputeResult{}, nil
}

// The whole import over HTTP, on the real (anonymised) league: preview,
// import, the job, the league's sync status, its seats and a claim.
func TestImportLifecycle(t *testing.T) {
	srv, clock := newTestServer()
	realStats(t, srv, clock)
	testToken = signIn(srv, "Sam P.", "sam-real@example.com")
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))

	rec := do(h, http.MethodGet, "/v1/imports/preview?fplLeagueId=90001", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("preview = %d %s", rec.Code, rec.Body)
	}
	var preview ImportPreviewResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &preview)
	if p := preview.Data; p.Name != "Fixture Draft League" || p.TeamCount != 12 || len(p.Seats) != 12 || p.CurrentGameweek != 6 ||
		!p.Importable || p.Scoring != ImportLeagueScoringH2h || !p.DraftComplete || p.Problem != nil || p.AlreadyImportedLeagueId != nil {
		t.Fatalf("preview = %+v", p)
	}

	rec = do(h, http.MethodPost, "/v1/imports", `{"draftLeagueUrl":"https://draft.premierleague.com/league/90001/standings","seat":3}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d: %s", rec.Code, rec.Body)
	}
	created := decodeImport(t, rec)
	id := created.Data.Id
	if got := rec.Header().Get("Location"); !ids.Valid("imp", id) || got != "/v1/imports/"+id || rec.Header().Get("Retry-After") != "2" {
		t.Errorf("id %q Location %q", id, got)
	}
	if d := created.Data; d.Status != ImportStatusQueued || d.FplLeagueId != 90001 || d.Error != nil || d.LeagueId != nil {
		t.Errorf("created = %+v", d)
	}
	// Nulls are present, not omitted.
	var raw struct{ Data map[string]json.RawMessage }
	_ = json.Unmarshal(rec.Body.Bytes(), &raw)
	if string(raw.Data["error"]) != "null" || string(raw.Data["leagueId"]) != "null" {
		t.Errorf("error/leagueId = %s / %s, want null", raw.Data["error"], raw.Data["leagueId"])
	}
	// The same caller again, by id: the same import.
	again := do(h, http.MethodPost, "/v1/imports", `{"fplLeagueId":90001,"seat":3}`)
	if again.Code != http.StatusOK || decodeImport(t, again).Data.Id != id || again.Header().Get("Location") != "" {
		t.Errorf("repeat = %d %s", again.Code, again.Body)
	}

	// The job (River runs it in the server).
	if err := srv.mirror.RunImport(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	done := decodeImport(t, do(h, http.MethodGet, "/v1/imports/"+id, "")).Data
	if done.Status != ImportStatusSucceeded || done.LeagueId == nil || done.Progress.Done != done.Progress.Total || done.Progress.Total < 70 {
		t.Fatalf("finished import = %+v", done)
	}
	leagueID := *done.LeagueId

	var sync LeagueSyncResponse
	rec = do(h, http.MethodGet, "/v1/leagues/"+leagueID+"/sync", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &sync); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("sync = %d %s", rec.Code, rec.Body)
	}
	sv := sync.Data
	if sv.LastSyncedAt == nil || sv.Pending != nil || sv.Note != nil || sv.Verification == nil ||
		sv.Verification.Counts.Mismatch != 0 || sv.Verification.Counts.Unverified != 0 || sv.Verification.Status != SyncReportStatusClean ||
		sv.Verification.Counts.Ok != 278 || len(sv.Verification.Failing) != 0 || sv.Verification.Unexplained != 0 {
		t.Fatalf("sync status = %+v, counts %+v", sv, sv.Verification.Counts)
	}

	// The feed carries FPL's raw codes for support.
	var feed struct {
		Data []struct {
			Kind        string  `json:"kind"`
			SourceState *string `json:"sourceState"`
		} `json:"data"`
	}
	_ = json.Unmarshal(do(h, http.MethodGet, "/v1/leagues/"+leagueID+"/transactions?limit=100", "").Body.Bytes(), &feed)
	codes := map[string]int{}
	for _, x := range feed.Data {
		if x.SourceState != nil {
			codes[*x.SourceState]++
		}
	}
	if len(feed.Data) != 100 || codes["w/a"]+codes["w/di"]+codes["w/do"]+codes["f/a"]+codes["p"]+codes["v"] != 100 {
		t.Errorf("feed source states = %v of %d", codes, len(feed.Data))
	}

	var seats SeatListResponse
	rec = do(h, http.MethodGet, "/v1/leagues/"+leagueID+"/seats", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &seats)
	claimed, links := 0, 0
	var link string
	for _, s := range seats.Data {
		if s.Claimed {
			claimed++
		}
		if s.Invite != nil {
			links++
			link = s.Invite.Code
			if s.Invite.ClaimTeam == nil || s.Invite.ClaimTeam.Id != s.Team.Id || s.Team.ManagerName != s.FplManagerName {
				t.Errorf("seat %+v", s)
			}
		}
	}
	if rec.Code != http.StatusOK || len(seats.Data) != 12 || claimed != 1 || links != 11 {
		t.Fatalf("seats = %d: %d seats, %d claimed, %d links", rec.Code, len(seats.Data), claimed, links)
	}

	// A friend claims a seat with its link; the league is playable here.
	friend := signIn(srv, "Friend", "friend@example.com")
	rec = doAs(h, friend, http.MethodGet, "/v1/invites/"+link, "")
	var ip InvitePreviewResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &ip)
	if rec.Code != http.StatusOK || !ip.Data.Joinable || ip.Data.ClaimTeam == nil {
		t.Fatalf("claim preview = %d %s", rec.Code, rec.Body)
	}
	if rec := doAs(h, friend, http.MethodPost, "/v1/invites/"+link+"/accept", `{}`); rec.Code != http.StatusCreated {
		t.Fatalf("claim = %d %s", rec.Code, rec.Body)
	}
	decodeProblem(t, doAs(h, friend, http.MethodPatch, "/v1/leagues/"+leagueID, `{"name":"Renamed"}`), http.StatusForbidden)
	if rec := do(h, http.MethodPatch, "/v1/leagues/"+leagueID, `{"name":"Renamed"}`); rec.Code != http.StatusOK {
		t.Errorf("rename = %d %s", rec.Code, rec.Body)
	}

	// The commissioner re-syncs: confirmed, queued, then the job resets the
	// league to FPL's state and every member sees the notice.
	decodeProblem(t, do(h, http.MethodPost, "/v1/leagues/"+leagueID+"/sync", `{"confirm":false}`), http.StatusUnprocessableEntity)
	rec = do(h, http.MethodPost, "/v1/leagues/"+leagueID+"/sync", `{"confirm":true}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &sync); err != nil || rec.Code != http.StatusAccepted ||
		sync.Data.Pending == nil || sync.Data.Pending.Kind != Resync {
		t.Fatalf("re-sync = %d %s", rec.Code, rec.Body)
	}
	if err := srv.mirror.Resync(context.Background(), leagueID); err != nil {
		t.Fatal(err)
	}
	rec = doAs(h, friend, http.MethodGet, "/v1/leagues/"+leagueID+"/sync", "")
	sync = LeagueSyncResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &sync); err != nil || rec.Code != http.StatusOK || sync.Data.Pending != nil ||
		sync.Data.Note == nil || sync.Data.Verification != nil {
		t.Fatalf("member's sync after the re-sync = %d %s", rec.Code, rec.Body)
	}

	// A shadow check (the scheduled read-only comparison) shows on the
	// commissioner's FPL status only.
	if sv.Shadow != nil {
		t.Errorf("shadow before any check = %+v", sv.Shadow)
	}
	if _, err := srv.mirror.ShadowCheck(context.Background(), leagueID, "after_deadline"); err != nil {
		t.Fatal(err)
	}
	sync = LeagueSyncResponse{}
	rec = do(h, http.MethodGet, "/v1/leagues/"+leagueID+"/sync", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &sync); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("sync = %d %s", rec.Code, rec.Body)
	}
	if sh := sync.Data.Shadow; sh == nil || sh.Reason != "after_deadline" || sh.Gameweek == nil || *sh.Gameweek != 6 ||
		sh.Status != SyncReportStatusClean || sh.Counts.Mismatch != 0 || sh.Counts.Ok < 278 || len(sh.Mismatches) != 0 || sh.Pending == nil {
		t.Fatalf("shadow = %s", rec.Body)
	}
	sync = LeagueSyncResponse{}
	rec = doAs(h, friend, http.MethodGet, "/v1/leagues/"+leagueID+"/sync", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &sync); err != nil || sync.Data.Shadow != nil {
		t.Fatalf("member's shadow = %s", rec.Body)
	}
	var lg LeagueResponse
	_ = json.Unmarshal(do(h, http.MethodGet, "/v1/leagues/"+leagueID, "").Body.Bytes(), &lg)
	if lg.Data.League.Name != "Fixture Draft League" {
		t.Errorf("name after the re-sync = %q, want FPL's", lg.Data.League.Name)
	}
}

func TestGetImportErrors(t *testing.T) {
	h := newTestHandler()
	p := decodeProblem(t, do(h, http.MethodGet, "/v1/imports/"+tImportNA, ""), http.StatusNotFound)
	if problemType(p) != problemTypeBase+"not-found" {
		t.Errorf("type = %q", problemType(p))
	}
	decodeProblem(t, do(h, http.MethodGet, "/v1/imports/imp_short", ""), http.StatusBadRequest)
}

// On a server whose stats are the demo world's (POLLER_SOURCE=demo) the
// preview says up front that FPL leagues cannot be imported here, and the
// import is refused with the same problem; with nothing ingested yet the
// preview says the player data is still loading.
func TestImportWithoutFPLStats(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		t.Run(store, func(t *testing.T) {
			var pool *pgxpool.Pool
			if store == "postgres" {
				pool = storetest.New(t)
			}
			srv, _ := newTestServerOn(pool)
			h := newHandler(testOptions(), newRoutes(testOptions(), srv))
			preview := func() ImportPreview {
				t.Helper()
				rec := do(h, http.MethodGet, "/v1/imports/preview?fplLeagueId=90001", "")
				var p ImportPreviewResponse
				if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil || rec.Code != http.StatusOK {
					t.Fatalf("preview = %d %s", rec.Code, rec.Body)
				}
				return p.Data
			}

			p := preview()
			if p.Importable || p.Problem == nil || problemType(*p.Problem) != problemTypeBase+"stats-not-ready" || p.Problem.Status != 503 ||
				p.Problem.Detail == nil || *p.Problem.Detail != "Player data is still loading. Try again in a few minutes." {
				t.Fatalf("before ingest: preview = %+v (problem %+v)", p, p.Problem)
			}

			if _, err := demo.LoadStats(context.Background(), srv.stats, testOptions().Logger, srv.now); err != nil {
				t.Fatal(err)
			}
			p = preview()
			if p.Importable || p.Problem == nil || problemType(*p.Problem) != problemTypeBase+"import-unavailable" || p.Problem.Status != 409 ||
				p.Problem.Detail == nil || *p.Problem.Detail != "This server isn't connected to live player data, so FPL Draft leagues can't be imported here." {
				t.Fatalf("demo stats: preview = %+v (problem %+v)", p, p.Problem)
			}
			if p.Name == "" || len(p.Seats) != p.TeamCount {
				t.Fatalf("the preview still describes the league: %+v", p)
			}
			prob := decodeProblem(t, do(h, http.MethodPost, "/v1/imports", `{"fplLeagueId":90001,"seat":1}`), http.StatusConflict)
			if problemType(prob) != problemTypeBase+"import-unavailable" {
				t.Fatalf("POST problem = %+v", prob)
			}
		})
	}
}
