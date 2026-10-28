package notifications

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// fake is a Deliverer answering from a script.
type fake struct {
	mu   sync.Mutex
	errs []error
	sent []Payload
}

func (f *fake) Deliver(_ context.Context, _ Subscription, p Payload) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var err error
	if len(f.errs) > 0 {
		err, f.errs = f.errs[0], f.errs[1:]
	}
	if err == nil {
		f.sent = append(f.sent, p)
	}
	return err
}

func TestDispatcherRetriesWithBackoffAndDropsGone(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		f := &fake{errs: []error{errors.New("503"), errors.New("timeout"), nil}}
		d := &Dispatcher{Store: s, Deliverers: map[string]Deliverer{KindWebPush: f}, Now: c.now, Batch: 10}
		sub, _ := s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/1"})
		_, _, _ = s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Title: "Goal", Key: "g"}, true)

		run := func() int {
			n, err := d.RunOnce(ctx)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
		if run() != 0 { // 503
			t.Fatal("sent on failure")
		}
		c.t = c.t.Add(Backoff[0] - time.Second)
		if run() != 0 || len(f.errs) != 2 {
			t.Fatal("retried before the backoff")
		}
		c.t = c.t.Add(time.Second)
		run() // timeout
		c.t = c.t.Add(Backoff[1])
		if run() != 1 || len(f.sent) != 1 || f.sent[0].Title != "Goal" {
			t.Fatalf("third try: %+v", f.sent)
		}
		if run() != 0 {
			t.Fatal("delivered twice")
		}

		// 410 drops the subscription and its queue.
		f.errs = []error{ErrGone}
		_, _, _ = s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Key: "g2"}, true)
		_, _, _ = s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Key: "g3"}, true)
		d.Batch = 1
		run()
		c.t = c.t.Add(24 * time.Hour)
		if run() != 0 {
			t.Fatal("sent to a dropped subscription")
		}
		again, _ := s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/1"})
		if again.ID == sub.ID {
			t.Fatal("gone subscription still stored")
		}
	})
}

func TestDispatcherGivesUpAfterBackoff(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	s := NewMemory(c.now)
	f := &fake{}
	for range len(Backoff) + 1 {
		f.errs = append(f.errs, errors.New("500"))
	}
	d := &Dispatcher{Store: s, Deliverers: map[string]Deliverer{KindWebPush: f}, Now: c.now, Batch: 10}
	_, _ = s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/1"})
	_, _, _ = s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Key: "g"}, true)
	for range len(Backoff) + 1 {
		_, _ = d.RunOnce(ctx)
		c.t = c.t.Add(3 * time.Hour)
	}
	if len(f.errs) != 0 {
		t.Fatalf("%d attempts left unused", len(f.errs))
	}
	if due, _ := s.Due(ctx, c.t.Add(24*time.Hour), time.Minute, 10); len(due) != 0 {
		t.Fatal("still queued after giving up")
	}
}

func TestExpoDelivererIsOffUntilEnabled(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	s := NewMemory(c.now)
	d := &Dispatcher{Store: s, Deliverers: map[string]Deliverer{KindExpo: Expo{}}, Now: c.now, Batch: 10}
	_, _ = s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindExpo, Endpoint: "ExponentPushToken[abc]"})
	_, _, _ = s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Key: "g"}, true)
	if n, err := d.RunOnce(ctx); n != 0 || err != nil {
		t.Fatalf("sent %d %v", n, err)
	}
	if due, _ := s.Due(ctx, c.t.Add(time.Hour), time.Minute, 10); len(due) != 0 {
		t.Fatal("disabled delivery kept for retry")
	}
	if err := (Expo{}).Deliver(ctx, Subscription{}, Payload{}); !errors.Is(err, ErrDisabled) {
		t.Fatalf("err %v", err)
	}
}
