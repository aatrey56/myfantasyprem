package mirror_test

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// repeatInput is the final-gameweek case where FPL recorded the same
// add/drop (fa1 for a15, Ash) once per entry of approved, in id order 1, 2...
// Waivers ran for gameweek 9. Imported transactions (mirrored) are made for
// the ids in imported, with the outcomes FPL recorded.
func repeatInput(t *testing.T, approved []bool) mirror.ShadowInput {
	in := finalCase.input(t)
	in.World.Players["fa1"] = stats.Player{ID: "fa1", DisplayName: "FA1", Position: scoring.FWD, ClubID: "c1"}
	in.State.Runs = map[league.RunKey]time.Time{{Kind: league.RunWaivers, Gameweek: 9}: shadowNow}
	for i, ok := range approved {
		in.Plan.Txns = append(in.Plan.Txns, mirror.PlanTxn{ExternalID: string(rune('1' + i)), Kind: league.TxnWaiver, Gameweek: 9,
			Seat: "1", In: "fa1", Out: "a15", Approved: ok, Priority: 1})
	}
	return in
}

func importTxn(in *mirror.ShadowInput, id string, approved bool) {
	in.Txns = append(in.Txns, league.Txn{ID: "txn_" + id, Kind: league.TxnWaiver, Gameweek: 9, TeamID: "tA", PlayerIn: "fa1",
		PlayerOut: "a15", Approved: approved})
	in.State.SetRef(league.RefTransaction, mirror.Provider, id, "txn_"+id)
}

func got(items map[string]mirror.Item, k string) string {
	it, ok := items[k]
	if !ok {
		return "absent"
	}
	return it.Status + " " + it.Ours + " / " + it.Theirs
}

// FPL records the same claim twice in one gameweek (lost, then won): both
// attempts are checked against their own imported transaction.
func TestShadowRepeatedMirroredWaiver(t *testing.T) {
	in := repeatInput(t, []bool{false, true})
	in.Mirroring = true
	importTxn(&in, "1", false)
	importTxn(&in, "2", true)
	items := byKey(mirror.Shadow(in))
	if g := got(items, "waiver:gw9:tA:fa1:a15"); g != "ok lost (imported) / lost (priority 1)" {
		t.Errorf("first attempt = %s", g)
	}
	if g := got(items, "waiver:gw9:tA:fa1:a15:2"); g != "ok won (imported) / won (priority 1)" {
		t.Errorf("second attempt = %s", g)
	}

	// The won attempt imported as lost: the lost one cannot mask it.
	in = repeatInput(t, []bool{false, true})
	in.Mirroring = true
	importTxn(&in, "1", false)
	importTxn(&in, "2", false)
	items = byKey(mirror.Shadow(in))
	if g := got(items, "waiver:gw9:tA:fa1:a15:2"); g != "mismatch lost (imported) / won (priority 1)" {
		t.Errorf("second attempt = %s", g)
	}
	if g := got(items, "waiver:gw9:tA:fa1:a15"); g != "ok lost (imported) / lost (priority 1)" {
		t.Errorf("first attempt = %s", g)
	}
}

func TestShadowRepeatedMirroredWaiverMissingImport(t *testing.T) {
	in := repeatInput(t, []bool{false, false})
	in.Mirroring = true
	importTxn(&in, "1", false) // the second attempt was never imported
	items := byKey(mirror.Shadow(in))
	if g := got(items, "waiver:gw9:tA:fa1:a15"); g != "ok lost (imported) / lost (priority 1)" {
		t.Errorf("first attempt = %s", g)
	}
	if g := got(items, "waiver:gw9:tA:fa1:a15:2"); g != "mismatch not imported / lost (priority 1)" {
		t.Errorf("second attempt = %s", g)
	}
}

func ourClaim(id string, order int, status string) league.Claim {
	return league.Claim{ID: id, TeamID: "tA", AddPlayerID: "fa1", DropPlayerID: "a15", Order: order, Gameweek: 9, Status: status}
}

func TestShadowRepeatedAppClaims(t *testing.T) {
	// Lost at order 1, won at order 2, on both sides.
	in := repeatInput(t, []bool{false, true})
	in.State.Claims = []league.Claim{ourClaim("clm_2", 2, league.ClaimWon), ourClaim("clm_1", 1, league.ClaimLost)}
	r := mirror.Shadow(in)
	items := byKey(r)
	if g := got(items, "waiver:gw9:tA:fa1:a15"); g != "ok lost (order 1) / lost (priority 1)" {
		t.Errorf("first attempt = %s", g)
	}
	if g := got(items, "waiver:gw9:tA:fa1:a15:2"); g != "ok won (order 2) / won (priority 1)" {
		t.Errorf("second attempt = %s", g)
	}
	if len(r.Mismatches()) != 0 {
		t.Errorf("mismatches %v", notOK(r))
	}

	// The outcomes swapped: each attempt is a mismatch.
	in = repeatInput(t, []bool{true, false})
	in.State.Claims = []league.Claim{ourClaim("clm_1", 1, league.ClaimLost), ourClaim("clm_2", 2, league.ClaimWon)}
	swapped := mirror.Shadow(in)
	if n := len(swapped.Mismatches()); n != 2 {
		t.Errorf("swapped outcomes: %d mismatches, want 2", n)
	}

	// A surplus of ours: FPL has one attempt, we have two.
	in = repeatInput(t, []bool{false})
	in.State.Claims = []league.Claim{ourClaim("clm_1", 1, league.ClaimLost), ourClaim("clm_2", 2, league.ClaimWon)}
	items = byKey(mirror.Shadow(in))
	if g := got(items, "waiver:gw9:tA:fa1:a15"); g != "ok lost (order 1) / lost (priority 1)" {
		t.Errorf("first attempt = %s", g)
	}
	if g := got(items, "waiver:gw9:tA:fa1:a15:2"); g != "mismatch won (order 2) / no claim" {
		t.Errorf("surplus claim = %s", g)
	}

	// A surplus of FPL's: two attempts there, one claim here.
	in = repeatInput(t, []bool{false, true})
	in.State.Claims = []league.Claim{ourClaim("clm_1", 1, league.ClaimLost)}
	items = byKey(mirror.Shadow(in))
	if g := got(items, "waiver:gw9:tA:fa1:a15:2"); g != "mismatch no claim / won (priority 1)" {
		t.Errorf("surplus FPL attempt = %s", g)
	}
}
