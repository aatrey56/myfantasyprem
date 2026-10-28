package notifications

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// Backoff is the wait before each retry; past the last, a delivery is
// given up.
var Backoff = []time.Duration{30 * time.Second, 2 * time.Minute, 10 * time.Minute, 30 * time.Minute, 2 * time.Hour}

// Dispatcher drains the push outbox.
type Dispatcher struct {
	Store      Store
	Deliverers map[string]Deliverer // by Subscription.Kind
	Now        func() time.Time
	Log        *slog.Logger
	// Lease is how long a claimed delivery stays hidden from other
	// dispatchers; it comes back if this one dies mid-send.
	Lease time.Duration
	Batch int
}

// RunOnce sends every due delivery and reports how many went out.
func (d *Dispatcher) RunOnce(ctx context.Context) (int, error) {
	now := d.Now()
	due, err := d.Store.Due(ctx, now, max(d.Lease, time.Minute), max(d.Batch, 1))
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, x := range due {
		err := d.deliver(ctx, x)
		switch {
		case err == nil:
			sent++
			err = d.Store.Delivered(ctx, x.ID)
		case errors.Is(err, ErrGone):
			d.log().InfoContext(ctx, "push subscription gone; dropping it", "subscription", x.Subscription.ID)
			err = d.Store.Drop(ctx, x.Subscription.ID)
		case errors.Is(err, ErrDisabled):
			err = d.Store.Delivered(ctx, x.ID)
		default:
			var next time.Time
			if x.Attempts < len(Backoff) {
				next = now.Add(Backoff[x.Attempts])
			}
			d.log().WarnContext(ctx, "push failed", "subscription", x.Subscription.ID, "attempt", x.Attempts+1,
				"giving_up", next.IsZero(), "err", err)
			err = d.Store.Retry(ctx, x.ID, next)
		}
		if err != nil {
			return sent, err
		}
	}
	return sent, nil
}

func (d *Dispatcher) deliver(ctx context.Context, x Delivery) error {
	del, ok := d.Deliverers[x.Subscription.Kind]
	if !ok {
		return ErrDisabled
	}
	return del.Deliver(ctx, x.Subscription, PayloadOf(x.Notification))
}

// Run drains the outbox every interval until ctx ends.
func (d *Dispatcher) Run(ctx context.Context, interval time.Duration) error {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		for {
			n, err := d.RunOnce(ctx)
			if err != nil {
				d.log().ErrorContext(ctx, "push dispatch", "err", err)
			}
			if err != nil || n < max(d.Batch, 1) {
				break
			}
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func (d *Dispatcher) log() *slog.Logger {
	if d.Log == nil {
		return slog.Default()
	}
	return d.Log
}
