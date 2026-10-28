package trades

import (
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// A commissioner cannot review a trade of their own team: commissioner
// mode approves it when the window closes, league votes run without the
// override.
func TestCommissionerCannotReviewOwnTrade(t *testing.T) {
	for _, mode := range []string{league.ReviewCommissioner, league.ReviewLeagueVote} {
		t.Run(mode, func(t *testing.T) {
			leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
				ctx := e.Ctx
				f := newFixture(t, e, 4, mode)
				s := f.s
				_, tr, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.MID)[0]}, []string{f.at(t, 1, scoring.MID)[0]}, "")
				if err != nil {
					t.Fatal(err)
				}
				if _, tr, err = s.Accept(ctx, f.users[1], f.l.ID, tr.ID); err != nil || tr.Status != league.TradeInReview {
					t.Fatalf("accept = %+v %v", tr, err)
				}
				v, got, _ := s.Get(ctx, f.users[0], f.l.ID, tr.ID)
				if acts := Actions(got, v.League, f.users[0], f.teams[0]); slices.Contains(acts, ActReview) {
					t.Fatalf("commissioner's actions on own trade = %v", acts)
				}
				for _, d := range []string{DecisionApprove, DecisionVeto} {
					if _, _, err := s.Review(ctx, f.users[0], f.l.ID, tr.ID, d, ""); slug(err) != "own-trade" {
						t.Fatalf("%s own trade: %v", d, err)
					}
				}
				e.Clock.Set(*tr.ReviewEndsAt)
				if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
					t.Fatalf("close: %d %v", n, err)
				}
				if _, got, _ := s.Get(ctx, f.users[0], f.l.ID, tr.ID); got.Status != league.TradeProcessed || got.DecidedBy != "" {
					t.Fatalf("at window close = %+v", got)
				}
			})
		})
	}
}
