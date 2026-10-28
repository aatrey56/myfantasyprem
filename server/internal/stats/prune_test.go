package stats

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// Pruning keeps the newest n snapshots per kind and gameweek and every
// final one.
func TestPruneSnapshots(t *testing.T) {
	pool := storetest.New(t)
	repo := NewPostgres(pool)
	ctx := context.Background()
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	gw := func(n int, checked bool) Gameweek {
		return Gameweek{Number: n, Name: fmt.Sprint("Gameweek ", n), DeadlineAt: base, WaiverDeadlineAt: base, TradeDeadlineAt: base, DataChecked: checked}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO seasons (id) VALUES ($1)`, Season); err != nil {
		t.Fatal(err)
	}
	if err := repo.Apply(ctx, Batch{Gameweeks: []Gameweek{gw(1, false), gw(2, false)}}); err != nil {
		t.Fatal(err)
	}
	snap := func(g, i int) Snapshot {
		return Snapshot{Kind: "live", Gameweek: g, FetchedAt: base.Add(time.Duration(i) * time.Minute), Payload: []byte(fmt.Sprintf(`{"n":%d}`, i))}
	}
	for i := range 5 {
		if err := repo.Apply(ctx, Batch{Snapshots: []Snapshot{snap(1, i), snap(2, i)}}); err != nil {
			t.Fatal(err)
		}
	}
	// Gameweek 1 is checked: its next snapshot is final.
	if err := repo.Apply(ctx, Batch{Gameweeks: []Gameweek{gw(1, true)}}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Apply(ctx, Batch{Snapshots: []Snapshot{snap(1, 9)}}); err != nil {
		t.Fatal(err)
	}
	n, err := repo.PruneSnapshots(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	// gw1: 6 snapshots, the newest is final and in the newest 2 -> 4 go.
	// gw2: 5 snapshots, keep 2 -> 3 go.
	if n != 7 {
		t.Fatalf("pruned %d, want 7", n)
	}
	var left, final int
	if err := pool.QueryRow(ctx, `SELECT count(*), count(*) FILTER (WHERE final) FROM stat_snapshots`).Scan(&left, &final); err != nil {
		t.Fatal(err)
	}
	if left != 4 || final != 1 {
		t.Fatalf("left %d (final %d)", left, final)
	}
	// A final snapshot outside the newest n stays.
	for i := 10; i < 13; i++ {
		if err := repo.Apply(ctx, Batch{Snapshots: []Snapshot{snap(1, i)}}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repo.PruneSnapshots(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE final) FROM stat_snapshots WHERE gameweek = 1`).Scan(&final); err != nil {
		t.Fatal(err)
	}
	if final != 4 {
		t.Fatalf("final snapshots of gameweek 1 = %d, want 4", final)
	}
}
