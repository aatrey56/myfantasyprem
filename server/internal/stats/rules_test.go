package stats

import (
	"bytes"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Points follow the scoring table FPL publishes in bootstrap-static
// (settings.scoring), not a copy compiled into the server; a table that
// differs from the standard 2026-27 one is used and logged.
func TestIngesterUsesPublishedScoring(t *testing.T) {
	repo := NewMemory()
	var logs bytes.Buffer
	in := NewIngester(repo, "fpl", func() time.Time { return now })
	in.Log = slog.New(slog.NewTextHandler(&logs, nil))
	src := source()
	b, err := src.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	b.Settings.Scoring.Goal.FWD = 5 // FPL changes a forward's goal to 5
	fixtures, _ := src.Fixtures(ctx)
	st, _ := src.GameweekStatus(ctx)
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
	ids := playerIDs(t, repo)
	rows, _ := repo.PlayerFixtures(ctx, 1)
	for _, r := range rows {
		// Forward 1004 scored once: 6 points with 4 a goal, 7 with 5.
		if r.PlayerID == ids["1004"] && r.Points != 7 {
			t.Errorf("forward with a goal = %d points, want 7 under the published table", r.Points)
		}
	}
	if !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "scoring") {
		t.Errorf("no warning for a non-standard table: %q", logs.String())
	}
}

func TestIngesterStandardScoringIsQuiet(t *testing.T) {
	var logs bytes.Buffer
	in := NewIngester(NewMemory(), "fpl", func() time.Time { return now })
	in.Log = slog.New(slog.NewTextHandler(&logs, nil))
	src := source()
	b, _ := src.Bootstrap(ctx)
	fixtures, _ := src.Fixtures(ctx)
	st, _ := src.GameweekStatus(ctx)
	if err := in.Season(ctx, b, fixtures, st); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in.Rules, scoring.StandardRules()) {
		t.Errorf("rules = %+v, want the standard table", in.Rules)
	}
	if strings.Contains(logs.String(), "level=WARN") {
		t.Errorf("warning for the standard table: %q", logs.String())
	}
}
