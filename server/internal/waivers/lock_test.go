package waivers

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// A player added as a free agent is locked until the next waiver run: a
// claim cannot be changed to drop him, and a run does not drop him.
func TestAddedPlayerLockHoldsForClaims(t *testing.T) { testAddedPlayerLock(t, false) }

// The same holds when gameweek 9's run is missed and caught up after its
// deadline: the moves count from gameweek 10, but the lock is still the
// one of gameweek 9's run.
func TestAddedPlayerLockHoldsForCaughtUpRun(t *testing.T) { testAddedPlayerLock(t, true) }

func testAddedPlayerLock(t *testing.T, late bool) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx, s := e.Ctx, f.s
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt)
		if _, err := s.Run(ctx, f.l.ID, 8); err != nil {
			t.Fatal(err)
		}
		// Free agency: team 1 adds fa for d; d is now on waivers.
		defs := f.rostered(t, 0, scoring.DEF, 8)
		d, d2 := defs[0], defs[1]
		fa := f.free(t, scoring.DEF, 1)[0]
		if _, _, err := s.AddFreeAgent(ctx, f.users[0], f.l.ID, fa, d); err != nil {
			t.Fatal(err)
		}
		_, c, err := s.CreateClaim(ctx, f.users[0], f.l.ID, d, d2)
		if err != nil || c.Gameweek != 9 {
			t.Fatalf("claim = %+v %v", c, err)
		}
		if _, _, err := s.UpdateClaim(ctx, f.users[0], f.l.ID, c.ID, fa); slug(err) != "player-locked" {
			t.Fatalf("change the drop to a locked player: %v", err)
		}

		// A claim that drops him anyway (stored before the lock rule) loses.
		err = e.Repo.Tx(ctx, func(tx league.Tx) error {
			st, err := tx.LoadState(ctx, f.l.ID)
			if err != nil {
				return err
			}
			st.Claim(c.ID).DropPlayerID = fa
			return tx.SaveState(ctx, st)
		})
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(f.w.Gameweeks[9].WaiverDeadlineAt)
		if late {
			e.Clock.Set(f.w.Gameweeks[9].DeadlineAt.Add(time.Hour)) // gameweek 10 is next
		}
		if ran, err := s.RunDue(ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("run: %v %v", ran, err)
		}
		_, done, _ := s.Claims(ctx, f.users[0], f.l.ID, true)
		for _, x := range done {
			if x.ID == c.ID && (x.Status != league.ClaimLost || x.FailureReason != league.ClaimRosterLocked) {
				t.Fatalf("claim dropping a locked player = %+v", x)
			}
		}
		if own := e.State(t, f.l.ID).Game.OwnerAt(10); own[fa] != f.teams[0] || own[d] != "" {
			t.Fatalf("owners: fa %q d %q", own[fa], own[d])
		}
	})
}
