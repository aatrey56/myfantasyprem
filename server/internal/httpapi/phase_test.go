package httpapi

import (
	"testing"
	"time"
)

// While gameweek 7 is live (Wednesday 14 October, its deadline long
// passed) and gameweek 8's waivers (Friday 16 October 12:30) have not run,
// free-agent moves are locked: 409 transactions-locked with reopensAt at
// that run. A claim for the run can still be made (ClaimsWhileLocked), and
// the league and its Status page say the phase.
func TestFreeAgentMoveRefusedWhileLocked(t *testing.T) {
	f := newDemoFixture(t)
	f.clock.Set(beforeGW8)
	reopens := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)
	add, drop := waiverPair(t, f, f.game.OwnerAt(8))
	body := `{"addPlayerId":"` + add + `","dropPlayerId":"` + drop + `"}`

	rec := doAs(f.h, f.token, "POST", "/v1/leagues/"+f.league+"/free-agent-moves", body, "Idempotency-Key", "locked-fa-1")
	p := decodeProblem(t, rec, 409)
	if problemType(p) != problemTypeBase+"transactions-locked" || p.ReopensAt == nil || !p.ReopensAt.Equal(reopens) {
		t.Fatalf("problem = %+v", p)
	}
	if rec := doAs(f.h, f.token, "POST", "/v1/leagues/"+f.league+"/waivers/claims", body, "Idempotency-Key", "locked-claim-1"); rec.Code != 201 {
		t.Fatalf("claim while locked = %d %s", rec.Code, rec.Body)
	}

	var home LeagueHomeResponse
	f.get(t, f.token, "/v1/leagues/"+f.league, &home)
	if tp := home.Data.TransactionPhase; tp == nil || tp.Phase != "locked" || tp.Gameweek != 8 || tp.Until == nil || !tp.Until.Equal(reopens) {
		t.Errorf("league phase = %+v", home.Data.TransactionPhase)
	}
	var page LeagueStatusPageResponse
	f.get(t, f.token, "/v1/leagues/"+f.league+"/status", &page)
	if tp := page.Data.TransactionPhase; tp.Phase != "locked" || tp.Until == nil || !tp.Until.Equal(reopens) {
		t.Errorf("status phase = %+v", tp)
	}
}
