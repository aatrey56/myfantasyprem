package stats

import (
	"errors"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// Fetch tickets belong to a season. Last season's gameweek 1, fetched and
// ingested up to ticket 50, says nothing about this season's gameweek 1:
// its first ticket is 1, an ingest with it applies, a lower one after a
// higher is still refused, and last season's row is left as it was.
func TestLiveTicketsArePerSeason(t *testing.T) {
	pool := storetest.New(t)
	repo := NewPostgres(pool)
	if _, err := pool.Exec(ctx, `INSERT INTO seasons (id) VALUES ('2025-26')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO live_fetched (season, gameweek, issued, ingested) VALUES ('2025-26', 1, 50, 50)`); err != nil {
		t.Fatal(err)
	}
	in, src, st := ingestAll(t, repo)
	live, err := src.EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.LiveTicket(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if first != 1 {
		t.Errorf("this season's first gameweek 1 ticket %d, want 1 (last season's 50 do not count)", first)
	}
	second, err := repo.LiveTicket(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.LiveTicket(ctx, second, live, st); err != nil {
		t.Errorf("ingest with ticket %d: %v, want applied", second, err)
	}
	if _, err := in.LiveTicket(ctx, first, live, st); !errors.Is(err, ErrStale) {
		t.Errorf("ingest with older ticket %d: %v, want ErrStale", first, err)
	}
	var issued, ingested int64
	if err := pool.QueryRow(ctx, `SELECT issued, ingested FROM live_fetched WHERE season = '2025-26' AND gameweek = 1`).Scan(&issued, &ingested); err != nil {
		t.Fatal(err)
	}
	if issued != 50 || ingested != 50 {
		t.Errorf("last season's gameweek 1: issued %d, ingested %d; want 50, 50 untouched", issued, ingested)
	}
}
