package jobs

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
)

// River runs the jobs on River (Postgres). Every API instance can run it:
// River hands each job to one worker, and periodic jobs (the tick, the
// prune) are enqueued by the elected leader only.
type River struct {
	client *river.Client[pgx.Tx]
}

// RiverConfig tunes River.
type RiverConfig struct {
	// TickEvery is the periodic tick (default 15 s).
	TickEvery time.Duration
	// PruneEvery is the housekeeping (default 1 h).
	PruneEvery time.Duration
	// Workers is the number of concurrent jobs (default 10).
	Workers int
}

// runWorker adapts Runner.Run to a River worker for args of type T.
type runWorker[T river.JobArgs] struct {
	river.WorkerDefaults[T]
	runner *Runner
}

func (w *runWorker[T]) Work(ctx context.Context, job *river.Job[T]) error {
	return w.runner.Run(ctx, job.Args)
}

// Timeout bounds one job; a tick across many leagues may take a while,
// and an FPL import, re-sync or check waits on upstream at 2 requests a
// second (a 16-team league at gameweek 38 is about 650 requests).
func (w *runWorker[T]) Timeout(job *river.Job[T]) time.Duration {
	switch job.Kind {
	case mirror.ImportArgs{}.Kind(), mirror.ResyncArgs{}.Kind(), mirror.CheckArgs{}.Kind(), mirror.ShadowArgs{}.Kind():
		return 15 * time.Minute
	case HistoryArgs{}.Kind():
		// About 700 players at 2 requests a second once a season.
		return 30 * time.Minute
	}
	return 2 * time.Minute
}

// NewRiver returns River jobs on pool, running runner's work. Call Start.
func NewRiver(pool *pgxpool.Pool, runner *Runner, cfg RiverConfig, log *slog.Logger) (*River, error) {
	if cfg.TickEvery <= 0 {
		cfg.TickEvery = 15 * time.Second
	}
	if cfg.PruneEvery <= 0 {
		cfg.PruneEvery = time.Hour
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 10
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &runWorker[draft.ClockArgs]{runner: runner})
	river.AddWorker(workers, &runWorker[trades.ReviewArgs]{runner: runner})
	river.AddWorker(workers, &runWorker[TickArgs]{runner: runner})
	river.AddWorker(workers, &runWorker[PruneArgs]{runner: runner})
	river.AddWorker(workers, &runWorker[mirror.ImportArgs]{runner: runner})
	river.AddWorker(workers, &runWorker[mirror.ResyncArgs]{runner: runner})
	river.AddWorker(workers, &runWorker[mirror.CheckArgs]{runner: runner})
	periodic := []*river.PeriodicJob{
		river.NewPeriodicJob(river.PeriodicInterval(cfg.TickEvery),
			func() (river.JobArgs, *river.InsertOpts) { return TickArgs{}, nil },
			&river.PeriodicJobOpts{ID: "tick", RunOnStart: true}),
		river.NewPeriodicJob(river.PeriodicInterval(cfg.PruneEvery),
			func() (river.JobArgs, *river.InsertOpts) { return PruneArgs{}, nil },
			&river.PeriodicJobOpts{ID: "prune", RunOnStart: true}),
	}
	if runner.Mirror != nil {
		river.AddWorker(workers, &runWorker[mirror.ShadowArgs]{runner: runner})
		river.AddWorker(workers, &runWorker[mirror.ShadowPlanArgs]{runner: runner})
		// Hourly: schedules the next 36 hours' shadow checks (docs/adr/0016).
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return mirror.ShadowPlanArgs{}, hourlyOpts()
			},
			&river.PeriodicJobOpts{ID: "fpl_shadow_plan", RunOnStart: true}))
	}
	if runner.History != nil {
		river.AddWorker(workers, &runWorker[HistoryArgs]{runner: runner})
		// Hourly: a no-op unless a player is new or a season has rolled.
		periodic = append(periodic, river.NewPeriodicJob(river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) {
				return HistoryArgs{}, hourlyOpts()
			},
			&river.PeriodicJobOpts{ID: "fetch_history", RunOnStart: true}))
	}
	client, err := river.NewClient(riverpgxv5.New(pool), &river.Config{
		Logger:       log,
		Queues:       map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: cfg.Workers}},
		Workers:      workers,
		PeriodicJobs: periodic,
	})
	if err != nil {
		return nil, err
	}
	return &River{client: client}, nil
}

// hourlyOpts makes an hourly periodic job unique per clock hour. Unique by
// args alone also matches completed jobs, which River keeps for 24 hours,
// so the job would run once a day.
func hourlyOpts() *river.InsertOpts {
	return &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Hour}}
}

// ScheduleTx inserts the job in tx (a pgx.Tx), to run at at. A job with
// the same arguments is inserted once.
func (r *River) ScheduleTx(ctx context.Context, tx any, args league.JobArgs, at time.Time) error {
	ra, ok := args.(river.JobArgs)
	if !ok {
		return errors.New("jobs: arguments are not River job arguments")
	}
	opts := &river.InsertOpts{ScheduledAt: at, UniqueOpts: river.UniqueOpts{ByArgs: true}}
	var err error
	if ptx, ok := tx.(pgx.Tx); ok {
		_, err = r.client.InsertTx(ctx, ptx, ra, opts)
	} else {
		_, err = r.client.Insert(ctx, ra, opts)
	}
	return err
}

// Start runs the workers until Stop.
func (r *River) Start(ctx context.Context) error { return r.client.Start(ctx) }

// Stop waits for running jobs to finish (bounded by ctx).
func (r *River) Stop(ctx context.Context) error { return r.client.Stop(ctx) }
