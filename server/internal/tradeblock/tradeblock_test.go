package tradeblock

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
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

// fixture is a 4-team in-season league on Monday of gameweek 8 (waivers
// run Thursday 17:30, the deadline is Friday 17:30).
type fixture struct {
	e     *leaguetest.Env
	s     *Service
	l     *league.League
	users []string
	teams []string
	w     *league.World
	notes *leaguetest.Notes
}

func newFixture(t *testing.T, e *leaguetest.Env) *fixture {
	review := league.ReviewNone
	l, users := e.InSeason(t, 4, league.SettingsInput{TradeReview: &review})
	notes := &leaguetest.Notes{}
	f := &fixture{e: e, s: New(e.Repo, e.Clock.Now, notes), l: l, users: users, w: e.World(t), notes: notes}
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

// free returns the best-ranked unowned player at pos.
func (f *fixture) free(t *testing.T, pos scoring.Position) string {
	t.Helper()
	own := f.e.State(t, f.l.ID).Game.OwnerAt(8)
	var out []string
	for id, p := range f.w.Players {
		if p.Position == pos && own[id] == "" {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(f.w.Players[a].DraftRank, f.w.Players[b].DraftRank), cmp.Compare(a, b))
	})
	return out[0]
}

// block returns the league's block as "team:player" pairs, oldest first.
func (f *fixture) block(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, b := range f.e.State(t, f.l.ID).Block {
		out = append(out, b.TeamID+":"+b.PlayerID)
	}
	return out
}

func (f *fixture) put(t *testing.T, user int, player string) {
	t.Helper()
	if _, _, err := f.s.Set(f.e.Ctx, f.users[user], f.l.ID, player, "", nil); err != nil {
		t.Fatal(err)
	}
}

func TestSetUpdateRemove(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx := e.Ctx
		m := f.at(t, 0, scoring.MID)[0]
		start := e.Clock.Now()

		// Positions are kept once each, in GKP DEF MID FWD order.
		_, got, err := f.s.Set(ctx, f.users[0], f.l.ID, m, "  open to offers ", []string{"FWD", "DEF", "FWD"})
		if err != nil {
			t.Fatal(err)
		}
		if got.TeamID != f.teams[0] || got.Note != "open to offers" || fmt.Sprint(got.Wanted) != "[DEF FWD]" || !got.AddedAt.Equal(start) {
			t.Fatalf("Set = %+v", got)
		}
		if k := f.notes.Kinds(); fmt.Sprint(k) != "["+league.ChangeTradeBlock+"]" {
			t.Fatalf("changes = %v", k)
		}

		// Changing the entry keeps when he went on the block.
		e.Clock.Advance(time.Hour)
		if _, got, err = f.s.Set(ctx, f.users[0], f.l.ID, m, "", []string{"GKP"}); err != nil {
			t.Fatal(err)
		}
		st := e.State(t, f.l.ID)
		if len(st.Block) != 1 || st.Block[0].Note != "" || fmt.Sprint(st.Block[0].Wanted) != "[GKP]" || !st.Block[0].AddedAt.Equal(start) {
			t.Fatalf("block after update = %+v", st.Block)
		}

		// Oldest first across teams.
		d := f.at(t, 1, scoring.DEF)[0]
		f.put(t, 1, d)
		if want := []string{f.teams[0] + ":" + m, f.teams[1] + ":" + d}; !slices.Equal(f.block(t), want) {
			t.Fatalf("block = %v, want %v", f.block(t), want)
		}

		// Only his team takes him off; taking off one not on the block is no error.
		if err := f.s.Remove(ctx, f.users[0], f.l.ID, d); slug(err) != "not-your-player" {
			t.Fatalf("remove another team's player: %v", err)
		}
		if err := f.s.Remove(ctx, f.users[0], f.l.ID, m); err != nil {
			t.Fatal(err)
		}
		if err := f.s.Remove(ctx, f.users[0], f.l.ID, m); err != nil {
			t.Fatal(err)
		}
		if want := []string{f.teams[1] + ":" + d}; !slices.Equal(f.block(t), want) {
			t.Fatalf("block after remove = %v, want %v", f.block(t), want)
		}
	})
}

func TestSetRules(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx := e.Ctx
		mine, theirs := f.at(t, 0, scoring.MID)[0], f.at(t, 1, scoring.MID)[0]
		if _, _, err := f.s.Set(ctx, f.users[0], f.l.ID, theirs, "", nil); slug(err) != "not-on-roster" {
			t.Fatalf("another team's player: %v", err)
		}
		if _, _, err := f.s.Set(ctx, f.users[0], f.l.ID, f.free(t, scoring.MID), "", nil); slug(err) != "not-on-roster" {
			t.Fatalf("a free agent: %v", err)
		}
		// 140 characters is the limit; 141 is one too many.
		if _, _, err := f.s.Set(ctx, f.users[0], f.l.ID, mine, strings.Repeat("é", 141), nil); slug(err) != "validation-failed" {
			t.Fatalf("141-character note: %v", err)
		}
		if _, _, err := f.s.Set(ctx, f.users[0], f.l.ID, mine, "", []string{"GK"}); slug(err) != "validation-failed" {
			t.Fatalf("unknown position: %v", err)
		}
		if _, _, err := f.s.Set(ctx, f.users[0], f.l.ID, mine, strings.Repeat("é", 140), nil); err != nil {
			t.Fatalf("140-character note: %v", err)
		}
		// A non-member and a member without a team cannot read or change it.
		outsider := e.User(t)
		if _, err := f.s.List(ctx, outsider, f.l.ID); slug(err) != "not-a-member" {
			t.Fatalf("outsider list: %v", err)
		}
		if _, _, err := f.s.Set(ctx, outsider, f.l.ID, mine, "", nil); slug(err) != "not-a-member" {
			t.Fatalf("outsider set: %v", err)
		}
		if len(f.notes.Kinds()) != 1 {
			t.Fatal("only the successful set is announced")
		}
	})
}

// A free-agent move that drops a player on the block takes him off it.
func TestDroppedForFreeAgentLeavesBlock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		notes := &leaguetest.Notes{}
		ws := waivers.New(e.Repo, e.Stats, e.Clock.Now, notes)
		drop, keep := f.at(t, 0, scoring.DEF)[0], f.at(t, 0, scoring.DEF)[1]
		f.put(t, 0, drop)
		f.put(t, 0, keep)
		// Free agency opens once gameweek 8's waivers have run.
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if _, err := ws.Run(e.Ctx, f.l.ID, 8); err != nil {
			t.Fatal(err)
		}
		e.Clock.Advance(time.Minute)
		notes.Kinds()
		if _, _, err := ws.AddFreeAgent(e.Ctx, f.users[0], f.l.ID, f.free(t, scoring.DEF), drop); err != nil {
			t.Fatal(err)
		}
		if want := []string{f.teams[0] + ":" + keep}; !slices.Equal(f.block(t), want) {
			t.Fatalf("block = %v, want %v", f.block(t), want)
		}
		if k := notes.Kinds(); !slices.Contains(k, league.ChangeTradeBlock) {
			t.Fatalf("changes = %v, want %s", k, league.ChangeTradeBlock)
		}
	})
}

// A waiver claim that wins and drops a player on the block takes him off.
func TestDroppedByWaiverClaimLeavesBlock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		notes := &leaguetest.Notes{}
		ws := waivers.New(e.Repo, e.Stats, e.Clock.Now, notes)
		drop := f.at(t, 2, scoring.FWD)[0]
		f.put(t, 2, drop)
		if _, _, err := ws.CreateClaim(e.Ctx, f.users[2], f.l.ID, f.free(t, scoring.FWD), drop); err != nil {
			t.Fatal(err)
		}
		// A pending claim leaves him on the block.
		if len(f.block(t)) != 1 {
			t.Fatalf("block with a pending claim = %v", f.block(t))
		}
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if ran, err := ws.RunDue(e.Ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("run: %v %v", ran, err)
		}
		if got := f.block(t); len(got) != 0 {
			t.Fatalf("block after the run = %v, want empty", got)
		}
		if k := notes.Kinds(); !slices.Contains(k, league.ChangeTradeBlock) {
			t.Fatalf("changes = %v, want %s", k, league.ChangeTradeBlock)
		}
	})
}

// A processed trade takes both sides' players off the block; an offer not
// yet accepted leaves them on it. A player traded back does not return to
// the block by himself.
func TestTradedLeavesBlock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx := e.Ctx
		notes := &leaguetest.Notes{}
		ts := trades.New(e.Repo, e.Stats, e.Clock.Now, notes)
		x, y := f.at(t, 0, scoring.MID)[0], f.at(t, 1, scoring.MID)[0]
		other := f.at(t, 1, scoring.MID)[1]
		f.put(t, 0, x)
		f.put(t, 1, y)
		f.put(t, 1, other)
		_, tr, err := ts.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{x}, []string{y}, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(f.block(t)) != 3 {
			t.Fatalf("block with an open offer = %v", f.block(t))
		}
		if _, tr, err = ts.Accept(ctx, f.users[1], f.l.ID, tr.ID); err != nil || tr.Status != league.TradeProcessed {
			t.Fatalf("accept = %+v %v", tr, err)
		}
		if want := []string{f.teams[1] + ":" + other}; !slices.Equal(f.block(t), want) {
			t.Fatalf("block after the trade = %v, want %v", f.block(t), want)
		}
		if k := notes.Kinds(); !slices.Contains(k, league.ChangeTradeBlock) {
			t.Fatalf("changes = %v, want %s", k, league.ChangeTradeBlock)
		}
		// Team 2 now holds x and can put him on its block.
		e.Clock.Advance(time.Minute)
		f.put(t, 1, x)
		if want := []string{f.teams[1] + ":" + other, f.teams[1] + ":" + x}; !slices.Equal(f.block(t), want) {
			t.Fatalf("block = %v, want %v", f.block(t), want)
		}
	})
}

// A trade processed after the trade deadline moves its players from the
// next gameweek; they leave the block at once, as they are no longer
// their teams' to trade.
func TestLateTradeLeavesBlock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx := e.Ctx
		ts := trades.New(e.Repo, e.Stats, e.Clock.Now, nil)
		x, y := f.at(t, 0, scoring.MID)[0], f.at(t, 1, scoring.MID)[0]
		f.put(t, 0, x)
		e.Clock.Set(f.w.Gameweeks[8].TradeDeadlineAt.Add(time.Minute))
		_, tr, err := ts.Propose(ctx, f.users[0], f.l.ID, f.teams[1], []string{x}, []string{y}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, tr, err = ts.Accept(ctx, f.users[1], f.l.ID, tr.ID); err != nil || tr.EffectiveGW != 9 {
			t.Fatalf("late trade = %+v %v", tr, err)
		}
		if got := f.block(t); len(got) != 0 {
			t.Fatalf("block = %v, want empty", got)
		}
		if _, _, err := f.s.Set(ctx, f.users[0], f.l.ID, x, "", nil); slug(err) != "not-on-roster" {
			t.Fatalf("re-block a player leaving: %v", err)
		}
	})
}

func TestBeforeTheSeason(t *testing.T) {
	e := leaguetest.New(t, nil)
	l, users := e.League(t, 4, league.SettingsInput{})
	s := New(e.Repo, e.Clock.Now, nil)
	if _, _, err := s.Set(e.Ctx, users[0], l.ID, "plr_01k7m2xq8v0000000000000001", "", nil); slug(err) != "season-not-started" {
		t.Fatalf("set before the draft: %v", err)
	}
	st, err := s.List(e.Ctx, users[1], l.ID)
	if err != nil || len(st.Block) != 0 {
		t.Fatalf("list before the draft = %+v %v", st, err)
	}
}

// A trade the commissioner approves in review takes its players off the
// block when it is processed; while it waits in review they stay listed.
func TestCommissionerApprovedTradeLeavesBlock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		review := league.ReviewCommissioner
		l, users := e.InSeason(t, 4, league.SettingsInput{TradeReview: &review})
		f := &fixture{e: e, s: New(e.Repo, e.Clock.Now, nil), l: l, users: users, w: e.World(t)}
		for _, tm := range l.Teams {
			f.teams = append(f.teams, tm.ID)
		}
		ctx := e.Ctx
		ts := trades.New(e.Repo, e.Stats, e.Clock.Now, nil)
		x, y := f.at(t, 1, scoring.MID)[0], f.at(t, 2, scoring.MID)[0]
		f.put(t, 1, x)
		f.put(t, 2, y)
		_, tr, err := ts.Propose(ctx, users[1], l.ID, f.teams[2], []string{x}, []string{y}, "")
		if err != nil {
			t.Fatal(err)
		}
		if _, tr, err = ts.Accept(ctx, users[2], l.ID, tr.ID); err != nil || tr.Status != league.TradeInReview {
			t.Fatalf("accept = %+v %v", tr, err)
		}
		if len(f.block(t)) != 2 {
			t.Fatalf("block with a trade in review = %v", f.block(t))
		}
		// users[0] is the commissioner, on neither side of the trade.
		if _, tr, err = ts.Review(ctx, users[0], l.ID, tr.ID, trades.DecisionApprove, ""); err != nil || tr.Status != league.TradeProcessed {
			t.Fatalf("approve = %+v %v", tr, err)
		}
		if got := f.block(t); len(got) != 0 {
			t.Fatalf("block after the approved trade = %v, want empty", got)
		}
	})
}
