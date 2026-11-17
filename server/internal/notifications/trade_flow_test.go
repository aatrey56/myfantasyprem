package notifications

import (
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
)

// tradeFlow is a 6-team league on the default review (a league vote, 24
// hours, a third of the 4 managers outside a trade: 2 vetoes), with the
// trade service reporting to Changes, as cmd/api wires it.
type tradeFlow struct {
	e     *leaguetest.Env
	s     *trades.Service
	store Store
	l     *league.League
	users []string
	teams []string
}

func newTradeFlow(t *testing.T, e *leaguetest.Env) *tradeFlow {
	t.Helper()
	l, users := e.InSeason(t, 6, league.SettingsInput{})
	var st Store = NewMemory(e.Clock.Now)
	if e.Pool != nil {
		st = NewPostgres(e.Pool, e.Clock.Now)
	}
	ch := &Changes{Leagues: e.Repo, Stats: e.Stats, Notifier: NewNotifier(st), Log: e.Log}
	f := &tradeFlow{e: e, s: trades.New(e.Repo, e.Stats, e.Clock.Now, ch), store: st, l: l, users: users}
	for _, tm := range l.Teams {
		f.teams = append(f.teams, tm.ID)
	}
	return f
}

// accepted has team 0 offer a midfielder for team 1's, and team 1 accept.
func (f *tradeFlow) accepted(t *testing.T) league.Trade {
	t.Helper()
	ctx, w := f.e.Ctx, f.e.World(t)
	mid := func(i int) string {
		for _, id := range f.e.State(t, f.l.ID).Game.RosterAt(f.teams[i], 8) {
			if w.Players[id].Position == scoring.MID {
				return id
			}
		}
		t.Fatalf("team %d has no midfielder", i)
		return ""
	}
	_, tr, err := f.s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{mid(0)}, []string{mid(1)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, tr, err = f.s.Accept(ctx, f.users[1], f.l.ID, tr.ID); err != nil {
		t.Fatal(err)
	}
	if tr.Status != league.TradeInReview {
		t.Fatalf("accepted = %s", tr.Status)
	}
	return tr
}

// inbox lists the types in manager i's inbox, sorted.
func (f *tradeFlow) inbox(t *testing.T, i int) []Type {
	t.Helper()
	ns, _, err := f.store.List(f.e.Ctx, f.users[i], 100, false)
	if err != nil {
		t.Fatal(err)
	}
	var out []Type
	for _, n := range ns {
		out = append(out, n.Type)
	}
	slices.Sort(out)
	return out
}

func (f *tradeFlow) want(t *testing.T, i int, want ...Type) {
	t.Helper()
	slices.Sort(want)
	if got := f.inbox(t, i); !slices.Equal(got, want) {
		t.Errorf("manager %d's inbox = %v, want %v", i, got, want)
	}
}

// A trade's outcome reaches the managers through the real service path:
// the change is reported after the commit, when the trade is no longer
// open (League.State holds open trades only).
func TestTradeOutcomeNotifications(t *testing.T) {
	t.Run("vetoed", func(t *testing.T) {
		leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
			f := newTradeFlow(t, e)
			tr := f.accepted(t)
			for _, i := range []int{2, 3} {
				if _, _, err := f.s.Vote(e.Ctx, f.users[i], f.l.ID, tr.ID, trades.VoteVeto); err != nil {
					t.Fatal(err)
				}
			}
			f.want(t, 0, TradeAccepted, TradeReviewed)
			f.want(t, 1, TradeOffered, TradeReviewed)
			for _, i := range []int{2, 3, 4, 5} {
				f.want(t, i, LeagueTradeInReview, LeagueTradeVetoed)
			}
		})
	})
	t.Run("processed", func(t *testing.T) {
		leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
			f := newTradeFlow(t, e)
			tr := f.accepted(t)
			e.Clock.Set(*tr.ReviewEndsAt)
			if n, err := f.s.CloseDue(e.Ctx, f.l.ID); n != 1 || err != nil {
				t.Fatalf("close: %d %v", n, err)
			}
			f.want(t, 0, TradeAccepted, TradeReviewed, TradeProcessed)
			f.want(t, 1, TradeOffered, TradeReviewed, TradeProcessed)
			for _, i := range []int{2, 3, 4, 5} {
				f.want(t, i, LeagueTradeInReview, LeagueTradeProcessed)
			}
		})
	})
	t.Run("failed", func(t *testing.T) {
		leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
			f := newTradeFlow(t, e)
			tr := f.accepted(t)
			err := e.Repo.Tx(e.Ctx, func(tx league.Tx) error {
				st, err := tx.LoadState(e.Ctx, f.l.ID)
				if err != nil {
					return err
				}
				st.Game.Release(f.teams[0], tr.Offered[0], 8)
				return tx.SaveState(e.Ctx, st)
			})
			if err != nil {
				t.Fatal(err)
			}
			e.Clock.Set(*tr.ReviewEndsAt)
			if n, err := f.s.CloseDue(e.Ctx, f.l.ID); n != 1 || err != nil {
				t.Fatalf("close: %d %v", n, err)
			}
			f.want(t, 0, TradeAccepted, TradeFailed)
			f.want(t, 1, TradeOffered, TradeFailed)
			for _, i := range []int{2, 3, 4, 5} {
				f.want(t, i, LeagueTradeInReview)
			}
		})
	})
}
