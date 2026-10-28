package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// The in-memory stores cannot grow without bound.

func TestMemoryIdempotencyIsBounded(t *testing.T) {
	m := newIdempotencyStore()
	m.max = 5
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for i := range 50 {
		claim, err := m.Begin(context.Background(), "usr_a", fmt.Sprintf("key-%04d", i), "fp", now.Add(time.Duration(i)*time.Second))
		if err != nil || claim.State != IdemNew {
			t.Fatalf("begin %d = %+v %v", i, claim, err)
		}
		_ = m.Finish(context.Background(), "usr_a", fmt.Sprintf("key-%04d", i), 201, http.Header{}, []byte("{}"))
	}
	m.mu.Lock()
	n := len(m.entries)
	m.mu.Unlock()
	if n > 5 {
		t.Fatalf("store holds %d entries, cap 5", n)
	}
	// The newest key still replays.
	claim, _ := m.Begin(context.Background(), "usr_a", "key-0049", "fp", now.Add(time.Minute))
	if claim.State != IdemReplay {
		t.Fatalf("newest key = %+v", claim)
	}
}

func TestPostgresIdempotencyPrune(t *testing.T) {
	pool := storetest.New(t)
	srv, _ := newTestServerOn(pool)
	uid := userOf(t, srv, testToken)
	p := NewPostgresIdempotency(pool)
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	for i, created := range []time.Time{at.Add(-25 * time.Hour), at.Add(-time.Hour)} {
		if _, err := p.Begin(context.Background(), uid, fmt.Sprintf("key-%04d", i), "fp", created); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := p.Prune(context.Background(), at.Add(-24*time.Hour)); err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if claim, _ := p.Begin(context.Background(), uid, "key-0001", "fp", at); claim.State != IdemInFlight {
		t.Fatalf("the recent record went: %+v", claim)
	}
}
