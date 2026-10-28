package mirror_test

import (
	"fmt"
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/tradeblock"
)

// A re-sync that shows a blocked player left his team and came back takes
// him off the block: the listing was for the stint that ended. A player
// whose stint FPL leaves unbroken stays on the block.
func TestResyncExitAndReturnLeavesBlock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		review := league.ReviewNone
		l, users := e.InSeason(t, 4, league.SettingsInput{TradeReview: &review})
		w := e.World(t)
		now := e.Clock.Now()
		gw := w.UpcomingGW(now)
		st := e.State(t, l.ID)
		x := st.Game.RosterAt(l.Teams[0].ID, gw)[0]
		y := st.Game.RosterAt(l.Teams[1].ID, gw)[0]
		tb := tradeblock.New(e.Repo, e.Clock.Now, nil)
		for i, p := range []string{x, y} {
			if _, _, err := tb.Set(e.Ctx, users[i], l.ID, p, "", nil); err != nil {
				t.Fatal(err)
			}
		}

		err := e.Repo.Tx(e.Ctx, func(tx league.Tx) error {
			st, err := tx.LoadState(e.Ctx, l.ID)
			if err != nil {
				return err
			}
			p := &mirror.Plan{Name: st.League.Name, Settings: st.League.Settings}
			seat := map[string]string{}
			for i, tm := range st.League.Teams {
				s := fmt.Sprint("seat", i+1)
				seat[tm.ID] = s
				st.SetRef(league.RefTeam, mirror.Provider, s, tm.ID)
				p.Seats = append(p.Seats, mirror.Seat{ExternalID: s, TeamName: tm.Name})
			}
			// FPL's replay: every stint as stored, except that x was dropped
			// before gameweek gw and his team picked him up again from it.
			for _, r := range st.Game.Rosters {
				ps := mirror.PlanSlot{Seat: seat[r.TeamID], PlayerID: r.PlayerID, FromGW: r.FromGW, ToGW: r.ToGW, Via: r.AcquiredVia}
				if r.PlayerID == x {
					ps.ToGW = gw + 1
					p.Rosters = append(p.Rosters, ps,
						mirror.PlanSlot{Seat: ps.Seat, PlayerID: x, FromGW: gw + 1, Via: "free_agent"})
					continue
				}
				p.Rosters = append(p.Rosters, ps)
			}
			// As a re-sync does: reset what was played here, then apply.
			mirror.Reset(st, p, now)
			mirror.Apply(st, p, w, now)
			return tx.SaveState(e.Ctx, st)
		})
		if err != nil {
			t.Fatal(err)
		}

		got := e.State(t, l.ID)
		if !got.Game.Holds(l.Teams[0].ID, x) {
			t.Fatalf("team 1 should hold x again after the re-sync")
		}
		var block []string
		for _, b := range got.Block {
			block = append(block, b.TeamID+":"+b.PlayerID)
		}
		if want := []string{l.Teams[1].ID + ":" + y}; !slices.Equal(block, want) {
			t.Fatalf("block = %v, want %v", block, want)
		}
	})
}
