package poller

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// Replays of real FPL Draft responses for 2026-27 (provider/fpl
// testdata/real-gw6: gameweek 6 finished, 7 next; live payloads for
// gameweeks 1 and 6 only, so 2-5 answer 404). The real bootstrap lists
// only the upcoming gameweeks' fixtures, so a past gameweek's fixtures
// are known only from its live payload.

const realDir = "../provider/fpl/testdata/real-gw6"

// Tuesday 13 October 2026, 12:00 UTC: gameweek 6 is over, 7 starts on
// Saturday.
var realClock = time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)

func realStores(t *testing.T, clock time.Time, test func(t *testing.T, f fixture)) {
	t.Run("memory", func(t *testing.T) { test(t, newFixtureAt(t, stats.NewMemory(), realDir, clock)) })
	t.Run("postgres", func(t *testing.T) {
		test(t, newFixtureAt(t, stats.NewPostgres(storetest.New(t)), realDir, clock))
	})
}

// realLive is the part of the real payloads the checks need.
type realLive struct {
	Elements map[string]struct {
		Stats struct {
			Minutes     int `json:"minutes"`
			TotalPoints int `json:"total_points"`
		} `json:"stats"`
	} `json:"elements"`
	Fixtures []struct {
		ID          int64     `json:"id"`
		Code        int64     `json:"code"`
		TeamH       int64     `json:"team_h"`
		TeamA       int64     `json:"team_a"`
		TeamHScore  int       `json:"team_h_score"`
		TeamAScore  int       `json:"team_a_score"`
		KickoffTime time.Time `json:"kickoff_time"`
	} `json:"fixtures"`
}

type realBootstrap struct {
	Elements []struct {
		ID   int64 `json:"id"`
		Code int64 `json:"code"`
	} `json:"elements"`
	Teams []struct {
		ID   int64 `json:"id"`
		Code int64 `json:"code"`
	} `json:"teams"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatal(err)
	}
}

func checkRealGW6(t *testing.T, f fixture) { t.Helper(); checkRealGW(t, f, 6, 315) }

// checkRealGW checks a stored gameweek against the real payloads: its ten
// fixtures with their teams, kickoff and score, and a row for each of the
// played players with minutes whose points equal FPL's total_points.
func checkRealGW(t *testing.T, f fixture, gw, played int) {
	t.Helper()
	var live realLive
	var boot realBootstrap
	readJSON(t, realDir+"/event/"+strconv.Itoa(gw)+"/live.json", &live)
	readJSON(t, realDir+"/bootstrap-static.json", &boot)
	ids := func(entity string) map[string]string {
		m, err := f.repo.ExternalIDs(ctx, "fpl", entity)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	playerIDs, clubIDs, fixtureIDs := ids("player"), ids("club"), ids("fixture")
	teamCode := map[int64]int64{}
	for _, tm := range boot.Teams {
		teamCode[tm.ID] = tm.Code
	}
	elementCode := map[string]int64{}
	for _, e := range boot.Elements {
		elementCode[strconv.FormatInt(e.ID, 10)] = e.Code
	}
	club := func(team int64) string { return clubIDs[strconv.FormatInt(teamCode[team], 10)] }

	stored, err := f.repo.Fixtures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]stats.Fixture{}
	for _, fx := range stored {
		byID[fx.ID] = fx
	}
	if len(live.Fixtures) != 10 {
		t.Fatalf("%d live fixtures", len(live.Fixtures))
	}
	for _, lf := range live.Fixtures {
		id, ok := fixtureIDs[strconv.FormatInt(lf.Code, 10)]
		if !ok {
			t.Errorf("fixture %d (code %d) not mapped", lf.ID, lf.Code)
			continue
		}
		fx := byID[id]
		if len(id) < 4 || id[:3] != "fx_" || fx.Gameweek != gw || fx.HomeClubID != club(lf.TeamH) || fx.AwayClubID != club(lf.TeamA) ||
			fx.HomeClubID == "" || fx.Status != stats.FixtureFinished || fx.HomeScore == nil || *fx.HomeScore != lf.TeamHScore ||
			fx.AwayScore == nil || *fx.AwayScore != lf.TeamAScore || fx.KickoffAt == nil || !fx.KickoffAt.Equal(lf.KickoffTime) {
			t.Errorf("fixture %d stored as %+v", lf.ID, fx)
		}
	}

	rows, err := f.repo.PlayerFixtures(ctx, gw)
	if err != nil {
		t.Fatal(err)
	}
	points := map[string]int{}
	for _, r := range rows {
		points[r.PlayerID] += r.Points
		if r.BonusProvisional {
			t.Errorf("player %s: bonus still provisional in a finished gameweek", r.PlayerID)
		}
	}
	withMinutes := 0
	for el, e := range live.Elements {
		if e.Stats.Minutes == 0 {
			continue
		}
		withMinutes++
		pid := playerIDs[strconv.FormatInt(elementCode[el], 10)]
		if got, ok := points[pid]; !ok || got != e.Stats.TotalPoints {
			t.Errorf("element %s: points %d (row %v), FPL %d", el, got, ok, e.Stats.TotalPoints)
		}
	}
	if withMinutes != played || len(points) < played {
		t.Errorf("%d players with minutes (want %d), %d with rows", withMinutes, played, len(points))
	}
	lines, err := f.repo.PlayerGameweeks(ctx, gw)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != len(live.Elements) {
		t.Errorf("%d gameweek lines, want %d", len(lines), len(live.Elements))
	}
}

// On start with gameweek 6 current and finished, its live payload alone
// supplies the fixtures: the poll succeeds and stores every score.
func TestRealDraftStartupIngestsFinishedGameweek(t *testing.T) {
	realStores(t, realClock, func(t *testing.T, f fixture) {
		f.p.Tick(ctx)
		if ps := f.state(t); ps.ConsecutiveFailures != 0 || ps.Gameweek != 6 || ps.LastError != "" {
			t.Fatalf("poll state = %+v", ps)
		}
		checkRealGW6(t, f)
		if f.src.calls["bootstrap"] != 1 {
			t.Errorf("bootstrap fetched %d times", f.src.calls["bootstrap"])
		}
		// Polling again changes nothing.
		before, _ := f.repo.PlayerFixtures(ctx, 6)
		f.advance(time.Hour)
		f.p.Tick(ctx)
		after, _ := f.repo.PlayerFixtures(ctx, 6)
		if ps := f.state(t); ps.ConsecutiveFailures != 0 || len(after) != len(before) {
			t.Errorf("second poll: %+v, rows %d then %d", ps, len(before), len(after))
		}
	})
}

// After the rollover to gameweek 7 the closed gameweek 6 is backfilled
// from its live payload, although no stored fixture places it.
func TestRealDraftBackfillsPastGameweekFromLive(t *testing.T) {
	realStores(t, realClock.Add(72*time.Hour), func(t *testing.T, f fixture) {
		next := 8
		f.src.mutateStatus = func(st *provider.GameweekStatus) {
			st.Current, st.Next, st.BonusAdded, st.BonusAddedOn = 7, &next, false, nil
		}
		f.p.Tick(ctx)
		if ps := f.state(t); ps.ConsecutiveFailures != 0 || ps.Gameweek != 7 {
			t.Fatalf("poll state = %+v", ps)
		}
		if f.src.calls["live/6"] != 1 {
			t.Fatalf("gameweek 6 fetched %d times", f.src.calls["live/6"])
		}
		checkRealGW6(t, f)
	})
}

// On start with gameweek 6 current, five closed gameweeks are queued but
// a poll fetches at most BackfillPerTick (4) of them, most recent first.
// Gameweek 1 came fifth and waited for the next poll, hours away once
// gameweek 6 is over, and since 2-5 failed every time they took the
// budget on every poll: gameweek 1 was never fetched.
func TestRealDraftBackfillReachesGameweek1(t *testing.T) {
	realStores(t, realClock, func(t *testing.T, f fixture) {
		for i := 0; i < 2 && f.src.calls["live/1"] == 0; i++ {
			f.advance(f.p.Tick(ctx))
		}
		if f.src.calls["live/1"] != 1 {
			t.Fatalf("gameweek 1 fetched %d times in two polls; live calls %v", f.src.calls["live/1"], f.src.calls)
		}
		checkRealGW(t, f, 1, 310)
		checkRealGW6(t, f)
	})
}

// While gameweeks never tried are still queued, the next poll comes after
// LiveInterval instead of the mode's wait; once only failing ones remain
// the mode's wait applies again, so they are not retried in a tight loop.
func TestBackfillBudgetShortensTheWait(t *testing.T) {
	f := newFixtureAt(t, stats.NewMemory(), realDir, realClock)
	f.p = f.newPoller(Config{BackfillPerTick: 2})
	var waits []time.Duration
	for range 4 {
		w := f.p.Tick(ctx)
		waits = append(waits, w)
		f.advance(w)
	}
	// Queued 5..1; 5 and 4 fail, then 3 and 2, then 1 is stored and 5, 4
	// are retried; after that only failing ones are left.
	short := f.p.Config.LiveInterval
	if waits[0] != short || waits[1] != short || waits[2] == short || waits[3] == short {
		t.Errorf("waits = %v, want %v twice, then the mode's wait", waits, short)
	}
	if f.src.calls["live/1"] != 1 {
		t.Fatalf("gameweek 1 fetched %d times", f.src.calls["live/1"])
	}
	checkRealGW(t, f, 1, 310)
}

// FPL Draft events have no data_checked: a finished one counts as
// checked (ADR 0005). Gameweek 6 is final at once, so the poller goes
// idle instead of refetching bootstrap in unchecked mode for ever, and a
// later change to its live data is still ingested and rescored.
func TestRealDraftFinishedGameweekIsFinal(t *testing.T) {
	realStores(t, realClock, func(t *testing.T, f fixture) {
		f.p.Tick(ctx)
		gws, err := f.repo.Gameweeks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range gws {
			if g.Number <= 6 && (!g.Finished || !g.DataChecked || !g.BonusAdded) || g.Number > 6 && g.DataChecked {
				t.Errorf("gameweek %d stored as %+v", g.Number, g)
			}
		}
		w, err := league.LoadWorld(ctx, f.repo, 6)
		if err != nil {
			t.Fatal(err)
		}
		if s := w.State(6); s != scoring.StateFinal {
			t.Errorf("gameweek 6 score state = %s, want final", s)
		}
		if ps := f.state(t); ps.Mode != stats.ModeIdle {
			t.Fatalf("mode = %s, want idle", ps.Mode)
		}

		// Polls in the following hours fetch bootstrap only when it is due.
		for range 6 {
			f.advance(10 * time.Minute)
			f.p.Tick(ctx)
		}
		if n := f.src.calls["bootstrap"]; n != 1 {
			t.Errorf("bootstrap fetched %d times in an hour", n)
		}

		// A correction after the gameweek is final is still stored and
		// rescored.
		var scorer string
		f.src.mutate = func(l *provider.EventLive) {
			if l.Gameweek != 6 {
				return
			}
			for i := range l.Players {
				if l.Players[i].Minutes > 0 && l.Players[i].GoalsScored == 0 {
					scorer = l.Players[i].PlayerExternalID
					l.Players[i].GoalsScored = 1
					return
				}
			}
		}
		n := len(f.rec.callsFor(6))
		f.advance(10 * time.Minute)
		f.p.Tick(ctx)
		if gw6 := f.rec.callsFor(6); len(gw6) != n+1 || gw6[n] != "gw6 changed=1 all=false" {
			t.Errorf("gameweek 6 recompute after a correction of %s = %v", scorer, gw6[n:])
		}
		if ps := f.state(t); ps.ConsecutiveFailures != 0 || ps.Mode != stats.ModeIdle {
			t.Errorf("poll state = %+v", ps)
		}
	})
}
