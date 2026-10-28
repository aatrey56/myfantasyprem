package trades

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// Tests in this file close gaps found by mutation testing (docs/testing.md).
// Expected values come from the rules (RESEARCH.md §7 "Trades", ADR 0008
// rules 13, 14 and 16, the TradeSettings contract), worked out by hand.

// ADR 0008 rule 14: vetoes needed = ceil(threshold × managers not in the
// trade), at least 1. Teams without a manager do not count.
func TestVotesNeeded(t *testing.T) {
	tr := league.Trade{OfferingTeamID: "t0", ReceivingTeamID: "t1"}
	leagueOf := func(teams int, threshold float64, unowned ...int) *league.League {
		l := &league.League{Settings: league.Settings{VetoThreshold: threshold}}
		for i := range teams {
			tm := league.Team{ID: "t" + string(rune('0'+i)), OwnerID: "u"}
			if slices.Contains(unowned, i) {
				tm.OwnerID = ""
			}
			l.Teams = append(l.Teams, tm)
		}
		return l
	}
	for _, tt := range []struct {
		name      string
		teams     int
		threshold float64
		unowned   []int
		want      int
	}{
		{"8 teams, 0.5: ceil(0.5×6) = 3", 8, 0.5, nil, 3},
		{"7 teams, 0.5: ceil(0.5×5) = ceil(2.5) = 3", 7, 0.5, nil, 3},
		{"4 teams, 0.5: ceil(0.5×2) = 1", 4, 0.5, nil, 1},
		{"8 teams, 1.0: all 6", 8, 1.0, nil, 6},
		// 0.1 × 10 is 1.0000000000000002 in floating point; the rule's
		// answer is exactly 1, not 2.
		{"12 teams, 0.1: ceil(0.1×10) = 1", 12, 0.1, nil, 1},
		{"12 teams, 0.3: ceil(0.3×10) = 3", 12, 0.3, nil, 3},
		{"8 teams, 0.34: ceil(0.34×6) = ceil(2.04) = 3", 8, 0.34, nil, 3},
		{"threshold 0 still needs one veto", 8, 0, nil, 1},
		{"2 teams: nobody can vote, one veto needed", 2, 0.5, nil, 1},
		{"8 teams, 2 unmanaged: ceil(0.5×4) = 2", 8, 0.5, []int{5, 6}, 2},
	} {
		if got := VotesNeeded(leagueOf(tt.teams, tt.threshold, tt.unowned...), tr); got != tt.want {
			t.Errorf("%s: got %d", tt.name, got)
		}
	}
}

// Who sees a trade and what they may do (ADR 0008 rule 13/14; the
// TradeAction contract).
func TestVisibilityAndActions(t *testing.T) {
	now := time.Unix(0, 0)
	l := &league.League{CommissionerID: "boss"}
	offered := league.Trade{OfferingTeamID: "A", ReceivingTeamID: "B", Status: league.TradeOffered}
	if Visible(offered, "C") || !Visible(offered, "A") || !Visible(offered, "B") {
		t.Error("an unanswered offer is private to the two teams")
	}
	// Failed before it was accepted (another trade took a player): still
	// private. Failed after acceptance (re-check at processing): public.
	failedOffer := offered
	failedOffer.Status = league.TradeFailed
	if Visible(failedOffer, "C") {
		t.Error("an offer that failed before acceptance became public")
	}
	failedAccepted := failedOffer
	failedAccepted.RespondedAt = &now
	if !Visible(failedAccepted, "C") {
		t.Error("an accepted trade that failed is not public")
	}

	ends := now.Add(24 * time.Hour)
	review := league.Trade{OfferingTeamID: "A", ReceivingTeamID: "B", Status: league.TradeInReview, ReviewMode: league.ReviewCommissioner,
		ReviewEndsAt: &ends}
	if acts := Actions(review, l, "ub", "B"); len(acts) != 0 {
		t.Errorf("receiving team on a trade in review: %v, want none", acts)
	}
	if acts := Actions(review, l, "uc", "C"); len(acts) != 0 {
		t.Errorf("other manager in commissioner mode: %v, want none", acts)
	}
	if acts := Actions(review, l, "boss", ""); !slices.Equal(acts, []string{ActReview}) {
		t.Errorf("commissioner: %v, want [review]", acts)
	}
	// League vote: teams in the trade cannot vote on it (ADR 0008 rule 14).
	vote := review
	vote.ReviewMode = league.ReviewLeagueVote
	for _, team := range []string{"A", "B"} {
		if acts := Actions(vote, l, "u"+team, team); len(acts) != 0 {
			t.Errorf("team %s in the trade: %v, want none", team, acts)
		}
	}
	if acts := Actions(vote, l, "uc", "C"); !slices.Equal(acts, []string{ActVote}) {
		t.Errorf("other manager in a league vote: %v, want [vote]", acts)
	}
	if acts := Actions(offered, &league.League{}, "", ""); len(acts) != 0 {
		t.Errorf("signed-out viewer: %v, want none", acts)
	}
	// A trade pending on FPL (in review, no window): FPL decides it.
	onFPL := review
	onFPL.ReviewEndsAt = nil
	if !PendingOnFPL(onFPL) || PendingOnFPL(review) {
		t.Error("PendingOnFPL misreads the review window")
	}
	if acts := Actions(onFPL, l, "boss", ""); len(acts) != 0 {
		t.Errorf("commissioner on a trade pending on FPL: %v, want none", acts)
	}
}

// A third team cannot act on an offer it cannot see: not found.
func TestOthersCannotTouchAnOffer(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.MID)[0]}, []string{f.at(t, 1, scoring.MID)[0]}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.Accept(e.Ctx, f.users[2], f.l.ID, tr.ID); slug(err) != "not-found" {
			t.Fatalf("third team accept: %v", err)
		}
	})
}

// Accepting processes the trade cleanly: no failure reason left on it.
func TestAcceptedTradeHasNoFailure(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.FWD)[0]}, []string{f.at(t, 1, scoring.FWD)[0]}, "")
		if err != nil {
			t.Fatal(err)
		}
		_, got, err := f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID)
		if err != nil || got.Status != league.TradeProcessed || got.FailureReason != "" {
			t.Fatalf("accepted = %+v, %v", got, err)
		}
	})
}

// Shape (ADR 0008 rule 13, contract): 1 to 5 players a side; message and
// review reason up to 280 characters. The trades are between teams 1, 2
// and 3 so the commissioner (team 0) is not a party and may review
// (ADR 0010: a commissioner cannot review their own team's trade).
func TestTradeShapeLimits(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewCommissioner)
		five1, five2 := f.at(t, 1, scoring.DEF), f.at(t, 2, scoring.DEF)
		if len(five1) != 5 || len(five2) != 5 {
			t.Fatalf("fixture rosters: %d, %d defenders", len(five1), len(five2))
		}
		_, tr, err := f.s.Propose(e.Ctx, f.users[1], f.l.ID, f.teams[2], five1, five2, strings.Repeat("é", 280))
		if err != nil {
			t.Fatalf("5-for-5 with a 280-character message: %v", err)
		}
		if _, _, err := f.s.Propose(e.Ctx, f.users[1], f.l.ID, f.teams[3], f.at(t, 1, scoring.MID)[:1], f.at(t, 3, scoring.MID)[:1], strings.Repeat("x", 281)); slug(err) != "trade-shape" {
			t.Fatalf("281-character message: %v", err)
		}
		six1 := append(slices.Clone(five1), f.at(t, 1, scoring.MID)[0])
		six3 := append(slices.Clone(f.at(t, 3, scoring.DEF)), f.at(t, 3, scoring.MID)[0])
		if _, _, err := f.s.Propose(e.Ctx, f.users[1], f.l.ID, f.teams[3], six1, six3, ""); slug(err) != "trade-shape" {
			t.Fatalf("6-for-6: %v", err)
		}
		if _, _, err := f.s.Accept(e.Ctx, f.users[2], f.l.ID, tr.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.Review(e.Ctx, f.users[0], f.l.ID, tr.ID, DecisionApprove, strings.Repeat("r", 280)); err != nil {
			t.Fatalf("280-character reason: %v", err)
		}
	})
}

// Contract TradeSettings.deadlineGameweek: "No trades after this
// gameweek's deadline". At the test start (Monday of gameweek 8, before its
// trade deadline) a trade counts for gameweek 8: allowed with deadline 8,
// refused with deadline 7. After gameweek 8's trade deadline (deadline − 48
// h, RESEARCH.md §7) it counts for 9: refused with deadline 8.
func TestLeagueTradeDeadline(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		set := func(gw int) {
			t.Helper()
			if _, err := e.Leagues.Update(e.Ctx, f.users[0], f.l.ID, nil, league.SettingsInput{TradeDeadlineGW: &gw}); err != nil {
				t.Fatal(err)
			}
		}
		propose := func(pos scoring.Position) (league.Trade, error) {
			_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, pos)[0]}, []string{f.at(t, 1, pos)[0]}, "")
			return tr, err
		}
		set(8)
		tr, err := propose(scoring.MID)
		if err != nil {
			t.Fatalf("effective gameweek 8, deadline 8: %v", err)
		}
		set(7)
		if _, err := propose(scoring.DEF); slug(err) != "trade-deadline" {
			t.Fatalf("effective gameweek 8, deadline 7: %v", err)
		}
		if _, _, err := f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID); slug(err) != "trade-deadline" {
			t.Fatalf("accept, effective gameweek 8, deadline 7: %v", err)
		}
		set(8)
		e.Clock.Set(f.w.Gameweeks[8].TradeDeadlineAt)
		if _, _, err := f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID); slug(err) != "trade-deadline" {
			t.Fatalf("accept at the trade deadline, effective gameweek 9, deadline 8: %v", err)
		}
		set(9)
		if _, got, err := f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID); err != nil || got.EffectiveGW != 9 {
			t.Fatalf("accept, effective gameweek 9, deadline 9 = %+v, %v", got, err)
		}
	})
}

// Gameweek 38 still takes trades until its trade deadline; an offer then
// expires at gameweek 38's waiver run (ADR 0008 rule 13).
func TestGameweek38Trade(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		g38 := f.w.Gameweeks[38]
		e.Clock.Set(g38.WaiverDeadlineAt.Add(-time.Hour).Add(-24 * time.Hour))
		if !e.Clock.Now().Before(g38.TradeDeadlineAt) {
			t.Fatalf("test clock %v is not before the gameweek 38 trade deadline %v", e.Clock.Now(), g38.TradeDeadlineAt)
		}
		_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.GKP)[0]}, []string{f.at(t, 1, scoring.GKP)[0]}, "")
		if err != nil {
			t.Fatalf("propose in gameweek 38: %v", err)
		}
		if !tr.ExpiresAt.Equal(g38.WaiverDeadlineAt) {
			t.Fatalf("expires %v, want gameweek 38's waiver run %v", tr.ExpiresAt, g38.WaiverDeadlineAt)
		}
		_, got, err := f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID)
		if err != nil || got.Status != league.TradeProcessed || got.EffectiveGW != 38 {
			t.Fatalf("accept in gameweek 38 = %+v, %v", got, err)
		}
		e.Clock.Set(g38.TradeDeadlineAt)
		if _, _, err := f.s.Propose(e.Ctx, f.users[2], f.l.ID, f.teams[3], []string{f.at(t, 2, scoring.GKP)[0]}, []string{f.at(t, 3, scoring.GKP)[0]}, ""); slug(err) != "trade-deadline" {
			t.Fatalf("propose after the last trade deadline: %v", err)
		}
	})
}

// Votes only exist in league-vote mode.
func TestNoVoteInCommissionerMode(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewCommissioner)
		_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{f.at(t, 0, scoring.MID)[0]}, []string{f.at(t, 1, scoring.MID)[0]}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.s.Vote(e.Ctx, f.users[2], f.l.ID, tr.ID, VoteVeto); slug(err) != "no-league-vote" {
			t.Fatalf("vote in commissioner mode: %v", err)
		}
	})
}

// No trades before the season.
func TestNoTradesBeforeTheDraft(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		l, users := e.League(t, 4, league.SettingsInput{})
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		if _, _, err := s.Propose(e.Ctx, users[0], l.ID, l.Teams[1].ID, []string{"a"}, []string{"b"}, ""); slug(err) != "season-not-started" {
			t.Fatalf("propose before the draft: %v", err)
		}
	})
}

// checkSquads asserts the two roster invariants in gameweek gw: no player
// on two teams, every team 2/5/5/3 (RESEARCH.md §7 "Squad").
func checkSquads(t *testing.T, f *fixture, gw int) {
	t.Helper()
	st := f.e.State(t, f.l.ID)
	owners := map[string]int{}
	for _, r := range st.Game.Rosters {
		if r.OnRosterAt(gw) {
			owners[r.PlayerID]++
		}
	}
	for p, n := range owners {
		if n > 1 {
			t.Errorf("gameweek %d: %s has %d owners", gw, p, n)
		}
	}
	for _, team := range f.teams {
		counts := map[scoring.Position]int{}
		for _, id := range st.Game.RosterAt(team, gw) {
			counts[f.w.Players[id].Position]++
		}
		if counts[scoring.GKP] != 2 || counts[scoring.DEF] != 5 || counts[scoring.MID] != 5 || counts[scoring.FWD] != 3 {
			t.Errorf("gameweek %d: team %s squad %v, want 2/5/5/3", gw, team, counts)
		}
	}
}

// Found by the roster property test (leaguetest.TestRosterInvariants).
// A trade processed after gameweek 8's trade deadline counts from gameweek
// 9, but the traded-away defender is still on the offering team for
// gameweek 8. ADR 0010: until the trade counts he is in a pending trade and
// cannot be dropped as a free agent (409 player-in-trade), so he is not
// owned by the receiving team from 9 while the offering team keeps the
// free agent as a sixth defender.
func TestFreeAgentDropOfAPlayerTradedForNextGameweek(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		e.Clock.Set(f.w.Gameweeks[8].TradeDeadlineAt.Add(time.Minute))
		out, in := f.at(t, 0, scoring.DEF)[0], f.at(t, 1, scoring.DEF)[0]
		_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{out}, []string{in}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, tr, err = f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID); err != nil || tr.EffectiveGW != 9 {
			t.Fatalf("accept = %+v, %v (want effective gameweek 9)", tr, err)
		}
		ws := waivers.New(e.Repo, e.Stats, e.Clock.Now, nil)
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if ran, err := ws.RunDue(e.Ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("waiver run: %v %v", ran, err)
		}
		st := e.State(t, f.l.ID)
		own8, own9 := st.Game.OwnerAt(8), st.Game.OwnerAt(9)
		var fa string
		for id, p := range f.w.Players {
			if p.Position == scoring.DEF && own8[id] == "" && own9[id] == "" && st.Lock(id, 9) == nil {
				fa = id
				break
			}
		}
		if _, _, err := ws.AddFreeAgent(e.Ctx, f.users[0], f.l.ID, fa, out); slug(err) != "player-in-trade" {
			t.Errorf("free-agent drop of a traded player: %v, want 409 player-in-trade", err)
		}
		checkSquads(t, f, 8)
		checkSquads(t, f, 9)
	})
}

// Found again by the roster property test, on the waiver-claim path the
// free-agent test above does not cover. A claim made on Monday drops
// defender `out` for gameweek 8. After gameweek 8's trade deadline the same
// team trades `out` away; the trade counts from gameweek 9 (ADR 0010), so
// `out` is still on the team when Thursday's waiver run decides the claim.
// ADR 0010: a player in a pending trade cannot be dropped at the waiver
// run, so the claim loses with roster_locked, and every squad stays
// 2/5/5/3 with one owner per player in gameweeks 8 and 9 (RESEARCH.md §7,
// ADR 0008). Before the fix (c80ff2a) the claim won: `out` was released
// from 8 and the claimed defender added from 8, while the trade still
// brought in his replacement from 9, so the team had six defenders in
// gameweek 9.
func TestClaimDropOfAPlayerTradedForNextGameweek(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		ws := waivers.New(e.Repo, e.Stats, e.Clock.Now, nil)
		out, in := f.at(t, 0, scoring.DEF)[0], f.at(t, 1, scoring.DEF)[0]
		st := e.State(t, f.l.ID)
		own8 := st.Game.OwnerAt(8)
		var add string
		for id, p := range f.w.Players {
			if p.Position == scoring.DEF && own8[id] == "" {
				add = id
				break
			}
		}
		_, claim, err := ws.CreateClaim(e.Ctx, f.users[0], f.l.ID, add, out)
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		e.Clock.Set(f.w.Gameweeks[8].TradeDeadlineAt.Add(time.Minute))
		_, tr, err := f.s.Propose(e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{out}, []string{in}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, tr, err = f.s.Accept(e.Ctx, f.users[1], f.l.ID, tr.ID); err != nil || tr.EffectiveGW != 9 {
			t.Fatalf("accept = %+v, %v (want effective gameweek 9)", tr, err)
		}
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if ran, err := ws.RunDue(e.Ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("waiver run: %v %v", ran, err)
		}
		_, done, err := ws.Claims(e.Ctx, f.users[0], f.l.ID, true)
		if err != nil || len(done) != 1 || done[0].ID != claim.ID || done[0].Status != league.ClaimLost || done[0].FailureReason != league.ClaimRosterLocked {
			t.Errorf("claim after the run = %+v %v, want lost (roster_locked)", done, err)
		}
		checkSquads(t, f, 8)
		checkSquads(t, f, 9)
	})
}
