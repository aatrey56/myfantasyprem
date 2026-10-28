package waivers

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Claims made after a gameweek's waiver deadline go to the next run even
// before the run has happened; the run's own claims are frozen.
func TestClaimsAfterTheWaiverDeadline(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx, s := e.Ctx, f.s
		defs := f.free(t, scoring.DEF, 2)
		drops := f.rostered(t, 0, scoring.DEF, 8)
		_, early, err := s.CreateClaim(ctx, f.users[0], f.l.ID, defs[0], drops[0])
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt.Add(time.Second))
		v, late, err := s.CreateClaim(ctx, f.users[0], f.l.ID, defs[1], drops[1])
		if err != nil || late.Gameweek != 9 || v.Cycle.RunGW != 9 {
			t.Fatalf("late claim = %+v %v (cycle %+v)", late, err, v.Cycle)
		}
		if _, _, err := s.UpdateClaim(ctx, f.users[0], f.l.ID, early.ID, drops[2]); slug(err) != "waivers-processing" {
			t.Fatalf("update a claim after its deadline: %v", err)
		}
		if err := s.CancelClaim(ctx, f.users[0], f.l.ID, early.ID); slug(err) != "waivers-processing" {
			t.Fatalf("cancel a claim after its deadline: %v", err)
		}
		if _, _, err := s.Reorder(ctx, f.users[0], f.l.ID, []string{late.ID, early.ID}); slug(err) != "waivers-processing" {
			t.Fatalf("reorder after the deadline: %v", err)
		}
		if ran, err := s.RunDue(ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("run: %v %v", ran, err)
		}
		_, pending, _ := s.Claims(ctx, f.users[0], f.l.ID, false)
		if len(pending) != 1 || pending[0].ID != late.ID {
			t.Fatalf("pending after gameweek 8's run = %+v", pending)
		}
		if own := e.State(t, f.l.ID).Game.OwnerAt(8); own[defs[0]] != f.teams[0] || own[defs[1]] != "" {
			t.Fatal("gameweek 8's run took the wrong claims")
		}
	})
}

// A claim made after gameweek 8's deadline keeps its place ahead of a
// newer claim once gameweek 8's run has decided the older ones.
func TestLateClaimKeepsOrderAfterRun(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		ctx, s := e.Ctx, f.s
		defs := f.free(t, scoring.DEF, 3)
		drops := f.rostered(t, 0, scoring.DEF, 8)
		for i := 0; i < 2; i++ {
			if _, _, err := s.CreateClaim(ctx, f.users[0], f.l.ID, defs[i], drops[i]); err != nil {
				t.Fatal(err)
			}
		}
		e.Clock.Set(f.w.Gameweeks[8].WaiverDeadlineAt.Add(time.Second))
		_, late, err := s.CreateClaim(ctx, f.users[0], f.l.ID, defs[2], drops[2])
		if err != nil || late.Gameweek != 9 || late.Order != 3 {
			t.Fatalf("late claim = %+v %v", late, err)
		}
		if ran, err := s.RunDue(ctx, f.l.ID); !ran || err != nil {
			t.Fatalf("run: %v %v", ran, err)
		}
		// drops[0] was dropped at the run: on waivers for gameweek 9.
		_, newer, err := s.CreateClaim(ctx, f.users[0], f.l.ID, drops[0], drops[3])
		if err != nil {
			t.Fatal(err)
		}
		_, pending, err := s.Claims(ctx, f.users[0], f.l.ID, false)
		if err != nil || len(pending) != 2 {
			t.Fatalf("pending = %+v %v", pending, err)
		}
		if pending[0].ID != late.ID || pending[1].ID != newer.ID || pending[0].Order != 1 || pending[1].Order != 2 {
			t.Fatalf("pending = %+v, want late (1) then newer (2)", pending)
		}
	})
}
