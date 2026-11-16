package poller_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// outageCase polls src at its current gameweek, seeds the demo league over
// gameweeks 1 to seed, then restarts with gameweek 7 current and polls six
// times, each with a season refresh; src.live counts the restart's calls.
type outageCase struct {
	repo    stats.Repo
	leagues *league.Service
	users   auth.Service
	src     *outage
	seed    int
}

func eachStore(t *testing.T, run func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service)) {
	t.Run("memory", func(t *testing.T) {
		run(t, stats.NewMemory(), league.NewService(league.NewMemory(), time.Now), auth.NewMemory(time.Now))
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		run(t, stats.NewPostgres(pool), league.NewService(league.NewPostgres(pool), time.Now), auth.NewPostgres(pool, time.Now))
	})
}

func (c outageCase) run(t *testing.T) (leagueID, userID string) {
	t.Helper()
	ctx := context.Background()
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	clock := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	sc := &league.Scorer{Leagues: c.leagues.Repo(), Stats: c.repo, Rules: scoring.StandardLineup(), Log: quiet}
	newPoller := func() *poller.Poller {
		p := poller.New(c.src, stats.NewIngester(c.repo, demo.ProviderName, now), c.repo, sc, poller.Config{}, quiet)
		p.Now = now
		p.Sleep = func(context.Context, time.Duration) error { return nil }
		return p
	}
	before := newPoller()
	for range 6 {
		before.Tick(ctx)
	}
	res, err := demo.SeedLeague(ctx, demo.Deps{Auth: c.users, Leagues: c.leagues, Stats: c.repo, Log: quiet, Now: now}, c.seed)
	if err != nil {
		t.Fatal(err)
	}
	c.src.current = 7
	clear(c.src.live)
	p := newPoller()
	for range 6 {
		clock = clock.Add(7 * time.Hour) // past BootstrapInterval: every poll refreshes the season
		p.Tick(ctx)
	}
	return res.LeagueID, res.UserIDs[0]
}

// checkCounted fails unless every matchup of gameweeks 1-6 is final and
// the table counts all six for every team.
func checkCounted(t *testing.T, c outageCase, leagueID, userID string) {
	t.Helper()
	ctx := context.Background()
	g, err := c.leagues.Repo().Game(ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	for gw := 1; gw <= 6; gw++ {
		for _, m := range g.MatchupsIn(gw) {
			if m.State != scoring.StateFinal {
				t.Errorf("gameweek %d matchup %s: state %s, want final", gw, m.ID, m.State)
			}
		}
	}
	l, err := c.leagues.Get(ctx, userID, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	w, err := league.LoadWorld(ctx, c.repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range league.Standings(l, g, w) {
		if r.Played != 6 {
			t.Errorf("team %s: played %d, want 6 (gameweeks 1-6 finished upstream)", r.TeamID, r.Played)
		}
	}
}

// The server polled gameweek 5 while its second half of fixtures was
// still in play and went down; FPL then finished 5 and 6. Gameweek 5 has
// rows and lines, but its stored fixtures say live, so it must be fetched
// again: otherwise its matchups stay live and the table drops it.
func TestRestartRefetchesPartlyIngestedFinishedGameweek(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 5,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 5, partial: true, live: map[int]int{}}}
		leagueID, userID := c.run(t)
		if c.src.live[5] != 1 || c.src.live[6] != 1 {
			t.Errorf("after the restart: live calls %v, want gameweeks 5 (partly ingested) and 6 (missing) once each", c.src.live)
		}
		checkCounted(t, c, leagueID, userID)
	})
}
