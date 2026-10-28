package stats

import (
	"context"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

var ctx = context.Background()

var now = time.Date(2026, 8, 22, 15, 30, 0, 0, time.UTC)

// impls runs a test against every Repo: Memory always, Postgres when
// TEST_DATABASE_URL is set.
func impls(t *testing.T, test func(t *testing.T, repo Repo)) {
	t.Run("memory", func(t *testing.T) { test(t, NewMemory()) })
	t.Run("postgres", func(t *testing.T) { test(t, NewPostgres(storetest.New(t))) })
}

// source reads the hand-crafted FPL fixtures in provider/fpl/testdata.
func source() *fpl.Client {
	return fpl.New(fpl.Options{
		Transport:   fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata")},
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
		MinInterval: -1,
	})
}

// ingestAll loads the season and the live gameweeks 1 and 2.
func ingestAll(t *testing.T, repo Repo) (*Ingester, *fpl.Client, *provider.GameweekStatus) {
	t.Helper()
	src := source()
	in := NewIngester(repo, "fpl", func() time.Time { return now })
	b, err := src.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, err := src.Fixtures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	st, err := src.GameweekStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Season(ctx, b, fixtures, st); err != nil {
		t.Fatal(err)
	}
	for _, gw := range []int{1, 2} {
		live, err := src.EventLive(ctx, gw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.Live(ctx, live, st); err != nil {
			t.Fatalf("gameweek %d: %v", gw, err)
		}
	}
	return in, src, st
}

// playerIDs maps FPL codes to our ids.
func playerIDs(t *testing.T, repo Repo) map[string]string {
	t.Helper()
	m, err := repo.ExternalIDs(ctx, "fpl", "player")
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSeasonIngest(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, src, st := ingestAll(t, repo)
		clubs, _ := repo.Clubs(ctx)
		players, _ := repo.Players(ctx)
		gws, _ := repo.Gameweeks(ctx)
		fixtures, _ := repo.Fixtures(ctx)
		if len(clubs) != 4 || len(players) != 30 || len(gws) != 3 || len(fixtures) != 8 {
			t.Fatalf("clubs %d players %d gameweeks %d fixtures %d", len(clubs), len(players), len(gws), len(fixtures))
		}
		if clubs[0].Code != "ASH" || clubs[0].Name != "Ashford" {
			t.Errorf("clubs sorted by name: %+v", clubs[0])
		}
		if !gws[0].BonusAdded || !gws[0].DataChecked || !gws[0].Finished || gws[1].BonusAdded || gws[1].Finished {
			t.Errorf("gameweek flags: %+v %+v", gws[0], gws[1])
		}
		status, ok, err := repo.Status(ctx)
		if err != nil || !ok || status.Current != 2 || status.Next != 3 || !status.WaiversProcessed {
			t.Errorf("status = %+v %v %v", status, ok, err)
		}
		postponed := 0
		for _, f := range fixtures {
			if f.Status == FixturePostponed {
				postponed++
				if f.Gameweek != 0 || f.KickoffAt != nil {
					t.Errorf("postponed fixture = %+v", f)
				}
			}
		}
		if postponed != 1 {
			t.Errorf("postponed = %d", postponed)
		}

		// Ids are stable: a second ingest maps every upstream id to the same one.
		before := playerIDs(t, repo)
		b, _ := src.Bootstrap(ctx)
		fx, _ := src.Fixtures(ctx)
		if err := in.Season(ctx, b, fx, st); err != nil {
			t.Fatal(err)
		}
		after := playerIDs(t, repo)
		if len(after) != 30 {
			t.Fatalf("mappings = %d", len(after))
		}
		for code, id := range before {
			if after[code] != id {
				t.Errorf("player %s remapped %s -> %s", code, id, after[code])
			}
		}
	})
}

// Golden test (ADR 0005): for a final gameweek our engine, fed the stats,
// reproduces FPL's own total for every player. The totals were also worked
// out by hand from RESEARCH.md §7 (see provider/fpl/testdata).
func TestGoldenPointsMatchUpstream(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		_, src, _ := ingestAll(t, repo)
		live, err := src.EventLive(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		ids := playerIDs(t, repo)
		rows, err := repo.PlayerFixtures(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]int{}
		for _, r := range rows {
			got[r.PlayerID] += r.Points
			if r.BonusProvisional {
				t.Errorf("final gameweek row has provisional bonus: %+v", r)
			}
		}
		for _, p := range live.Players {
			if got[ids[p.PlayerExternalID]] != p.TotalPoints {
				t.Errorf("player %s: we score %d, FPL %d", p.PlayerExternalID, got[ids[p.PlayerExternalID]], p.TotalPoints)
			}
		}
		// Hand-checked spot values: a defender with a goal, defensive
		// contribution, a yellow and 3 bonus; a midfielder sent off.
		if got[ids["1002"]] != 12 || got[ids["1007"]] != 6 || got[ids["1006"]] != -1 || got[ids["1009"]] != 11 {
			t.Errorf("spot values: %d %d %d %d", got[ids["1002"]], got[ids["1007"]], got[ids["1006"]], got[ids["1009"]])
		}
	})
}

// While bonus is unconfirmed it is projected from BPS per fixture with
// FPL's tie rules, and marked provisional.
func TestProvisionalBonus(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		ingestAll(t, repo)
		ids := playerIDs(t, repo)
		rows, _ := repo.PlayerFixtures(ctx, 2)
		byPlayer := map[string]PlayerFixture{}
		for _, r := range rows {
			byPlayer[r.PlayerID] = r
		}
		want := map[string]struct{ bonus, points int }{
			// Ashford 1-1 Brookvale (over, bonus not added): tie for first.
			"1006": {3, 11}, "1010": {3, 11}, "1005": {1, 4}, "1007": {0, 2},
			// Cliffton 0-2 Northgate (in play).
			"1003": {3, 11}, "1002": {2, 8}, "1001": {1, 7}, "1011": {0, 2},
		}
		for code, w := range want {
			r := byPlayer[ids[code]]
			if r.Stats.Bonus != w.bonus || r.Points != w.points || !r.BonusProvisional {
				t.Errorf("player %s: bonus %d points %d provisional %v, want %d %d true",
					code, r.Stats.Bonus, r.Points, r.BonusProvisional, w.bonus, w.points)
			}
		}
		if _, ok := byPlayer[ids["1004"]]; ok {
			t.Errorf("a player who has not played has a row")
		}
	})
}

func TestLiveIsIdempotentAndReportsChanges(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, src, st := ingestAll(t, repo)
		live, err := src.EventLive(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := in.Live(ctx, live, st)
		if err != nil || len(changed) != 0 {
			t.Fatalf("repeat poll changed %v (err %v)", changed, err)
		}
		rows, _ := repo.PlayerFixtures(ctx, 2)
		for _, r := range rows {
			if r.Rev != 1 {
				t.Fatalf("rev moved on an identical poll: %+v", r)
			}
		}

		// Northgate's midfielder scores again.
		for i := range live.Players {
			if live.Players[i].PlayerExternalID == "1003" {
				live.Players[i].GoalsScored = 2
			}
		}
		live.Payload = append([]byte(nil), live.Payload...)
		live.Payload = append(live.Payload, ' ')
		changed, err = in.Live(ctx, live, st)
		ids := playerIDs(t, repo)
		if err != nil || len(changed) != 1 || changed[0] != ids["1003"] {
			t.Fatalf("changed = %v, %v", changed, err)
		}
		rows, _ = repo.PlayerFixtures(ctx, 2)
		for _, r := range rows {
			if r.PlayerID == ids["1003"] && (r.Rev != 2 || r.Points != 16) {
				t.Errorf("updated row = %+v", r)
			}
		}

		// Confirmed bonus replaces the projection.
		st2 := *st
		st2.BonusAdded = true
		if _, err := in.Live(ctx, live, &st2); err != nil {
			t.Fatal(err)
		}
		rows, _ = repo.PlayerFixtures(ctx, 2)
		for _, r := range rows {
			if r.BonusProvisional || r.Stats.Bonus != 0 {
				t.Errorf("after bonus added (upstream says 0): %+v", r)
			}
		}
	})
}

func TestLiveBeforeBootstrapIsUnmapped(t *testing.T) {
	in := NewIngester(NewMemory(), "fpl", func() time.Time { return now })
	live, err := source().EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Live(ctx, live, nil); err == nil {
		t.Fatal("live data for unknown players was accepted")
	}
}

func TestSnapshotsAreDeduplicated(t *testing.T) {
	repo := NewMemory()
	in, src, st := ingestAll(t, repo)
	n := repo.SnapshotCount
	live, _ := src.EventLive(ctx, 2)
	if _, err := in.Live(ctx, live, st); err != nil {
		t.Fatal(err)
	}
	if repo.SnapshotCount != n {
		t.Errorf("identical payload stored again: %d -> %d", n, repo.SnapshotCount)
	}
}

func TestFreshness(t *testing.T) {
	good := now.Add(-time.Minute)
	cases := []struct {
		name      string
		ps        PollState
		wantAsOf  time.Time
		wantStale bool
	}{
		{"never polled", PollState{}, now, false},
		{"live, recent", PollState{Mode: ModeLive, LastGoodAt: good}, good, false},
		{"live, 3 minutes", PollState{Mode: ModeLive, LastGoodAt: now.Add(-3*time.Minute - time.Second)}, now.Add(-3*time.Minute - time.Second), true},
		{"idle on schedule", PollState{Mode: ModeIdle, LastGoodAt: now.Add(-5 * time.Hour), NextPollAt: now.Add(time.Hour)}, now, false},
		{"idle, poller missed its slot", PollState{Mode: ModeIdle, LastGoodAt: now.Add(-25 * time.Hour), NextPollAt: now.Add(-time.Hour)}, now.Add(-25 * time.Hour), true},
		{"idle, failing", PollState{Mode: ModeIdle, LastGoodAt: now.Add(-time.Hour), NextPollAt: now.Add(time.Minute), ConsecutiveFailures: 2}, now.Add(-time.Hour), true},
	}
	for _, c := range cases {
		asOf, stale := Freshness(c.ps, now)
		if !asOf.Equal(c.wantAsOf) || stale != c.wantStale {
			t.Errorf("%s: asOf %v stale %v, want %v %v", c.name, asOf, stale, c.wantAsOf, c.wantStale)
		}
	}
}

func TestPollStateRoundTrip(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		if ps, err := repo.PollState(ctx); err != nil || ps.Mode != "" {
			t.Fatalf("empty = %+v %v", ps, err)
		}
		want := PollState{Mode: ModeLive, Gameweek: 7, LastAttemptAt: now, LastGoodAt: now.Add(-time.Minute),
			NextPollAt: now.Add(30 * time.Second), LastError: "boom", ConsecutiveFailures: 1}
		if err := repo.SavePollState(ctx, want); err != nil {
			t.Fatal(err)
		}
		got, err := repo.PollState(ctx)
		if err != nil || got != want {
			t.Errorf("got %+v, want %+v (%v)", got, want, err)
		}
	})
}

// The live case: gameweek 7 between match days, the poller idle until
// 16 hours on. Last good poll Sat 17 Oct 2026 19:09 UTC.
func TestStaleAtFollowsThePollerSchedule(t *testing.T) {
	good := time.Date(2026, 10, 17, 19, 9, 0, 0, time.UTC)
	idle := PollState{Mode: ModeIdle, Gameweek: 7, LastGoodAt: good, NextPollAt: good.Add(16 * time.Hour)}
	due := time.Date(2026, 10, 18, 11, 12, 0, 0, time.UTC) // 11:09 poll + 3 min grace
	if got := StaleAt(idle); !got.Equal(due) {
		t.Fatalf("idle StaleAt = %v, want %v", got, due)
	}
	// Four hours later the data is not delayed: the poller is on schedule.
	for _, at := range []time.Time{good.Add(4 * time.Hour), due.Add(-time.Second)} {
		if _, stale := Freshness(idle, at); stale {
			t.Errorf("idle stale at %v", at)
		}
	}
	// The poll is overdue past its grace: now it is.
	if asOf, stale := Freshness(idle, due.Add(time.Second)); !stale || !asOf.Equal(good) {
		t.Errorf("overdue: asOf %v stale %v", asOf, stale)
	}

	live := PollState{Mode: ModeLive, Gameweek: 7, LastGoodAt: good, NextPollAt: good.Add(30 * time.Second)}
	if got := StaleAt(live); !got.Equal(good.Add(3 * time.Minute)) {
		t.Errorf("live StaleAt = %v", got)
	}
	failing := idle
	failing.ConsecutiveFailures = 2
	if got := StaleAt(failing); !got.Equal(good.Add(3 * time.Minute)) {
		t.Errorf("failing StaleAt = %v", got)
	}
	if got := StaleAt(PollState{}); !got.IsZero() {
		t.Errorf("never polled StaleAt = %v", got)
	}
}
