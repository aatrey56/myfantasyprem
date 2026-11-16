package pollerwire

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// The API's poller and the admin backfill are both built here, so both
// carry the Live hook (goal, assist and clean-sheet notifications) and the
// News hook: a backfill of the current gameweek that stored its changes
// without them would leave the running poller nothing to notify about.
func TestNewInstallsLiveAndNewsHooks(t *testing.T) {
	n := notifications.NewNotifier(notifications.NewMemory(time.Now))
	p := New(Options{Source: "demo", Leagues: league.NewMemory(), Stats: stats.NewMemory(), Notifier: n,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	sc, ok := p.Scorer.(*league.Scorer)
	if !ok {
		t.Fatalf("scorer %T, want *league.Scorer", p.Scorer)
	}
	if _, ok := sc.Live.(*notifications.Live); !ok {
		t.Errorf("scorer Live hook %T, want *notifications.Live", sc.Live)
	}
	if _, ok := p.News.(*notifications.News); !ok {
		t.Errorf("poller News hook %T, want *notifications.News", p.News)
	}
}
