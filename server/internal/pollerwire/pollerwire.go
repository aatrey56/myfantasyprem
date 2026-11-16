// Package pollerwire builds the stats poller with the hooks every caller
// needs: cmd/api's running poller and cmd/admin's backfill share it, so a
// backfill of the current gameweek notifies as a poll would, and the two
// cannot drift.
package pollerwire

import (
	"log/slog"
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
