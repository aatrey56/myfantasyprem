package tags

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

func list(t *testing.T, s Store, user, league string) string {
	t.Helper()
	es, err := s.List(ctx, user, league)
	if err != nil {
		t.Fatal(err)
	}
	out := ""
	for _, e := range es {
		out += fmt.Sprintf("%s=%s ", e.PlayerID[len(e.PlayerID)-2:], e.Tag)
	}
	return out
}

func TestSetListRemove(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		for i, tag := range []string{Hot, Cold, Sell} {
			e, err := s.Set(ctx, userA, league1, player(i+1), tag)
			if err != nil {
				t.Fatal(err)
			}
			if e.Tag != tag || !e.UpdatedAt.Equal(c.t) {
				t.Fatalf("Set = %+v, want %s at %s", e, tag, c.t)
			}
			c.t = c.t.Add(time.Minute)
		}
		if got, want := list(t, s, userA, league1), "03=sell 02=cold 01=hot "; got != want {
			t.Fatalf("List = %q, want newest first %q", got, want)
		}
		// Retagging replaces the tag and makes it the newest.
		if _, err := s.Set(ctx, userA, league1, player(1), Avoid); err != nil {
			t.Fatal(err)
		}
		if got, want := list(t, s, userA, league1), "01=avoid 03=sell 02=cold "; got != want {
			t.Fatalf("after retag List = %q, want %q", got, want)
		}
		if err := s.Remove(ctx, userA, league1, player(3)); err != nil {
			t.Fatal(err)
		}
		if err := s.Remove(ctx, userA, league1, player(3)); err != nil {
			t.Fatal(err)
		}
		if got, want := list(t, s, userA, league1), "01=avoid 02=cold "; got != want {
			t.Fatalf("after remove List = %q, want %q", got, want)
		}
	})
}

func TestSameTagKeepsTime(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		first, err := s.Set(ctx, userA, league1, player(1), Target)
		if err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(time.Hour)
		again, err := s.Set(ctx, userA, league1, player(1), Target)
		if err != nil {
			t.Fatal(err)
		}
		if !again.UpdatedAt.Equal(first.UpdatedAt) {
			t.Fatalf("same tag moved updatedAt: %s, want %s", again.UpdatedAt, first.UpdatedAt)
		}
	})
}

func TestUnknownTag(t *testing.T) {
	impls(t, func(t *testing.T, s Store, _ *clock) {
		for _, tag := range []string{"", "HOT", "🔥", "watch"} {
			if _, err := s.Set(ctx, userA, league1, player(1), tag); !errors.Is(err, ErrUnknownTag) {
				t.Errorf("Set(%q) = %v, want ErrUnknownTag", tag, err)
			}
		}
		if got := list(t, s, userA, league1); got != "" {
			t.Fatalf("List = %q, want empty", got)
		}
	})
}

func TestTagsArePerUserAndLeague(t *testing.T) {
	impls(t, func(t *testing.T, s Store, _ *clock) {
		for _, x := range []struct{ user, league, tag string }{
			{userA, league1, Hot},
			{userA, league2, Cold},
			{userB, league1, Avoid},
		} {
			if _, err := s.Set(ctx, x.user, x.league, player(1), x.tag); err != nil {
				t.Fatal(err)
			}
		}
		for _, x := range []struct{ user, league, want string }{
			{userA, league1, "01=hot "},
			{userA, league2, "01=cold "},
			{userB, league1, "01=avoid "},
			{userB, league2, ""},
		} {
			if got := list(t, s, x.user, x.league); got != x.want {
				t.Errorf("List(%s, %s) = %q, want %q", x.user, x.league, got, x.want)
			}
		}
		if err := s.Remove(ctx, userB, league1, player(1)); err != nil {
			t.Fatal(err)
		}
		if got := list(t, s, userA, league1); got != "01=hot " {
			t.Fatalf("A's tags = %q after B's remove", got)
		}
	})
}
