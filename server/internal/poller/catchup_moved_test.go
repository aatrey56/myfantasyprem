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

// Gameweek 6's live data lists every player at 0 minutes although its
// fixtures say finished with scores: matches were played, so the payload
// is wrong, not a voided gameweek. It is rejected (no lines stored, its
// matchups not final) and fetched again with backoff.
func TestAllZeroStatsForPlayedFixturesAreRejected(t *testing.T) {
	eachStore(t, func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		c := outageCase{repo: repo, leagues: leagues, users: users, seed: 4,
			src: &outage{StatsSource: demo.Source(slog.New(slog.NewTextHandler(io.Discard, nil))), current: 4, zeroed: true, live: map[int]int{}}}
		leagueID, _ := c.run(t)
		ctx := context.Background()
		if lines, _ := repo.PlayerGameweeks(ctx, 6); len(lines) != 0 {
			t.Errorf("all-zero gameweek 6 stored %d lines, want the payload rejected", len(lines))
		}
		if c.src.live[6] < 2 {
			t.Errorf("rejected gameweek 6 fetched %d times over six season refreshes, want it fetched again", c.src.live[6])
		}
		g, err := leagues.Repo().Game(ctx, leagueID)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range g.MatchupsIn(6) {
			if m.State == scoring.StateFinal {
				t.Errorf("gameweek 6 matchup %s final on rejected stats", m.ID)
			}
		}
	})
}
