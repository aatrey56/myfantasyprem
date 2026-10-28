package demo

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

var ctx = context.Background()

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestSeed(t *testing.T) {
	run := func(t *testing.T, d Deps) {
		res, err := Seed(ctx, d)
		if err != nil {
			t.Fatal(err)
		}
		if res.Gameweek != 7 || len(res.UserIDs) != len(Managers) {
			t.Fatalf("result = %+v", res)
		}
		l, err := d.Leagues.Get(ctx, res.UserIDs[0], res.LeagueID)
		if err != nil {
			t.Fatal(err)
		}
		if l.Status != league.StatusInSeason || len(l.Teams) != 9 || l.MemberCount() != 9 {
			t.Fatalf("league = %+v", l)
		}
		g, err := d.Leagues.Repo().Game(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		w, err := league.LoadWorld(ctx, d.Stats)
		if err != nil {
			t.Fatal(err)
		}
		rules := scoring.StandardLineup()
		for _, tm := range l.Teams {
			roster := map[string]scoring.Position{}
			for _, id := range g.RosterAt(tm.ID, 1) {
				roster[id] = w.Players[id].Position
			}
			if v := rules.ValidateSquad(roster); len(v) != 0 || len(roster) != 15 {
				t.Errorf("team %s squad: %d players, %v", tm.Name, len(roster), v)
			}
			lineup := league.EffectiveLineup(g, w, tm.ID, 1)
			if v := rules.ValidateLineup(roster, lineup.Slots[:11], lineup.Slots[11:]); len(v) != 0 {
				t.Errorf("team %s lineup: %v", tm.Name, v)
			}
			for gw := 1; gw <= 7; gw++ {
				if _, ok := g.Score(tm.ID, gw); !ok {
					t.Errorf("team %s has no gameweek %d score", tm.Name, gw)
				}
			}
		}
		for gw := 1; gw <= 38; gw++ {
			ms := g.MatchupsIn(gw)
			avg := 0
			for _, m := range ms {
				if m.AwayTeamID == "" {
					avg++
				}
			}
			if len(ms) != 5 || avg != 1 {
				t.Fatalf("gameweek %d: %d matchups, %d against the average", gw, len(ms), avg)
			}
		}
		if s := g.MatchupsIn(7)[0].State; s != scoring.StateLive {
			t.Errorf("gameweek 7 state = %s", s)
		}
		if s := g.MatchupsIn(6)[0].State; s != scoring.StateFinal {
			t.Errorf("gameweek 6 state = %s", s)
		}
		table := league.Standings(l, g, w)
		played := 0
		for _, r := range table {
			played += r.Played
		}
		if played != 9*6 {
			t.Errorf("standings count %d results, want 54", played)
		}

		// Seeding again reuses the league and changes no score.
		res2, err := Seed(ctx, d)
		if err != nil || res2.LeagueID != res.LeagueID {
			t.Fatalf("reseed = %+v %v", res2, err)
		}
		g2, _ := d.Leagues.Repo().Game(ctx, l.ID)
		for _, s := range g2.Scores {
			if s.Rev != 1 {
				t.Fatalf("reseed changed a score: %+v", s)
			}
		}
	}
	t.Run("memory", func(t *testing.T) {
		run(t, Deps{Auth: auth.NewMemory(time.Now), Leagues: league.NewService(league.NewMemory(), time.Now), Stats: stats.NewMemory(), Log: quiet()})
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		run(t, Deps{Auth: auth.NewPostgres(pool, time.Now), Leagues: league.NewService(league.NewPostgres(pool), time.Now), Stats: stats.NewPostgres(pool), Log: quiet()})
	})
}
