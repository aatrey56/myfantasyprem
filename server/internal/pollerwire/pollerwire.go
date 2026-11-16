// Package pollerwire builds the stats poller with the hooks every caller
// needs: cmd/api's running poller and cmd/admin's backfill share it, so a
// backfill of the current gameweek notifies as a poll would, and the two
// cannot drift.
package pollerwire

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Options configure New.
type Options struct {
	// Source is "demo" for the embedded demo world (never calls FPL);
	// anything else reads FPL at FPLBaseURL with FPLUserAgent, stored under
	// that provider name ("fpl" when empty).
	Source       string
	FPLBaseURL   string
	FPLUserAgent string
	Leagues      league.Repo
	Stats        stats.Repo
	// Notify hears the scorer's changes (matchups.updated, corrections).
	Notify league.Notifier
	// Notifier delivers the Live (goals, assists, clean sheets) and News
	// (injuries, suspensions) notifications.
	Notifier *notifications.Notifier
	Log      *slog.Logger
}

// New returns the poller: its scorer carries Notify and the Live hook, and
// its News hook reports player news. Gameweeks before the current one are
// rescored quietly (Poller.Quiet), so a backfill of an old gameweek stays
// quiet and one of the current gameweek does not.
func New(o Options) *poller.Poller {
	var src provider.StatsSource
	providerName := o.Source
	if providerName == "" {
		providerName = "fpl"
	}
	if o.Source == demo.ProviderName {
		src = demo.Source(o.Log)
		providerName = demo.ProviderName
	} else {
		src = fpl.New(fpl.Options{BaseURL: o.FPLBaseURL, UserAgent: o.FPLUserAgent, Logger: o.Log})
	}
	scorer := &league.Scorer{Leagues: o.Leagues, Stats: o.Stats, Rules: scoring.StandardLineup(), Log: o.Log, Notify: o.Notify,
		Live: notifications.NewLive(o.Notifier, o.Log)}
	p := poller.New(src, stats.NewIngester(o.Stats, providerName, time.Now), o.Stats, scorer, poller.Config{}, o.Log)
	p.News = &notifications.News{Leagues: o.Leagues, Notifier: o.Notifier, Log: o.Log}
	return p
}

// Backfiller fills in, for a re-sync (mirror.Service.Backfill), the
// finished gameweeks the poller has not loaded. It has its own poller:
// the running one's catch-up state is its loop's alone. Calls are
// serialised here; the store's per-gameweek locks and fetch tickets
// serialise its ingests with the running poller's.
type Backfiller struct {
	mu sync.Mutex
	p  *poller.Poller
}

// NewBackfiller returns a Backfiller whose poller is New(o).
func NewBackfiller(o Options) *Backfiller { return &Backfiller{p: New(o)} }

// MissingFinished lists the finished gameweeks up to the current one
// whose stats are missing or unsettled (Poller.MissingFinished).
func (b *Backfiller) MissingFinished(ctx context.Context) ([]int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.p.MissingFinished(ctx)
}

// Backfill fetches, ingests and rescores finished gameweek gw
// (Poller.Backfill; quiet before the current gameweek).
func (b *Backfiller) Backfill(ctx context.Context, gw int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _, err := b.p.Backfill(ctx, gw, false)
	return err
}
