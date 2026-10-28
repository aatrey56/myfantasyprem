package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Points are scored with the table upstream publishes, so the breakdown a
// player's fixture shows must come from that same table: its lines sum to
// the points even when the table is not FPL's standard one (Codex review).
func TestBreakdownSumsToPointsUnderAPublishedTable(t *testing.T) {
	ctx := context.Background()
	repo := stats.NewMemory()
	src := demo.Source(testOptions().Logger)
	b, err := src.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// A table that differs from the standard one on lines every gameweek
	// has: appearances and goals.
	b.Settings.Scoring.Minutes60Plus = 3
	b.Settings.Scoring.Goal.FWD = 7
	b.Settings.Scoring.Goal.MID = 6
	fixtures, _ := src.Fixtures(ctx)
	st, _ := src.GameweekStatus(ctx)
	in := stats.NewIngester(repo, demo.ProviderName, time.Now)
	if err := in.Season(ctx, b, fixtures, st); err != nil {
		t.Fatal(err)
	}
	live, err := src.EventLive(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.Live(ctx, live, st); err != nil {
		t.Fatal(err)
	}
	// The API reads through a fresh World, as another instance would.
	w, err := league.LoadWorld(ctx, repo, 1)
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for id := range w.Rows[1] {
		for _, fx := range playerFixtures(w, id, 1) {
			sum := 0
			for _, l := range fx.Breakdown {
				sum += l.Points
			}
			if sum != fx.Points {
				t.Errorf("player %s fixture %s: breakdown sums to %d, points %d", id, fx.FixtureId, sum, fx.Points)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no fixtures checked")
	}
}
