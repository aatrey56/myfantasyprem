// Package leaguetest sets up leagues for the rule packages' tests (draft,
// lineup, waivers, trades, jobs): the demo season's stats, users, a full
// league, on the memory stores and, when TEST_DATABASE_URL is set, on a
// fresh Postgres database. Test code only.
package leaguetest

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// Start is the default test time: Monday of gameweek 8 in the demo season
// (gameweek 7 is being played; gameweek 8's waivers run on Thursday at
// 17:30 UTC and its deadline is Friday at 17:30 UTC).
var Start = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

// Clock is a manually advanced clock.
type Clock struct {
	mu sync.Mutex
	t  time.Time
}

// Now returns the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Advance moves the clock forward.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// Set moves the clock to t.
func (c *Clock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t
}

// Env is one set of stores with the demo season loaded.
type Env struct {
	Ctx     context.Context
	Clock   *Clock
	Auth    auth.Service
	Repo    league.Repo
	Leagues *league.Service
	Stats   stats.Repo
	Log     *slog.Logger
	// Pool is the Postgres pool, nil on the memory stores.
	Pool *pgxpool.Pool
	n    int
}

// Name is "memory" or "postgres".
func (e *Env) Name() string {
	if e.Pool == nil {
		return "memory"
	}
	return "postgres"
}

// Each runs test on the memory stores and, when TEST_DATABASE_URL is set,
// on Postgres.
func Each(t *testing.T, test func(t *testing.T, e *Env)) {
	t.Run("memory", func(t *testing.T) { test(t, New(t, nil)) })
	t.Run("postgres", func(t *testing.T) { test(t, New(t, storetest.New(t))) })
}

// New returns an Env on pool (nil: memory) with the demo season's stats.
func New(t testing.TB, pool *pgxpool.Pool) *Env {
	t.Helper()
	e := &Env{Ctx: context.Background(), Clock: &Clock{t: Start}, Pool: pool,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if pool == nil {
		e.Auth, e.Repo, e.Stats = auth.NewMemory(e.Clock.Now), league.NewMemory(), stats.NewMemory()
	} else {
		e.Auth, e.Repo, e.Stats = auth.NewPostgres(pool, e.Clock.Now), league.NewPostgres(pool), stats.NewPostgres(pool)
	}
	e.Leagues = league.NewService(e.Repo, e.Clock.Now).WithStats(e.Stats)
	if _, err := demo.LoadStats(e.Ctx, e.Stats, e.Log, e.Clock.Now); err != nil {
		t.Fatal(err)
	}
	return e
}

// User creates a user and returns its id.
func (e *Env) User(t testing.TB) string {
	t.Helper()
	e.n++
	u, err := e.Auth.CreateUser(e.Ctx, fmt.Sprintf("Manager %d", e.n), fmt.Sprintf("m%d-%d@example.com", e.n, time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// League creates a full pre-draft league of n teams; users[0] is the
// commissioner, users[i] owns teams[i].
func (e *Env) League(t testing.TB, n int, in league.SettingsInput) (*league.League, []string) {
	t.Helper()
	users := []string{e.User(t)}
	l, err := e.Leagues.Create(e.Ctx, users[0], "Test League", n, "Team 1", in)
	if err != nil {
		t.Fatal(err)
	}
	inv, err := e.Leagues.CreateInvite(e.Ctx, users[0], l.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 2; i <= n; i++ {
		u := e.User(t)
		users = append(users, u)
		if l, _, err = e.Leagues.AcceptInvite(e.Ctx, u, inv.Code, fmt.Sprintf("Team %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	return l, users
}

// InSeason creates a full league of n teams and drafts it the way autopick
// would (demo.SnakeDraft, join order), with rosters from the upcoming
// gameweek, default lineups, the schedule and the waiver order, as the
// draft's completion does.
func (e *Env) InSeason(t testing.TB, n int, in league.SettingsInput) (*league.League, []string) {
	t.Helper()
	l, users := e.League(t, n, in)
	w := e.World(t)
	now := e.Clock.Now()
	first := w.UpcomingGW(now)
	var teams []string
	for _, tm := range l.Teams {
		teams = append(teams, tm.ID)
	}
	rosters := demo.SnakeDraft(teams, w.Players)
	err := e.Repo.Tx(e.Ctx, func(tx league.Tx) error {
		s, err := tx.LoadState(e.Ctx, l.ID)
		if err != nil {
			return err
		}
		for _, id := range teams {
			for _, p := range rosters[id] {
				s.Game.Acquire(id, p, first, "draft")
			}
			s.Game.SetLineup(id, first, league.DefaultLineup(rosters[id], w.Players), now)
		}
		s.NewMatchups = league.RoundRobin(teams, first, 38)
		for i := len(teams) - 1; i >= 0; i-- {
			s.Priority = append(s.Priority, teams[i])
		}
		s.League.Status = league.StatusInSeason
		return tx.SaveState(e.Ctx, s)
	})
	if err != nil {
		t.Fatal(err)
	}
	l, err = e.Repo.Get(e.Ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	return l, users
}

// World loads the season's players and gameweeks.
func (e *Env) World(t testing.TB) *league.World {
	t.Helper()
	w, err := league.SeasonWorld(e.Ctx, e.Stats)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

// State reads a league's state.
func (e *Env) State(t testing.TB, leagueID string) *league.State {
	t.Helper()
	s, err := e.Repo.State(e.Ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// Notes records the changes reported to it.
type Notes struct {
	mu      sync.Mutex
	Changes []league.Change
}

// Notify records changes.
func (n *Notes) Notify(_ context.Context, changes ...league.Change) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.Changes = append(n.Changes, changes...)
}

// Kinds returns the recorded change kinds and forgets them.
func (n *Notes) Kinds() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	var out []string
	for _, c := range n.Changes {
		out = append(out, c.Kind)
	}
	n.Changes = nil
	return out
}
