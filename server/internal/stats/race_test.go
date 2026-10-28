package stats

import (
	"context"
	"sync"
	"testing"
	"time"
)

// racer runs before its first Apply: another ingest that read the id maps
// at the same moment, before either committed.
type racer struct {
	Repo
	first func() error
}

func (r *racer) Apply(ctx context.Context, b Batch) error {
	if f := r.first; f != nil {
		r.first = nil
		if err := f(); err != nil {
			return err
		}
	}
	return r.Repo.Apply(ctx, b)
}

// Two ingests race: both find no id for the same new upstream player (a
// signing) and mint their own. One upstream id still means one of ours
// (ADR 0006): the testdata has 4 clubs, 30 players and 8 fixtures, so the
// store holds exactly those, each one mapped. Before the fix the loser's
// mapping was dropped but its player row kept: 31 players, one of them
// unmapped (never updated again) and draftable forever. The same holds
// for a whole new season, and for two ingests applying at the same time.
func TestRacingSeasonIngestsMintOneID(t *testing.T) {
	for _, tc := range []struct {
		name                string
		signing, concurrent bool
	}{
		{"new signing", true, false},
		{"new season", false, false},
		{"new signing, concurrent", true, true},
		{"new season, concurrent", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) { racingSeason(t, tc.signing, tc.concurrent) })
	}
}

func racingSeason(t *testing.T, signing, concurrent bool) {
	impls(t, func(t *testing.T, repo Repo) {
		src := source()
		b, err := src.Bootstrap(ctx)
		if err != nil {
			t.Fatal(err)
		}
		fixtures, err := src.Fixtures(ctx)
		if err != nil {
			t.Fatal(err)
		}
		st, err := src.GameweekStatus(ctx)
		if err != nil {
			t.Fatal(err)
		}
		clock := func() time.Time { return now }
		if signing {
			// The season is known except its last player, who signs now.
			before := *b
			before.Players = before.Players[:len(before.Players)-1]
			if err := NewIngester(repo, "fpl", clock).Season(ctx, &before, fixtures, st); err != nil {
				t.Fatal(err)
			}
		}
		if concurrent {
			// Both read the id maps, then both apply at once.
			var ready sync.WaitGroup
			ready.Add(2)
			wait := func() error { ready.Done(); ready.Wait(); return nil }
			errs := make(chan error, 2)
			for range 2 {
				go func() {
					errs <- NewIngester(&racer{Repo: repo, first: wait}, "fpl", clock).Season(ctx, b, fixtures, st)
				}()
			}
			for range 2 {
				if err := <-errs; err != nil {
					t.Fatal(err)
				}
			}
		} else {
			other := NewIngester(repo, "fpl", clock)
			r := &racer{Repo: repo, first: func() error { return other.Season(ctx, b, fixtures, st) }}
			if err := NewIngester(r, "fpl", clock).Season(ctx, b, fixtures, st); err != nil {
				t.Fatal(err)
			}
		}

		clubs, _ := repo.Clubs(ctx)
		players, _ := repo.Players(ctx)
		fxs, _ := repo.Fixtures(ctx)
		if len(clubs) != 4 || len(players) != 30 || len(fxs) != 8 {
			t.Fatalf("clubs %d players %d fixtures %d, want 4, 30, 8", len(clubs), len(players), len(fxs))
		}
		mapped := func(entity string, n int) map[string]bool {
			m, err := repo.ExternalIDs(ctx, "fpl", entity)
			if err != nil || len(m) != n {
				t.Fatalf("%s mappings = %d %v, want %d", entity, len(m), err, n)
			}
			out := map[string]bool{}
			for _, id := range m {
				out[id] = true
			}
			return out
		}
		clubIDs, playerIDs, fixtureIDs := mapped("club", 4), mapped("player", 30), mapped("fixture", 8)
		for _, c := range clubs {
			if !clubIDs[c.ID] {
				t.Errorf("club %s (%s) has no upstream id", c.ID, c.Name)
			}
		}
		for _, p := range players {
			if !playerIDs[p.ID] || !clubIDs[p.ClubID] {
				t.Errorf("player %s (%s) unmapped or on an unmapped club %s", p.ID, p.DisplayName, p.ClubID)
			}
		}
		for _, f := range fxs {
			if !fixtureIDs[f.ID] || !clubIDs[f.HomeClubID] || !clubIDs[f.AwayClubID] {
				t.Errorf("fixture %s unmapped or between unmapped clubs", f.ID)
			}
		}
	})
}
