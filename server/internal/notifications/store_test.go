package notifications

import (
	"context"
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

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// impls runs a test against every Store: Memory always, Postgres when
// TEST_DATABASE_URL is set (see storetest).
func impls(t *testing.T, test func(t *testing.T, s Store, c *clock)) {
	start := time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)
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

func ptr[T any](v T) *T { return &v }

func TestPreferencesDefaultOnAllOffAllOn(t *testing.T) {
	impls(t, func(t *testing.T, s Store, _ *clock) {
		off, err := s.Disabled(ctx, userA, league1)
		if err != nil || len(off) != 0 {
			t.Fatalf("default: %v %v, want nothing off", off, err)
		}
		if err := s.SetPreferences(ctx, userA, league1, nil, map[Type]bool{PlayerGoal: false}); err != nil {
			t.Fatal(err)
		}
		off, _ = s.Disabled(ctx, userA, league1)
		if len(off) != 1 || !off[PlayerGoal] {
			t.Fatalf("one off: %v", off)
		}
		if other, _ := s.Disabled(ctx, userA, league2); len(other) != 0 {
			t.Fatalf("another league changed: %v", other)
		}
		// All off, then one back on in the same request.
		if err := s.SetPreferences(ctx, userA, league1, ptr(false), map[Type]bool{TradeOffered: true}); err != nil {
			t.Fatal(err)
		}
		off, _ = s.Disabled(ctx, userA, league1)
		if len(off) != len(Catalog)-1 || off[TradeOffered] {
			t.Fatalf("all off but trades: %d off, trade_offered %v", len(off), off[TradeOffered])
		}
		if err := s.SetPreferences(ctx, userA, league1, ptr(true), nil); err != nil {
			t.Fatal(err)
		}
		if off, _ = s.Disabled(ctx, userA, league1); len(off) != 0 {
			t.Fatalf("all on: %v", off)
		}
	})
}

func TestAddDedupesAndListsNewestFirst(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		n := Notification{UserID: userA, LeagueID: league1, Type: PlayerGoal, Title: "Goal", Body: "b", Key: "k1"}
		first, ok, err := s.Add(ctx, n, false)
		if err != nil || !ok || first.ID == "" {
			t.Fatalf("first add: %+v %v %v", first, ok, err)
		}
		if _, ok, _ := s.Add(ctx, n, false); ok {
			t.Fatal("same key added twice")
		}
		// The key is per user: B may have the same one.
		if _, ok, _ := s.Add(ctx, Notification{UserID: userB, Type: PlayerGoal, Key: "k1"}, false); !ok {
			t.Fatal("other user's key blocked")
		}
		c.t = c.t.Add(time.Minute)
		second, _, _ := s.Add(ctx, Notification{UserID: userA, Type: DraftStarting, Title: "Draft", Key: "k2"}, false)
		list, unread, err := s.List(ctx, userA, 50, false)
		if err != nil || len(list) != 2 || unread != 2 || list[0].ID != second.ID || list[1].LeagueID != league1 {
			t.Fatalf("list: %+v unread %d %v", list, unread, err)
		}
		if err := s.MarkRead(ctx, userA, []string{first.ID}, false); err != nil {
			t.Fatal(err)
		}
		list, unread, _ = s.List(ctx, userA, 50, true)
		if len(list) != 1 || unread != 1 || list[0].ID != second.ID {
			t.Fatalf("unread only: %+v %d", list, unread)
		}
		_ = s.MarkRead(ctx, userA, nil, true)
		if _, unread, _ = s.List(ctx, userA, 50, false); unread != 0 {
			t.Fatalf("after all read: %d", unread)
		}
		if _, unread, _ = s.List(ctx, userB, 50, false); unread != 1 {
			t.Fatalf("B's inbox touched: %d", unread)
		}
	})
}

func TestOutboxRetryAndDrop(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		sub, err := s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/1", P256dh: "p", Auth: "a"})
		if err != nil {
			t.Fatal(err)
		}
		again, _ := s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/1"})
		if again.ID != sub.ID {
			t.Fatalf("resubscribe made %s, want %s", again.ID, sub.ID)
		}
		// No push when the preference is off: inbox only.
		_, _, _ = s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Key: "quiet"}, false)
		n, _, _ := s.Add(ctx, Notification{UserID: userA, Type: PlayerGoal, Title: "T", Key: "loud"}, true)
		due, err := s.Due(ctx, c.t, time.Minute, 10)
		if err != nil || len(due) != 1 || due[0].Notification.ID != n.ID || due[0].Subscription.Endpoint != "https://push.example/1" {
			t.Fatalf("due: %+v %v", due, err)
		}
		// Leased: not due again until the lease ends.
		if again, _ := s.Due(ctx, c.t, time.Minute, 10); len(again) != 0 {
			t.Fatalf("leased delivery due again: %+v", again)
		}
		if err := s.Retry(ctx, due[0].ID, c.t.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if early, _ := s.Due(ctx, c.t.Add(4*time.Minute), time.Minute, 10); len(early) != 0 {
			t.Fatal("due before its retry time")
		}
		late, _ := s.Due(ctx, c.t.Add(5*time.Minute), time.Minute, 10)
		if len(late) != 1 || late[0].Attempts != 1 {
			t.Fatalf("retry: %+v", late)
		}
		if err := s.Drop(ctx, sub.ID); err != nil {
			t.Fatal(err)
		}
		if gone, _ := s.Due(ctx, c.t.Add(time.Hour), time.Minute, 10); len(gone) != 0 {
			t.Fatalf("dropped subscription still has deliveries: %+v", gone)
		}
		// A device that changes hands moves to the new user.
		s1, _ := s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/2"})
		s2, _ := s.Subscribe(ctx, Subscription{UserID: userB, Kind: KindWebPush, Endpoint: "https://push.example/2"})
		if s1.ID == s2.ID || s2.UserID != userB {
			t.Fatalf("handover: %+v %+v", s1, s2)
		}
		if err := s.Unsubscribe(ctx, userA, s2.ID); err != nil {
			t.Fatal(err)
		}
		_, _, _ = s.Add(ctx, Notification{UserID: userB, Type: PlayerGoal, Key: "b"}, true)
		if d, _ := s.Due(ctx, c.t.Add(2*time.Hour), time.Minute, 10); len(d) != 1 {
			t.Fatalf("A removed B's device: %+v", d)
		}
	})
}
