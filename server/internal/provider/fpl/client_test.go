package fpl

import (
	"context"
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

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// testdata holds hand-crafted responses in FPL Draft's documented shapes
// (RESEARCH.md §6), never fetched from FPL. Gameweek 1 is final, gameweek
// 2 is live (one match over, one in progress), gameweek 3 is a double for
// two clubs, and one fixture is postponed.

var ctx = context.Background()

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// noSleep records waits instead of sleeping.
type noSleep struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (n *noSleep) sleep(ctx context.Context, d time.Duration) error {
	n.mu.Lock()
	n.waits = append(n.waits, d)
	n.mu.Unlock()
	return ctx.Err()
}

func dirClient() *Client {
	return New(Options{Transport: DirTransport{FS: os.DirFS("testdata")}, Logger: quiet(), MinInterval: -1})
}

func TestBootstrapMapsPlayersClubsGameweeksAndFixtures(t *testing.T) {
	c := dirClient()
	b, err := c.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Clubs) != 4 || len(b.Players) != 30 || len(b.Gameweeks) != 3 || len(b.Payload) == 0 {
		t.Fatalf("clubs %d players %d gameweeks %d", len(b.Clubs), len(b.Players), len(b.Gameweeks))
	}
	if c := b.Clubs[0]; c.ExternalID != "901" || c.ShortName != "NOR" || c.Name != "Northgate" || c.SeasonTeamID != 1 {
		t.Errorf("club = %+v", c)
	}
	p := b.Players[3]
	if p.ExternalID != "1004" || p.SeasonElementID != 4 || p.DisplayName != "Okafor" || p.Position != provider.FWD ||
		p.ClubExternalID != "901" || p.Availability != provider.AvailabilityDoubtful || p.DraftRank != 2 || p.News == "" {
		t.Errorf("player = %+v", p)
	}
	for _, want := range []struct {
		i    int
		want provider.Availability
	}{{7, provider.AvailabilityInjured}, {11, provider.AvailabilitySuspended}, {17, provider.AvailabilityUnavailable}} {
		if got := b.Players[want.i].Availability; got != want.want {
			t.Errorf("player %d availability = %s, want %s", want.i, got, want.want)
		}
	}
	gw := b.Gameweeks[1]
	if gw.Number != 2 || gw.Name != "Gameweek 2" || !gw.DeadlineAt.Equal(time.Date(2026, 8, 21, 17, 30, 0, 0, time.UTC)) ||
		!gw.WaiverDeadlineAt.Equal(gw.DeadlineAt.Add(-24*time.Hour)) || gw.Finished || gw.DataChecked {
		t.Errorf("gameweek = %+v", gw)
	}
	if !b.Gameweeks[0].Finished || !b.Gameweeks[0].DataChecked {
		t.Errorf("gameweek 1 = %+v", b.Gameweeks[0])
	}
	if b.Settings.SquadSize != 15 || b.Settings.Scoring.Goal.DEF != 6 || b.Settings.Scoring.RedCard != -3 {
		t.Errorf("settings = %+v", b.Settings)
	}

	fixtures, err := c.Fixtures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 8 {
		t.Fatalf("fixtures = %d", len(fixtures))
	}
	status := map[string]provider.FixtureStatus{}
	for _, f := range fixtures {
		status[f.ExternalID] = f.Status
	}
	want := map[string]provider.FixtureStatus{
		"2601": provider.FixtureFinished, "2603": provider.FixtureFinishedProvisional,
		"2604": provider.FixtureLive, "2605": provider.FixtureScheduled, "2608": provider.FixturePostponed,
	}
	for code, st := range want {
		if status[code] != st {
			t.Errorf("fixture %s status = %s, want %s", code, status[code], st)
		}
	}
	for _, f := range fixtures {
		if f.ExternalID == "2608" && (f.Gameweek != 0 || f.KickoffAt != nil) {
			t.Errorf("postponed fixture = %+v", f)
		}
		if f.ExternalID == "2601" && (f.HomeClubExternalID != "901" || f.AwayClubExternalID != "902" || *f.HomeScore != 2) {
			t.Errorf("fixture 2601 = %+v", f)
		}
	}
}

// FPL's own explain breakdown adds up to its total for every player: the
// fixture is internally consistent, so the scoring golden tests can trust it.
func TestEventLiveMapsStatsAndExplain(t *testing.T) {
	live, err := dirClient().EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if live.Gameweek != 1 || len(live.Players) != 30 || len(live.Fixtures) != 2 || live.FetchedAt.IsZero() {
		t.Fatalf("live = gw %d, %d players, %d fixtures", live.Gameweek, len(live.Players), len(live.Fixtures))
	}
	byID := map[string]provider.PlayerLive{}
	for _, p := range live.Players {
		byID[p.PlayerExternalID] = p
		sum := 0
		for _, fe := range p.Explain {
			for _, l := range fe.Lines {
				sum += l.Points
			}
		}
		if sum != p.TotalPoints {
			t.Errorf("player %s: explain sums to %d, total_points %d", p.PlayerExternalID, sum, p.TotalPoints)
		}
	}
	p := byID["1002"]
	if p.Minutes != 90 || p.GoalsScored != 1 || p.YellowCards != 1 || p.Bonus != 3 || p.BPS != 38 ||
		p.TotalPoints != 12 || len(p.Explain) != 1 || p.Explain[0].FixtureExternalID != "2601" {
		t.Errorf("player 1002 = %+v", p)
	}
	// Only 1002 carries starts in this fixture; the others predate it.
	if p.Starts == nil || *p.Starts != 1 {
		t.Errorf("player 1002 starts = %v, want 1", p.Starts)
	}
	if s := byID["1001"].Starts; s != nil {
		t.Errorf("player 1001 starts = %d, want nil (not sent)", *s)
	}
	if p := byID["1008"]; p.Minutes != 0 || len(p.Explain) != 0 {
		t.Errorf("unused sub = %+v", p)
	}
	// The underlying stats, sent as decimal strings.
	for name, got := range map[string]*float64{
		"influence": p.Influence, "creativity": p.Creativity, "threat": p.Threat, "ict": p.ICTIndex,
		"xg": p.ExpectedGoals, "xa": p.ExpectedAssists, "xgi": p.ExpectedGoalInvolvements, "xgc": p.ExpectedGoalsConceded,
	} {
		want := map[string]float64{"influence": 58.4, "creativity": 21.7, "threat": 44, "ict": 12.4,
			"xg": 0.71, "xa": 0.12, "xgi": 0.83, "xgc": 1.04}[name]
		if got == nil || *got != want {
			t.Errorf("player 1002 %s = %v, want %v", name, got, want)
		}
	}
	if p.Recoveries != 4 || p.Tackles != 3 || p.ClearancesBlocksInterceptions != 8 || p.DefensiveContribution != 11 {
		t.Errorf("player 1002 defensive stats = %+v", p)
	}
	// A payload without them (most players here only carry influence).
	if p := byID["1001"]; p.Influence == nil || *p.Influence != 0 || p.Creativity != nil || p.ExpectedGoals != nil {
		t.Errorf("player 1001 influence %v, creativity %v, xG %v", p.Influence, p.Creativity, p.ExpectedGoals)
	}
}

// The decimal stats score nothing: numbers are read like strings, and one
// that does not parse is dropped instead of failing the snapshot.
func TestEventLiveToleratesDecimalStats(t *testing.T) {
	const body = `{"elements":{
	  "1":{"stats":{"minutes":90,"influence":12.5,"creativity":"n/a","threat":null,"ict_index":"","expected_goals":"0.05"},"explain":[]}},
	 "fixtures":[]}`
	_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		if r.URL.Path == "/api/event/1/live" {
			_, _ = io.WriteString(w, body)
			return true
		}
		return false
	})
	live, err := c.EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	p := live.Players[0]
	if p.Influence == nil || *p.Influence != 12.5 || p.Creativity != nil || p.Threat != nil || p.ICTIndex != nil ||
		p.ExpectedGoals == nil || *p.ExpectedGoals != 0.05 || p.ExpectedAssists != nil {
		t.Errorf("decimals = %v %v %v %v %v %v", p.Influence, p.Creativity, p.Threat, p.ICTIndex, p.ExpectedGoals, p.ExpectedAssists)
	}
}

// The per-fixture BPS comes from the fixtures' own stats ("bps", split
// into home and away lists), so a double gameweek can rank a player in
// each match (FPL awards bonus per match). Gameweek 3 is a double for
// Northgate: element 4 plays 2605 (BPS 5) and 2607 (BPS 40).
func TestEventLiveSplitsBPSPerFixture(t *testing.T) {
	const body = `{"elements":{
	  "4":{"stats":{"minutes":150,"bps":45,"total_points":4},"explain":[
	    [[{"stat":"minutes","points":2,"value":90}],5],
	    [[{"stat":"minutes","points":2,"value":60}],7]]},
	  "11":{"stats":{"minutes":60,"bps":30,"total_points":2},"explain":[[[{"stat":"minutes","points":2,"value":60}],7]]}},
	 "fixtures":[
	  {"id":5,"code":2605,"event":3,"team_h":1,"team_a":3,"kickoff_time":"2026-08-29T14:00:00Z","started":true,"finished_provisional":true,
	   "stats":[{"identifier":"bps","h":[{"value":5,"element":4}],"a":[]}]},
	  {"id":7,"code":2607,"event":3,"team_h":1,"team_a":4,"kickoff_time":"2026-08-29T16:30:00Z","started":true,
	   "stats":[{"identifier":"goals_scored","h":[],"a":[]},{"identifier":"bps","h":[{"value":40,"element":4}],"a":[{"value":30,"element":11}]}]}]}`
	_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		if r.URL.Path == "/api/event/3/live" {
			_, _ = io.WriteString(w, body)
			return true
		}
		return false
	})
	live, err := c.EventLive(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	bps := map[[2]string]int{}
	for _, p := range live.Players {
		for _, fe := range p.Explain {
			if fe.BPS == nil {
				t.Errorf("player %s in %s: no per-fixture BPS", p.PlayerExternalID, fe.FixtureExternalID)
				continue
			}
			bps[[2]string{p.PlayerExternalID, fe.FixtureExternalID}] = *fe.BPS
		}
	}
	want := map[[2]string]int{{"1004", "2605"}: 5, {"1004", "2607"}: 40, {"1011", "2607"}: 30}
	for k, w := range want {
		if bps[k] != w {
			t.Errorf("player %s in %s: BPS %d, want %d", k[0], k[1], bps[k], w)
		}
	}

	// The recorded gameweek 2 carries fixture stats too.
	live, err = dirClient().EventLive(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range live.Players {
		for _, fe := range p.Explain {
			if fe.BPS == nil || *fe.BPS != p.BPS {
				t.Errorf("gameweek 2 player %s: fixture BPS %v, gameweek BPS %d", p.PlayerExternalID, fe.BPS, p.BPS)
			}
		}
	}
}

func TestGameweekStatus(t *testing.T) {
	st, err := dirClient().GameweekStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != 2 || st.Next == nil || *st.Next != 3 || st.BonusAdded || st.DataChecked || !st.WaiversProcessed {
		t.Errorf("status = %+v", st)
	}
}

// event-status confirms bonus per matchday; the gameweek's bonus is added
// only when every one of its days is.
func TestGameweekStatusBonusPerMatchday(t *testing.T) {
	const body = `{"leagues":"","status":[
	  {"bonus_added":true,"date":"2026-08-22","event":2,"points":"r"},
	  {"bonus_added":false,"date":"2026-08-23","event":2,"points":"l"},
	  {"bonus_added":true,"date":"2026-08-16","event":1,"points":"r"}]}`
	_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		if r.URL.Path == "/api/pl/event-status" {
			_, _ = io.WriteString(w, body)
			return true
		}
		return false
	})
	st, err := c.GameweekStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.BonusAdded || !st.BonusAddedOn["2026-08-22"] || st.BonusAddedOn["2026-08-23"] || len(st.BonusAddedOn) != 1 {
		t.Errorf("status = %+v", st)
	}
}

// server serves testdata, with optional overrides by path.
type server struct {
	mu       sync.Mutex
	hits     map[string]int
	override func(w http.ResponseWriter, r *http.Request, hit int) bool
	ua       string
}

func newServer(t *testing.T, override func(w http.ResponseWriter, r *http.Request, hit int) bool) (*server, *Client, *noSleep) {
	t.Helper()
	s := &server{hits: map[string]int{}, override: override}
	files := DirTransport{FS: os.DirFS("testdata")}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		hit := s.hits[r.URL.Path]
		s.ua = r.Header.Get("User-Agent")
		s.mu.Unlock()
		if s.override != nil && s.override(w, r, hit) {
			return
		}
		resp, _ := files.RoundTrip(r)
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(ts.Close)
	ns := &noSleep{}
	c := New(Options{BaseURL: ts.URL + "/api", Logger: quiet(), Sleep: ns.sleep, MinInterval: -1})
	return s, c, ns
}

func TestRetriesTransientFailuresWithBackoff(t *testing.T) {
	s, c, ns := newServer(t, func(w http.ResponseWriter, r *http.Request, hit int) bool {
		if r.URL.Path == "/api/bootstrap-static" && hit <= 2 {
			if hit == 1 {
				w.Header().Set("Retry-After", "7")
				w.WriteHeader(http.StatusTooManyRequests)
			} else {
				w.WriteHeader(http.StatusBadGateway)
			}
			return true
		}
		return false
	})
	if _, err := c.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if s.hits["/api/bootstrap-static"] != 3 {
		t.Errorf("hits = %d, want 3", s.hits["/api/bootstrap-static"])
	}
	if len(ns.waits) != 2 || ns.waits[0] < 7*time.Second {
		t.Errorf("waits = %v; the first must honour Retry-After: 7", ns.waits)
	}
	if !strings.HasPrefix(s.ua, "MyFantasyPrem") {
		t.Errorf("User-Agent = %q", s.ua)
	}
}

func TestGivesUpAfterMaxAttemptsAndDoesNotRetryClientErrors(t *testing.T) {
	s, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		switch r.URL.Path {
		case "/api/bootstrap-static":
			w.WriteHeader(http.StatusServiceUnavailable)
			return true
		case "/api/game":
			w.WriteHeader(http.StatusForbidden)
			return true
		}
		return false
	})
	_, err := c.Bootstrap(ctx)
	if !errors.Is(err, provider.ErrUpstream) || errors.Is(err, provider.ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if s.hits["/api/bootstrap-static"] != 4 {
		t.Errorf("attempts = %d, want 4", s.hits["/api/bootstrap-static"])
	}
	if _, err := c.GameweekStatus(ctx); !errors.Is(err, provider.ErrUpstream) {
		t.Fatalf("403: err = %v", err)
	}
	if s.hits["/api/game"] != 1 {
		t.Errorf("403 retried: %d attempts", s.hits["/api/game"])
	}
	if _, err := c.EventLive(ctx, 39); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("gameweek 39: err = %v", err)
	}
}

func TestNotFound(t *testing.T) {
	_, c, _ := newServer(t, nil)
	if _, err := c.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.EventLive(ctx, 30); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestConditionalRequests(t *testing.T) {
	var sawIfNoneMatch bool
	s, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, hit int) bool {
		if r.URL.Path != "/api/event/1/live" {
			return false
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			sawIfNoneMatch = true
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		w.Header().Set("ETag", `"v1"`)
		return false
	})
	first, err := c.EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	second, err := c.EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !sawIfNoneMatch || s.hits["/api/event/1/live"] != 2 || len(second.Players) != len(first.Players) {
		t.Errorf("conditional request: sent=%v hits=%d", sawIfNoneMatch, s.hits["/api/event/1/live"])
	}
}

func TestRejectsMalformedResponses(t *testing.T) {
	cases := []struct {
		name, path, body string
		call             func(*Client) error
	}{
		{"not json", "/api/bootstrap-static", `<html>Service Unavailable</html>`,
			func(c *Client) error { _, err := c.Bootstrap(ctx); return err }},
		{"truncated", "/api/bootstrap-static", `{"elements": [`,
			func(c *Client) error { _, err := c.Bootstrap(ctx); return err }},
		{"no teams", "/api/bootstrap-static", `{"elements":[{"id":1,"code":1,"web_name":"A","team":1,"element_type":1}],"teams":[],"events":{"data":[]}}`,
			func(c *Client) error { _, err := c.Bootstrap(ctx); return err }},
		{"unknown team", "/api/bootstrap-static",
			`{"elements":[{"id":1,"code":1,"web_name":"A","team":9,"element_type":1}],"teams":[{"id":1,"code":1,"name":"A","short_name":"AAA"},{"id":2,"code":2,"name":"B","short_name":"BBB"}],"events":{"data":[{"id":1,"deadline_time":"2026-08-14T17:30:00Z"}]}}`,
			func(c *Client) error { _, err := c.Bootstrap(ctx); return err }},
		{"live unknown element", "/api/event/1/live", `{"elements":{"999":{"stats":{"minutes":90},"explain":[]}},"fixtures":[]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 1); return err }},
		{"live unknown fixture", "/api/event/1/live", `{"elements":{"1":{"stats":{"minutes":90},"explain":[[[{"stat":"minutes","points":2,"value":90}],77]]}},"fixtures":[]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 1); return err }},
		{"live negative minutes", "/api/event/1/live", `{"elements":{"1":{"stats":{"minutes":-5},"explain":[]}},"fixtures":[]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 1); return err }},
		{"live missing stats", "/api/event/1/live", `{"elements":{"1":{"explain":[]}},"fixtures":[]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 1); return err }},
		{"live wrong gameweek fixture", "/api/event/1/live",
			`{"elements":{},"fixtures":[{"id":9,"code":9,"event":5,"team_h":1,"team_a":2,"kickoff_time":"2026-08-15T14:00:00Z"}]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 1); return err }},
		// ADR 0005: an empty snapshot after kickoff would zero every score.
		{"live empty after kickoff", "/api/event/1/live", `{"elements":{},"fixtures":[]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 1); return err }},
		{"live empty with started fixture", "/api/event/2/live",
			`{"elements":{},"fixtures":[{"id":3,"code":2603,"event":2,"team_h":2,"team_a":3,"kickoff_time":"2026-08-22T14:00:00Z","started":true,"finished":false,"finished_provisional":true}]}`,
			func(c *Client) error { _, err := c.EventLive(ctx, 2); return err }},
		{"game without current", "/api/game", `{"next_event":3}`,
			func(c *Client) error { _, err := c.GameweekStatus(ctx); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
				if r.URL.Path == tc.path {
					_, _ = io.WriteString(w, tc.body)
					return true
				}
				return false
			})
			err := tc.call(c)
			if !errors.Is(err, provider.ErrUpstream) || !errors.Is(err, provider.ErrInvalid) {
				t.Fatalf("err = %v, want ErrUpstream and ErrInvalid", err)
			}
		})
	}
}

func TestPacesRequests(t *testing.T) {
	ns := &noSleep{}
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	c := New(Options{
		Transport: DirTransport{FS: os.DirFS("testdata")}, Logger: quiet(), Sleep: ns.sleep,
		MinInterval: 500 * time.Millisecond, Now: func() time.Time { return now },
	})
	for range 3 {
		if _, err := c.GameweekStatus(ctx); err != nil {
			t.Fatal(err)
		}
	}
	var spaced int
	for _, w := range ns.waits {
		if w > 0 {
			spaced++
		}
	}
	if spaced == 0 {
		t.Errorf("no request was delayed: %v", ns.waits)
	}
}

// Before a gameweek's first kickoff upstream may legitimately list nobody.
func TestEventLiveEmptyBeforeKickoffIsAccepted(t *testing.T) {
	_, c, _ := newServer(t, func(w http.ResponseWriter, r *http.Request, _ int) bool {
		if r.URL.Path == "/api/event/3/live" {
			_, _ = io.WriteString(w, `{"elements":{},"fixtures":[]}`)
			return true
		}
		return false
	})
	live, err := c.EventLive(ctx, 3)
	if err != nil || len(live.Players) != 0 {
		t.Fatalf("live = %+v, %v", live, err)
	}
}
