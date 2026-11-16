package poller_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
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

// stamped serves live data fetched at a fixed time; before, if set, runs
// once inside the first EventLive call, before it returns (another process
// ingesting in the meantime); extraGoals is added to one scorer's goals.
type stamped struct {
	provider.StatsSource
	at         time.Time
	extraGoals int
	before     func()
}

func (s *stamped) EventLive(ctx context.Context, gw int) (*provider.EventLive, error) {
	if f := s.before; f != nil {
		s.before = nil
		f()
	}
	l, err := s.StatsSource.EventLive(ctx, gw)
	if err != nil {
		return nil, err
	}
	cp := *l
	cp.FetchedAt = s.at
	cp.Players = append([]provider.PlayerLive(nil), l.Players...)
	for i, pl := range cp.Players {
		if pl.Minutes > 0 && len(pl.Explain) == 1 {
			cp.Players[i].GoalsScored += s.extraGoals
			break
		}
	}
	return &cp, nil
}

// An admin backfill fetches gameweek 5 and, before it ingests, the running
// poller fetches and ingests newer corrected data. The backfill's older
// snapshot must not overwrite the newer rows: it is refused
// (stats.ErrStale) and the stored rows are the poller's. The backfill's
// clock runs ahead (12:05 against the poller's 12:00): fetch tickets, not
// clocks, say which snapshot is newer.
func TestBackfillWithOlderSnapshotCannotOverwriteNewerIngest(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, _ auth.Service) {
		ctx := context.Background()
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		base := &outage{StatsSource: demo.Source(quiet), current: 7, live: map[int]int{}}
		sc := &league.Scorer{Leagues: leagues.Repo(), Stats: repo, Rules: scoring.StandardLineup(), Log: quiet}
		t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
		newPoller := func(src provider.StatsSource) *poller.Poller {
			p := poller.New(src, stats.NewIngester(repo, demo.ProviderName, time.Now), repo, sc, poller.Config{}, quiet)
			p.Sleep = func(context.Context, time.Duration) error { return nil }
			return p
		}
		newer := newPoller(&stamped{StatsSource: base, at: t0})
		goals := func() int {
			rows, err := repo.PlayerFixtures(ctx, 5)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, r := range rows {
				n += r.Stats.Goals
			}
			return n
		}
		want := -1
		old := &stamped{StatsSource: base, at: t0.Add(5 * time.Minute), extraGoals: 3, before: func() {
			if _, _, err := newer.Backfill(ctx, 5, false); err != nil {
				t.Errorf("newer ingest: %v", err)
			}
			want = goals()
		}}
		_, _, err := newPoller(old).Backfill(ctx, 5, false)
		if !errors.Is(err, stats.ErrStale) {
			t.Errorf("backfill with the older snapshot: err %v, want stats.ErrStale", err)
		}
		got := goals()
		if got != want {
			t.Errorf("gameweek 5 rows have %d goals, want the newer snapshot's %d", got, want)
		}
	})
}

// The clock steps back between two polls (NTP, a VM restore): the second
// snapshot is stamped 11:55, before the first's 12:00. Both are fetched in
// turn, so the second is the newer and is ingested, not refused.
func TestBackwardClockStepDoesNotRefuseTheNextPoll(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, _ auth.Service) {
		ctx := context.Background()
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		base := &outage{StatsSource: demo.Source(quiet), current: 7, live: map[int]int{}}
		sc := &league.Scorer{Leagues: leagues.Repo(), Stats: repo, Rules: scoring.StandardLineup(), Log: quiet}
		t0 := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
		for i, at := range []time.Time{t0, t0.Add(-5 * time.Minute)} {
			p := poller.New(&stamped{StatsSource: base, at: at}, stats.NewIngester(repo, demo.ProviderName, time.Now), repo, sc, poller.Config{}, quiet)
			p.Sleep = func(context.Context, time.Duration) error { return nil }
			if _, _, err := p.Backfill(ctx, 5, false); err != nil {
				t.Errorf("poll %d fetched at %s: %v, want it ingested", i+1, at.Format(time.Kitchen), err)
			}
		}
	})
}

// A retry of an ingest with the ticket it already applied (ingestLive
// retries after refreshing bootstrap) is the same snapshot: it is applied
// again, not refused as stale.
func TestEqualTicketRetryIsIdempotent(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, _ auth.Service) {
		ctx := context.Background()
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		src := &outage{StatsSource: demo.Source(quiet), current: 7, live: map[int]int{}}
		sc := &league.Scorer{Leagues: leagues.Repo(), Stats: repo, Rules: scoring.StandardLineup(), Log: quiet}
		p := poller.New(src, stats.NewIngester(repo, demo.ProviderName, time.Now), repo, sc, poller.Config{}, quiet)
		p.Sleep = func(context.Context, time.Duration) error { return nil }
		if _, _, err := p.Backfill(ctx, 5, false); err != nil {
			t.Fatal(err)
		}
		ticket, err := repo.LiveTicket(ctx, 5)
		if err != nil {
			t.Fatal(err)
		}
		live, err := src.EventLive(ctx, 5)
		if err != nil {
			t.Fatal(err)
		}
		st := &provider.GameweekStatus{Current: 5, BonusAdded: true, DataChecked: true}
		in := stats.NewIngester(repo, demo.ProviderName, time.Now)
		for i := range 2 {
			if _, err := in.LiveTicket(ctx, ticket, live, st); err != nil {
				t.Errorf("ingest %d with ticket %d: %v, want applied", i+1, ticket, err)
			}
		}
		// An older ticket after it is stale.
		if _, err := in.LiveTicket(ctx, ticket-1, live, st); !errors.Is(err, stats.ErrStale) {
			t.Errorf("ingest with ticket %d after %d: %v, want stats.ErrStale", ticket-1, ticket, err)
		}
	})
}
