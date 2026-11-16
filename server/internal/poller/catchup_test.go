package poller_test

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// outage replays the demo season (gameweeks 1-7, 7 current) as upstream
// looked at gameweek `current`: later events not finished, and, like FPL,
// fixtures listing only gameweeks from `current` on, still scheduled.
type outage struct {
	provider.StatsSource
	current int
	// partial makes the current gameweek's live data show its second
	// half of fixtures still in play (the server went down mid-gameweek).
	partial bool
	// moved lists an extra gameweek 6 fixture while current < 6, which
	// upstream then moves out of the gameweek: no later fixture list or
	// live payload has it.
	moved bool
	mu    sync.Mutex
	live  map[int]int
}

func (o *outage) GameweekStatus(ctx context.Context) (*provider.GameweekStatus, error) {
	st, err := o.StatsSource.GameweekStatus(ctx)
	if err != nil || st.Current == o.current {
		return st, err
	}
	next := o.current + 1
	return &provider.GameweekStatus{Current: o.current, Next: &next, BonusAdded: true, DataChecked: true}, nil
}

func (o *outage) Bootstrap(ctx context.Context) (*provider.Bootstrap, error) {
	b, err := o.StatsSource.Bootstrap(ctx)
	if err != nil {
		return nil, err
	}
	cp := *b
	cp.Gameweeks = nil
	for _, g := range b.Gameweeks {
		if g.Number > o.current || (g.Number == o.current && o.current < 7) {
			g.Finished, g.DataChecked = false, false
		}
		cp.Gameweeks = append(cp.Gameweeks, g)
	}
	return &cp, nil
}

func (o *outage) Fixtures(ctx context.Context) ([]provider.Fixture, error) {
	fs, err := o.StatsSource.Fixtures(ctx)
	if err != nil {
		return nil, err
	}
	var out []provider.Fixture
	for _, f := range fs {
		if f.Gameweek < o.current {
			continue
		}
		if f.Gameweek > o.current {
			f.Status, f.HomeScore, f.AwayScore = provider.FixtureScheduled, nil, nil
		}
		out = append(out, f)
	}
	if o.moved && o.current < 6 {
		for _, f := range out {
			if f.Gameweek == 6 {
				kick := f.KickoffAt.Add(48 * time.Hour)
				out = append(out, provider.Fixture{ExternalID: movedFixture, Gameweek: 6, KickoffAt: &kick,
					HomeClubExternalID: f.AwayClubExternalID, AwayClubExternalID: f.HomeClubExternalID,
					Status: provider.FixtureScheduled})
				break
			}
		}
	}
	return out, nil
}

// movedFixture is the external id of outage.moved's fixture.
const movedFixture = "moved-1"

func (o *outage) EventLive(ctx context.Context, gw int) (*provider.EventLive, error) {
	o.mu.Lock()
	o.live[gw]++
	o.mu.Unlock()
	l, err := o.StatsSource.EventLive(ctx, gw)
	if err != nil || !o.partial || gw != o.current {
		return l, err
	}
	cp := *l
	cp.Fixtures = nil
	for i, f := range l.Fixtures {
		if o.partial && gw == o.current && i >= len(l.Fixtures)/2 {
			f.Status = provider.FixtureLive
		}
		cp.Fixtures = append(cp.Fixtures, f)
	}
	return &cp, nil
}

// The production outage, on the demo season: the server polled up to
// gameweek 4 (storing 5-7 as scheduled fixtures), was down for 5 and 6
// and came back with gameweek 7 current. Before the fix the stale
// scheduled fixtures made the backfill drop 5 and 6 as "nothing played":
// no stats, team scores 0, matchups upcoming at 0-0, and the standings
// counted each as a draw. Now each restart poll catches up one missing
// finished gameweek, newest first, and scores it as the live path would.
func TestRestartCatchesUpFinishedGameweeksMissedOffline(t *testing.T) {
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	run := func(t *testing.T, repo stats.Repo, leagues *league.Service, users auth.Service) {
		ctx := context.Background()
		clock := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
		now := func() time.Time { return clock }
		src := &outage{StatsSource: demo.Source(quiet), current: 4, live: map[int]int{}}
		sc := &league.Scorer{Leagues: leagues.Repo(), Stats: repo, Rules: scoring.StandardLineup(), Log: quiet}
		newPoller := func() *poller.Poller {
			p := poller.New(src, stats.NewIngester(repo, demo.ProviderName, now), repo, sc, poller.Config{}, quiet)
			p.Now = now
			p.Sleep = func(context.Context, time.Duration) error { return nil }
			return p
		}

		// Before the outage: gameweeks 1-4 polled and scored.
		before := newPoller()
		for range 6 {
			before.Tick(ctx)
		}
		res, err := demo.SeedLeague(ctx, demo.Deps{Auth: users, Leagues: leagues, Stats: repo, Log: quiet, Now: now}, 4)
		if err != nil {
			t.Fatal(err)
		}
		for g := 1; g <= 4; g++ {
			if rows, _ := repo.PlayerGameweeks(ctx, g); len(rows) == 0 {
				t.Fatalf("setup: gameweek %d has no stats", g)
			}
		}
		for g := 5; g <= 6; g++ {
			if rows, _ := repo.PlayerFixtures(ctx, g); len(rows) != 0 {
				t.Fatalf("setup: gameweek %d already has %d rows", g, len(rows))
			}
		}

		// Restart with gameweek 7 current.
		src.current = 7
		clear(src.live)
		p := newPoller()
		p.Tick(ctx)
		if src.live[6] != 1 || src.live[5] != 0 {
			t.Fatalf("first poll fetched gameweek 6 %d times and 5 %d times (one missing gameweek per poll, newest first); calls %v",
				src.live[6], src.live[5], src.live)
		}
		clock = clock.Add(time.Minute)
		p.Tick(ctx)
		if src.live[5] != 1 || src.live[6] != 1 {
			t.Fatalf("second poll: live calls %v, want gameweek 5 once", src.live)
		}
		for g := 1; g <= 4; g++ {
			if src.live[g] != 0 {
				t.Errorf("gameweek %d (stored) refetched %d times", g, src.live[g])
			}
		}

		g, err := leagues.Repo().Game(ctx, res.LeagueID)
		if err != nil {
			t.Fatal(err)
		}
		for gw := 5; gw <= 6; gw++ {
			if rows, _ := repo.PlayerFixtures(ctx, gw); len(rows) == 0 {
				t.Errorf("gameweek %d: no player stats after catch-up", gw)
			}
			ms := g.MatchupsIn(gw)
			if len(ms) != 5 {
				t.Fatalf("gameweek %d: %d matchups", gw, len(ms))
			}
			for _, m := range ms {
				if m.State != scoring.StateFinal {
					t.Errorf("gameweek %d matchup %s: state %s, want final", gw, m.ID, m.State)
				}
				home, ok := g.Score(m.HomeTeamID, gw)
				if !ok || home.Points == 0 || home.State != scoring.StateFinal || m.HomePoints != home.Points {
					t.Errorf("gameweek %d matchup %s: home %d, team score %+v (%v)", gw, m.ID, m.HomePoints, home, ok)
				}
				if m.AwayTeamID != "" {
					away, _ := g.Score(m.AwayTeamID, gw)
					if m.AwayPoints != away.Points || away.Points == 0 {
						t.Errorf("gameweek %d matchup %s: away %d, team score %d", gw, m.ID, m.AwayPoints, away.Points)
					}
				}
			}
		}

		// The table counts 1-6: each of the 9 teams played 6, and a draw
		// only where the two sides' points are equal.
		l, err := leagues.Get(ctx, res.UserIDs[0], res.LeagueID)
		if err != nil {
			t.Fatal(err)
		}
		w, err := league.LoadWorld(ctx, repo)
		if err != nil {
			t.Fatal(err)
		}
		draws := map[string]int{}
		for _, m := range g.Matchups {
			if m.Gameweek <= 6 && m.HomePoints == m.AwayPoints {
				draws[m.HomeTeamID]++
				draws[m.AwayTeamID]++
			}
		}
		for _, r := range league.Standings(l, g, w) {
			if r.Played != 6 || r.Draws != draws[r.TeamID] {
				t.Errorf("team %s: played %d, draws %d (want 6, %d)", r.TeamID, r.Played, r.Draws, draws[r.TeamID])
			}
		}
	}
	t.Run("memory", func(t *testing.T) {
		run(t, stats.NewMemory(), league.NewService(league.NewMemory(), time.Now), auth.NewMemory(time.Now))
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		run(t, stats.NewPostgres(pool), league.NewService(league.NewPostgres(pool), time.Now), auth.NewPostgres(pool, time.Now))
	})
}
