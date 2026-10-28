package poller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

var ctx = context.Background()

// flaky wraps the recorded FPL fixtures and can fail on demand.
type flaky struct {
	provider.StatsSource
	mu     sync.Mutex
	fail   error
	mutate func(*provider.EventLive)
	// mutateBoot edits a copy of the bootstrap; mutateStatus the status.
	mutateBoot   func(*provider.Bootstrap)
	mutateStatus func(*provider.GameweekStatus)
	// liveErr fails EventLive for one gameweek.
	liveErr map[int]error
	calls   map[string]int
}

func (f *flaky) err(op string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[op]++
	return f.fail
}

func (f *flaky) Bootstrap(ctx context.Context) (*provider.Bootstrap, error) {
	if err := f.err("bootstrap"); err != nil {
		return nil, err
	}
	b, err := f.StatsSource.Bootstrap(ctx)
	if err != nil || f.mutateBoot == nil {
		return b, err
	}
	cp := *b
	cp.Players = slices.Clone(b.Players)
	f.mutateBoot(&cp)
	return &cp, nil
}

func (f *flaky) Fixtures(ctx context.Context) ([]provider.Fixture, error) {
	if err := f.err("fixtures"); err != nil {
		return nil, err
	}
	return f.StatsSource.Fixtures(ctx)
}

func (f *flaky) GameweekStatus(ctx context.Context) (*provider.GameweekStatus, error) {
	if err := f.err("status"); err != nil {
		return nil, err
	}
	st, err := f.StatsSource.GameweekStatus(ctx)
	if err == nil && f.mutateStatus != nil {
		f.mutateStatus(st)
	}
	return st, err
}

func (f *flaky) EventLive(ctx context.Context, gw int) (*provider.EventLive, error) {
	if err := f.err("live"); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.calls[fmt.Sprintf("live/%d", gw)]++
	lerr := f.liveErr[gw]
	f.mu.Unlock()
	if lerr != nil {
		return nil, lerr
	}
	live, err := f.StatsSource.EventLive(ctx, gw)
	if err == nil && f.mutate != nil {
		f.mutate(live)
	}
	return live, err
}

// recorder is a Recomputer that remembers its calls. failures > 0 makes
// that many calls fail.
type recorder struct {
	calls    []string
	failures int
	// during runs inside the next call, as a concurrent writer would.
	during func()
}

func (r *recorder) Recompute(_ context.Context, gw int, changed []string, all bool) (league.RecomputeResult, error) {
	r.calls = append(r.calls, fmt.Sprintf("gw%d changed=%d all=%v", gw, len(changed), all))
	if d := r.during; d != nil {
		r.during = nil
		d()
	}
	if r.failures > 0 {
		r.failures--
		return league.RecomputeResult{}, errors.New("recompute: database unavailable")
	}
	return league.RecomputeResult{Leagues: 1}, nil
}

// callsFor lists the recompute calls of one gameweek.
func (r *recorder) callsFor(gw int) []string {
	var out []string
	for _, c := range r.calls {
		if strings.HasPrefix(c, fmt.Sprintf("gw%d ", gw)) {
			out = append(out, c)
		}
	}
	return out
}

type fixture struct {
	p      *Poller
	src    *flaky
	repo   stats.Repo
	rec    *recorder
	clock  *time.Time
	sleeps *[]time.Duration
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	return newFixtureOn(t, stats.NewMemory())
}

// stores runs a test over each stats store: Memory always, Postgres when
// TEST_DATABASE_URL is set.
func stores(t *testing.T, test func(t *testing.T, f fixture)) {
	t.Run("memory", func(t *testing.T) { test(t, newFixtureOn(t, stats.NewMemory())) })
	t.Run("postgres", func(t *testing.T) { test(t, newFixtureOn(t, stats.NewPostgres(storetest.New(t)))) })
}

func newFixtureOn(t *testing.T, repo stats.Repo) fixture {
	t.Helper()
	// Saturday 22 August 2026, 15:30: Cliffton v Northgate is in play.
	return newFixtureAt(t, repo, "../provider/fpl/testdata", time.Date(2026, 8, 22, 15, 30, 0, 0, time.UTC))
}

// newFixtureAt is a fixture over the recorded responses in dir, at clock.
func newFixtureAt(t *testing.T, repo stats.Repo, dir string, clock time.Time) fixture {
	t.Helper()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	src := &flaky{
		StatsSource: fpl.New(fpl.Options{
			Transport: fpl.DirTransport{FS: os.DirFS(dir)}, Logger: quiet, MinInterval: -1,
		}),
		calls:   map[string]int{},
		liveErr: map[int]error{},
	}
	rec := &recorder{}
	f := fixture{src: src, repo: repo, rec: rec, clock: &clock, sleeps: &[]time.Duration{}}
	f.p = f.newPoller(Config{})
	return f
}

// newPoller is a poller over the fixture's source and store, as after a
// process restart.
func (f fixture) newPoller(cfg Config) *Poller {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := func() time.Time { return *f.clock }
	p := New(f.src, stats.NewIngester(f.repo, "fpl", now), f.repo, f.rec, cfg, quiet)
	p.Now = now
	p.Sleep = func(_ context.Context, d time.Duration) error {
		*f.sleeps = append(*f.sleeps, d)
		return nil
	}
	return p
}

func (f fixture) advance(d time.Duration) { *f.clock = f.clock.Add(d) }

func (f fixture) state(t *testing.T) stats.PollState {
	t.Helper()
	ps, err := f.repo.PollState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestGoodPollIngestsScoresAndPollsFastWhileLive(t *testing.T) {
	f := newFixture(t)
	wait := f.p.Tick(ctx)
	if wait != 30*time.Second {
		t.Errorf("wait = %v, want 30s while a match is live", wait)
	}
	ps := f.state(t)
	if ps.Mode != stats.ModeLive || ps.Gameweek != 2 || !ps.LastGoodAt.Equal(*f.clock) || ps.ConsecutiveFailures != 0 ||
		!ps.NextPollAt.Equal(f.clock.Add(30*time.Second)) {
		t.Errorf("poll state = %+v", ps)
	}
	rows, _ := f.repo.PlayerFixtures(ctx, 2)
	if len(rows) == 0 {
		t.Fatal("no live rows ingested")
	}
	if gw2 := f.rec.callsFor(2); len(gw2) != 1 || gw2[0] != fmt.Sprintf("gw2 changed=%d all=true", len(uniquePlayers(rows))) {
		t.Errorf("recompute calls = %v", f.rec.calls)
	}

	// The same upstream data again: nothing changes, nothing is rescored
	// beyond the cheap check, and bootstrap is not refetched.
	n := len(f.rec.calls)
	f.advance(30 * time.Second)
	f.p.Tick(ctx)
	if got := f.rec.calls[n:]; len(got) != 1 || got[0] != "gw2 changed=0 all=false" {
		t.Errorf("idempotent poll recompute = %v", got)
	}
	if f.src.calls["bootstrap"] != 1 {
		t.Errorf("bootstrap fetched %d times", f.src.calls["bootstrap"])
	}
	asOf, stale := stats.Freshness(f.state(t), *f.clock)
	if stale || !asOf.Equal(*f.clock) {
		t.Errorf("freshness = %v %v", asOf, stale)
	}
}

func uniquePlayers(rows []stats.PlayerFixture) map[string]bool {
	out := map[string]bool{}
	for _, r := range rows {
		out[r.PlayerID] = true
	}
	return out
}

func TestBadPollsKeepLastGoodSnapshotGoStaleAndRecover(t *testing.T) {
	f := newFixture(t)
	f.p.Tick(ctx)
	good := f.state(t).LastGoodAt
	before, _ := f.repo.PlayerFixtures(ctx, 2)

	// Upstream breaks: transport errors, then a malformed body.
	f.src.fail = fmt.Errorf("fpl: GET /game: %w: status 503", provider.ErrUpstream)
	var waits []time.Duration
	for i := 0; i < 3; i++ {
		f.advance(50 * time.Second)
		waits = append(waits, f.p.Tick(ctx))
	}
	f.src.fail = fmt.Errorf("fpl: event live: %w: %w: bad", provider.ErrUpstream, provider.ErrInvalid)
	f.advance(50 * time.Second)
	waits = append(waits, f.p.Tick(ctx))

	ps := f.state(t)
	if ps.ConsecutiveFailures != 4 || !ps.LastGoodAt.Equal(good) || ps.Mode != stats.ModeLive || ps.LastError == "" {
		t.Errorf("after failures: %+v", ps)
	}
	if !(waits[0] < waits[1] && waits[1] < waits[2]) {
		t.Errorf("no backoff: %v", waits)
	}
	after, _ := f.repo.PlayerFixtures(ctx, 2)
	if len(after) != len(before) {
		t.Errorf("a failed poll changed the data: %d -> %d rows", len(before), len(after))
	}
	if f.p.Metrics.PollFailures.Value() != 4 || f.p.Metrics.Invalid.Value() != 1 {
		t.Errorf("metrics: failures %d invalid %d", f.p.Metrics.PollFailures.Value(), f.p.Metrics.Invalid.Value())
	}

	// 3 minutes without a good poll while live: stale, served at the last
	// good snapshot's time.
	asOf, stale := stats.Freshness(ps, *f.clock)
	if !stale || !asOf.Equal(good) || f.p.Metrics.Stale.Value() != 1 {
		t.Errorf("freshness after 3 min = %v %v (metric %d)", asOf, stale, f.p.Metrics.Stale.Value())
	}

	// Recovery.
	f.src.fail = nil
	f.advance(time.Minute)
	if wait := f.p.Tick(ctx); wait != 30*time.Second {
		t.Errorf("wait after recovery = %v", wait)
	}
	ps = f.state(t)
	if ps.ConsecutiveFailures != 0 || ps.LastError != "" || !ps.LastGoodAt.Equal(*f.clock) {
		t.Errorf("after recovery: %+v", ps)
	}
	if _, stale := stats.Freshness(ps, *f.clock); stale || f.p.Metrics.Stale.Value() != 0 {
		t.Error("still stale after a good poll")
	}
}

func TestChangedStatsAreRescored(t *testing.T) {
	f := newFixture(t)
	f.p.Tick(ctx)
	f.src.mutate = func(l *provider.EventLive) {
		for i := range l.Players {
			if l.Players[i].PlayerExternalID == "1003" {
				l.Players[i].GoalsScored = 2
			}
		}
	}
	f.advance(30 * time.Second)
	f.p.Tick(ctx)
	if last := f.rec.calls[len(f.rec.calls)-1]; last != "gw2 changed=1 all=false" {
		t.Errorf("recompute = %s", last)
	}
	if f.p.Metrics.RowsChanged.Value() == 0 {
		t.Error("changed rows not counted")
	}
}

func TestDecide(t *testing.T) {
	now := time.Date(2026, 8, 22, 15, 30, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	fx := func(status string, kickoff *time.Time) stats.Fixture {
		return stats.Fixture{Status: status, KickoffAt: kickoff}
	}
	cases := []struct {
		name     string
		fixtures []stats.Fixture
		gw       stats.Gameweek
		next     *time.Time
		mode     string
		wait     time.Duration
	}{
		{"in play", []stats.Fixture{fx(stats.FixtureFinished, at(-3*time.Hour)), fx(stats.FixtureLive, at(-time.Hour))}, stats.Gameweek{}, nil, stats.ModeLive, 30 * time.Second},
		{"past kickoff, not flagged yet", []stats.Fixture{fx(stats.FixtureScheduled, at(-time.Minute))}, stats.Gameweek{}, nil, stats.ModeLive, 30 * time.Second},
		{"all over, bonus pending", []stats.Fixture{fx(stats.FixtureFinishedProvisional, at(-3*time.Hour))}, stats.Gameweek{}, nil, stats.ModeBonus, 10 * time.Minute},
		{"bonus added, not checked", []stats.Fixture{fx(stats.FixtureFinished, at(-3*time.Hour))}, stats.Gameweek{BonusAdded: true}, nil, stats.ModeUnchecked, 6 * time.Hour},
		{"checked, next kickoff in 2h", []stats.Fixture{fx(stats.FixtureFinished, at(-3*time.Hour))}, stats.Gameweek{BonusAdded: true, DataChecked: true}, at(2 * time.Hour), stats.ModeIdle, 2 * time.Hour},
		{"before the gameweek, kickoff in 3 days", []stats.Fixture{fx(stats.FixtureScheduled, at(72*time.Hour))}, stats.Gameweek{}, at(72 * time.Hour), stats.ModeIdle, 24 * time.Hour},
		// Slow modes never sleep through the next kickoff.
		{"bonus pending, next kickoff in 4 min", []stats.Fixture{fx(stats.FixtureFinishedProvisional, at(-3*time.Hour))}, stats.Gameweek{}, at(4 * time.Minute), stats.ModeBonus, 4 * time.Minute},
		{"not checked, next kickoff in 2h", []stats.Fixture{fx(stats.FixtureFinished, at(-3*time.Hour))}, stats.Gameweek{BonusAdded: true}, at(2 * time.Hour), stats.ModeUnchecked, 2 * time.Hour},
		{"between two matches of a matchday", []stats.Fixture{fx(stats.FixtureFinishedProvisional, at(-2*time.Hour)), fx(stats.FixtureScheduled, at(time.Hour))}, stats.Gameweek{}, at(time.Hour), stats.ModeIdle, time.Hour},
	}
	for _, c := range cases {
		mode, wait := Decide(Config{}, now, c.fixtures, c.gw, c.next)
		if mode != c.mode || wait != c.wait {
			t.Errorf("%s: %s %v, want %s %v", c.name, mode, wait, c.mode, c.wait)
		}
	}
}

// From 2026-27 FPL scores lock at 09:00 UK time on the day after the
// gameweek's last match (fantasyfootballscout.co.uk, 2026-07-20), when
// FPL marks the data checked. Once bonus is added the poller wakes at the
// lock instead of up to 6 hours later, polls every 10 minutes for two
// hours after it, then falls back to every 6 hours.
func TestDecideWaitsForTheScoreLock(t *testing.T) {
	// The last match kicks off at 20:00 BST on Saturday 22 August; the
	// lock is Sunday 23 August 09:00 BST (08:00 UTC).
	kickoff := time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC)
	fixtures := []stats.Fixture{
		{Status: stats.FixtureFinished, KickoffAt: ptr(kickoff.Add(-5 * time.Hour))},
		{Status: stats.FixtureFinished, KickoffAt: &kickoff},
	}
	gw := stats.Gameweek{BonusAdded: true}
	cases := []struct {
		now  time.Time
		wait time.Duration
	}{
		{time.Date(2026, 8, 23, 4, 0, 0, 0, time.UTC), 4 * time.Hour},
		{time.Date(2026, 8, 22, 23, 0, 0, 0, time.UTC), 6 * time.Hour},
		{time.Date(2026, 8, 23, 8, 30, 0, 0, time.UTC), 10 * time.Minute},
		{time.Date(2026, 8, 23, 11, 0, 0, 0, time.UTC), 6 * time.Hour},
	}
	for _, c := range cases {
		mode, wait := Decide(Config{}, c.now, fixtures, gw, nil)
		if mode != stats.ModeUnchecked || wait != c.wait {
			t.Errorf("at %v: %s %v, want %s %v", c.now, mode, wait, stats.ModeUnchecked, c.wait)
		}
	}
	// In winter the lock is 09:00 GMT.
	winter := time.Date(2026, 12, 26, 20, 0, 0, 0, time.UTC)
	_, wait := Decide(Config{}, time.Date(2026, 12, 27, 5, 0, 0, 0, time.UTC),
		[]stats.Fixture{{Status: stats.FixtureFinished, KickoffAt: &winter}}, gw, nil)
	if wait != 4*time.Hour {
		t.Errorf("winter lock: wait %v, want 4h", wait)
	}
}

func ptr[T any](v T) *T { return &v }

func TestRunStopsWithContext(t *testing.T) {
	f := newFixture(t)
	f.src.fail = errors.New("down")
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- f.p.Run(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
}

func setGoals(ext string, goals int) func(*provider.EventLive) {
	return func(l *provider.EventLive) {
		for i := range l.Players {
			if l.Players[i].PlayerExternalID == ext {
				l.Players[i].GoalsScored = goals
			}
		}
	}
}

func rowsOf(t *testing.T, f fixture, gw int) []stats.PlayerFixture {
	t.Helper()
	rows, err := f.repo.PlayerFixtures(ctx, gw)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// ADR 0005: bad data never zeroes a score. An empty or truncated live
// snapshot after kickoff is a failed poll and the last good rows stay.
func TestIncompleteLiveSnapshotIsAFailedPoll(t *testing.T) {
	for name, mutate := range map[string]func(*provider.EventLive){
		"empty":   func(l *provider.EventLive) { l.Players, l.Fixtures = nil, nil },
		"partial": func(l *provider.EventLive) { l.Players = l.Players[:3] },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.p.Tick(ctx)
			before := rowsOf(t, f, 2)
			calls := len(f.rec.calls)

			f.src.mutate = mutate
			f.advance(30 * time.Second)
			f.p.Tick(ctx)
			ps := f.state(t)
			if ps.ConsecutiveFailures != 1 || ps.LastError == "" || f.p.Metrics.Invalid.Value() != 1 {
				t.Errorf("poll state = %+v, invalid = %d", ps, f.p.Metrics.Invalid.Value())
			}
			if after := rowsOf(t, f, 2); len(after) != len(before) {
				t.Errorf("rows %d -> %d: the snapshot replaced the last good one", len(before), len(after))
			}
			if len(f.rec.calls) != calls {
				t.Errorf("rescored on a rejected snapshot: %v", f.rec.calls[calls:])
			}
		})
	}
}

// If the ingest committed but rescoring failed, the next poll (with the
// same upstream data, so nothing "changed") must still rescore.
func TestFailedRescoreIsRetried(t *testing.T) {
	f := newFixture(t)
	f.p.Tick(ctx)
	f.src.mutate = setGoals("1003", 2)
	f.rec.failures = 1
	f.advance(30 * time.Second)
	f.p.Tick(ctx)
	if ps := f.state(t); ps.ConsecutiveFailures != 1 {
		t.Fatalf("a failed rescore was a good poll: %+v", ps)
	}

	n := len(f.rec.calls)
	f.advance(30 * time.Second)
	f.p.Tick(ctx)
	got := f.rec.calls[n:]
	if len(got) == 0 || (got[0] != "gw2 changed=0 all=true" && got[0] != "gw2 changed=1 all=false") {
		t.Errorf("retry recompute = %v, want gw2 rescored", got)
	}
	if ps := f.state(t); ps.ConsecutiveFailures != 0 {
		t.Errorf("after retry: %+v", ps)
	}
	// Once it succeeded, the marker is cleared.
	n = len(f.rec.calls)
	f.advance(30 * time.Second)
	f.p.Tick(ctx)
	if got := f.rec.calls[n:]; len(got) != 1 || got[0] != "gw2 changed=0 all=false" {
		t.Errorf("after a successful retry recompute = %v", got)
	}
}

// A player FPL added after the last bootstrap ingest must not block live
// scoring until the next scheduled bootstrap refresh.
func TestNewPlayerInLiveDataRefreshesBootstrap(t *testing.T) {
	f := newFixture(t)
	f.p.Tick(ctx)
	f.src.mutateBoot = func(b *provider.Bootstrap) {
		np := b.Players[0]
		np.ExternalID, np.SeasonElementID, np.DisplayName = "9999", 9999, "Newcomer"
		b.Players = append(b.Players, np)
	}
	f.src.mutate = func(l *provider.EventLive) {
		l.Players = append(l.Players, provider.PlayerLive{PlayerExternalID: "9999"})
	}
	f.advance(30 * time.Second)
	f.p.Tick(ctx)
	if ps := f.state(t); ps.ConsecutiveFailures != 0 {
		t.Fatalf("poll failed: %+v", ps)
	}
	if f.src.calls["bootstrap"] != 2 {
		t.Errorf("bootstrap fetched %d times, want an immediate refresh", f.src.calls["bootstrap"])
	}
	ext, _ := f.repo.ExternalIDs(ctx, "fpl", "player")
	if ext["9999"] == "" {
		t.Error("new player not mapped")
	}
}

// A 404 for the live endpoint is only normal before the gameweek's first
// kickoff.
func TestLiveNotFound(t *testing.T) {
	notFound := fmt.Errorf("fpl: GET /event/2/live: %w", provider.ErrNotFound)

	f := newFixture(t) // 15:30 on matchday: gameweek 2 is under way
	f.src.liveErr[2] = notFound
	f.p.Tick(ctx)
	if ps := f.state(t); ps.ConsecutiveFailures != 1 || ps.LastError == "" {
		t.Errorf("404 after kickoff: %+v, want a failed poll", ps)
	}

	f = newFixture(t)
	moveToGameweek3(f) // gameweek 3 is current, its first kickoff 2 hours away
	f.src.liveErr[3] = fmt.Errorf("fpl: GET /event/3/live: %w", provider.ErrNotFound)
	f.p.Tick(ctx)
	if ps := f.state(t); ps.ConsecutiveFailures != 0 {
		t.Errorf("404 before kickoff: %+v, want a good poll", ps)
	}
}

// moveToGameweek3 makes upstream report gameweek 3 as current and sets
// the clock before its first kickoff.
func moveToGameweek3(f fixture) {
	*f.clock = time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	f.src.mutateStatus = func(st *provider.GameweekStatus) {
		st.Current, st.Next, st.BonusAdded = 3, nil, false
	}
}

// On rollover the previous gameweek's final payload is fetched, so late
// corrections are stored and rescored.
func TestRolloverFetchesPreviousGameweekFinalPayload(t *testing.T) {
	f := newFixture(t)
	f.p.Tick(ctx)
	f.src.mutate = func(l *provider.EventLive) {
		if l.Gameweek == 2 {
			setGoals("1003", 2)(l)
		}
	}
	moveToGameweek3(f)
	n2 := f.src.calls["live/2"]
	f.p.Tick(ctx)
	if f.src.calls["live/2"] != n2+1 {
		t.Fatalf("gameweek 2 live fetched %d more times, want 1", f.src.calls["live/2"]-n2)
	}
	if gw2 := f.rec.callsFor(2); gw2[len(gw2)-1] != "gw2 changed=1 all=true" {
		t.Errorf("gameweek 2 recompute = %v", gw2)
	}
	if ps := f.state(t); ps.ConsecutiveFailures != 0 || ps.Gameweek != 3 {
		t.Errorf("poll state = %+v", ps)
	}
}

// A restart across the boundary still reconciles the previous gameweek.
func TestRestartAcrossRolloverReconciles(t *testing.T) {
	f := newFixture(t)
	f.p.Tick(ctx)
	f.src.mutate = func(l *provider.EventLive) {
		if l.Gameweek == 2 {
			setGoals("1003", 2)(l)
		}
	}
	moveToGameweek3(f)
	p := f.newPoller(Config{})
	p.Tick(ctx)
	if gw2 := f.rec.callsFor(2); gw2[len(gw2)-1] != "gw2 changed=1 all=true" {
		t.Errorf("gameweek 2 recompute after restart = %v", gw2)
	}
}

// A fresh database mid-season loads the earlier gameweeks: the previous
// one first, then a bounded number per poll, paced politely.
func TestFreshDatabaseBackfillsEarlierGameweeks(t *testing.T) {
	f := newFixture(t)
	moveToGameweek3(f)
	f.p = f.newPoller(Config{BackfillPerTick: 1})
	f.p.Tick(ctx)
	if ps := f.state(t); ps.ConsecutiveFailures != 0 {
		t.Fatalf("poll state = %+v", ps)
	}
	if len(rowsOf(t, f, 2)) == 0 {
		t.Fatal("previous gameweek not loaded")
	}
	if len(rowsOf(t, f, 1)) != 0 {
		t.Fatal("backfill not bounded per poll")
	}
	f.advance(time.Hour)
	f.p.Tick(ctx)
	if len(rowsOf(t, f, 1)) == 0 {
		t.Fatal("gameweek 1 not backfilled")
	}
	if len(f.rec.callsFor(1)) == 0 {
		t.Error("gameweek 1 not scored")
	}
	for _, d := range *f.sleeps {
		if d < 500*time.Millisecond {
			t.Errorf("backfill paced at %v, want at most 2 requests per second", d)
		}
	}
	if len(*f.sleeps) == 0 {
		t.Error("backfill requests not paced")
	}
	// Nothing left: the next poll fetches only the current gameweek.
	n1 := f.src.calls["live/1"]
	f.advance(time.Hour)
	f.p.Tick(ctx)
	if f.src.calls["live/1"] != n1 {
		t.Error("gameweek 1 fetched again after the backfill")
	}
}

// A database from before per-player gameweek lines were kept has rows but
// no lines for earlier gameweeks: a restart backfills them, once.
func TestRestartBackfillsMissingGameweekLines(t *testing.T) {
	stores(t, func(t *testing.T, f fixture) {
		moveToGameweek3(f)
		f.p.Tick(ctx)
		f.advance(time.Hour)
		f.p.Tick(ctx)
		rows := rowsOf(t, f, 1)
		if len(rows) == 0 {
			t.Fatal("gameweek 1 not loaded")
		}
		// Drop gameweek 1's lines, keeping its rows.
		if err := f.repo.Apply(ctx, stats.Batch{Live: &stats.LiveRows{Gameweek: 1, Rows: rows, Players: []stats.PlayerGameweek{}}}); err != nil {
			t.Fatal(err)
		}
		if lines, _ := f.repo.PlayerGameweeks(ctx, 1); len(lines) != 0 {
			t.Fatalf("%d lines left", len(lines))
		}
		n1 := f.src.calls["live/1"]
		p := f.newPoller(Config{})
		p.Tick(ctx)
		if f.src.calls["live/1"] != n1+1 {
			t.Fatalf("gameweek 1 fetched %d times after the restart, want once", f.src.calls["live/1"]-n1)
		}
		if lines, _ := f.repo.PlayerGameweeks(ctx, 1); len(lines) == 0 {
			t.Fatal("gameweek 1 lines not backfilled")
		}
		// Complete now: another restart leaves gameweek 1 alone.
		p = f.newPoller(Config{})
		p.Tick(ctx)
		if f.src.calls["live/1"] != n1+1 {
			t.Error("gameweek 1 fetched again once complete")
		}
	})
}

// Lines kept before starts was read have minutes but no starts: a restart
// re-reads their gameweek, once, and fills it.
func TestRestartBackfillsMissingStarts(t *testing.T) {
	stores(t, func(t *testing.T, f fixture) {
		moveToGameweek3(f)
		f.p.Tick(ctx)
		f.advance(time.Hour)
		f.p.Tick(ctx)
		rows := rowsOf(t, f, 1)
		lines, _ := f.repo.PlayerGameweeks(ctx, 1)
		if len(rows) == 0 || len(lines) == 0 {
			t.Fatal("gameweek 1 not loaded")
		}
		// Store gameweek 1's lines as they were before starts was read.
		for i := range lines {
			lines[i].Starts = nil
		}
		if err := f.repo.Apply(ctx, stats.Batch{Live: &stats.LiveRows{Gameweek: 1, Rows: rows, Players: lines}}); err != nil {
			t.Fatal(err)
		}
		hasStarts := func() bool {
			lines, _ := f.repo.PlayerGameweeks(ctx, 1)
			for _, l := range lines {
				if l.Starts != nil {
					return true
				}
			}
			return false
		}
		if hasStarts() {
			t.Fatal("starts still stored")
		}
		n1 := f.src.calls["live/1"]
		p := f.newPoller(Config{})
		p.Tick(ctx)
		if f.src.calls["live/1"] != n1+1 {
			t.Fatalf("gameweek 1 fetched %d times after the restart, want once", f.src.calls["live/1"]-n1)
		}
		if !hasStarts() {
			t.Fatal("gameweek 1 starts not backfilled")
		}
		p = f.newPoller(Config{})
		p.Tick(ctx)
		if f.src.calls["live/1"] != n1+1 {
			t.Error("gameweek 1 fetched again once it has starts")
		}
	})
}

// A source that never sends starts leaves every line without it. Its
// gameweeks are re-read once at most (the check is stored), not on every
// restart: restart twice, gameweek 1 is fetched once in all.
func TestRestartWithoutStartsReadsOnce(t *testing.T) {
	stores(t, func(t *testing.T, f fixture) {
		f.src.mutate = func(l *provider.EventLive) {
			for i := range l.Players {
				l.Players[i].Starts = nil
			}
		}
		moveToGameweek3(f)
		f.p.Tick(ctx)
		f.advance(time.Hour)
		f.p.Tick(ctx)
		if lines, _ := f.repo.PlayerGameweeks(ctx, 1); len(lines) == 0 {
			t.Fatal("gameweek 1 not loaded")
		}
		n1 := f.src.calls["live/1"]
		for range 2 {
			p := f.newPoller(Config{})
			p.Tick(ctx)
		}
		if got := f.src.calls["live/1"] - n1; got > 1 {
			t.Errorf("gameweek 1 fetched %d times over two restarts, want at most 1", got)
		}
	})
}

// A rescore that failed after its ingest committed survives a restart: the
// next poll sees identical upstream data (nothing "changed"), yet the
// gameweek is still rescored in full.
func TestFailedRescoreSurvivesRestart(t *testing.T) {
	stores(t, func(t *testing.T, f fixture) {
		f.p.Tick(ctx)
		f.src.mutate = setGoals("1003", 2)
		f.rec.failures = 1
		f.advance(30 * time.Second)
		f.p.Tick(ctx)
		if ps := f.state(t); ps.ConsecutiveFailures != 1 {
			t.Fatalf("a failed rescore was a good poll: %+v", ps)
		}

		p := f.newPoller(Config{})
		n := len(f.rec.calls)
		f.advance(30 * time.Second)
		p.Tick(ctx)
		if got := f.rec.calls[n:]; len(got) == 0 || got[0] != "gw2 changed=0 all=true" {
			t.Errorf("recompute after restart = %v, want gw2 rescored in full", got)
		}
		// Once it succeeded, the marker is cleared.
		n = len(f.rec.calls)
		f.advance(30 * time.Second)
		p.Tick(ctx)
		if got := f.rec.calls[n:]; len(got) != 1 || got[0] != "gw2 changed=0 all=false" {
			t.Errorf("after a successful rescore recompute = %v", got)
		}
	})
}

// A gameweek marked again while its rescore runs (another poller ingested
// newer stats) is not cleared by that rescore: the next cycle rescores it
// in full.
func TestMarkSetDuringRescoreSurvives(t *testing.T) {
	stores(t, func(t *testing.T, f fixture) {
		f.p.Tick(ctx)
		f.src.mutate = setGoals("1003", 2)
		f.rec.during = func() {
			if err := f.repo.Apply(ctx, stats.Batch{Rescore: []int{2}}); err != nil {
				t.Error(err)
			}
		}
		f.advance(30 * time.Second)
		f.p.Tick(ctx)
		if ps := f.state(t); ps.ConsecutiveFailures != 0 {
			t.Fatalf("poll failed: %+v", ps)
		}
		if marks, err := f.repo.RescorePending(ctx); err != nil || len(marks) != 1 || marks[0].Gameweek != 2 {
			t.Fatalf("pending after the rescore = %+v %v, want gameweek 2", marks, err)
		}

		n := len(f.rec.calls)
		f.advance(30 * time.Second)
		f.p.Tick(ctx)
		if got := f.rec.calls[n:]; len(got) != 1 || got[0] != "gw2 changed=0 all=true" {
			t.Errorf("next recompute = %v, want gw2 rescored in full", got)
		}
		if marks, err := f.repo.RescorePending(ctx); err != nil || len(marks) != 0 {
			t.Errorf("pending after the full rescore = %+v %v, want none", marks, err)
		}
	})
}

// The same for an earlier gameweek: its backfill commits rows, rescoring
// fails, the process restarts. The rows are stored now, so it is not
// backfilled again, but it must still be scored.
func TestFailedBackfillRescoreSurvivesRestart(t *testing.T) {
	stores(t, func(t *testing.T, f fixture) {
		moveToGameweek3(f)
		f.p = f.newPoller(Config{BackfillPerTick: 1})
		f.p.Tick(ctx) // gameweek 2 settled
		if len(rowsOf(t, f, 1)) != 0 {
			t.Fatal("backfill not bounded per poll")
		}
		f.rec.failures = 1
		f.advance(time.Hour)
		f.p.Tick(ctx) // gameweek 1 backfilled; its rescore fails
		if len(rowsOf(t, f, 1)) == 0 {
			t.Fatal("gameweek 1 not backfilled")
		}
		if gw1 := f.rec.callsFor(1); len(gw1) != 1 {
			t.Fatalf("gameweek 1 recompute = %v, want one failed call", gw1)
		}

		p := f.newPoller(Config{BackfillPerTick: 1})
		f.advance(time.Hour)
		p.Tick(ctx)
		if gw1 := f.rec.callsFor(1); len(gw1) != 2 || gw1[1] != "gw1 changed=0 all=true" {
			t.Errorf("gameweek 1 recompute after restart = %v, want a full rescore", gw1)
		}
		// Cleared once it succeeded.
		f.advance(time.Hour)
		p.Tick(ctx)
		if gw1 := f.rec.callsFor(1); len(gw1) != 2 {
			t.Errorf("gameweek 1 rescored again: %v", gw1)
		}
	})
}

// lockSource is a one-match season whose bonus is added and whose data
// FPL marks checked at checkedAt. Like the FPL client, GameweekStatus
// reports DataChecked from the last bootstrap it fetched (event-status has
// no such flag).
type lockSource struct {
	now        func() time.Time
	checkedAt  time.Time
	kickoff    time.Time
	bootstraps int
	bootSaw    bool
}

func (s *lockSource) Bootstrap(context.Context) (*provider.Bootstrap, error) {
	s.bootstraps++
	s.bootSaw = !s.now().Before(s.checkedAt)
	return &provider.Bootstrap{
		Clubs:   []provider.Club{{ExternalID: "1", ShortName: "AAA", Name: "A"}, {ExternalID: "2", ShortName: "BBB", Name: "B"}},
		Players: []provider.Player{{ExternalID: "p1", DisplayName: "P", ClubExternalID: "1", Position: provider.MID}},
		Gameweeks: []provider.Gameweek{{
			Number: 1, Name: "Gameweek 1", DeadlineAt: s.kickoff.Add(-24 * time.Hour),
			WaiverDeadlineAt: s.kickoff.Add(-48 * time.Hour), TradeDeadlineAt: s.kickoff.Add(-72 * time.Hour),
			DataChecked: s.bootSaw,
		}},
	}, nil
}

func (s *lockSource) Fixtures(context.Context) ([]provider.Fixture, error) {
	k := s.kickoff
	return []provider.Fixture{{ExternalID: "f1", Gameweek: 1, HomeClubExternalID: "1", AwayClubExternalID: "2",
		KickoffAt: &k, Status: provider.FixtureFinished}}, nil
}

func (s *lockSource) GameweekStatus(context.Context) (*provider.GameweekStatus, error) {
	return &provider.GameweekStatus{Current: 1, BonusAdded: true, DataChecked: s.bootSaw}, nil
}

func (s *lockSource) EventLive(context.Context, int) (*provider.EventLive, error) {
	// One row: a gameweek under way that lists no players is rejected.
	return &provider.EventLive{Gameweek: 1, Players: []provider.PlayerLive{{
		PlayerExternalID: "p1", Minutes: 90, Explain: []provider.FixtureExplain{{FixtureExternalID: "f1"}},
	}}}, nil
}

// FPL marks a gameweek's data checked at the 09:00 UK score lock, and only
// bootstrap carries that flag. The poller watches the lock every 10
// minutes, so each of those polls must refresh bootstrap: otherwise
// official turns final only at the next 6-hourly refresh (Codex review).
func TestLockWatchRefreshesBootstrapSoFinalIsPrompt(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	// The last match kicks off at 20:00 BST on Saturday 22 August; the
	// lock is 09:00 BST (08:00 UTC) on Sunday 23 August.
	clock := time.Date(2026, 8, 22, 23, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	lock := time.Date(2026, 8, 23, 8, 0, 0, 0, time.UTC)
	src := &lockSource{now: now, checkedAt: lock, kickoff: time.Date(2026, 8, 22, 19, 0, 0, 0, time.UTC)}
	repo := stats.NewMemory()
	p := New(src, stats.NewIngester(repo, "fpl", now), repo, &recorder{}, Config{}, quiet)
	p.Now = now

	checked := func() bool {
		gws, err := repo.Gameweeks(ctx)
		if err != nil {
			t.Fatal(err)
		}
		return len(gws) == 1 && gws[0].DataChecked
	}
	var seenAt time.Time
	for range 20 {
		at := clock
		wait := p.Tick(ctx)
		if checked() {
			seenAt = at
			break
		}
		clock = clock.Add(wait)
	}
	if seenAt.IsZero() {
		t.Fatalf("data never marked checked (bootstraps %d)", src.bootstraps)
	}
	// The lock-watch polls run every 10 minutes from the lock, so the
	// first one at or after FPL sets the flag must store it.
	if want := lock.Add(10 * time.Minute); seenAt.After(want) {
		t.Errorf("checked stored at %v, want by %v", seenAt, want)
	}
	ps, _ := repo.PollState(ctx)
	if ps.Mode != stats.ModeIdle {
		t.Errorf("mode after checked = %s, want idle", ps.Mode)
	}
}
