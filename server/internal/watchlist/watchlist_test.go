package watchlist

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

var ctx = context.Background()

const (
	userA   = "usr_01k7m2xq8z0000000000000001"
	userB   = "usr_01k7m2xq8z0000000000000002"
	league1 = "lg_01k7m2xq8rfv3c9a4t6wbn5hdy"
	league2 = "lg_01k7m2xq8rfv3c9a4t6wbn5hd0"
)

func player(n int) string { return fmt.Sprintf("plr_01k7m2xq8v%016d", n) }

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// impls runs a test against every Store: Memory always, Postgres when
// TEST_DATABASE_URL is set (see storetest).
func impls(t *testing.T, test func(t *testing.T, s Store, c *clock)) {
	start := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	t.Run("memory", func(t *testing.T) {
		c := &clock{t: start}
		test(t, NewMemory(c.now), c)
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		for _, sql := range []string{
			`INSERT INTO seasons (id) VALUES ('2026-27') ON CONFLICT DO NOTHING`,
			`INSERT INTO users (id, display_name) VALUES ('` + userA + `', 'A'), ('` + userB + `', 'B')`,
			`INSERT INTO leagues (id, name, season, status) VALUES ('` + league1 + `', 'One', '2026-27', 'in_season'),
			 ('` + league2 + `', 'Two', '2026-27', 'in_season')`,
		} {
			if _, err := pool.Exec(ctx, sql); err != nil {
				t.Fatal(err)
			}
		}
		c := &clock{t: start}
		test(t, NewPostgres(pool, c.now), c)
	})
}

func ids(es []Entry) []string {
	out := []string{}
	for _, e := range es {
		out = append(out, e.PlayerID)
	}
	return out
}

func TestAddListRemove(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		for i := 1; i <= 3; i++ {
			e, err := s.Add(ctx, userA, league1, player(i))
			if err != nil {
				t.Fatal(err)
			}
			if !e.AddedAt.Equal(c.t) || e.PlayerID != player(i) {
				t.Fatalf("Add = %+v, want %s at %s", e, player(i), c.t)
			}
			c.t = c.t.Add(time.Minute)
		}
		got, err := s.List(ctx, userA, league1)
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{player(3), player(2), player(1)}; fmt.Sprint(ids(got)) != fmt.Sprint(want) {
			t.Fatalf("List = %v, want newest first %v", ids(got), want)
		}
		if err := s.Remove(ctx, userA, league1, player(2)); err != nil {
			t.Fatal(err)
		}
		// Removing again, or a player never watched, is no error.
		if err := s.Remove(ctx, userA, league1, player(2)); err != nil {
			t.Fatal(err)
		}
		if err := s.Remove(ctx, userA, league1, player(9)); err != nil {
			t.Fatal(err)
		}
		got, _ = s.List(ctx, userA, league1)
		if want := []string{player(3), player(1)}; fmt.Sprint(ids(got)) != fmt.Sprint(want) {
			t.Fatalf("after remove List = %v, want %v", ids(got), want)
		}
	})
}

func TestAddIsIdempotent(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		first, err := s.Add(ctx, userA, league1, player(1))
		if err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(time.Hour)
		again, err := s.Add(ctx, userA, league1, player(1))
		if err != nil {
			t.Fatal(err)
		}
		if !again.AddedAt.Equal(first.AddedAt) {
			t.Fatalf("second Add moved addedAt: %s, want %s", again.AddedAt, first.AddedAt)
		}
		got, _ := s.List(ctx, userA, league1)
		if len(got) != 1 {
			t.Fatalf("List has %d entries, want 1", len(got))
		}
	})
}

func TestListsArePerUserAndLeague(t *testing.T) {
	impls(t, func(t *testing.T, s Store, _ *clock) {
		for _, x := range []struct{ user, league, player string }{
			{userA, league1, player(1)},
			{userA, league2, player(2)},
			{userB, league1, player(3)},
		} {
			if _, err := s.Add(ctx, x.user, x.league, x.player); err != nil {
				t.Fatal(err)
			}
		}
		for _, x := range []struct {
			user, league string
			want         []string
		}{
			{userA, league1, []string{player(1)}},
			{userA, league2, []string{player(2)}},
			{userB, league1, []string{player(3)}},
			{userB, league2, []string{}},
		} {
			got, err := s.List(ctx, x.user, x.league)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(ids(got)) != fmt.Sprint(x.want) {
				t.Errorf("List(%s, %s) = %v, want %v", x.user, x.league, ids(got), x.want)
			}
		}
		// B removing A's player leaves A's list alone.
		if err := s.Remove(ctx, userB, league1, player(1)); err != nil {
			t.Fatal(err)
		}
		if got, _ := s.List(ctx, userA, league1); len(got) != 1 {
			t.Fatalf("A's list = %v after B's remove", ids(got))
		}
	})
}

func TestFullAtMax(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		for i := 1; i <= Max; i++ {
			if _, err := s.Add(ctx, userA, league1, player(i)); err != nil {
				t.Fatalf("Add %d: %v", i, err)
			}
			c.t = c.t.Add(time.Second)
		}
		if _, err := s.Add(ctx, userA, league1, player(Max+1)); !errors.Is(err, ErrFull) {
			t.Fatalf("Add past Max = %v, want ErrFull", err)
		}
		// Re-adding a watched player still answers with its entry.
		if _, err := s.Add(ctx, userA, league1, player(1)); err != nil {
			t.Fatalf("re-Add at Max = %v", err)
		}
		// Another league's list is separate.
		if _, err := s.Add(ctx, userA, league2, player(Max+1)); err != nil {
			t.Fatal(err)
		}
	})
}
