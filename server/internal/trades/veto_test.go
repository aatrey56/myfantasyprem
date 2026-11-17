package trades

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// voteFixture is a 12-team league on the default settings (ADR 0017): a
// league vote, 24 hours, a third of the 10 managers not in a trade (4).
func voteFixture(t *testing.T, e *leaguetest.Env) *fixture {
	l, users := e.InSeason(t, 12, league.SettingsInput{})
	notes := &leaguetest.Notes{}
	f := &fixture{e: e, s: New(e.Repo, e.Stats, e.Clock.Now, notes), l: l, users: users, w: e.World(t), notes: notes}
	for _, tm := range l.Teams {
		f.teams = append(f.teams, tm.ID)
	}
	return f
}

// accepted proposes a trade between teams 0 and 1 and has team 1 accept.
func (f *fixture) accepted(t *testing.T, pos scoring.Position) league.Trade {
	t.Helper()
	ctx := f.e.Ctx
	_, tr, err := f.s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, pos)[0]}, []string{f.at(t, 1, pos)[0]}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, tr, err = f.s.Accept(ctx, f.users[1], f.l.ID, tr.ID); err != nil {
		t.Fatal(err)
	}
	return tr
}

// trade reads a trade as its offering manager sees it.
func (f *fixture) trade(t *testing.T, id string) league.Trade {
	t.Helper()
	_, tr, err := f.s.Get(f.e.Ctx, f.users[0], f.l.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	return tr
}

func TestDefaultReviewIsAVeto(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		s := f.l.Settings
		if s.TradeReview != league.ReviewLeagueVote || s.ReviewHours != 24 {
			t.Fatalf("defaults = %+v", s)
		}
		tr := f.accepted(t, scoring.MID)
		if tr.Status != league.TradeInReview || tr.ReviewMode != league.ReviewLeagueVote ||
			tr.ReviewEndsAt == nil || !tr.ReviewEndsAt.Equal(e.Clock.Now().Add(24*time.Hour)) {
			t.Fatalf("accepted = %+v", tr)
		}
		if n := VotesNeeded(f.l, tr); n != 4 {
			t.Fatalf("votes needed = %d, want 4 of 10", n)
		}
		// The contract's number is a float32: a threshold that went through
		// one must not tip a third of 9 managers (3) up to 4.
		for managers, want := range map[int]int{3: 1, 6: 2, 9: 3, 10: 4, 18: 6} {
			l := &league.League{Settings: league.Settings{VetoThreshold: float64(float32(league.DefaultVetoThreshold))}}
			for i := range managers + 2 {
				l.Teams = append(l.Teams, league.Team{ID: string(rune('a' + i)), OwnerID: "u" + string(rune('a'+i))})
			}
			if got := VotesNeeded(l, league.Trade{OfferingTeamID: "a", ReceivingTeamID: "b"}); got != want {
				t.Errorf("%d managers: %d votes needed, want %d", managers, got, want)
			}
		}
	})
}

// Eligibility, one ballot per manager, withdrawing, and the threshold.
func TestVetoVoting(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		tr := f.accepted(t, scoring.MID)
		vote := func(i int, v string) (league.Trade, error) {
			_, got, err := s.Vote(ctx, f.users[i], f.l.ID, tr.ID, v)
			return got, err
		}

		// Only managers outside the trade vote; the two teams may not.
		for _, i := range []int{0, 1} {
			if _, err := vote(i, VoteVeto); slug(err) != "in-trade" {
				t.Fatalf("team %d votes on its own trade: %v", i, err)
			}
		}
		v, _ := s.List(ctx, f.users[2], f.l.ID)
		if acts := Actions(tr, v.League, f.users[2], f.teams[2]); !slices.Equal(acts, []string{ActVote}) {
			t.Fatalf("eligible manager's actions = %v", acts)
		}
		if acts := Actions(tr, v.League, f.users[1], f.teams[1]); len(acts) != 0 {
			t.Fatalf("a team in the trade has actions %v", acts)
		}
		if _, err := vote(2, "abstain"); slug(err) != "invalid-request" {
			t.Fatalf("unknown vote: %v", err)
		}

		// One ballot each: vetoing twice counts once.
		for range 2 {
			if got, err := vote(2, VoteVeto); err != nil {
				t.Fatal(err)
			} else if veto, _ := Tally(got); veto != 1 {
				t.Fatalf("after a repeated veto: %d vetoes", veto)
			}
		}
		// A veto can be withdrawn while the review is open, and withdrawing
		// twice is harmless.
		for range 2 {
			got, err := vote(2, VoteWithdraw)
			if err != nil {
				t.Fatal(err)
			}
			if veto, allow := Tally(got); veto != 0 || allow != 0 || len(got.Votes) != 0 {
				t.Fatalf("after withdrawing: %+v", got.Votes)
			}
		}
		if got := f.trade(t, tr.ID); len(got.Votes) != 0 || got.Status != league.TradeInReview {
			t.Fatalf("stored trade after withdrawing = %+v", got)
		}

		// Three vetoes of four needed leave it in review; the fourth vetoes it
		// at once.
		for i := 2; i <= 4; i++ {
			if got, err := vote(i, VoteVeto); err != nil || got.Status != league.TradeInReview {
				t.Fatalf("veto %d = %+v %v", i, got.Status, err)
			}
		}
		got, err := vote(5, VoteVeto)
		if err != nil || got.Status != league.TradeVetoed || got.ProcessedAt == nil {
			t.Fatalf("fourth veto = %+v %v", got, err)
		}
		// Nothing moves, and neither a vote, a withdrawal nor the window's end changes it.
		offered, requested := tr.Offered[0], tr.Requested[0]
		if owners := e.State(t, f.l.ID).Game.OwnerAt(8); owners[offered] != f.teams[0] || owners[requested] != f.teams[1] {
			t.Fatal("a vetoed trade moved players")
		}
		if _, err := vote(6, VoteVeto); slug(err) != "trade-not-in-review" {
			t.Fatalf("vote after the veto: %v", err)
		}
		if _, err := vote(5, VoteWithdraw); slug(err) != "trade-not-in-review" {
			t.Fatalf("withdraw after the veto: %v", err)
		}
		e.Clock.Set(tr.ReviewEndsAt.Add(time.Minute))
		if n, err := s.CloseDue(ctx, f.l.ID); n != 0 || err != nil {
			t.Fatalf("close after a veto: %d %v", n, err)
		}
		if got := f.trade(t, tr.ID); got.Status != league.TradeVetoed {
			t.Fatalf("after the window = %s", got.Status)
		}
	})
}

// A trade that is not vetoed goes through by itself when the review ends.
func TestReviewEndExecutes(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		tr := f.accepted(t, scoring.DEF)
		for i := 2; i <= 4; i++ { // one short of the threshold
			if _, _, err := s.Vote(ctx, f.users[i], f.l.ID, tr.ID, VoteVeto); err != nil {
				t.Fatal(err)
			}
		}
		// Not before the window closes.
		e.Clock.Set(tr.ReviewEndsAt.Add(-time.Second))
		if n, err := s.CloseDue(ctx, f.l.ID); n != 0 || err != nil {
			t.Fatalf("close early: %d %v", n, err)
		}
		e.Clock.Set(*tr.ReviewEndsAt)
		if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		got := f.trade(t, tr.ID)
		if got.Status != league.TradeProcessed || got.EffectiveGW == 0 {
			t.Fatalf("after the window = %+v", got)
		}
		owners := e.State(t, f.l.ID).Game.OwnerAt(got.EffectiveGW)
		if owners[tr.Offered[0]] != f.teams[1] || owners[tr.Requested[0]] != f.teams[0] {
			t.Fatal("the players did not change teams")
		}
	})
}

// Several instances (or a job and the tick) closing the same review run it
// once: one trade processed, one feed entry, the players swapped once.
func TestReviewEndIsIdempotentUnderConcurrency(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		tr := f.accepted(t, scoring.FWD)
		e.Clock.Set(*tr.ReviewEndsAt)
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			changed int
		)
		for range 6 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				n, err := s.CloseDue(ctx, f.l.ID)
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				changed += n
				mu.Unlock()
			}()
		}
		wg.Wait()
		if changed != 1 {
			t.Fatalf("%d closes did work, want exactly one", changed)
		}
		feed, err := e.Repo.Transactions(ctx, f.l.ID)
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, x := range feed {
			if x.Kind == league.TxnTrade && x.TradeID == tr.ID {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("%d feed entries for the trade, want 1", n)
		}
		// And once more, later: nothing left to do.
		if n, err := s.CloseDue(ctx, f.l.ID); n != 0 || err != nil {
			t.Fatalf("close again: %d %v", n, err)
		}
		owners := e.State(t, f.l.ID).Game.OwnerAt(f.trade(t, tr.ID).EffectiveGW)
		if owners[tr.Offered[0]] != f.teams[1] || owners[tr.Requested[0]] != f.teams[0] {
			t.Fatal("the players did not change teams exactly once")
		}
	})
}

// The rosters are checked again when the review ends: a trade whose player
// left fails with its reason and moves nobody.
func TestReviewEndRevalidates(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		tr := f.accepted(t, scoring.MID)
		out := tr.Offered[0]
		err := e.Repo.Tx(ctx, func(tx league.Tx) error {
			st, err := tx.LoadState(ctx, f.l.ID)
			if err != nil {
				return err
			}
			st.Game.Release(f.teams[0], out, 8)
			return tx.SaveState(ctx, st)
		})
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(*tr.ReviewEndsAt)
		if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		got := f.trade(t, tr.ID)
		if got.Status != league.TradeFailed || got.FailureReason != league.TradeFailNotOnRoster {
			t.Fatalf("after the window = %+v", got)
		}
		if e.State(t, f.l.ID).Game.OwnerAt(8)[tr.Requested[0]] != f.teams[1] {
			t.Fatal("a failed trade moved a player")
		}
	})
}

// An offer made before a league's review setting changed (migration 24)
// still works: accepted afterwards, it enters the new review.
func TestOpenOfferSurvivesTheSwitch(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 12, league.ReviewNone)
		ctx := e.Ctx
		_, tr, err := f.s.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.GKP)[0]}, []string{f.at(t, 1, scoring.GKP)[0]}, "")
		if err != nil {
			t.Fatal(err)
		}
		err = e.Repo.Tx(ctx, func(tx league.Tx) error {
			l, err := tx.Load(ctx, f.l.ID)
			if err != nil {
				return err
			}
			l.Settings.TradeReview = league.ReviewLeagueVote
			l.Settings.ReviewHours = league.DefaultReviewHours
			l.Settings.VetoThreshold = league.DefaultVetoThreshold
			return tx.Save(ctx, l)
		})
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := f.s.Accept(ctx, f.users[1], f.l.ID, tr.ID)
		if err != nil || got.Status != league.TradeInReview || got.ReviewMode != league.ReviewLeagueVote {
			t.Fatalf("accepted after the switch = %+v %v", got, err)
		}
	})
}

// The commissioner overriding a trade is logged in the same transaction;
// the members' votes are not.
func TestCommissionerReviewIsLogged(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		offer := func(pos scoring.Position) league.Trade {
			t.Helper()
			_, tr, err := s.Propose(ctx, f.users[1], f.l.ID, f.teams[2], []string{f.at(t, 1, pos)[0]}, []string{f.at(t, 2, pos)[0]}, "")
			if err != nil {
				t.Fatal(err)
			}
			if _, tr, err = s.Accept(ctx, f.users[2], f.l.ID, tr.ID); err != nil {
				t.Fatal(err)
			}
			return tr
		}
		log := func() []league.AuditEntry {
			all, err := e.Repo.Audit(ctx, f.l.ID, 0, 50)
			if err != nil {
				t.Fatal(err)
			}
			// Filling the league took invites; those are not what is tested.
			return slices.DeleteFunc(all, func(x league.AuditEntry) bool { return x.Action == league.AuditInviteCreated })
		}
		a := offer(scoring.DEF)
		if _, _, err := s.Vote(ctx, f.users[3], f.l.ID, a.ID, VoteVeto); err != nil {
			t.Fatal(err)
		}
		if len(log()) != 0 {
			t.Fatalf("a member's vote was logged: %+v", log())
		}
		// The commissioner voting and withdrawing as a manager is not a
		// commissioner move either.
		for _, v := range []string{VoteVeto, VoteWithdraw} {
			if _, _, err := s.Vote(ctx, f.users[0], f.l.ID, a.ID, v); err != nil {
				t.Fatal(err)
			}
		}
		if len(log()) != 0 {
			t.Fatalf("the commissioner's own vote was logged: %+v", log())
		}
		// A refused review writes nothing either.
		if _, _, err := s.Review(ctx, f.users[3], f.l.ID, a.ID, DecisionVeto, ""); slug(err) != "not-commissioner" {
			t.Fatalf("manager review: %v", err)
		}
		if len(log()) != 0 {
			t.Fatalf("a refused review was logged: %+v", log())
		}
		if _, _, err := s.Review(ctx, f.users[0], f.l.ID, a.ID, DecisionVeto, "Not fair"); err != nil {
			t.Fatal(err)
		}
		b := offer(scoring.MID)
		if _, _, err := s.Review(ctx, f.users[0], f.l.ID, b.ID, DecisionApprove, ""); err != nil {
			t.Fatal(err)
		}
		es := log()
		if len(es) != 2 || es[0].Action != league.AuditTradeApproved || es[1].Action != league.AuditTradeVetoed ||
			es[1].ActorID != f.users[0] || !slices.Equal(es[1].TeamIDs, []string{f.teams[1], f.teams[2]}) ||
			es[1].Before["status"] != league.TradeInReview || es[1].After["status"] != league.TradeVetoed {
			t.Fatalf("log = %+v", es)
		}
	})
}

// The commissioner can change the league's review at any time (ADR 0017): a
// trade already in review keeps the mode and window it was accepted with,
// and only offers accepted afterwards use the new mode.
func TestInReviewTradeKeepsItsModeWhenTheLeagueChanges(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 12, league.ReviewCommissioner)
		ctx := e.Ctx
		propose := func(a, b int, pos scoring.Position) league.Trade {
			t.Helper()
			_, tr, err := f.s.Propose(ctx, f.users[a], f.l.ID, f.teams[b], []string{f.at(t, a, pos)[0]}, []string{f.at(t, b, pos)[0]}, "")
			if err != nil {
				t.Fatal(err)
			}
			return tr
		}
		accept := func(b int, tr league.Trade) league.Trade {
			t.Helper()
			_, got, err := f.s.Accept(ctx, f.users[b], f.l.ID, tr.ID)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}
		ptr := func(v string) *string { return &v }
		hours := 48
		first := accept(1, propose(0, 1, scoring.GKP))
		// An offer made while the league was on commissioner review, still open.
		open := propose(2, 3, scoring.DEF)
		if first.ReviewMode != league.ReviewCommissioner {
			t.Fatalf("first = %+v", first)
		}
		window := *first.ReviewEndsAt

		e.Clock.Advance(time.Hour)
		l, err := e.Leagues.Update(ctx, f.users[0], f.l.ID, nil, league.SettingsInput{
			TradeReview: ptr(league.ReviewLeagueVote), ReviewHours: &hours,
		})
		if err != nil || l.Settings.TradeReview != league.ReviewLeagueVote || l.Settings.ReviewHours != 48 {
			t.Fatalf("switch: %+v %v", l, err)
		}

		// The trade in review is still a commissioner review with its window,
		// and nobody can veto it by vote.
		got := f.trade(t, first.ID)
		if got.Status != league.TradeInReview || got.ReviewMode != league.ReviewCommissioner || !got.ReviewEndsAt.Equal(window) {
			t.Fatalf("in-review trade after the switch = %+v", got)
		}
		if _, _, err := f.s.Vote(ctx, f.users[4], f.l.ID, first.ID, VoteVeto); slug(err) != "no-league-vote" {
			t.Fatalf("vote on a commissioner review: %v", err)
		}
		// An offer accepted after the change uses the new mode and period.
		second := accept(3, open)
		if second.ReviewMode != league.ReviewLeagueVote || !second.ReviewEndsAt.Equal(e.Clock.Now().Add(48*time.Hour)) {
			t.Fatalf("accepted after the switch = %+v", second)
		}
		if _, _, err := f.s.Vote(ctx, f.users[4], f.l.ID, second.ID, VoteVeto); err != nil {
			t.Fatalf("vote on the new trade: %v", err)
		}
		// And the old window ends on its own schedule, not the new 48 hours.
		e.Clock.Set(window)
		if n, err := f.s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		if got := f.trade(t, first.ID); got.Status != league.TradeProcessed {
			t.Fatalf("the old trade did not end with its window: %+v", got)
		}
		if got := f.trade(t, second.ID); got.Status != league.TradeInReview {
			t.Fatalf("the new trade ended with the old window: %+v", got)
		}
	})
}

// The veto threshold is frozen when a trade is accepted, like its review
// mode and window: changing the league's threshold mid-vote changes neither
// a vote nor the window's end of a trade already in review. The managers
// are still counted when the votes are.
func TestVetoThresholdIsFrozenAtAccept(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		tr := f.accepted(t, scoring.MID) // a third of 10: 4
		if tr.VetoThreshold == nil || *tr.VetoThreshold != league.DefaultVetoThreshold {
			t.Fatalf("accepted with threshold %v", tr.VetoThreshold)
		}
		quarter := 0.25 // ceil(2.5) = 3 of 10
		l, err := e.Leagues.Update(ctx, f.users[0], f.l.ID, nil, league.SettingsInput{VetoThreshold: &quarter})
		if err != nil {
			t.Fatal(err)
		}
		if n := VotesNeeded(l, f.trade(t, tr.ID)); n != 4 {
			t.Fatalf("votes needed after the change = %d, want the frozen 4", n)
		}
		// Three vetoes would block it under the new threshold; not this one.
		for i := 2; i <= 4; i++ {
			if _, _, err := s.Vote(ctx, f.users[i], f.l.ID, tr.ID, VoteVeto); err != nil {
				t.Fatal(err)
			}
		}
		if got := f.trade(t, tr.ID); got.Status != league.TradeInReview {
			t.Fatalf("after 3 vetoes = %s, want in review (4 needed)", got.Status)
		}
		// Nor at the window's end: it executes.
		e.Clock.Set(*tr.ReviewEndsAt)
		if n, err := s.CloseDue(ctx, f.l.ID); n != 1 || err != nil {
			t.Fatalf("close: %d %v", n, err)
		}
		if got := f.trade(t, tr.ID); got.Status != league.TradeProcessed {
			t.Fatalf("at the window's end = %s, want processed", got.Status)
		}

		// A trade accepted after the change uses the new threshold.
		next := f.accepted(t, scoring.FWD)
		if next.VetoThreshold == nil || *next.VetoThreshold != quarter || VotesNeeded(l, next) != 3 {
			t.Fatalf("accepted after the change: threshold %v, %d needed", next.VetoThreshold, VotesNeeded(l, next))
		}
		// A trade from before the threshold was kept counts against the league's.
		legacy := next
		legacy.VetoThreshold = nil
		if n := VotesNeeded(l, legacy); n != 3 {
			t.Fatalf("legacy trade: %d needed, want 3", n)
		}
	})
}

// Vetoes racing each other and the closing workers: the threshold's veto
// wins once, the trade never executes, and nothing later changes that.
func TestConcurrentVetoesNeverExecute(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := voteFixture(t, e)
		ctx, s := e.Ctx, f.s
		tr := f.accepted(t, scoring.GKP) // 4 of 10 veto it
		e.Clock.Set(tr.ReviewEndsAt.Add(-time.Minute))
		var (
			wg             sync.WaitGroup
			mu             sync.Mutex
			counted, late  int
			closed, closes int
		)
		for i := 2; i <= 9; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, got, err := s.Vote(ctx, f.users[i], f.l.ID, tr.ID, VoteVeto)
				mu.Lock()
				defer mu.Unlock()
				switch {
				case err == nil && (got.Status == league.TradeInReview || got.Status == league.TradeVetoed):
					counted++
				case slug(err) == "trade-not-in-review":
					late++
				default:
					t.Errorf("vote %d = %s %v", i, got.Status, err)
				}
			}()
		}
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				n, err := s.CloseDue(ctx, f.l.ID)
				if err != nil {
					t.Error(err)
				}
				mu.Lock()
				closes++
				closed += n
				mu.Unlock()
			}()
		}
		wg.Wait()
		got := f.trade(t, tr.ID)
		if counted != 4 || late != 4 || got.Status != league.TradeVetoed || len(got.Votes) != 4 {
			t.Fatalf("%d vetoes counted, %d refused, trade %s with %d votes; want 4, 4, vetoed, 4", counted, late, got.Status, len(got.Votes))
		}
		if closed != 0 {
			t.Fatalf("%d of %d closes did work before the window's end", closed, closes)
		}
		// The window ends, every worker runs again: still vetoed, nobody moved.
		e.Clock.Set(*tr.ReviewEndsAt)
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if n, err := s.CloseDue(ctx, f.l.ID); n != 0 || err != nil {
					t.Errorf("close after the veto: %d %v", n, err)
				}
			}()
		}
		wg.Wait()
		if got := f.trade(t, tr.ID); got.Status != league.TradeVetoed {
			t.Fatalf("after the window = %s", got.Status)
		}
		owners := e.State(t, f.l.ID).Game.OwnerAt(38)
		if owners[tr.Offered[0]] != f.teams[0] || owners[tr.Requested[0]] != f.teams[1] {
			t.Fatal("a vetoed trade moved players")
		}
		feed, err := e.Repo.Transactions(ctx, f.l.ID)
		if err != nil {
			t.Fatal(err)
		}
		var entries []string
		for _, x := range feed {
			if x.Kind == league.TxnTrade && x.TradeID == tr.ID {
				entries = append(entries, x.Status)
			}
		}
		if !slices.Equal(entries, []string{league.TradeVetoed}) {
			t.Fatalf("feed entries for the trade = %v, want one vetoed", entries)
		}
	})
}
