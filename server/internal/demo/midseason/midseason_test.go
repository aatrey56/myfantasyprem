package midseason

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// The league is drafted on the Saturday of gameweek 7 (after its
// deadline), so it starts scoring at gameweek 8; two managers let their
// pick clock run out, and both autopicks are in the activity feed.
func TestSeed(t *testing.T) {
	ctx := context.Background()
	run := func(t *testing.T, d demo.Deps) {
		res, err := demo.Seed(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		id, err := Seed(ctx, d, res.UserIDs)
		if err != nil {
			t.Fatal(err)
		}
		st, err := d.Leagues.Repo().State(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if st.League.Name != LeagueName || st.League.Status != league.StatusInSeason || st.League.Settings.StartGameweek != 8 ||
			len(st.League.Teams) != len(Managers) || st.League.MemberCount() != len(Managers) ||
			st.Draft == nil || st.Draft.Status != league.DraftComplete {
			t.Fatalf("league %s %s, start %d, %d teams, draft %+v", st.League.Name, st.League.Status,
				st.League.Settings.StartGameweek, len(st.League.Teams), st.Draft)
		}
		if st.League.CommissionerID != res.UserIDs[0] {
			t.Errorf("commissioner %s, want the demo account", st.League.CommissionerID)
		}
		if len(st.Game.Matchups) == 0 {
			t.Fatal("no schedule")
		}
		for _, m := range st.Game.Matchups {
			if m.Gameweek < 8 {
				t.Fatalf("matchup in gameweek %d", m.Gameweek)
			}
		}
		for _, r := range st.Game.Rosters {
			if r.FromGW != 8 {
				t.Fatalf("roster slot from gameweek %d", r.FromGW)
			}
		}
		missed := 0
		for _, p := range st.Draft.Picks {
			if p.MissedClock {
				missed++
			}
		}
		feed, err := d.Leagues.Repo().Transactions(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if missed != 2 || len(feed) != 2 || feed[0].Kind != league.TxnDraftAutopick || feed[0].Gameweek != 8 {
			t.Fatalf("%d missed clocks, feed %+v", missed, feed)
		}
		// Seeding again reuses it.
		again, err := Seed(ctx, d, res.UserIDs)
		if err != nil || again != id {
			t.Fatalf("reseed = %s %v, want %s", again, err, id)
		}
	}
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	t.Run("memory", func(t *testing.T) {
		run(t, demo.Deps{Auth: auth.NewMemory(time.Now), Leagues: league.NewService(league.NewMemory(), time.Now), Stats: stats.NewMemory(), Log: quiet})
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		run(t, demo.Deps{Auth: auth.NewPostgres(pool, time.Now), Leagues: league.NewService(league.NewPostgres(pool), time.Now), Stats: stats.NewPostgres(pool), Log: quiet})
	})
}
