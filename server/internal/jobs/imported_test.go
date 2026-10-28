package jobs

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
)

// An imported league plays like a native one from the moment it is
// imported (owner decision 2026-10-14, docs/adr/0012): waiver claims,
// free-agent moves, trades and lineups succeed, and the tick runs its
// waivers like any other league's.
func TestImportedLeagueIsPlayableAndRunByTheTick(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		r := runner(e, &leaguetest.Notes{})
		w := e.World(t)
		l, users := e.InSeason(t, 4, league.SettingsInput{})
		err := e.Repo.Tx(e.Ctx, func(tx league.Tx) error {
			lg, err := tx.Load(e.Ctx, l.ID)
			if err != nil {
				return err
			}
			lg.Source, lg.FPLLeagueID = league.SourceFPL, 90001
			return tx.Save(e.Ctx, lg)
		})
		if err != nil {
			t.Fatal(err)
		}
		st := e.State(t, l.ID)
		ta, tb := l.Teams[0].ID, l.Teams[1].ID
		mine, theirs := st.Game.RosterAt(ta, 8), st.Game.RosterAt(tb, 8)
		owned := st.Game.OwnerAt(8)
		freeLike := func(pos string, not string) string {
			for id, p := range w.Players {
				if _, ok := owned[id]; !ok && string(p.Position) == pos && id != not {
					return id
				}
			}
			t.Fatalf("no free %s", pos)
			return ""
		}
		claimed := freeLike(string(w.Players[mine[1]].Position), "")

		if _, _, err := r.Waivers().CreateClaim(e.Ctx, users[0], l.ID, claimed, mine[1]); err != nil {
			t.Errorf("claim: %v", err)
		}
		if _, _, err := r.Trades().Propose(e.Ctx, users[0], l.ID, tb, mine[:1], theirs[:1], ""); err != nil {
			t.Errorf("trade: %v", err)
		}
		lu := league.EffectiveLineup(st.Game, w, ta, 8)
		if err := r.Lineups().Set(e.Ctx, users[0], l.ID, ta, 8, lu.Slots[:11], lu.Slots[11:]); err != nil {
			t.Errorf("lineup: %v", err)
		}

		// Past gameweek 8's waiver deadline the tick runs the league's
		// waivers: the claim wins.
		e.Clock.Set(w.Gameweeks[8].WaiverDeadlineAt.Add(time.Minute))
		res, err := r.Tick(e.Ctx)
		if err != nil {
			t.Fatal(err)
		}
		if res.WaiverRuns != 1 || res.Errors != 0 {
			t.Fatalf("tick on an imported league = %+v", res)
		}
		after := e.State(t, l.ID)
		if owner := after.Game.OwnerAt(8)[claimed]; owner != ta {
			t.Errorf("claimed player's owner = %q, want %q", owner, ta)
		}

		// After the run, free agency is open.
		added := freeLike(string(w.Players[mine[2]].Position), claimed)
		if _, _, err := r.Waivers().AddFreeAgent(e.Ctx, users[0], l.ID, added, mine[2]); err != nil {
			t.Errorf("free agent: %v", err)
		}
		if owner := e.State(t, l.ID).Game.OwnerAt(8)[added]; owner != ta {
			t.Errorf("free agent's owner = %q, want %q", owner, ta)
		}
	})
}

// The FPL jobs need the mirror service.
func TestRunDispatchesMirrorJobs(t *testing.T) {
	e := leaguetest.New(t, nil)
	r := runner(e, nil)
	if err := r.Run(e.Ctx, mirror.ImportArgs{ImportID: ids.New("imp")}); err == nil {
		t.Error("import without a mirror ran")
	}
	if err := r.Run(e.Ctx, mirror.ResyncArgs{LeagueID: ids.New("lg")}); err == nil {
		t.Error("re-sync without a mirror ran")
	}
	r.Mirror = &mirror.Service{Leagues: e.Repo, Stats: e.Stats, Store: mirror.NewMemoryStore(), Now: e.Clock.Now}
	if err := r.Run(e.Ctx, mirror.ImportArgs{ImportID: ids.New("imp")}); err != nil {
		t.Errorf("unknown import: %v", err)
	}
	if err := r.Run(e.Ctx, mirror.ResyncArgs{LeagueID: ids.New("lg")}); err != nil {
		t.Errorf("re-sync of a league that is gone: %v", err)
	}
	if err := r.Run(e.Ctx, mirror.CheckArgs{LeagueID: ids.New("lg")}); err != nil {
		t.Errorf("check of a league that is gone: %v", err)
	}
}
