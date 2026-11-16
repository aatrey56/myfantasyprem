package mirror_test

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/poller/pollertest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// toggleFS serves the capture without gameweek 6's live stats until
// shown is set: upstream finished it while the server was down.
type toggleFS struct{ shown *atomic.Bool }

func (f toggleFS) cur() fs.FS {
	if f.shown.Load() {
		return os.DirFS(realDir)
	}
	return withoutLive(6)
}

func (f toggleFS) Open(name string) (fs.File, error) { return f.cur().Open(name) }

func (f toggleFS) ReadFile(name string) ([]byte, error) { return fs.ReadFile(f.cur(), name) }

// backfiller is pollerwire.Backfiller's job, on the test's source: a
// poller of its own with the real scorer. It counts Backfill calls; fail
// makes them fail.
type backfiller struct {
	p     *poller.Poller
	calls []int
	fail  error
}

func (b *backfiller) MissingFinished(ctx context.Context) ([]int, error) {
	return b.p.MissingFinished(ctx)
}

func (b *backfiller) Backfill(ctx context.Context, gw int) error {
	b.calls = append(b.calls, gw)
	if b.fail != nil {
		return b.fail
	}
	_, _, err := b.p.Backfill(ctx, gw, false)
	return err
}

func newPoller(e *env) *poller.Poller {
	sc := &league.Scorer{Leagues: e.leagues, Stats: e.stats, Rules: scoring.StandardLineup(), Log: quiet()}
	p := poller.New(e.src, stats.NewIngester(e.stats, "fpl", e.now), e.stats, sc, poller.Config{}, quiet())
	p.Now = e.now
	p.Sleep = func(context.Context, time.Duration) error { return nil }
	return p
}

func withBackfiller(e *env) *backfiller {
	b := &backfiller{p: newPoller(e)}
	e.svc.Backfill = b
	return b
}

// gw6Settled checks gameweek 6 is ingested, its matchups final, and the
// re-sync's report and a shadow check agree with FPL on it.
func gw6Settled(t *testing.T, e *env, leagueID string) {
	t.Helper()
	if rows, _ := e.stats.PlayerFixtures(ctx, 6); len(rows) == 0 {
		t.Fatal("gameweek 6 not ingested")
	}
	st := e.state(t, leagueID)
	n := 0
	for _, m := range st.Game.Matchups {
		if m.Gameweek == 6 {
			n++
			if m.State != scoring.StateFinal {
				t.Errorf("gw6 matchup %s is %s, want final", m.ID, m.State)
			}
		}
	}
	if n == 0 {
		t.Fatal("no gw6 matchups")
	}
	rep, err := e.svc.Report(ctx, leagueID)
	if err != nil || rep == nil {
		t.Fatalf("report %v %v", rep, err)
	}
	checked := 0
	for _, it := range rep.Items {
		if it.Gameweek != 6 || (it.Kind != mirror.KindPoints && it.Kind != mirror.KindResult) {
			continue
		}
		checked++
		if it.Status != mirror.ItemOK {
			t.Errorf("report item %s: %s (ours %s, FPL %s) %s", it.Key, it.Status, it.Ours, it.Theirs, it.Note)
		}
	}
	if checked == 0 {
		t.Error("no gw6 points or results verified")
	}
	sh, err := e.svc.ShadowCheck(ctx, leagueID, mirror.ReasonManual)
	if err != nil {
		t.Fatal(err)
	}
	if ms := sh.Mismatches(); len(ms) != 0 {
		t.Errorf("shadow check mismatches %+v", ms)
	}
}

// Gameweek 6 finished upstream while the server was down: the league,
// imported then, has no stats for it. The commissioner's re-sync fills it
// in (ingest, rescore), so its matchups are final with FPL's scores, and
// says so in its notice.
func TestResyncBackfillsFinishedGameweek(t *testing.T) {
	shown := &atomic.Bool{}
	realEnvsFS(t, toggleFS{shown}, func(t *testing.T, e *env) {
		shown.Store(false)
		_, st, _ := importReal(t, e)
		if rows, _ := e.stats.PlayerFixtures(ctx, 6); len(rows) != 0 {
			t.Fatal("gameweek 6 ingested before upstream had it")
		}
		shown.Store(true)
		b := withBackfiller(e)
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, st.League.ID); err != nil {
			t.Fatal(err)
		}
		if len(b.calls) != 1 || b.calls[0] != 6 {
			t.Errorf("backfill calls %v, want [6]", b.calls)
		}
		rec, _ := e.svc.Store.Sync(ctx, st.League.ID)
		if rec == nil || rec.LastError != "" || !strings.HasSuffix(rec.Note, " Filled in GW 6.") {
			t.Fatalf("sync record %+v", rec)
		}
		gw6Settled(t, e, st.League.ID)
	})
}

// Nothing missing: a re-sync backfills nothing and says nothing about it.
func TestResyncWithNothingMissingBackfillsNothing(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		_, st, _ := importReal(t, e)
		b := withBackfiller(e)
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, st.League.ID); err != nil {
			t.Fatal(err)
		}
		if len(b.calls) != 0 {
			t.Errorf("backfill calls %v, want none", b.calls)
		}
		rec, _ := e.svc.Store.Sync(ctx, st.League.ID)
		if rec == nil || rec.LastError != "" || strings.Contains(rec.Note, "GW") {
			t.Errorf("sync record %+v", rec)
		}
	})
}

// A failed backfill does not fail the re-sync: it is noted, and the
// poller's catch-up fills the gameweek in later.
func TestResyncBackfillFailureIsNotedAndCaughtUpLater(t *testing.T) {
	shown := &atomic.Bool{}
	realEnvsFS(t, toggleFS{shown}, func(t *testing.T, e *env) {
		shown.Store(false)
		_, st, _ := importReal(t, e)
		shown.Store(true)
		b := withBackfiller(e)
		b.fail = errors.New("upstream down")
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, st.League.ID); err != nil {
			t.Fatal(err)
		}
		if len(b.calls) != 1 || b.calls[0] != 6 {
			t.Errorf("backfill calls %v, want [6]", b.calls)
		}
		rec, _ := e.svc.Store.Sync(ctx, st.League.ID)
		if rec == nil || rec.LastError != "" || rec.LastSyncedAt == nil || !rec.LastSyncedAt.Equal(e.now()) ||
			!strings.Contains(rec.Note, "Stats for GW 6 aren't available yet") {
			t.Fatalf("sync record %+v", rec)
		}
		if rows, _ := e.stats.PlayerFixtures(ctx, 6); len(rows) != 0 {
			t.Fatal("gameweek 6 ingested by a failed backfill")
		}
		// The running poller's catch-up retries it and rescores the league.
		pollertest.TickUntilIdle(t, ctx, newPoller(e))
		*e.clock = e.clock.Add(20 * time.Minute)
		b.fail = nil
		if err := e.svc.Resync(ctx, st.League.ID); err != nil {
			t.Fatal(err)
		}
		if len(b.calls) != 1 {
			t.Errorf("caught-up gameweek backfilled again: %v", b.calls)
		}
		gw6Settled(t, e, st.League.ID)
	})
}
