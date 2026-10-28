package trades

import (
	"cmp"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/lineup"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// free returns the n best-ranked unowned, unlocked players at pos.
func (f *fixture) free(t *testing.T, pos scoring.Position, n int) []string {
	t.Helper()
	st := f.e.State(t, f.l.ID)
	own := st.Game.OwnerAt(8)
	var out []string
	for id, p := range f.w.Players {
		if p.Position == pos && own[id] == "" && st.Lock(id, 0) == nil {
			out = append(out, id)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		return cmp.Or(cmp.Compare(f.w.Players[a].DraftRank, f.w.Players[b].DraftRank), cmp.Compare(a, b))
	})
	return out[:n]
}

// lateTrade swaps team 1's midfielder x for team 2's y after gameweek 8's
// trade deadline, so it counts from gameweek 9 (ADR 0010).
func (f *fixture) lateTrade(t *testing.T) (x, y string) {
	t.Helper()
	x, y = f.at(t, 0, scoring.MID)[0], f.at(t, 1, scoring.MID)[0]
	f.e.Clock.Set(f.w.Gameweeks[8].TradeDeadlineAt.Add(time.Minute))
	_, tr, err := f.s.Propose(f.e.Ctx, f.users[0], f.l.ID, f.teams[1], []string{x}, []string{y}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, tr, err = f.s.Accept(f.e.Ctx, f.users[1], f.l.ID, tr.ID); err != nil || tr.Status != league.TradeProcessed || tr.EffectiveGW != 9 {
		t.Fatalf("late trade = %+v %v", tr, err)
	}
	return x, y
}

func squadSizes(t *testing.T, f *fixture, gws ...int) {
	t.Helper()
	g := f.e.State(t, f.l.ID).Game
	for _, gw := range gws {
		for i, tm := range f.teams {
			if n := len(g.RosterAt(tm, gw)); n != 15 {
				t.Errorf("team %d has %d players in gameweek %d", i+1, n, gw)
			}
		}
	}
}

// A trade processed after the trade deadline moves its players from the
// next gameweek; until then they stay put: not dropped, not claimed away,
// not traded again, so no squad ever has 16 players.
func TestLateTradePlayersStayPut(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		ctx := e.Ctx
		ws := waivers.New(e.Repo, e.Stats, e.Clock.Now, nil)
		mids := f.free(t, scoring.MID, 2)
		// A claim made before the trade that drops x.
		_, early, err := ws.CreateClaim(ctx, f.users[0], f.l.ID, mids[0], f.at(t, 0, scoring.MID)[0])
		if err != nil {
			t.Fatal(err)
		}
		x, y := f.lateTrade(t)

		if _, _, err := ws.CreateClaim(ctx, f.users[0], f.l.ID, mids[1], x); slug(err) != "player-in-trade" {
			t.Fatalf("claim dropping a traded player: %v", err)
		}
		if _, _, err := f.s.Propose(ctx, f.users[1], f.l.ID, f.teams[2], []string{x}, []string{f.at(t, 2, scoring.MID)[0]}, ""); slug(err) != "player-in-trade" {
			t.Fatalf("trade a traded player again: %v", err)
		}

		// The waiver run denies the early claim.
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if ran, err := ws.RunDue(ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("run: %v %v", ran, err)
		}
		_, done, _ := ws.Claims(ctx, f.users[0], f.l.ID, true)
		if len(done) != 1 || done[0].ID != early.ID || done[0].Status != league.ClaimLost || done[0].FailureReason != league.ClaimRosterLocked {
			t.Fatalf("early claim = %+v", done)
		}

		// Free agency: neither side can drop a player of the trade.
		if _, _, err := ws.AddFreeAgent(ctx, f.users[0], f.l.ID, mids[1], x); slug(err) != "player-in-trade" {
			t.Fatalf("free agent for x: %v", err)
		}
		if _, _, err := ws.AddFreeAgent(ctx, f.users[1], f.l.ID, mids[1], y); slug(err) != "player-in-trade" {
			t.Fatalf("free agent for y: %v", err)
		}

		st := e.State(t, f.l.ID)
		if st.Game.OwnerAt(8)[x] != f.teams[0] || st.Game.OwnerAt(9)[x] != f.teams[1] ||
			st.Game.OwnerAt(8)[y] != f.teams[1] || st.Game.OwnerAt(9)[y] != f.teams[0] {
			t.Fatal("the trade's players moved")
		}
		if st.Lock(x, 0) != nil || st.Lock(y, 0) != nil {
			t.Fatalf("locks = %+v", st.Locks)
		}
		squadSizes(t, f, 8, 9)

		// Once gameweek 8's deadline passes the trade is in force and x is
		// team 2's to move.
		e.Clock.Set(f.w.Gameweeks[8].DeadlineAt)
		if _, _, err := ws.CreateClaim(ctx, f.users[1], f.l.ID, mids[1], x); err != nil {
			t.Fatalf("claim dropping x for gameweek 9: %v", err)
		}
	})
}

// Saving gameweek 8's lineup leaves the gameweek 9 lineup a late trade
// wrote alone.
func TestLateTradeKeepsNextLineup(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e, 4, league.ReviewNone)
		ls := lineup.New(e.Repo, e.Stats, e.Clock.Now, nil)
		before, _ := e.State(t, f.l.ID).Game.LineupAt(f.teams[0], 8)
		x, y := f.lateTrade(t)
		want := slices.Clone(before.Slots)
		want[slices.Index(want, x)] = y

		g := e.State(t, f.l.ID).Game
		if l9, ok := g.LineupAt(f.teams[0], 9); !ok || l9.FromGW != 9 || !slices.Equal(l9.Slots, want) {
			t.Fatalf("gameweek 9 after the trade = %+v", l9)
		}
		edit := slices.Clone(before.Slots)
		edit[12], edit[13] = edit[13], edit[12]
		if err := ls.Set(e.Ctx, f.users[0], f.l.ID, f.teams[0], 8, edit[:11], edit[11:]); err != nil {
			t.Fatal(err)
		}
		g = e.State(t, f.l.ID).Game
		if l8, _ := g.LineupAt(f.teams[0], 8); !slices.Equal(l8.Slots, edit) {
			t.Fatalf("gameweek 8 = %v", l8.Slots)
		}
		if l9 := league.EffectiveLineup(g, f.w, f.teams[0], 9); !slices.Equal(l9.Slots, want) {
			t.Fatalf("gameweek 9 after editing 8 = %v, want %v", l9.Slots, want)
		}
	})
}
