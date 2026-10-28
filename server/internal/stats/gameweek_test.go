package stats

import (
	"testing"
)

// Every listed player's gameweek line is stored as upstream gives it, ICT
// and expected stats included (nil where upstream has none), and replaced
// with each ingest.
func TestLiveStoresPlayerGameweeks(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, src, st := ingestAll(t, repo)
		ids := playerIDs(t, repo)
		gw1, err := repo.PlayerGameweeks(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(gw1) != 30 {
			t.Fatalf("gameweek 1 has %d lines, want every listed player (30)", len(gw1))
		}
		all, err := repo.PlayerGameweeks(ctx, 0)
		if err != nil {
			t.Fatal(err)
		}
		gw2, _ := repo.PlayerGameweeks(ctx, 2)
		if len(all) != len(gw1)+len(gw2) || all[0].Gameweek != 1 || all[len(all)-1].Gameweek != 2 {
			t.Fatalf("all gameweeks: %d lines (%d + %d)", len(all), len(gw1), len(gw2))
		}

		mine, err := repo.PlayerGameweeksOf(ctx, ids["1002"])
		if err != nil {
			t.Fatal(err)
		}
		if len(mine) == 0 || mine[0].Gameweek != 1 {
			t.Fatalf("player 1002 = %+v", mine)
		}
		p := mine[0]
		f := func(v *float64) any {
			if v == nil {
				return nil
			}
			return *v
		}
		if p.PlayerID != ids["1002"] || p.Minutes != 90 || p.GoalsScored != 1 || p.YellowCards != 1 || p.Bonus != 3 ||
			p.BPS != 38 || p.ClearancesBlocksInterceptions != 8 || p.Tackles != 3 || p.Recoveries != 4 ||
			p.DefensiveContribution != 11 || p.GoalsConceded != 1 {
			t.Errorf("player 1002 gameweek 1 = %+v", p)
		}
		got := []any{f(p.Influence), f(p.Creativity), f(p.Threat), f(p.ICTIndex), f(p.ExpectedGoals),
			f(p.ExpectedAssists), f(p.ExpectedGoalInvolvements), f(p.ExpectedGoalsConceded)}
		want := []any{58.4, 21.7, 44.0, 12.4, 0.71, 0.12, 0.83, 1.04}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("player 1002 underlying stat %d = %v, want %v", i, got[i], want[i])
			}
		}
		if p.Starts == nil || *p.Starts != 1 {
			t.Errorf("player 1002 starts = %v, want 1", p.Starts)
		}
		for _, x := range gw1 {
			if x.PlayerID == ids["1001"] && x.Starts != nil {
				t.Errorf("player 1001 starts = %d, want nil (not sent)", *x.Starts)
			}
			if x.PlayerID == ids["1001"] && (x.Influence == nil || *x.Influence != 0 || x.Creativity != nil) {
				t.Errorf("player 1001: influence %v, creativity %v (want 0 and nil)", f(x.Influence), f(x.Creativity))
			}
		}

		// A corrected snapshot without a player drops his line.
		live, err := src.EventLive(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		var kept []int
		for i, pl := range live.Players {
			if pl.PlayerExternalID != "1008" { // an unused sub: no fixture rows
				kept = append(kept, i)
			}
		}
		players := live.Players
		live.Players = nil
		for _, i := range kept {
			live.Players = append(live.Players, players[i])
		}
		if _, err := in.Live(ctx, live, st); err != nil {
			t.Fatal(err)
		}
		gw1, _ = repo.PlayerGameweeks(ctx, 1)
		if len(gw1) != 29 {
			t.Fatalf("after the correction gameweek 1 has %d lines, want 29", len(gw1))
		}
		for _, x := range gw1 {
			if x.PlayerID == ids["1008"] {
				t.Fatal("the dropped player's line is still stored")
			}
		}
	})
}

// A line stored before starts was read gets it from the next ingest of the
// gameweek: the backfill of lines kept without the column.
func TestLiveFillsStartsOnReingest(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, src, st := ingestAll(t, repo)
		ids := playerIDs(t, repo)
		live, err := src.EventLive(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		starts := func() *int {
			gw1, err := repo.PlayerGameweeks(ctx, 1)
			if err != nil {
				t.Fatal(err)
			}
			for _, x := range gw1 {
				if x.PlayerID == ids["1002"] {
					return x.Starts
				}
			}
			t.Fatal("no line for player 1002")
			return nil
		}
		old := live.Players
		live.Players = nil
		for _, pl := range old {
			pl.Starts = nil
			live.Players = append(live.Players, pl)
		}
		if _, err := in.Live(ctx, live, st); err != nil {
			t.Fatal(err)
		}
		if s := starts(); s != nil {
			t.Fatalf("starts = %d after a payload without it, want nil", *s)
		}
		live.Players = old
		if _, err := in.Live(ctx, live, st); err != nil {
			t.Fatal(err)
		}
		if s := starts(); s == nil || *s != 1 {
			t.Fatalf("starts = %v after the re-ingest, want 1", s)
		}
	})
}
