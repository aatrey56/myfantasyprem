// Package poller is the single global stats poller (docs/ARCHITECTURE.md
// "Live-scoring data flow", RESEARCH.md §9.4). One poller serves every
// league: it reads the StatsSource, ingests into tier-1 stats and rescores
// the leagues whose players moved.
//
// Cadence follows the current gameweek's state:
//
//	live       a match is on (or past kickoff and not over)   LiveInterval (30 s)
//	bonus      every match over, bonus not confirmed         BonusInterval (10 min)
//	unchecked  bonus added, data not checked                 UncheckedInterval (6 h), but wakes at the
//	                                                         09:00 UK score lock and polls every
//	                                                         BonusInterval for LockWatch after it
//	idle       otherwise                                     until the next kickoff, at most IdleInterval (24 h)
//
// Every wait is capped at the next kickoff and at DeadlineMargin before the
// next gameweek deadline. Waits are measured on the wall clock (see
// waitUntil): Go timers stop while the host sleeps, so a laptop server
// would otherwise oversleep a wait by the time its lid was shut. Bootstrap (players, clubs,
// gameweeks, fixtures) is refreshed at most every BootstrapInterval,
// whenever the current gameweek changes, when live data names a player the
// store does not know yet (or a fixture that neither the store nor the
// live data's own fixtures describe), and on every poll in unchecked
// mode: the checked flag (classic FPL's data_checked; for FPL Draft, which
// has none, the event's finished, ADR 0005) is in bootstrap, so the polls
// around the score lock must refetch it or official would turn final only
// at the next 6-hourly refresh. A Draft gameweek is final once finished,
// so the poller leaves unchecked mode then and goes idle.
//
// Earlier gameweeks: on start and on every rollover the previous
// gameweek's final live payload is fetched again (late corrections), and
// gameweeks with no stored rows, or lines without starts, are backfilled, BackfillPerTick per cycle
// and at most two requests per second. Gameweeks that failed go to the
// back of the queue, and while gameweeks never tried remain the next
// cycle comes after LiveInterval, not the mode's wait of hours. Upstream's bootstrap lists only
// upcoming fixtures, so a past gameweek's fixtures (teams, kickoff, status
// and score) come from its live payload.
//
// Reliability rules (ADR 0005): a failed or malformed poll changes no data
// (the last good snapshot stays), backs off exponentially and is recorded in
// poll_state; responses turn stale 3 minutes after the last good poll while
// matches are live (stats.Freshness). Polls are idempotent: the same
// upstream data writes nothing and rescores nothing.
package poller

import (
	"context"
	"errors"
	"expvar"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Config sets the cadence. Zero values get the defaults.
type Config struct {
	LiveInterval      time.Duration // default 30s
	BonusInterval     time.Duration // default 10m
	UncheckedInterval time.Duration // default 6h
	IdleInterval      time.Duration // default 24h
	BootstrapInterval time.Duration // default 6h
	// MaxBackoff caps the wait after failures. Default 10m.
	MaxBackoff time.Duration
	// BackfillPerTick bounds the earlier gameweeks fetched per cycle.
	// Default 4.
	BackfillPerTick int
	// BackfillPace is the gap before each earlier-gameweek request.
	// Default 500ms (at most 2 requests per second).
	BackfillPace time.Duration
}

func (c Config) withDefaults() Config {
	def := func(d *time.Duration, v time.Duration) {
		if *d <= 0 {
			*d = v
		}
	}
	def(&c.LiveInterval, 30*time.Second)
	def(&c.BonusInterval, 10*time.Minute)
	def(&c.UncheckedInterval, 6*time.Hour)
	def(&c.IdleInterval, 24*time.Hour)
	def(&c.BootstrapInterval, 6*time.Hour)
	def(&c.MaxBackoff, 10*time.Minute)
	def(&c.BackfillPace, 500*time.Millisecond)
	if c.BackfillPerTick <= 0 {
		c.BackfillPerTick = 4
	}
	return c
}

// Recomputer rescores leagues; *league.Scorer implements it.
type Recomputer interface {
	Recompute(ctx context.Context, gw int, changedPlayers []string, all bool) (league.RecomputeResult, error)
}

// Metrics are the poller's counters, published under expvar "poller".
type Metrics struct {
	Polls          expvar.Int // poll cycles started
	PollFailures   expvar.Int // cycles that failed (no data written)
	Invalid        expvar.Int // failures caused by a malformed upstream response
	RowsChanged    expvar.Int // players whose stats changed
	LeaguesScored  expvar.Int // league rescoring runs
	TeamsChanged   expvar.Int // team scores that changed
	LastGoodUnix   expvar.Int
	ConsecFailures expvar.Int
	Stale          expvar.Int // 1 while live data is stale
	Reconciled     expvar.Int // earlier gameweeks re-fetched and rescored
	ReconcileFails expvar.Int // earlier-gameweek fetches that failed (retried)
	Mode           expvar.String
}

var (
	publishOnce sync.Once
	published   *Metrics
)

// PublishedMetrics returns the process-wide metrics, registered with
// expvar on first use.
func PublishedMetrics() *Metrics {
	publishOnce.Do(func() {
		published = &Metrics{}
		m := expvar.NewMap("poller")
		m.Set("polls", &published.Polls)
		m.Set("poll_failures", &published.PollFailures)
		m.Set("invalid_responses", &published.Invalid)
		m.Set("players_changed", &published.RowsChanged)
		m.Set("leagues_scored", &published.LeaguesScored)
		m.Set("teams_changed", &published.TeamsChanged)
		m.Set("last_good_unix", &published.LastGoodUnix)
		m.Set("consecutive_failures", &published.ConsecFailures)
		m.Set("stale", &published.Stale)
		m.Set("gameweeks_reconciled", &published.Reconciled)
		m.Set("reconcile_failures", &published.ReconcileFails)
		m.Set("mode", &published.Mode)
	})
	return published
}

// Poller polls one StatsSource for every league.
type Poller struct {
	Source   provider.StatsSource
	Ingester *stats.Ingester
	Stats    stats.Repo
	Scorer   Recomputer
	Config   Config
	Log      *slog.Logger
	Metrics  *Metrics
	Now      func() time.Time
	// Sleep paces earlier-gameweek requests; it returns ctx's error.
	Sleep func(context.Context, time.Duration) error
	// Wait is one bounded timer wait between polls (at most WaitChunk);
	// it returns ctx's error. Run rechecks the wall clock after each.
	Wait func(context.Context, time.Duration) error
	// News, if set, hears the players before and after each bootstrap
	// ingest (player news notifications).
	News NewsHook

	lastBootstrap time.Time
	lastGoodAt    time.Time                // wall clock of the last good poll
	seasonGW      int                      // the current gameweek of the last bootstrap ingest
	status        *provider.GameweekStatus // this cycle's
	lastGW        int                      // the current gameweek of the last good poll
	// lastMode is the mode the previous good poll chose.
	lastMode string
	// rescore marks gameweeks to rescore in full until it succeeds: this
	// cycle's durable marks (stats.Repo.RescorePending, set by an ingest
	// that changed rows) plus in-process ones (a failed full rescore that
	// changed no rows).
	rescore map[int]bool
	// durable is the subset of rescore read from the store this cycle.
	durable map[int]bool
	// planned is set once the startup reconciliation is queued; pending
	// holds earlier gameweeks still to fetch (true: always re-fetch,
	// false: only while no rows are stored).
	planned bool
	pending map[int]bool
	// failed holds queued gameweeks whose last fetch failed; they go to
	// the back of the queue so they cannot starve the others.
	failed map[int]bool
	// startsChecked holds the gameweeks re-read without getting starts
	// (stats.Repo.StartsChecked).
	startsChecked map[int]bool
}

// NewsHook hears the players before and after a bootstrap ingest, and the
// season's current gameweek.
type NewsHook interface {
	PlayerNews(ctx context.Context, before, after map[string]stats.Player, gw int)
}

// players returns the stored players, or nil before the first ingest.
func (p *Poller) players(ctx context.Context) map[string]stats.Player {
	w, err := league.SeasonWorld(ctx, p.Stats)
	if err != nil {
		return nil
	}
	return w.Players
}

// New returns a Poller with defaults filled in.
func New(src provider.StatsSource, ing *stats.Ingester, repo stats.Repo, scorer Recomputer, cfg Config, log *slog.Logger) *Poller {
	return &Poller{
		Source: src, Ingester: ing, Stats: repo, Scorer: scorer, Config: cfg.withDefaults(),
		Log: log, Metrics: &Metrics{}, Now: time.Now, Sleep: sleep, Wait: sleep,
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Run polls until ctx ends. It never returns an upstream error: failures
// are logged, recorded and retried.
func (p *Poller) Run(ctx context.Context) error {
	p.Config = p.Config.withDefaults()
	p.Log.InfoContext(ctx, "poller started")
	for {
		wait := p.Tick(ctx)
		if p.waitUntil(ctx, p.Now().Add(wait), wait) != nil {
			p.Log.InfoContext(context.Background(), "poller stopped")
			return nil
		}
	}
}

const (
	// WaitChunk bounds one timer wait between polls.
	WaitChunk = time.Minute
	// ClockJump is how far the wall clock may run ahead of the timers
	// during a wait before it counts as a jump (the host slept).
	ClockJump = 2 * time.Minute
	// DeadlineMargin is how long before a gameweek deadline the poller
	// polls once, so news and fixtures are fresh for it.
	DeadlineMargin = 30 * time.Minute
)

// waitUntil waits until the wall-clock instant due, or until wait has
// passed on the timers if the wall clock was set back. Go timers run on
// the monotonic clock, which stops while macOS sleeps, so it waits in
// chunks of at most WaitChunk and rereads the wall clock after each: after
// a resume the poll comes as soon as it is due. When the wall clock ran
// more than ClockJump ahead of the timers it logs the jump and returns at
// once if the last good poll is older than the expected interval (wait).
// It returns ctx's error.
func (p *Poller) waitUntil(ctx context.Context, due time.Time, wait time.Duration) error {
	due = due.Round(0) // strip the monotonic reading: compare wall clocks
	start := p.Now().Round(0)
	var slept, total time.Duration
	for {
		now := p.Now().Round(0)
		if !now.Before(due) || total >= wait {
			return ctx.Err()
		}
		if jump := now.Sub(start) - slept; jump > ClockJump {
			overdue := p.lastGoodAt.IsZero() || now.Sub(p.lastGoodAt.Round(0)) >= wait
			p.Log.WarnContext(ctx, "poller: wall clock jumped ahead (host asleep?)",
				"jump", jump.Round(time.Second), "last_good_at", p.lastGoodAt, "poll_now", overdue)
			if overdue {
				return ctx.Err()
			}
			start, slept = now, 0
		}
		d := min(due.Sub(now), WaitChunk, wait-total)
		if err := p.Wait(ctx, d); err != nil {
			return err
		}
		slept += d
		total += d
	}
}

// Decide picks the mode and the wait until the next poll from the current
// gameweek's fixtures and flags. It is pure.
func Decide(cfg Config, now time.Time, fixtures []stats.Fixture, gw stats.Gameweek, nextKickoff *time.Time) (string, time.Duration) {
	cfg = cfg.withDefaults()
	started, allOver := false, len(fixtures) > 0
	live := false
	for _, f := range fixtures {
		pastKickoff := f.KickoffAt != nil && !now.Before(*f.KickoffAt)
		if f.Started() || pastKickoff {
			started = true
		}
		if !f.Over() {
			allOver = false
			// Past kickoff but not over: live, even if upstream has not
			// flagged it started yet.
			if f.Started() || pastKickoff {
				live = true
			}
		}
	}
	mode, wait := stats.ModeIdle, cfg.IdleInterval
	switch {
	case live:
		return stats.ModeLive, cfg.LiveInterval
	case started && allOver && !gw.BonusAdded:
		mode, wait = stats.ModeBonus, cfg.BonusInterval
	case started && allOver && !gw.DataChecked:
		mode, wait = stats.ModeUnchecked, uncheckedWait(cfg, now, fixtures)
	}
	// Never sleep through a kickoff.
	if nextKickoff != nil {
		if until := nextKickoff.Sub(now); until < wait {
			wait = max(until, cfg.LiveInterval)
		}
	}
	return mode, wait
}

// LockWatch is how long after the 09:00 score lock the poller keeps
// polling at BonusInterval for FPL to mark the data checked.
const LockWatch = 2 * time.Hour

// uncheckedWait is the wait while bonus is added but the data is not
// checked. FPL locks scores at 09:00 UK time the day after the last match
// (2026-27) and marks the data checked then, so the poller wakes at the
// lock, watches closely for LockWatch, then backs off to
// UncheckedInterval.
func uncheckedWait(cfg Config, now time.Time, fixtures []stats.Fixture) time.Duration {
	lock, ok := stats.ScoreLock(fixtures)
	switch {
	case !ok:
		return cfg.UncheckedInterval
	case now.Before(lock):
		return min(lock.Sub(now), cfg.UncheckedInterval)
	case now.Before(lock.Add(LockWatch)):
		return cfg.BonusInterval
	}
	return cfg.UncheckedInterval
}

// backoff is the wait after n consecutive failures: the mode's interval
// doubled per failure, capped.
func (p *Poller) backoff(interval time.Duration, n int) time.Duration {
	d := interval
	for i := 1; i < n && d < p.Config.MaxBackoff; i++ {
		d *= 2
	}
	return min(d, p.Config.MaxBackoff)
}

// Tick runs one poll cycle and returns the wait until the next one.
func (p *Poller) Tick(ctx context.Context) time.Duration {
	start := p.Now()
	p.Metrics.Polls.Add(1)
	ps, err := p.Stats.PollState(ctx)
	if err != nil {
		p.Log.ErrorContext(ctx, "poller: read poll state", "err", err)
	}
	ps.LastAttemptAt = start

	mode, interval, gw, err := p.poll(ctx)
	if err != nil {
		p.Metrics.PollFailures.Add(1)
		if errors.Is(err, provider.ErrInvalid) {
			p.Metrics.Invalid.Add(1)
		}
		ps.ConsecutiveFailures++
		ps.LastError = err.Error()
		if ps.Mode == "" {
			ps.Mode = stats.ModeIdle
		}
		base := p.Config.LiveInterval
		if ps.Mode != stats.ModeLive {
			base = min(p.Config.BonusInterval, p.Config.MaxBackoff)
		}
		wait := p.backoff(base, ps.ConsecutiveFailures)
		ps.NextPollAt = start.Add(wait)
		_, stale := stats.Freshness(ps, start)
		p.record(ctx, ps, stale)
		p.Log.WarnContext(ctx, "poll failed; keeping the last good snapshot",
			"err", err, "mode", ps.Mode, "consecutive_failures", ps.ConsecutiveFailures,
			"last_good_at", ps.LastGoodAt, "stale", stale, "retry_in", wait)
		return wait
	}
	ps.Mode, ps.Gameweek = mode, gw
	ps.LastGoodAt = p.Now()
	p.lastGoodAt = ps.LastGoodAt
	ps.NextPollAt = ps.LastGoodAt.Add(interval)
	ps.ConsecutiveFailures, ps.LastError = 0, ""
	p.record(ctx, ps, false)
	p.Metrics.LastGoodUnix.Set(ps.LastGoodAt.Unix())
	p.Log.InfoContext(ctx, "poll ok", "mode", mode, "gameweek", gw, "next_in", interval,
		"duration_ms", p.Now().Sub(start).Milliseconds())
	return interval
}

func (p *Poller) record(ctx context.Context, ps stats.PollState, stale bool) {
	p.Metrics.ConsecFailures.Set(int64(ps.ConsecutiveFailures))
	p.Metrics.Mode.Set(ps.Mode)
	if stale {
		p.Metrics.Stale.Set(1)
	} else {
		p.Metrics.Stale.Set(0)
	}
	if err := p.Stats.SavePollState(ctx, ps); err != nil {
		p.Log.ErrorContext(ctx, "poller: save poll state", "err", err)
	}
}

// poll fetches and ingests; on any error nothing has been written except
// what an earlier, complete step committed (each step is one transaction
// and idempotent, so the next cycle simply repeats it).
func (p *Poller) poll(ctx context.Context) (mode string, interval time.Duration, gw int, err error) {
	st, err := p.Source.GameweekStatus(ctx)
	if err != nil {
		return "", 0, 0, err
	}
	gw, p.status = st.Current, st
	// Unchecked mode waits for data_checked, which only bootstrap carries.
	watching := p.lastMode == stats.ModeUnchecked
	if p.lastBootstrap.IsZero() || p.Now().Sub(p.lastBootstrap) >= p.Config.BootstrapInterval || gw != p.seasonGW || watching {
		if err := p.refreshSeason(ctx, st); err != nil {
			return "", 0, 0, err
		}
	} else if err := p.Ingester.Status(ctx, st); err != nil {
		return "", 0, 0, err
	}

	if err := p.loadRescore(ctx); err != nil {
		return "", 0, 0, err
	}
	w, err := league.LoadWorld(ctx, p.Stats, gw)
	if err != nil {
		return "", 0, 0, err
	}
	now := p.Now()
	mode, interval = Decide(p.Config, now, w.FixturesIn(gw), w.Gameweeks[gw], nextKickoff(w, now))

	newGW := gw != p.lastGW
	if mode != stats.ModeIdle || newGW || w.Started(gw) || p.rescore[gw] {
		live, err := p.Source.EventLive(ctx, gw)
		switch {
		case errors.Is(err, provider.ErrNotFound) && !kickedOff(w, gw, now):
			// Upstream publishes the live endpoint around the first
			// kickoff; before it a 404 is normal.
			if p.rescore[gw] {
				if err := p.rescoreGW(ctx, gw, nil, true); err != nil {
					return "", 0, 0, err
				}
			}
		case err != nil:
			return "", 0, 0, err
		default:
			changed, err := p.ingestLive(ctx, live, st)
			if err != nil {
				return "", 0, 0, err
			}
			p.Metrics.RowsChanged.Add(int64(len(changed)))
			if err := p.rescoreGW(ctx, gw, changed, newGW); err != nil {
				return "", 0, 0, err
			}
			// Live data can move the state (a match ends): decide again.
			if w, err = league.LoadWorld(ctx, p.Stats, gw); err == nil {
				mode, interval = Decide(p.Config, p.Now(), w.FixturesIn(gw), w.Gameweeks[gw], nextKickoff(w, p.Now()))
			}
		}
	}
	// Earlier gameweeks still marked (their rescore failed, perhaps before
	// a restart): their rows are stored, so rescore them in full.
	for _, g := range slices.Sorted(maps.Keys(p.rescore)) {
		if g == gw {
			continue
		}
		if err := p.rescoreGW(ctx, g, nil, true); err != nil {
			return "", 0, 0, err
		}
	}
	if err := p.plan(ctx, gw); err != nil {
		return "", 0, 0, err
	}
	interval = capAtDeadline(p.Now(), interval, w, p.Config.LiveInterval)
	p.lastGW, p.lastMode = gw, mode
	if p.reconcile(ctx, gw) {
		// The backfill budget ran out with gameweeks never tried: carry
		// on soon rather than after an idle or unchecked wait of hours.
		interval = min(interval, p.Config.LiveInterval)
	}
	return mode, interval, gw, nil
}

// refreshSeason fetches and ingests bootstrap and fixtures.
func (p *Poller) refreshSeason(ctx context.Context, st *provider.GameweekStatus) error {
	b, err := p.Source.Bootstrap(ctx)
	if err != nil {
		return err
	}
	fixtures, err := p.Source.Fixtures(ctx)
	if err != nil {
		return err
	}
	var before map[string]stats.Player
	if p.News != nil {
		before = p.players(ctx)
	}
	if err := p.Ingester.Season(ctx, b, fixtures, st); err != nil {
		return err
	}
	p.lastBootstrap, p.seasonGW = p.Now(), st.Current
	if p.News != nil && len(before) > 0 {
		p.News.PlayerNews(ctx, before, p.players(ctx), st.Current)
	}
	return nil
}

// ingestLive ingests one gameweek's live data. If it names a player added
// upstream since the last bootstrap ingest, bootstrap is refreshed at once
// and the ingest retried. (Fixtures come with the live data itself.)
func (p *Poller) ingestLive(ctx context.Context, live *provider.EventLive, st *provider.GameweekStatus) ([]string, error) {
	changed, err := p.Ingester.Live(ctx, live, st)
	if !errors.Is(err, stats.ErrUnmapped) {
		return changed, err
	}
	p.Log.InfoContext(ctx, "live data references unknown ids; refreshing bootstrap", "gameweek", live.Gameweek, "err", err)
	// p.status is this cycle's real season status; st may describe a
	// closed gameweek being settled.
	if err := p.refreshSeason(ctx, p.status); err != nil {
		return nil, err
	}
	return p.Ingester.Live(ctx, live, st)
}

// rescoreGW rescores a gameweek. A failure marks it for a full rescore on
// the next cycle, because the ingest that found the changes has committed.
func (p *Poller) rescoreGW(ctx context.Context, gw int, changed []string, all bool) error {
	if p.rescore == nil {
		p.rescore = map[int]bool{}
	}
	all = all || p.rescore[gw]
	p.rescore[gw] = true
	// Read the mark before recomputing: if another poller marks the
	// gameweek again meanwhile, the mark gets a new version and survives
	// the ClearRescore below, so that change is rescored too.
	marks, err := p.Stats.RescorePending(ctx)
	if err != nil {
		return err
	}
	res, err := p.Scorer.Recompute(ctx, gw, changed, all)
	if err != nil {
		return err
	}
	// The ingest that found changes marked the gameweek durably; clear the
	// mark only now that its scores are correct.
	if i := slices.IndexFunc(marks, func(m stats.RescoreMark) bool { return m.Gameweek == gw }); i >= 0 &&
		(p.durable[gw] || len(changed) > 0) {
		if err := p.Stats.ClearRescore(ctx, marks[i]); err != nil {
			return err
		}
		delete(p.durable, gw)
	}
	delete(p.rescore, gw)
	p.Metrics.LeaguesScored.Add(int64(res.Leagues))
	p.Metrics.TeamsChanged.Add(int64(len(res.ChangedTeams)))
	if res.Leagues > 0 {
		p.Log.InfoContext(ctx, "rescored leagues", "gameweek", gw, "leagues", res.Leagues,
			"skipped", res.Skipped, "teams_changed", len(res.ChangedTeams), "players_changed", len(changed))
	}
	return nil
}

// loadRescore merges the store's pending rescores into p.rescore.
func (p *Poller) loadRescore(ctx context.Context) error {
	marks, err := p.Stats.RescorePending(ctx)
	if err != nil {
		return err
	}
	if p.rescore == nil {
		p.rescore = map[int]bool{}
	}
	p.durable = map[int]bool{}
	for _, m := range marks {
		p.rescore[m.Gameweek], p.durable[m.Gameweek] = true, true
	}
	return nil
}

// plan queues earlier gameweeks: on start, the previous gameweek (its
// final data may have changed while we were down) and every earlier one
// with no stored rows, no per-player gameweek lines (a database from
// before lines were kept) or lines with minutes but no starts (lines kept
// before starts was read); on rollover, every gameweek that closed.
func (p *Poller) plan(ctx context.Context, gw int) error {
	if p.pending == nil {
		p.pending = map[int]bool{}
	}
	if p.planned {
		for g := max(p.lastGW, 1); g < gw; g++ {
			p.pending[g] = true
		}
		return nil
	}
	rows, err := p.Stats.PlayerFixtures(ctx, 0)
	if err != nil {
		return err
	}
	stored := map[int]bool{}
	for _, r := range rows {
		stored[r.Gameweek] = true
	}
	lines, err := p.Stats.PlayerGameweeks(ctx, 0)
	if err != nil {
		return err
	}
	byGW := map[int][]stats.PlayerGameweek{}
	for _, l := range lines {
		byGW[l.Gameweek] = append(byGW[l.Gameweek], l)
	}
	if gw > 1 {
		p.pending[gw-1] = true
	}
	checked, err := p.Stats.StartsChecked(ctx)
	if err != nil {
		return err
	}
	p.startsChecked = checked
	for g := 1; g < gw-1; g++ {
		if !stored[g] || !p.linesComplete(g, byGW[g]) {
			p.pending[g] = false
		}
	}
	p.planned = true
	return nil
}

// reconcile fetches up to BackfillPerTick queued gameweeks, most recent
// first, paced politely; gameweeks that failed before go last. Failures
// are logged and retried; they do not fail the poll (the current
// gameweek's data is good) and never replace stored rows (the ingest
// rejects incomplete snapshots). It reports whether the budget ran out
// while a gameweek not yet tried was still queued.
func (p *Poller) reconcile(ctx context.Context, current int) bool {
	if len(p.pending) == 0 {
		return false
	}
	w, err := league.LoadWorld(ctx, p.Stats, -1) // fixtures and flags; no rows needed
	if err != nil {
		p.Log.WarnContext(ctx, "poller: reconcile: load", "err", err)
		return false
	}
	if p.failed == nil {
		p.failed = map[int]bool{}
	}
	queue := slices.Collect(maps.Keys(p.pending))
	slices.SortFunc(queue, func(a, b int) int {
		if p.failed[a] != p.failed[b] {
			if p.failed[a] {
				return 1
			}
			return -1
		}
		return b - a
	})
	done := 0
	for i, g := range queue {
		// A closed gameweek with no stored fixtures is fetched anyway:
		// upstream's bootstrap lists only upcoming fixtures, so its live
		// data is where they come from.
		if g >= current || (!w.Started(g) && len(w.FixturesIn(g)) > 0) {
			p.drop(g) // nothing played yet: nothing to load
			continue
		}
		if !p.pending[g] && p.loaded(ctx, g) {
			p.drop(g)
			continue
		}
		if done >= p.Config.BackfillPerTick {
			return slices.ContainsFunc(queue[i:], func(g int) bool { return !p.failed[g] })
		}
		done++
		if err := p.Sleep(ctx, p.Config.BackfillPace); err != nil {
			return false
		}
		if err := p.settle(ctx, g, w.Gameweeks[g]); err != nil {
			p.failed[g] = true
			p.Metrics.ReconcileFails.Add(1)
			p.Log.WarnContext(ctx, "poller: reconcile failed; retrying next cycle", "gameweek", g, "err", err)
			if errors.Is(err, provider.ErrUpstream) {
				return false // upstream is struggling: stop for this cycle
			}
			continue
		}
		if err := p.markStartsChecked(ctx, g); err != nil {
			p.Log.WarnContext(ctx, "poller: reconcile: mark starts checked", "gameweek", g, "err", err)
		}
		p.drop(g)
		p.Metrics.Reconciled.Add(1)
	}
	return false
}

// drop removes a gameweek from the backfill queue.
func (p *Poller) drop(g int) {
	delete(p.pending, g)
	delete(p.failed, g)
}

// loaded reports whether a gameweek has stored rows and complete
// gameweek lines.
func (p *Poller) loaded(ctx context.Context, g int) bool {
	rows, err := p.Stats.PlayerFixtures(ctx, g)
	if err != nil || len(rows) == 0 {
		return false
	}
	lines, err := p.Stats.PlayerGameweeks(ctx, g)
	return err == nil && p.linesComplete(g, lines)
}

// linesComplete reports whether a gameweek's lines are there and, once
// anyone played, carry starts (lines kept before starts was read lack it)
// or were already re-read for them (StartsChecked: the source sends none).
func (p *Poller) linesComplete(g int, lines []stats.PlayerGameweek) bool {
	return len(lines) > 0 && (p.startsChecked[g] || hasStarts(lines))
}

// hasStarts reports whether a gameweek's lines carry starts, or nobody
// played in it.
func hasStarts(lines []stats.PlayerGameweek) bool {
	played, started := false, false
	for _, l := range lines {
		played = played || l.Minutes > 0
		started = started || l.Starts != nil
	}
	return started || !played
}

// markStartsChecked stores that gameweek g was read and its source sent
// no starts, so restarts do not re-read and rescore it again for them.
func (p *Poller) markStartsChecked(ctx context.Context, g int) error {
	lines, err := p.Stats.PlayerGameweeks(ctx, g)
	if err != nil || len(lines) == 0 || hasStarts(lines) {
		return err
	}
	if err := p.Stats.MarkStartsChecked(ctx, g); err != nil {
		return err
	}
	if p.startsChecked == nil {
		p.startsChecked = map[int]bool{}
	}
	p.startsChecked[g] = true
	return nil
}

// settle fetches a closed gameweek's live data, ingests it and rescores
// every league for it.
func (p *Poller) settle(ctx context.Context, g int, info stats.Gameweek) error {
	live, err := p.Source.EventLive(ctx, g)
	if err != nil {
		return err
	}
	st := &provider.GameweekStatus{Current: g, BonusAdded: info.BonusAdded, DataChecked: info.DataChecked}
	changed, err := p.ingestLive(ctx, live, st)
	if err != nil {
		return err
	}
	p.Metrics.RowsChanged.Add(int64(len(changed)))
	return p.rescoreGW(ctx, g, changed, true)
}

// kickedOff reports whether any fixture of the gameweek has started or is
// past its kickoff time.
func kickedOff(w *league.World, gw int, now time.Time) bool {
	for _, f := range w.FixturesIn(gw) {
		if f.Started() || (f.KickoffAt != nil && !now.Before(*f.KickoffAt)) {
			return true
		}
	}
	return false
}

// capAtDeadline caps wait so the poller polls once DeadlineMargin before
// each gameweek deadline (never sooner than floor). Inside the margin the
// wait is left alone: that one poll suffices.
func capAtDeadline(now time.Time, wait time.Duration, w *league.World, floor time.Duration) time.Duration {
	for _, g := range w.Gameweeks {
		at := g.DeadlineAt.Add(-DeadlineMargin)
		if g.DeadlineAt.IsZero() || !at.After(now) {
			continue
		}
		if until := at.Sub(now); until < wait {
			wait = max(until, floor)
		}
	}
	return wait
}

// nextKickoff is the earliest kickoff after now.
func nextKickoff(w *league.World, now time.Time) *time.Time {
	var best *time.Time
	for _, f := range w.Fixtures {
		if f.KickoffAt != nil && f.KickoffAt.After(now) && (best == nil || f.KickoffAt.Before(*best)) {
			best = f.KickoffAt
		}
	}
	return best
}
