package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
)

// Memory runs jobs in this process: a timer per scheduled job and a ticker
// for the tick. Nothing survives a restart, which is fine for the
// database-less dev server it serves (the tick catches up on start).
type Memory struct {
	runner *Runner
	ctx    context.Context

	mu      sync.Mutex
	stopped bool
	timers  []*time.Timer
	wg      sync.WaitGroup
}

// NewMemory returns a Memory scheduler for runner; jobs run with ctx.
func NewMemory(ctx context.Context, runner *Runner) *Memory {
	return &Memory{runner: runner, ctx: ctx}
}

// ScheduleTx runs args at at, in a goroutine (never inside the caller's
// transaction, whose lock it would wait for).
func (m *Memory) ScheduleTx(_ context.Context, _ any, args league.JobArgs, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return nil
	}
	m.wg.Add(1)
	t := time.AfterFunc(max(time.Until(at), 0), func() {
		defer m.wg.Done()
		if err := m.runner.Run(m.ctx, args); err != nil && m.ctx.Err() == nil {
			m.runner.log().ErrorContext(m.ctx, "job failed", "kind", args.Kind(), "err", err)
		}
	})
	m.timers = append(m.timers, t)
	return nil
}

// Run ticks every interval and once at once, and prunes hourly until ctx
// ends, then stops the timers.
func (m *Memory) Run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	prune := time.NewTicker(time.Hour)
	defer tick.Stop()
	defer prune.Stop()
	for {
		if _, err := m.runner.Tick(ctx); err != nil && ctx.Err() == nil {
			m.runner.log().ErrorContext(ctx, "tick failed", "err", err)
		}
		select {
		case <-ctx.Done():
			m.stop()
			return
		case <-tick.C:
		case <-prune.C:
			_ = m.runner.Prune(ctx)
			_ = m.runner.Run(ctx, mirror.ShadowPlanArgs{})
		}
	}
}

func (m *Memory) stop() {
	m.mu.Lock()
	m.stopped = true
	for _, t := range m.timers {
		if t.Stop() {
			m.wg.Done()
		}
	}
	m.mu.Unlock()
	m.wg.Wait()
}
