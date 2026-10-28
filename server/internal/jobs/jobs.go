// Package jobs runs the league work that happens at a time rather than on
// a request (ADR 0010): draft starts and pick clocks, lineup-lock
// announcements, waiver runs, trade expiry and review windows, and the
// housekeeping (realtime replay buffer, stat snapshots), and importing,
// re-syncing and checking FPL Draft leagues on request (package mirror).
// Nothing syncs an imported league automatically (docs/adr/0012).
//
// Runner holds the work; Tick does everything that is due across all
// leagues and is idempotent, so running it twice, late or on two instances
// changes nothing. On Postgres the jobs are River jobs (River.ScheduleTx
// inserts the precise ones, a draft clock or a review window, in the
// transaction that set them; a periodic tick job, run by River's leader,
// is the backstop that makes every deadline durable across restarts). In
// memory (the database-less dev server) Memory runs timers and a ticker.
package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/lineup"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// TickArgs is the periodic tick.
type TickArgs struct{}

// Kind names the job.
func (TickArgs) Kind() string { return "tick" }

// PruneArgs is the periodic housekeeping.
type PruneArgs struct{}

// Kind names the job.
func (PruneArgs) Kind() string { return "prune" }

// HistoryArgs fetches players' previous seasons (history.Fetcher): every
// player once per season, and new players as they appear.
type HistoryArgs struct{}

// Kind names the job.
func (HistoryArgs) Kind() string { return "fetch_history" }

// Runner does the jobs' work on the stores.
type Runner struct {
	Leagues league.Repo
	Stats   stats.Repo
	Now     func() time.Time
	Notify  league.Notifier
	Log     *slog.Logger
	// PruneRealtime drops replay events older than the cutoff; nil skips.
	PruneRealtime func(ctx context.Context, before time.Time) (int, error)
	// PruneSnapshots keeps the newest n stat snapshots per kind and
	// gameweek plus every final one; nil skips.
	PruneSnapshots func(ctx context.Context, keep int) (int, error)
	// KeepSnapshots is n for PruneSnapshots (default 50).
	KeepSnapshots int
	// PruneAuth drops expired or used magic links and expired sessions
	// (auth.Service.Prune); nil skips.
	PruneAuth func(ctx context.Context) (int, error)
	// PruneIdempotency drops idempotency records created before the
	// cutoff; nil skips.
	PruneIdempotency func(ctx context.Context, before time.Time) (int, error)
	// Mirror imports, re-syncs and checks FPL Draft leagues
	// (docs/adr/0012); nil fails those jobs.
	Mirror *mirror.Service
	// History fetches previous seasons; nil fails that job.
	History *history.Fetcher
	// Reminders, if set, sends a league's deadline reminders (draft
	// starting, waivers due, free agency closing, an invalid lineup) on
	// every tick; it de-duplicates them itself.
	Reminders func(ctx context.Context, leagueID string, now time.Time) error
}

func (r *Runner) log() *slog.Logger {
	if r.Log == nil {
		return slog.Default()
	}
	return r.Log
}

func (r *Runner) now() time.Time {
	if r.Now == nil {
		return time.Now()
	}
	return r.Now()
}

// Draft, Lineups, Waivers and Trades are the rule services on r's stores.
func (r *Runner) Draft() *draft.Service { return draft.New(r.Leagues, r.Stats, r.now, r.Notify) }

func (r *Runner) Lineups() *lineup.Service { return lineup.New(r.Leagues, r.Stats, r.now, r.Notify) }

func (r *Runner) Waivers() *waivers.Service { return waivers.New(r.Leagues, r.Stats, r.now, r.Notify) }

func (r *Runner) Trades() *trades.Service { return trades.New(r.Leagues, r.Stats, r.now, r.Notify) }

// TickResult counts what a tick did.
type TickResult struct {
	DraftsStarted, ClocksSettled, LineupLocks, WaiverRuns, TradesClosed, Errors int
}

// Tick does all due work across leagues. One league's failure is logged
// and counted and does not stop the others. Without season data it does
// nothing.
func (r *Runner) Tick(ctx context.Context) (TickResult, error) {
	var res TickResult
	if _, ok, err := r.Stats.Status(ctx); err != nil || !ok {
		return res, err
	}
	each := func(status string, fn func(id string) error) error {
		ids, err := r.Leagues.LeaguesWithStatus(ctx, status)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := fn(id); err != nil {
				res.Errors++
				r.log().ErrorContext(ctx, "job failed", "league", id, "err", err)
			}
		}
		return nil
	}
	count := func(n *int, ok bool, err error) error {
		if ok {
			*n++
		}
		return err
	}
	dr, lu, wv, tr := r.Draft(), r.Lineups(), r.Waivers(), r.Trades()
	err := errors.Join(
		each(league.StatusPreDraft, func(id string) error {
			r.remind(ctx, id)
			ok, err := dr.StartIfDue(ctx, id)
			return count(&res.DraftsStarted, ok, err)
		}),
		each(league.StatusDrafting, func(id string) error {
			ok, err := dr.Expire(ctx, id)
			return count(&res.ClocksSettled, ok, err)
		}),
		each(league.StatusInSeason, func(id string) error {
			r.remind(ctx, id)
			ok, err := lu.LockDue(ctx, id)
			if err := count(&res.LineupLocks, ok, err); err != nil {
				return err
			}
			ok, err = wv.RunDue(ctx, id)
			if err := count(&res.WaiverRuns, ok, err); err != nil {
				return err
			}
			n, err := tr.CloseDue(ctx, id)
			res.TradesClosed += n
			return err
		}),
	)
	return res, err
}

// remind sends the league's due reminders; a failure is logged only, so
// it never holds up the league's real work.
func (r *Runner) remind(ctx context.Context, leagueID string) {
	if r.Reminders == nil {
		return
	}
	if err := r.Reminders(ctx, leagueID, r.now()); err != nil {
		r.log().ErrorContext(ctx, "reminders failed", "league", leagueID, "err", err)
	}
}

// Prune is the housekeeping: realtime events older than five minutes,
// stat snapshots beyond the newest KeepSnapshots per kind and gameweek
// (final ones are kept), expired or used magic links, expired sessions,
// and idempotency records over a day old.
func (r *Runner) Prune(ctx context.Context) error {
	var errs []error
	if r.PruneRealtime != nil {
		n, err := r.PruneRealtime(ctx, r.now().Add(-5*time.Minute))
		errs = append(errs, err)
		r.log().DebugContext(ctx, "pruned realtime events", "count", n)
	}
	if r.PruneSnapshots != nil {
		keep := r.KeepSnapshots
		if keep <= 0 {
			keep = 50
		}
		n, err := r.PruneSnapshots(ctx, keep)
		errs = append(errs, err)
		if n > 0 {
			r.log().InfoContext(ctx, "pruned stat snapshots", "count", n, "keep", keep)
		}
	}
	if r.PruneAuth != nil {
		n, err := r.PruneAuth(ctx)
		errs = append(errs, err)
		r.log().DebugContext(ctx, "pruned magic links and sessions", "count", n)
	}
	if r.PruneIdempotency != nil {
		n, err := r.PruneIdempotency(ctx, r.now().Add(-24*time.Hour))
		errs = append(errs, err)
		r.log().DebugContext(ctx, "pruned idempotency records", "count", n)
	}
	return errors.Join(errs...)
}

// Run does one job by its arguments (the River workers and Memory call it).
func (r *Runner) Run(ctx context.Context, args league.JobArgs) error {
	switch a := args.(type) {
	case draft.ClockArgs:
		_, err := r.Draft().Expire(ctx, a.LeagueID)
		return err
	case trades.ReviewArgs:
		_, err := r.Trades().CloseDue(ctx, a.LeagueID)
		return err
	case TickArgs:
		_, err := r.Tick(ctx)
		return err
	case PruneArgs:
		return r.Prune(ctx)
	case HistoryArgs:
		if r.History == nil {
			return errors.New("jobs: no history fetcher for " + a.Kind())
		}
		res, err := r.History.Run(ctx, history.SeasonStart(r.Now()))
		if err == nil && (res.Fetched > 0 || res.Failed > 0) {
			r.Log.InfoContext(ctx, "player history fetched", "fetched", res.Fetched, "skipped", res.Skipped,
				"missing", res.Missing, "failed", res.Failed)
		}
		return err
	case mirror.ImportArgs:
		if r.Mirror == nil {
			return errors.New("jobs: no mirror service for " + a.Kind())
		}
		return r.Mirror.RunImport(ctx, a.ImportID)
	case mirror.ResyncArgs:
		if r.Mirror == nil {
			return errors.New("jobs: no mirror service for " + a.Kind())
		}
		return r.Mirror.Resync(ctx, a.LeagueID)
	case mirror.CheckArgs:
		if r.Mirror == nil {
			return errors.New("jobs: no mirror service for " + a.Kind())
		}
		return r.Mirror.Check(ctx, a.LeagueID)
	case mirror.ShadowArgs:
		if r.Mirror == nil {
			return errors.New("jobs: no mirror service for " + a.Kind())
		}
		// A failed check is logged, not retried: the next slot checks again.
		if _, err := r.Mirror.ShadowCheck(ctx, a.LeagueID, a.Reason); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			r.log().WarnContext(ctx, "shadow check failed", "league", a.LeagueID, "reason", a.Reason, "err", err)
		}
		return nil
	case mirror.ShadowPlanArgs:
		if r.Mirror == nil {
			return nil
		}
		_, err := r.Mirror.PlanShadow(ctx)
		return err
	}
	return errors.New("jobs: unknown job " + args.Kind())
}
