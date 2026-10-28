package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

var ctx = context.Background()

const (
	lg1  = "lg_01k7m2xq8rfv3c9a4t6wbn5hd1"
	lg2  = "lg_01k7m2xq8rfv3c9a4t6wbn5hd2"
	usr  = "usr_0000000000000000000000000a"
	sess = "01K7M2XQ8RFV3C9A4T6WBN5HD1"
)

func TestFrame(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	ev := Event{LeagueID: lg1, Seq: 7, Type: "trade.updated", SentAt: at, Body: json.RawMessage(`{"trade":{"id":"x","viewerActions":[]}}`),
		PerTeam: map[string]json.RawMessage{"tm_a": json.RawMessage(`{"trade":{"id":"x","viewerActions":["accept"]}}`)}}
	got, ok := ev.Frame("tm_b")
	want := `{"type":"trade.updated","leagueId":"` + lg1 + `","seq":7,"sentAt":"2026-10-17T14:00:00Z","trade":{"id":"x","viewerActions":[]}}`
	if !ok || string(got) != want {
		t.Fatalf("frame = %s", got)
	}
	got, _ = ev.Frame("tm_a")
	if !strings.Contains(string(got), `"accept"`) {
		t.Fatalf("per-team frame = %s", got)
	}
	ev.Audience = []string{"tm_a"}
	if _, ok := ev.Frame("tm_b"); ok {
		t.Fatal("frame outside the audience")
	}
	empty := Event{LeagueID: lg1, Seq: 1, Type: "league.updated", SentAt: at, Body: json.RawMessage(`{}`)}
	if got, _ := empty.Frame(""); !json.Valid(got) || strings.Contains(string(got), ",}") {
		t.Fatalf("empty body frame = %s", got)
	}
}

func stores(t *testing.T, test func(t *testing.T, s Store, now *time.Time)) {
	t.Run("memory", func(t *testing.T) {
		now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
		test(t, NewMemory(func() time.Time { return now }), &now)
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		for _, id := range []string{lg1, lg2} {
			if _, err := pool.Exec(ctx, `INSERT INTO seasons (id) VALUES ('2026-27') ON CONFLICT DO NOTHING`); err != nil {
				t.Fatal(err)
			}
			if _, err := pool.Exec(ctx, `INSERT INTO leagues (id, name, season, status) VALUES ($1, 'L', '2026-27', 'in_season')`, id); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, display_name) VALUES ($1, 'U')`, usr); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO sessions (id, user_id, access_sha256, access_expires_at, refresh_sha256, refresh_expires_at)
			VALUES ($1, $2, 'a', now() + interval '1 hour', 'r', now() + interval '1 day')`, sess, usr); err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
		test(t, NewPostgres(pool, func() time.Time { return now }), &now)
	})
}

func TestStore(t *testing.T) {
	stores(t, func(t *testing.T, s Store, now *time.Time) {
		for i := range 3 {
			ev, err := s.Append(ctx, Event{LeagueID: lg1, Type: "waivers.processed", Body: json.RawMessage(`{"gameweek":8}`)})
			if err != nil || ev.Seq != int64(i+1) || !ev.SentAt.Equal(*now) {
				t.Fatalf("append %d = %+v %v", i, ev, err)
			}
			*now = now.Add(2 * time.Minute)
		}
		if ev, _ := s.Append(ctx, Event{LeagueID: lg2, Type: "league.updated", Audience: []string{"tm_x"},
			PerTeam: map[string]json.RawMessage{"tm_x": json.RawMessage(`{}`)}}); ev.Seq != 1 {
			t.Fatalf("second league seq = %d", ev.Seq)
		}
		evs, err := s.Since(ctx, lg1, 1)
		if err != nil || len(evs) != 2 || evs[0].Seq != 2 || string(evs[1].Body) != `{"gameweek": 8}` && string(evs[1].Body) != `{"gameweek":8}` {
			t.Fatalf("since = %+v %v", evs, err)
		}
		ev, ok, err := s.Get(ctx, lg2, 1)
		if err != nil || !ok || ev.Audience[0] != "tm_x" || ev.PerTeam["tm_x"] == nil {
			t.Fatalf("get = %+v %v %v", ev, ok, err)
		}
		// Keep five minutes: the first event (sent at 14:00) goes at 14:05+.
		n, err := s.Prune(ctx, now.Add(-5*time.Minute))
		if err != nil || n != 1 {
			t.Fatalf("pruned %d %v", n, err)
		}

		want := Ticket{UserID: usr, SessionID: sess, LeagueID: lg1}
		tk, exp, err := s.IssueTicket(ctx, want, TicketTTL)
		if err != nil || !exp.Equal(now.Add(TicketTTL)) {
			t.Fatalf("ticket: %v %v", exp, err)
		}
		got, err := s.RedeemTicket(ctx, tk)
		if err != nil || got != want {
			t.Fatalf("redeem = %+v %v", got, err)
		}
		if _, err := s.RedeemTicket(ctx, tk); !errors.Is(err, ErrBadTicket) {
			t.Fatalf("second use: %v", err)
		}
		tk2, _, _ := s.IssueTicket(ctx, Ticket{UserID: usr}, TicketTTL)
		*now = now.Add(TicketTTL)
		if _, err := s.RedeemTicket(ctx, tk2); !errors.Is(err, ErrBadTicket) {
			t.Fatalf("expired ticket: %v", err)
		}

		// A revocation takes the unused tickets it covers with it.
		for _, r := range []Revocation{{SessionID: sess}, {UserID: usr}, {UserID: usr, LeagueID: lg1}} {
			tk, _, _ := s.IssueTicket(ctx, want, TicketTTL)
			if err := s.Revoke(ctx, r); err != nil {
				t.Fatal(err)
			}
			if _, err := s.RedeemTicket(ctx, tk); !errors.Is(err, ErrBadTicket) {
				t.Fatalf("ticket survived %+v: %v", r, err)
			}
		}
		other, _, _ := s.IssueTicket(ctx, Ticket{UserID: usr, LeagueID: lg2}, TicketTTL)
		_ = s.Revoke(ctx, Revocation{UserID: usr, LeagueID: lg1})
		if _, err := s.RedeemTicket(ctx, other); err != nil {
			t.Fatalf("another league's ticket went: %v", err)
		}
	})
}

// Revoke closes exactly the sockets it covers, once, with the right code.
func TestHubRevoke(t *testing.T) {
	hub := NewHub(NewMemory(time.Now), member(map[string]string{
		"u1/" + lg1: "tm_a", "u1/" + lg2: "tm_c", "u2/" + lg1: "tm_b",
	}))
	type kick struct {
		code   int
		reason string
	}
	connect := func(user, session string, leagues ...string) (*Client, chan kick) {
		k := make(chan kick, 2)
		c := hub.Connect(user, session, func(code int, reason string) { k <- kick{code, reason} })
		for _, l := range leagues {
			if err := hub.Subscribe(ctx, c, l, nil); err != nil {
				t.Fatal(err)
			}
		}
		return c, k
	}
	expect := func(k chan kick, code int) {
		t.Helper()
		select {
		case got := <-k:
			if got.code != code {
				t.Fatalf("close code = %d (%s), want %d", got.code, got.reason, code)
			}
		case <-time.After(time.Second):
			t.Fatalf("not closed (want %d)", code)
		}
	}
	quiet := func(k chan kick) {
		t.Helper()
		select {
		case got := <-k:
			t.Fatalf("closed with %d (%s)", got.code, got.reason)
		case <-time.After(50 * time.Millisecond):
		}
	}

	// A league revocation closes the user's sockets on that league only.
	a1, ka1 := connect("u1", "s1", lg1)
	a2, ka2 := connect("u1", "s1", lg2)
	b, kb := connect("u2", "s2", lg1)
	hub.Revoke(Revocation{UserID: "u1", LeagueID: lg1})
	expect(ka1, CloseForbidden)
	quiet(ka2)
	quiet(kb)
	hub.Publish(Event{LeagueID: lg1, Seq: 1, Type: "league.updated"})
	if len(drain(a1)) != 0 || len(drain(b)) != 1 || hub.Subscribed(lg1) != 1 {
		t.Fatal("a revoked socket still gets events")
	}

	// A session revocation closes that session's sockets only.
	c3, kc3 := connect("u1", "s3", lg2)
	hub.Revoke(Revocation{SessionID: "s1"})
	expect(ka2, CloseSessionEnded)
	quiet(kc3)

	// A user revocation closes every socket of the user, and only once.
	hub.Revoke(Revocation{UserID: "u1"})
	hub.Revoke(Revocation{UserID: "u1"})
	expect(kc3, CloseSessionEnded)
	quiet(kc3)
	quiet(kb)
	if hub.Connected() != 1 {
		t.Fatalf("connected = %d", hub.Connected())
	}
	_, _ = a2, c3

	// A subscribe whose membership check raced a revocation fails.
	slow := make(chan struct{})
	hub2 := NewHub(NewMemory(time.Now), func(context.Context, string, string) (string, error) {
		<-slow
		return "tm_a", nil
	})
	c := hub2.Connect("u1", "s1", nil)
	done := make(chan error)
	go func() { done <- hub2.Subscribe(ctx, c, lg1, nil) }()
	time.Sleep(20 * time.Millisecond)
	hub2.Revoke(Revocation{UserID: "u1", LeagueID: lg1})
	close(slow)
	if err := <-done; !errors.Is(err, ErrRevoked) || hub2.Subscribed(lg1) != 0 {
		t.Fatalf("racing subscribe = %v (%d subscribed)", err, hub2.Subscribed(lg1))
	}
}

func member(teams map[string]string) Membership {
	return func(_ context.Context, userID, leagueID string) (string, error) {
		team, ok := teams[userID+"/"+leagueID]
		if !ok {
			return "", errors.New("not a member")
		}
		return team, nil
	}
}

func drain(c *Client) []string {
	var out []string
	for {
		select {
		case f := <-c.Send:
			out = append(out, string(f))
		default:
			return out
		}
	}
}

func TestHubReplayAndOrder(t *testing.T) {
	st := NewMemory(time.Now)
	hub := NewHub(st, member(map[string]string{"u1/" + lg1: "tm_a", "u2/" + lg1: "tm_b"}))
	pub := &Publisher{Store: st, Hub: hub}
	for range 3 {
		if _, err := pub.Publish(ctx, Event{LeagueID: lg1, Type: "league.updated"}); err != nil {
			t.Fatal(err)
		}
	}
	c := hub.Connect("u1", "s_u1", nil)
	if err := hub.Subscribe(ctx, hub.Connect("u3", "s_u3", nil), lg1, nil); err == nil {
		t.Fatal("a non-member subscribed")
	}
	one := int64(1)
	if err := hub.Subscribe(ctx, c, lg1, &one); err != nil {
		t.Fatal(err)
	}
	pub.Publish(ctx, Event{LeagueID: lg1, Type: "league.updated"})
	pub.Publish(ctx, Event{LeagueID: lg1, Type: "trade.updated", Audience: []string{"tm_b"}})
	pub.Publish(ctx, Event{LeagueID: lg1, Type: "lineup.locked"})
	got := drain(c)
	var seqs []string
	for _, f := range got {
		var m struct{ Seq int64 }
		_ = json.Unmarshal([]byte(f), &m)
		seqs = append(seqs, string(rune('0'+m.Seq)))
	}
	if strings.Join(seqs, "") != "2346" {
		t.Fatalf("seqs = %v (frames %v)", seqs, got)
	}
	// A duplicate delivery (another instance's notification) is dropped.
	ev, _, _ := st.Get(ctx, lg1, 6)
	hub.Publish(ev)
	if len(drain(c)) != 0 {
		t.Fatal("duplicate delivered")
	}
	hub.Unsubscribe(c, lg1)
	pub.Publish(ctx, Event{LeagueID: lg1, Type: "league.updated"})
	if len(drain(c)) != 0 || hub.Subscribed(lg1) != 0 {
		t.Fatal("delivered after unsubscribe")
	}
}

// Live events published during a replay wait and come after it.
func TestHubHoldsLiveEventsDuringReplay(t *testing.T) {
	st := &slowStore{Memory: NewMemory(time.Now), started: make(chan struct{}), release: make(chan struct{})}
	hub := NewHub(st, member(map[string]string{"u1/" + lg1: "tm_a"}))
	pub := &Publisher{Store: st, Hub: hub}
	pub.Publish(ctx, Event{LeagueID: lg1, Type: "a"})
	c := hub.Connect("u1", "s_u1", nil)
	zero := int64(0)
	done := make(chan error)
	go func() { done <- hub.Subscribe(ctx, c, lg1, &zero) }()
	<-st.started
	pub.Publish(ctx, Event{LeagueID: lg1, Type: "b"})
	close(st.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	got := drain(c)
	if len(got) != 2 || !strings.Contains(got[0], `"seq":1`) || !strings.Contains(got[1], `"seq":2`) {
		t.Fatalf("frames = %v", got)
	}
}

type slowStore struct {
	*Memory
	once             sync.Once
	started, release chan struct{}
}

func (s *slowStore) Since(ctx context.Context, leagueID string, after int64) ([]Event, error) {
	evs, err := s.Memory.Since(ctx, leagueID, after)
	s.once.Do(func() { close(s.started) })
	<-s.release
	return evs, err
}

func TestHubKicksSlowClients(t *testing.T) {
	st := NewMemory(time.Now)
	hub := NewHub(st, member(map[string]string{"u1/" + lg1: ""}))
	kicked := make(chan string, 1)
	c := hub.Connect("u1", "s_u1", func(_ int, reason string) { kicked <- reason })
	if err := hub.Subscribe(ctx, c, lg1, nil); err != nil {
		t.Fatal(err)
	}
	pub := &Publisher{Store: st, Hub: hub}
	for range SendBuffer + 1 {
		pub.Publish(ctx, Event{LeagueID: lg1, Type: "league.updated"})
	}
	select {
	case r := <-kicked:
		if r != "slow consumer" {
			t.Fatalf("reason = %q", r)
		}
	case <-time.After(time.Second):
		t.Fatal("slow client not kicked")
	}
}

// Events appended by any instance reach the hub through LISTEN/NOTIFY.
func TestListen(t *testing.T) {
	pool := storetest.New(t)
	if _, err := pool.Exec(ctx, `INSERT INTO seasons (id) VALUES ('2026-27');
		INSERT INTO leagues (id, name, season, status) VALUES ('`+lg1+`', 'L', '2026-27', 'in_season')`); err != nil {
		t.Fatal(err)
	}
	st := NewPostgres(pool, time.Now)
	hub := NewHub(st, member(map[string]string{"u1/" + lg1: "tm_a"}))
	kicked := make(chan int, 1)
	c := hub.Connect("u1", "s_u1", func(code int, _ string) { kicked <- code })
	if err := hub.Subscribe(ctx, c, lg1, nil); err != nil {
		t.Fatal(err)
	}
	lctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go Listen(lctx, pool, st, hub, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Another instance's publisher: no local hub.
	other := &Publisher{Store: NewPostgres(pool, time.Now)}
	deadline := time.After(10 * time.Second)
listening:
	for {
		if _, err := other.Publish(ctx, Event{LeagueID: lg1, Type: "league.updated"}); err != nil {
			t.Fatal(err)
		}
		select {
		case f := <-c.Send:
			if !strings.Contains(string(f), `"type":"league.updated"`) {
				t.Fatalf("frame = %s", f)
			}
			break listening
		case <-time.After(200 * time.Millisecond):
			// The listener may not have started yet; publish again.
		case <-deadline:
			t.Fatal("no event through LISTEN/NOTIFY")
		}
	}
	// Another instance revokes the user's membership: this hub closes
	// the socket.
	if err := other.Store.Revoke(ctx, Revocation{UserID: "u1", LeagueID: lg1}); err != nil {
		t.Fatal(err)
	}
	select {
	case code := <-kicked:
		if code != CloseForbidden {
			t.Fatalf("close code = %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("no revocation through LISTEN/NOTIFY")
	}
}

// A revocation that arrives right after Connect publishes the client
// must reach the transport's close callback, not a placeholder.
func TestHubRevokeRightAfterConnect(t *testing.T) {
	hub := NewHub(NewMemory(time.Now), member(nil))
	kicked := make(chan int, 1)
	hub.Connect("u1", "s1", func(code int, _ string) { kicked <- code })
	hub.Revoke(Revocation{SessionID: "s1"}) // the very next thing that happens
	select {
	case code := <-kicked:
		if code != CloseSessionEnded {
			t.Fatalf("code = %d", code)
		}
	case <-time.After(time.Second):
		t.Fatal("the revocation never closed the socket")
	}
}

// Under -race: the close callback is never written while a revocation
// can read it.
func TestHubConnectRevokeNoRace(t *testing.T) {
	hub := NewHub(NewMemory(time.Now), member(nil))
	for i := range 200 {
		sid := "s" + strconv.Itoa(i)
		done := make(chan struct{})
		go func() { hub.Revoke(Revocation{SessionID: sid}); close(done) }()
		c := hub.Connect("u1", sid, func(int, string) {})
		<-done
		hub.Disconnect(c)
	}
}

// The revocation epochs only live while a subscribe can race them: they
// do not pile up for every user ever revoked.
func TestHubEpochsDoNotGrow(t *testing.T) {
	hub := NewHub(NewMemory(time.Now), member(map[string]string{"u1/" + lg1: "tm_a"}))
	for i := range 1000 {
		hub.Revoke(Revocation{UserID: "gone" + strconv.Itoa(i)})
		hub.Revoke(Revocation{UserID: "left" + strconv.Itoa(i), LeagueID: lg1})
	}
	c := hub.Connect("u1", "s1", nil)
	if err := hub.Subscribe(ctx, c, lg1, nil); err != nil {
		t.Fatal(err)
	}
	hub.Revoke(Revocation{UserID: "u1", LeagueID: lg1})
	if err := hub.Subscribe(ctx, hub.Connect("u2", "s2", nil), lg1, nil); err == nil {
		t.Fatal("a non-member subscribed")
	}
	hub.mu.Lock()
	n := len(hub.epoch)
	hub.mu.Unlock()
	if n != 0 {
		t.Fatalf("hub keeps %d revocation epochs with no subscribe in flight", n)
	}
}
