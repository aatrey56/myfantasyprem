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

// movedFixtureState returns the stored gameweek and status of the moved
// fixture.
func movedFixtureState(t *testing.T, repo stats.Repo) (int, string) {
	t.Helper()
	ctx := context.Background()
	ids, err := repo.ExternalIDs(ctx, demo.ProviderName, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	fs, err := repo.Fixtures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fs {
		if f.ID == ids[movedFixture] {
			return f.Gameweek, f.Status
		}
	}
	t.Fatalf("setup: the moved fixture was never stored")
	return 0, ""
}

// A gameweek 6 fixture stored as scheduled before the outage is missing
// from gameweek 6's live data, but nothing upstream says it moved: the
// fixture list no longer covers gameweek 6. A missing fixture is not
// evidence of a postponement (the payload may be partial), so it stays
// scheduled in gameweek 6, which stays unsettled and is fetched again.
func TestFixtureMissingFromLiveDataIsNotPostponed(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 4,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 4, moved: movedGone, live: map[int]int{}}}
		c.run(t)
		if gw, status := movedFixtureState(t, repo); gw != 6 || status != stats.FixtureScheduled {
			t.Errorf("fixture missing from live data: gameweek %d, status %s; want 6, scheduled", gw, status)
		}
		if c.src.live[6] < 2 {
			t.Errorf("unsettled gameweek 6 fetched %d times over six season refreshes, want it fetched again", c.src.live[6])
		}
	})
}

// Upstream's fixture list moves a gameweek 6 fixture out of the gameweek
// (event null, no kickoff: postponed). That is positive evidence: the
// fixture is postponed, gameweek 6 is final and counts, and it is fetched
// once, not on every refresh.
func TestFinishedGameweekPostponesFixtureMovedOut(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 4,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 4, moved: movedPostponed, live: map[int]int{}}}
		leagueID, userID := c.run(t)
		if gw, status := movedFixtureState(t, repo); gw != 0 || status != stats.FixturePostponed {
			t.Errorf("moved fixture: gameweek %d, status %s; want 0, postponed", gw, status)
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
