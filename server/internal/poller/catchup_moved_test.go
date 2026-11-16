package poller_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// A gameweek 6 fixture stored as scheduled before the outage was moved
// out of the gameweek upstream: no later payload has it. Once FPL marks
// gameweek 6 finished, the fixture is postponed, so the gameweek is final
// and counts, and it is fetched once, not on every refresh.
func TestFinishedGameweekPostponesFixtureMovedOut(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 4,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 4, moved: true, live: map[int]int{}}}
		ctx := context.Background()
		leagueID, userID := c.run(t)
		ids, err := repo.ExternalIDs(ctx, demo.ProviderName, "fixture")
		if err != nil {
			t.Fatal(err)
		}
		fs, err := repo.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range fs {
			if f.ID == ids[movedFixture] {
				found = true
				if f.Status != stats.FixturePostponed || f.Gameweek != 0 {
					t.Errorf("moved fixture: gameweek %d, status %s; want 0, postponed", f.Gameweek, f.Status)
				}
			}
		}
		if !found {
			t.Fatalf("setup: the moved fixture was never stored")
		}
		if c.src.live[6] != 1 {
			t.Errorf("gameweek 6 fetched %d times over six season refreshes, want 1", c.src.live[6])
		}
		checkCounted(t, c, leagueID, userID)
	})
}

// Gameweek 6 was voided: every player has 0 minutes, so the ingest
// stores lines but no rows. It is fetched once, not on every refresh
// (each with a full rescore of every league).
func TestVoidedFinishedGameweekIsFetchedOnce(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 4,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 4, voided: true, live: map[int]int{}}}
		c.run(t)
		if rows, _ := repo.PlayerFixtures(context.Background(), 6); len(rows) != 0 {
			t.Fatalf("setup: voided gameweek 6 has %d rows", len(rows))
		}
		if c.src.live[6] != 1 || c.src.live[5] != 1 {
			t.Errorf("over six season refreshes: live calls %v, want gameweeks 5 and 6 once each", c.src.live)
		}
	})
}
