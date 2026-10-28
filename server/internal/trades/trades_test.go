package trades

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

func slug(err error) string {
	var e *league.Error
	if errors.As(err, &e) {
		return e.Slug
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

type fixture struct {
	e     *leaguetest.Env
	s     *Service
	l     *league.League
	users []string
	teams []string
	w     *league.World
	notes *leaguetest.Notes
}

func newFixture(t *testing.T, e *leaguetest.Env, n int, review string) *fixture {
	in := league.SettingsInput{TradeReview: &review}
	l, users := e.InSeason(t, n, in)
	notes := &leaguetest.Notes{}
	f := &fixture{e: e, s: New(e.Repo, e.Stats, e.Clock.Now, notes), l: l, users: users, w: e.World(t), notes: notes}
	for _, tm := range l.Teams {
		f.teams = append(f.teams, tm.ID)
	}
	return f
}

// at returns team i's players at pos in gameweek 8, sorted.
func (f *fixture) at(t *testing.T, i int, pos scoring.Position) []string {
	t.Helper()
	var out []string
	for _, id := range f.e.State(t, f.l.ID).Game.RosterAt(f.teams[i], 8) {
		if f.w.Players[id].Position == pos {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func TestTradeWithoutReview(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		ctx, s := e.Ctx, f.s
		mid0, mid1 := f.at(t, 0, scoring.MID)[0], f.at(t, 1, scoring.MID)[0]
		fwd1 := f.at(t, 1, scoring.FWD)[0]

		if _, _, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[0], []string{mid0}, []string{mid0}, ""); slug(err) != "trade-shape" {
			t.Fatalf("self trade: %v", err)
		}
		if _, _, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{mid0}, []string{fwd1}, ""); slug(err) != "trade-shape" {
			t.Fatalf("positions differ: %v", err)
		}
		_, _, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{mid1}, []string{mid0}, "")
		var le *league.Error
		if !errors.As(err, &le) || le.Slug != "trade-shape" || len(le.Errors) != 2 {
			t.Fatalf("players on the wrong sides: %v", err)
		}
		_, offer, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{mid0}, []string{mid1}, "Fair?")
		if err != nil {
			t.Fatal(err)
		}
		if offer.Status != league.TradeOffered || offer.ExpiresAt != f.w.Gameweeks[8].WaiverDeadlineAt.UTC() {
			t.Fatalf("offer = %+v", offer)
		}
		// A competing offer for the same player, from team 3.
		mid2 := f.at(t, 2, scoring.MID)[0]
		_, rival, err := s.Propose(ctx, f.users[2], f.l.ID, f.teams[1], []string{mid2}, []string{mid1}, "")
		if err != nil {
			t.Fatal(err)
		}

		// Only the two teams see an offer; actions depend on the side.
		if _, _, err := s.Get(ctx, f.users[3], f.l.ID, offer.ID); slug(err) != "not-found" {
			t.Fatalf("third team sees the offer: %v", err)
		}
		v, got, _ := s.Get(ctx, f.users[1], f.l.ID, offer.ID)
		if a := Actions(got, v.League, f.users[1], f.teams[1]); !slices.Equal(a, []string{ActAccept, ActDecline}) {
			t.Fatalf("receiver actions = %v", a)
		}
		if a := Actions(got, v.League, f.users[0], f.teams[0]); !slices.Equal(a, []string{ActWithdraw}) {
			t.Fatalf("offerer actions = %v", a)
		}
		if _, _, err := s.Accept(ctx, f.users[0], f.l.ID, offer.ID); slug(err) != "not-your-trade" {
			t.Fatalf("offerer accepts: %v", err)
		}

		_, done, err := s.Accept(ctx, f.users[1], f.l.ID, offer.ID)
		if err != nil {
			t.Fatal(err)
		}
		if done.Status != league.TradeProcessed || done.EffectiveGW != 8 || done.ProcessedAt == nil {
			t.Fatalf("accepted = %+v", done)
		}
		st := e.State(t, f.l.ID)
		own := st.Game.OwnerAt(8)
		if own[mid0] != f.teams[1] || own[mid1] != f.teams[0] {
			t.Fatalf("players did not move")
		}
		if st.Game.OwnerAt(7)[mid0] != "" && st.Game.OwnerAt(7)[mid0] != f.teams[0] {
			t.Fatal("gameweek 7 changed")
		}
		if lu, _ := st.Game.LineupAt(f.teams[0], 8); !slices.Contains(lu.Slots, mid1) {
			t.Fatalf("team 1's lineup lacks the new player")
		}
		all, _ := e.Repo.Trades(ctx, f.l.ID)
		for _, tr := range all {
			if tr.ID == rival.ID && (tr.Status != league.TradeFailed || tr.FailureReason != league.TradeFailNotOnRoster) {
				t.Fatalf("rival offer = %+v", tr)
			}
		}
		// Now the whole league sees it; nobody can act on it.
		v, got, err = s.Get(ctx, f.users[3], f.l.ID, offer.ID)
		if err != nil || len(Actions(got, v.League, f.users[3], f.teams[3])) != 0 {
			t.Fatalf("processed trade for a third team: %v", err)
		}
		if _, _, err := s.Decline(ctx, f.users[1], f.l.ID, offer.ID); slug(err) != "trade-not-open" {
			t.Fatalf("decline after processing: %v", err)
		}
		txns, _ := e.Repo.Transactions(ctx, f.l.ID)
		if len(txns) != 1 || txns[0].Kind != league.TxnTrade || txns[0].Status != league.TradeProcessed {
			t.Fatalf("feed = %+v", txns)
		}
		if !slices.Contains(f.notes.Kinds(), league.ChangeTrade) {
			t.Fatal("no trade change reported")
		}

		// Decline and withdraw.
		def0, def1 := f.at(t, 0, scoring.DEF)[0], f.at(t, 1, scoring.DEF)[0]
		_, o2, _ := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{def0}, []string{def1}, "")
		if _, d, err := s.Decline(ctx, f.users[1], f.l.ID, o2.ID); err != nil || d.Status != league.TradeDeclined {
			t.Fatalf("decline = %+v %v", d, err)
		}
		_, o3, _ := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{def0}, []string{def1}, "")
		if _, _, err := s.Withdraw(ctx, f.users[1], f.l.ID, o3.ID); slug(err) != "not-your-trade" {
			t.Fatalf("receiver withdraws: %v", err)
		}
		if _, d, err := s.Withdraw(ctx, f.users[0], f.l.ID, o3.ID); err != nil || d.Status != league.TradeWithdrawn {
			t.Fatalf("withdraw = %+v %v", d, err)
		}
		// A declined offer between two teams stays private to them.
		if _, _, err := s.Get(ctx, f.users[3], f.l.ID, o2.ID); slug(err) != "not-found" {
			t.Fatalf("third team sees a declined offer: %v", err)
		}

		// Unanswered offers expire at the next waiver run.
		_, o4, _ := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{def0}, []string{def1}, "")
		if n, err := s.CloseDue(ctx, f.l.ID); n != 0 || err != nil {
			t.Fatalf("early close: %d %v", n, err)
		}
		e.Clock.Set(o4.ExpiresAt)
		if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		if _, got, _ := s.Get(ctx, f.users[0], f.l.ID, o4.ID); got.Status != league.TradeExpired {
			t.Fatalf("expired = %+v", got)
		}
	})
}

func TestCommissionerReview(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewCommissioner)
		ctx, s := e.Ctx, f.s
		propose := func(pos scoring.Position, k int) league.Trade {
			t.Helper()
			_, tr, err := s.Propose(ctx, f.users[1], f.l.ID, f.teams[2], []string{f.at(t, 1, pos)[k]}, []string{f.at(t, 2, pos)[k]}, "")
			if err != nil {
				t.Fatal(err)
			}
			_, tr, err = s.Accept(ctx, f.users[2], f.l.ID, tr.ID)
			if err != nil {
				t.Fatal(err)
			}
			return tr
		}
		a := propose(scoring.MID, 0)
		if a.Status != league.TradeInReview || a.ReviewMode != league.ReviewCommissioner || a.ReviewEndsAt == nil ||
			!a.ReviewEndsAt.Equal(e.Clock.Now().Add(24*time.Hour)) {
			t.Fatalf("in review = %+v", a)
		}
		// The rest of the league sees it now; only the commissioner reviews.
		v, got, err := s.Get(ctx, f.users[3], f.l.ID, a.ID)
		if err != nil || len(Actions(got, v.League, f.users[3], f.teams[3])) != 0 {
			t.Fatalf("third team: %v %v", err, Actions(got, v.League, f.users[3], f.teams[3]))
		}
		if acts := Actions(got, v.League, f.users[0], f.teams[0]); !slices.Equal(acts, []string{ActReview}) {
			t.Fatalf("commissioner actions = %v", acts)
		}
		if _, _, err := s.Review(ctx, f.users[3], f.l.ID, a.ID, DecisionApprove, ""); slug(err) != "not-commissioner" {
			t.Fatalf("member reviews: %v", err)
		}
		if _, _, err := s.Vote(ctx, f.users[3], f.l.ID, a.ID, VoteVeto); slug(err) != "no-league-vote" {
			t.Fatalf("vote in commissioner mode: %v", err)
		}
		// Players in review cannot be offered again.
		if _, _, err := s.Propose(ctx, f.users[3], f.l.ID, f.teams[1], []string{f.at(t, 3, scoring.MID)[0]}, []string{a.Offered[0]}, ""); slug(err) != "player-in-trade-review" {
			t.Fatalf("offer a player in review: %v", err)
		}
		_, got, err = s.Review(ctx, f.users[0], f.l.ID, a.ID, DecisionApprove, "")
		if err != nil || got.Status != league.TradeProcessed || got.DecidedBy != f.users[0] {
			t.Fatalf("approved = %+v %v", got, err)
		}

		b := propose(scoring.DEF, 0)
		_, got, err = s.Review(ctx, f.users[0], f.l.ID, b.ID, DecisionVeto, "Lopsided")
		if err != nil || got.Status != league.TradeVetoed || got.DecisionReason != "Lopsided" {
			t.Fatalf("vetoed = %+v %v", got, err)
		}
		if _, _, err := s.Review(ctx, f.users[0], f.l.ID, b.ID, DecisionApprove, ""); slug(err) != "trade-not-in-review" {
			t.Fatalf("review twice: %v", err)
		}

		// Undecided when the window closes: approved.
		c := propose(scoring.FWD, 0)
		e.Clock.Set(c.ReviewEndsAt.Add(-time.Second))
		if n, _ := s.CloseDue(ctx, f.l.ID); n != 0 {
			t.Fatal("closed early")
		}
		e.Clock.Set(*c.ReviewEndsAt)
		if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		_, got, _ = s.Get(ctx, f.users[1], f.l.ID, c.ID)
		if got.Status != league.TradeProcessed || got.DecidedBy != "" {
			t.Fatalf("approved at window close = %+v", got)
		}
		txns, _ := e.Repo.Transactions(ctx, f.l.ID)
		if len(txns) != 3 {
			t.Fatalf("feed = %d entries", len(txns))
		}
	})
}

func TestLeagueVote(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 6, league.ReviewLeagueVote)
		ctx, s := e.Ctx, f.s
		offer := func(pos scoring.Position) league.Trade {
			t.Helper()
			_, tr, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, pos)[0]}, []string{f.at(t, 1, pos)[0]}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, tr, err = s.Accept(ctx, f.users[1], f.l.ID, tr.ID); err != nil {
				t.Fatal(err)
			}
			return tr
		}
		a := offer(scoring.MID)
		v, _ := s.List(ctx, f.users[2], f.l.ID)
		if n := VotesNeeded(v.League, a); n != 2 {
			t.Fatalf("votes needed = %d, want ceil(0.5 × 4) = 2", n)
		}
		if acts := Actions(a, v.League, f.users[2], f.teams[2]); !slices.Equal(acts, []string{ActVote}) {
			t.Fatalf("voter actions = %v", acts)
		}
		if _, _, err := s.Vote(ctx, f.users[1], f.l.ID, a.ID, VoteVeto); slug(err) != "in-trade" {
			t.Fatalf("vote on own trade: %v", err)
		}
		if _, got, err := s.Vote(ctx, f.users[2], f.l.ID, a.ID, VoteVeto); err != nil || got.Status != league.TradeInReview {
			t.Fatalf("first veto = %+v %v", got, err)
		}
		// A vote can change.
		if _, got, _ := s.Vote(ctx, f.users[2], f.l.ID, a.ID, VoteAllow); got.Votes[f.teams[2]] != VoteAllow {
			t.Fatalf("changed vote = %+v", got.Votes)
		}
		s.Vote(ctx, f.users[2], f.l.ID, a.ID, VoteVeto)
		_, got, err := s.Vote(ctx, f.users[3], f.l.ID, a.ID, VoteVeto)
		if err != nil || got.Status != league.TradeVetoed {
			t.Fatalf("second veto = %+v %v", got, err)
		}
		if _, _, err := s.Vote(ctx, f.users[4], f.l.ID, a.ID, VoteVeto); slug(err) != "trade-not-in-review" {
			t.Fatalf("vote after the veto: %v", err)
		}

		// One veto is not enough: processed when the window closes.
		b := offer(scoring.DEF)
		s.Vote(ctx, f.users[4], f.l.ID, b.ID, VoteVeto)
		s.Vote(ctx, f.users[5], f.l.ID, b.ID, VoteAllow)
		e.Clock.Set(*b.ReviewEndsAt)
		if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		_, got, _ = s.Get(ctx, f.users[0], f.l.ID, b.ID)
		if got.Status != league.TradeProcessed {
			t.Fatalf("after the window = %+v", got)
		}
		if veto, allow := Tally(got); veto != 1 || allow != 1 {
			t.Fatalf("tally %d/%d", veto, allow)
		}
	})
}

// Processing re-checks the rosters: a player who left fails the trade.
func TestProcessingRevalidates(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewCommissioner)
		ctx, s := e.Ctx, f.s
		// Between teams 2 and 3: the commissioner (team 1) reviews it.
		out, in := f.at(t, 1, scoring.FWD)[0], f.at(t, 2, scoring.FWD)[0]
		_, tr, _ := s.Propose(ctx, f.users[1], f.l.ID, f.teams[2], []string{out}, []string{in}, "")
		_, tr, err := s.Accept(ctx, f.users[2], f.l.ID, tr.ID)
		if err != nil {
			t.Fatal(err)
		}
		// The offered player leaves (as a deleted account's cleanup could).
		err = e.Repo.Tx(ctx, func(tx league.Tx) error {
			st, err := tx.LoadState(ctx, f.l.ID)
			if err != nil {
				return err
			}
			st.Game.Release(f.teams[1], out, 8)
			return tx.SaveState(ctx, st)
		})
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := s.Review(ctx, f.users[0], f.l.ID, tr.ID, DecisionApprove, "")
		if err != nil || got.Status != league.TradeFailed || got.FailureReason != league.TradeFailNotOnRoster {
			t.Fatalf("approved but invalid = %+v %v", got, err)
		}
		if e.State(t, f.l.ID).Game.OwnerAt(8)[in] != f.teams[2] {
			t.Fatal("a failed trade moved a player")
		}
	})
}

// Accept and withdraw racing: exactly one wins.
func TestAcceptWithdrawRace(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		ctx, s := e.Ctx, f.s
		_, tr, err := s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.GKP)[0]}, []string{f.at(t, 1, scoring.GKP)[0]}, "")
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		var acc, wd error
		wg.Add(2)
		go func() { defer wg.Done(); _, _, acc = s.Accept(ctx, f.users[1], f.l.ID, tr.ID) }()
		go func() { defer wg.Done(); _, _, wd = s.Withdraw(ctx, f.users[0], f.l.ID, tr.ID) }()
		wg.Wait()
		if (acc == nil) == (wd == nil) {
			t.Fatalf("accept %v, withdraw %v: want exactly one to win", acc, wd)
		}
		loser := acc
		if acc == nil {
			loser = wd
		}
		if slug(loser) != "trade-not-open" {
			t.Fatalf("loser: %v", loser)
		}
	})
}
