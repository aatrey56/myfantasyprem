package trades

import (
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Every action checks its deadline itself, without waiting for the tick.
func TestDeadlinesWithoutTheTick(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		ctx := e.Ctx
		f := newFixture(t, e, 6, league.ReviewLeagueVote)
		s := f.s
		offer := func(k int) league.Trade {
			t.Helper()
			_, tr, err := s.Propose(ctx, f.users[1], f.l.ID, f.teams[2], []string{f.at(t, 1, scoring.DEF)[k]}, []string{f.at(t, 2, scoring.DEF)[k]}, "")
			if err != nil {
				t.Fatal(err)
			}
			return tr
		}
		a, b, c := offer(0), offer(1), offer(2)
		e.Clock.Set(a.ExpiresAt)
		if _, _, err := s.Accept(ctx, f.users[2], f.l.ID, a.ID); slug(err) != "trade-expired" {
			t.Fatalf("accept after expiry: %v", err)
		}
		if _, _, err := s.Decline(ctx, f.users[2], f.l.ID, b.ID); slug(err) != "trade-expired" {
			t.Fatalf("decline after expiry: %v", err)
		}
		if _, _, err := s.Withdraw(ctx, f.users[1], f.l.ID, c.ID); slug(err) != "trade-expired" {
			t.Fatalf("withdraw after expiry: %v", err)
		}

		// A review window that has closed takes no more votes or decisions.
		_, d, err := s.Propose(ctx, f.users[1], f.l.ID, f.teams[2], []string{f.at(t, 1, scoring.FWD)[0]}, []string{f.at(t, 2, scoring.FWD)[0]}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, d, err = s.Accept(ctx, f.users[2], f.l.ID, d.ID); err != nil || d.Status != league.TradeInReview {
			t.Fatalf("accept = %+v %v", d, err)
		}
		e.Clock.Set(*d.ReviewEndsAt)
		if _, _, err := s.Vote(ctx, f.users[3], f.l.ID, d.ID, VoteVeto); slug(err) != "trade-review-closed" {
			t.Fatalf("vote after the window: %v", err)
		}
		if _, _, err := s.Review(ctx, f.users[0], f.l.ID, d.ID, DecisionVeto, ""); slug(err) != "trade-review-closed" {
			t.Fatalf("review after the window: %v", err)
		}
		if n, err := s.CloseDue(ctx, f.l.ID); n != 4 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		if _, got, _ := s.Get(ctx, f.users[1], f.l.ID, d.ID); got.Status != league.TradeProcessed {
			t.Fatalf("after the window = %+v", got)
		}
	})
}
