package stats

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// A season refresh (bootstrap and fixtures) fetched before a newer live
// ingest of a gameweek commits after it (a re-sync's backfill poller
// racing the running one). Its fixtures of that gameweek keep the live
// status and scores; their schedule still updates. A gameweek with no
// newer live ingest takes the season's fixtures as they are.
func TestSeasonNeverRegressesNewerLiveFixtures(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, src, st := ingestAll(t, repo)
		fixtures, err := src.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b, err := src.Bootstrap(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// The season refresh starts: the live tickets issued so far.
		asOf, err := repo.LiveIssued(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// Its snapshot is older: every match unplayed, and kickoffs moved.
		kick := time.Date(2026, 1, 1, 15, 0, 0, 0, time.UTC)
		stale := make([]provider.Fixture, len(fixtures))
		for i, f := range fixtures {
			f.Status, f.HomeScore, f.AwayScore, f.KickoffAt = provider.FixtureScheduled, nil, nil, &kick
			stale[i] = f
		}
		// Meanwhile the running poller fetches and ingests gameweek 2.
		ticket, err := repo.LiveTicket(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		live, err := src.EventLive(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.LiveTicket(ctx, ticket, live, st); err != nil {
			t.Fatal(err)
		}
		before, err := repo.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := in.SeasonAsOf(ctx, asOf, b, stale, st); err != nil {
			t.Fatal(err)
		}
		after, err := repo.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		was := map[string]Fixture{}
		for _, f := range before {
			was[f.ID] = f
		}
		seen := map[int]bool{}
		for _, f := range after {
			seen[f.Gameweek] = true
			if f.KickoffAt == nil || !f.KickoffAt.Equal(kick) {
				t.Errorf("gameweek %d fixture %s kickoff %v, want the season's %v", f.Gameweek, f.ID, f.KickoffAt, kick)
			}
			w := was[f.ID]
			switch f.Gameweek {
			case 2:
				if f.Status != w.Status || !sameScore(f.HomeScore, w.HomeScore) || !sameScore(f.AwayScore, w.AwayScore) {
					t.Errorf("gameweek 2 fixture %s: %s %v-%v, want the newer live %s %v-%v",
						f.ID, f.Status, f.HomeScore, f.AwayScore, w.Status, w.HomeScore, w.AwayScore)
				}
			default:
				if f.Status != FixtureScheduled || f.HomeScore != nil {
					t.Errorf("gameweek %d fixture %s: %s %v, want the season's scheduled, no score", f.Gameweek, f.ID, f.Status, f.HomeScore)
				}
			}
		}
		if !seen[1] || !seen[2] {
			t.Fatalf("fixtures cover gameweeks %v, want 1 and 2", seen)
		}
		if w := was[firstIn(before, 2)]; w.Status == FixtureScheduled {
			t.Fatalf("gameweek 2 fixture %s was scheduled before the refresh: the test proves nothing", w.ID)
		}
	})
}

func sameScore(a, b *int) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func firstIn(fxs []Fixture, gw int) string {
	for _, f := range fxs {
		if f.Gameweek == gw {
			return f.ID
		}
	}
	return ""
}

// FPL postpones a fixture by clearing its kickoff and keeping its event
// number. A season refresh that carries this, racing a newer live ingest of
// that gameweek, must still postpone the unplayed fixture; only played
// fixtures keep their live state.
func TestSeasonPostponesUnplayedFixtureDespiteNewerLive(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, src, st := ingestAll(t, repo)
		fixtures, err := src.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b, err := src.Bootstrap(ctx)
		if err != nil {
			t.Fatal(err)
		}
		// A gameweek 2 match the season still lists as unplayed.
		target := ""
		unplayed := make([]provider.Fixture, len(fixtures))
		for i, f := range fixtures {
			if f.Gameweek == 2 && target == "" {
				target = f.ExternalID
				f.Status, f.HomeScore, f.AwayScore = provider.FixtureScheduled, nil, nil
			}
			unplayed[i] = f
		}
		if target == "" {
			t.Fatal("no gameweek 2 fixture")
		}
		if err := in.Season(ctx, b, unplayed, st); err != nil {
			t.Fatal(err)
		}
		asOf, err := repo.LiveIssued(ctx)
		if err != nil {
			t.Fatal(err)
		}
		ticket, err := repo.LiveTicket(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		live, err := src.EventLive(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		// The live payload also lists it unplayed.
		for i, f := range live.Fixtures {
			if f.ExternalID == target {
				live.Fixtures[i].Status = provider.FixtureScheduled
				live.Fixtures[i].HomeScore, live.Fixtures[i].AwayScore = nil, nil
			}
		}
		if _, err := in.LiveTicket(ctx, ticket, live, st); err != nil {
			t.Fatal(err)
		}
		season := make([]provider.Fixture, len(unplayed))
		for i, f := range unplayed {
			if f.ExternalID == target {
				f.Status, f.KickoffAt = provider.FixturePostponed, nil
			}
			season[i] = f
		}
		if err := in.SeasonAsOf(ctx, asOf, b, season, st); err != nil {
			t.Fatal(err)
		}
		after, err := repo.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		postponed := 0
		for _, f := range after {
			if f.Gameweek != 2 {
				continue
			}
			if f.Status == FixturePostponed {
				postponed++
				if f.KickoffAt != nil {
					t.Errorf("postponed fixture %s kickoff %v, want none", f.ID, f.KickoffAt)
				}
			} else if f.KickoffAt == nil {
				t.Errorf("fixture %s is %s with no kickoff, want postponed", f.ID, f.Status)
			}
		}
		if postponed != 1 {
			t.Errorf("%d postponed gameweek 2 fixtures, want the 1 the season postponed", postponed)
		}
	})
}
