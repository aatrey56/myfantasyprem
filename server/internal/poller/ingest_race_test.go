package poller_test

import (
	"context"
	"io"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// diffHook runs between an ingest's read of a gameweek's stored rows (the
// diff) and its write: another ingest that lands in between.
type diffHook struct {
	stats.Repo
	gw      int
	between func()
}

func (d *diffHook) PlayerFixtures(ctx context.Context, gw int) ([]stats.PlayerFixture, error) {
	rows, err := d.Repo.PlayerFixtures(ctx, gw)
	if f := d.between; f != nil && gw == d.gw {
		d.between = nil
		f()
	}
	return rows, err
}

// The live poll diffs its snapshot A of gameweek 5 against the stored
// rows (also A: nothing changed); then an admin backfill that took an
// older ticket writes snapshot B (a scorer with 3 more goals), rescores
// and clears its mark; then the poll writes A. Before the fix the poll's
// diff was still the empty one from before B: no rescore and no mark, so
// the rows were A and the scores B for good. The diff is now taken against
// the rows the write replaces, so the scores are A's.
func TestIngestDiffIsAgainstTheRowsItReplaces(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		ctx := context.Background()
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		const gw = 5
		base := &outage{StatsSource: demo.Source(quiet), current: gw, live: map[int]int{}}
		res, err := demo.Seed(ctx, demo.Deps{Auth: users, Leagues: leagues, Stats: repo, Log: quiet, Now: time.Now})
		if err != nil {
			t.Fatal(err)
		}
		sc := &league.Scorer{Leagues: leagues.Repo(), Stats: repo, Rules: scoring.StandardLineup(), Log: quiet}
		// scores lists each team's points and bench points, in team order.
		scores := func() [][3]any {
			g, err := leagues.Repo().Game(ctx, res.LeagueID)
			if err != nil {
				t.Fatal(err)
			}
			var out [][3]any
			for _, s := range g.Scores {
				if s.Gameweek == gw {
					out = append(out, [3]any{s.TeamID, s.Points, s.BenchPoints})
				}
			}
			slices.SortFunc(out, func(a, b [3]any) int { return strings.Compare(a[0].(string), b[0].(string)) })
			return out
		}
		newPoller := func(src provider.StatsSource, in stats.Repo) *poller.Poller {
			p := poller.New(src, stats.NewIngester(in, demo.ProviderName, time.Now), repo, sc, poller.Config{}, quiet)
			p.Sleep = func(context.Context, time.Duration) error { return nil }
			return p
		}
		hook := &diffHook{Repo: repo, gw: gw}
		live := newPoller(base, hook)
		live.Tick(ctx) // settles the seed's marks and the startup catch-up

		// The backfill takes its ticket and fetches B, then waits.
		fetched, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			src := &blocking{StatsSource: &stamped{StatsSource: base, extraGoals: 3}, gw: gw, fetched: fetched, release: release}
			_, _, err := newPoller(src, repo).Backfill(ctx, gw, true)
			done <- err
		}()
		<-fetched
		// The poll takes a newer ticket, fetches A and diffs; B lands
		// between its diff and its write.
		hook.between = func() {
			close(release)
			if err := <-done; err != nil {
				t.Errorf("setup: backfill of B: %v", err)
			}
		}
		live.Tick(ctx)
		if hook.between != nil {
			t.Fatal("setup: the backfill of B never ran")
		}
		live.Tick(ctx) // a later poll of the same A
		got := scores()
		if _, err := sc.Recompute(ctx, gw, nil, true); err != nil {
			t.Fatal(err)
		}
		want := scores()
		if len(want) == 0 {
			t.Fatal("setup: no gameweek 5 scores")
		}
		if !slices.Equal(got, want) {
			t.Errorf("gameweek 5 scores after the race:\n got  %v\n want %v (scored on the stored snapshot A)", got, want)
		}
	})
}

// blocking signals fetched once its gameweek's live data is fetched and
// returns it only after release.
type blocking struct {
	provider.StatsSource
	gw               int
	fetched, release chan struct{}
}

func (b *blocking) EventLive(ctx context.Context, gw int) (*provider.EventLive, error) {
	l, err := b.StatsSource.EventLive(ctx, gw)
	if gw == b.gw {
		close(b.fetched)
		<-b.release
	}
	return l, err
}
