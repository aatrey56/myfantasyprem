package poller

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// waiter is a poller whose timer waits move a fake wall clock; jump, if
// set, is added to the clock after the given chunk (the host slept).
type waiter struct {
	p      *Poller
	clock  time.Time
	chunks []time.Duration
	jumpAt int
	jump   time.Duration
}

func newWaiter() *waiter {
	w := &waiter{clock: time.Date(2026, 10, 24, 18, 29, 0, 0, time.UTC), jumpAt: -1}
	w.p = &Poller{Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return w.clock }}
	w.p.Wait = func(_ context.Context, d time.Duration) error {
		w.chunks = append(w.chunks, d)
		w.clock = w.clock.Add(d)
		if len(w.chunks) == w.jumpAt {
			w.clock = w.clock.Add(w.jump)
		}
		return nil
	}
	return w
}

func TestWaitIsChunked(t *testing.T) {
	w := newWaiter()
	start := w.clock
	if err := w.p.waitUntil(ctx, start.Add(150*time.Second), 150*time.Second); err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{time.Minute, time.Minute, 30 * time.Second}; !slices.Equal(w.chunks, want) {
		t.Errorf("chunks %v, want %v", w.chunks, want)
	}
}

// The owner's server on 24 Oct: an idle wait of 19h30m, then the lid was
// shut for 20 h. The poll must come at once on resume, not 19 h later.
func TestWaitPollsAtOnceAfterHostSleep(t *testing.T) {
	w := newWaiter()
	w.p.lastGoodAt = w.clock
	wait := 19*time.Hour + 30*time.Minute
	w.jumpAt, w.jump = 2, 20*time.Hour
	if err := w.p.waitUntil(ctx, w.clock.Add(wait), wait); err != nil {
		t.Fatal(err)
	}
	if len(w.chunks) != 2 {
		t.Errorf("waited %d chunks, want 2 (poll on resume)", len(w.chunks))
	}
}

// A jump that leaves the poll not yet due keeps waiting, until the wall
// clock reaches the due time.
func TestWaitShortSleepKeepsDueTime(t *testing.T) {
	w := newWaiter()
	w.p.lastGoodAt = w.clock
	start := w.clock
	w.jumpAt, w.jump = 1, 2*time.Hour
	if err := w.p.waitUntil(ctx, start.Add(3*time.Hour), 3*time.Hour); err != nil {
		t.Fatal(err)
	}
	if !w.clock.Equal(start.Add(3 * time.Hour)) {
		t.Errorf("returned at %v, want %v", w.clock, start.Add(3*time.Hour))
	}
	if n := len(w.chunks); n != 60 {
		t.Errorf("waited %d chunks, want 60 (one hour left after the jump)", n)
	}
}

// A wall clock set back never stretches the wait past its timer length.
func TestWaitClockSetBack(t *testing.T) {
	w := newWaiter()
	start := w.clock
	w.jumpAt, w.jump = 1, -time.Hour
	if err := w.p.waitUntil(ctx, start.Add(5*time.Minute), 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	var total time.Duration
	for _, c := range w.chunks {
		total += c
	}
	if total != 5*time.Minute {
		t.Errorf("waited %v, want 5m", total)
	}
}

func TestWaitStopsWithContext(t *testing.T) {
	w := newWaiter()
	c, cancel := context.WithCancel(ctx)
	cancel()
	w.p.Wait = sleep
	if err := w.p.waitUntil(c, w.clock.Add(time.Hour), time.Hour); err == nil {
		t.Fatal("want ctx error")
	}
}

func TestIdleWaitCappedBeforeDeadline(t *testing.T) {
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	w := &league.World{Gameweeks: map[int]stats.Gameweek{
		9:  {Number: 9, DeadlineAt: now.Add(-24 * time.Hour)},
		10: {Number: 10, DeadlineAt: now.Add(3 * time.Hour)},
	}}
	if got := capAtDeadline(now, 24*time.Hour, w, 30*time.Second); got != 2*time.Hour+30*time.Minute {
		t.Errorf("wait %v, want 2h30m (deadline minus margin)", got)
	}
	// Inside the margin the deadline poll has happened: wait unchanged.
	if got := capAtDeadline(now.Add(160*time.Minute), time.Hour, w, 30*time.Second); got != time.Hour {
		t.Errorf("inside margin: wait %v, want 1h", got)
	}
}

// Kickoff cap: an idle wait never runs past the next kickoff.
func TestIdleWaitCappedAtKickoff(t *testing.T) {
	now := time.Date(2026, 10, 24, 18, 29, 0, 0, time.UTC)
	kickoff := time.Date(2026, 10, 25, 14, 0, 0, 0, time.UTC)
	done := stats.Fixture{Status: stats.FixtureFinished, KickoffAt: ptr(now.Add(-4 * time.Hour))}
	mode, wait := Decide(Config{}, now, []stats.Fixture{done}, stats.Gameweek{BonusAdded: true, DataChecked: true}, &kickoff)
	if mode != stats.ModeIdle || wait != kickoff.Sub(now) {
		t.Errorf("Decide = %s %v, want idle %v", mode, wait, kickoff.Sub(now))
	}
}

// Run end to end: after the first poll the host sleeps 20 h inside the
// wait; the second poll comes on the next chunk.
func TestRunPollsAfterHostSleep(t *testing.T) {
	f := newFixture(t)
	// A 19 h cadence: without wall-clock waits the second poll would
	// take 1,140 more chunks.
	f.p.Config.LiveInterval = 19 * time.Hour
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	waits := 0
	f.p.Wait = func(_ context.Context, d time.Duration) error {
		waits++
		f.advance(d)
		if waits == 1 {
			f.advance(20 * time.Hour)
		}
		if f.p.Metrics.Polls.Value() >= 2 {
			cancel()
		}
		return c.Err()
	}
	done := make(chan error, 1)
	go func() { done <- f.p.Run(c) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cancel()
		t.Fatal("Run did not poll again after the sleep")
	}
	if n := f.p.Metrics.Polls.Value(); n != 2 || waits != 2 {
		t.Errorf("polls %d after %d chunks, want 2 polls after 2 (the sleep, then the cancel)", n, waits)
	}
}
