package poller_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Gameweek 6 keeps failing (its payload is always incomplete) and 5
// failed once, transiently. Retries rotate by last attempt, so 6 cannot
// starve 5: before the fix the highest failed gameweek was always retried
// first and 5 was never fetched again.
func TestPersistentlyFailingGameweekDoesNotStarveAnother(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 4, polls: 6,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 4,
				zeroed: true, failFirst: 5, live: map[int]int{}}}
		leagueID, _ := c.run(t)
		ctx := context.Background()
		if c.src.live[5] != 2 {
			t.Errorf("gameweek 5 fetched %d times over six polls, want twice (failed once, then settled); calls %v", c.src.live[5], c.src.live)
		}
		if lines, _ := repo.PlayerGameweeks(ctx, 5); len(lines) == 0 {
			t.Error("gameweek 5 has no lines: its retry was starved")
		}
		g, err := leagues.Repo().Game(ctx, leagueID)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range g.MatchupsIn(5) {
			if m.State != scoring.StateFinal {
				t.Errorf("gameweek 5 matchup %s: state %s, want final", m.ID, m.State)
			}
		}
		if c.src.live[6] < 3 {
			t.Errorf("gameweek 6 fetched %d times, want it still retried", c.src.live[6])
		}
	})
}
