package mirror_test

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
)

func fplTrade(ext, status string) mirror.PlanTrade {
	return mirror.PlanTrade{ExternalID: ext, Status: status, Offer: "1", Receive: "2",
		Offered: []string{"a2"}, Requested: []string{"b2"}, Gameweek: 9}
}

func localTrade(id, status string) league.Trade {
	return league.Trade{ID: id, OfferingTeamID: "tA", ReceivingTeamID: "tB", Offered: []string{"a2"},
		Requested: []string{"b2"}, Status: status, EffectiveGW: 9}
}

// FPL trades declined, withdrawn or expired before the import were never
// imported (nothing happened): they are no mismatch.
func TestShadowIgnoresClosedTradesNeverImported(t *testing.T) {
	in := finalCase.input(t)
	in.Plan.Trades = []mirror.PlanTrade{fplTrade("5", league.TradeDeclined), fplTrade("6", league.TradeWithdrawn),
		fplTrade("7", league.TradeExpired)}
	r := mirror.Shadow(in)
	if r.Status != mirror.ReportClean {
		t.Fatalf("status %s, failing %v", r.Status, notOK(r))
	}
}

// A trade imported while pending and then declined on FPL is compared.
func TestShadowComparesImportedTradeClosedOnFPL(t *testing.T) {
	in := finalCase.input(t)
	in.Plan.Trades = []mirror.PlanTrade{fplTrade("5", league.TradeDeclined)}
	in.State.Trades = []league.Trade{localTrade("trd_1", league.TradeInReview)}
	in.State.Refs[league.RefKey{Entity: league.RefTrade, Provider: mirror.Provider, ExternalID: "5"}] = "trd_1"
	it := byKey(mirror.Shadow(in))["trade:fpl5"]
	if it.Status != mirror.ItemMismatch || it.Ours != league.TradeInReview || it.Theirs != league.TradeDeclined {
		t.Fatalf("item %+v", it)
	}
}

// The same trade offered again after a veto: each FPL trade is matched to
// its own local trade (by import ref, else in order), not to the last one.
func TestShadowMatchesRepeatedTradesOneToOne(t *testing.T) {
	in := finalCase.input(t)
	// FPL's feed is oldest first; the store loads trades newest first.
	vetoed, processed := fplTrade("5", league.TradeVetoed), fplTrade("6", league.TradeProcessed)
	vetoed.OfferedAt, processed.OfferedAt = shadowNow.Add(-48*time.Hour), shadowNow.Add(-24*time.Hour)
	in.Plan.Trades = []mirror.PlanTrade{vetoed, processed}
	first, again := localTrade("trd_1", league.TradeVetoed), localTrade("trd_2", league.TradeProcessed)
	first.CreatedAt, again.CreatedAt = shadowNow.Add(-48*time.Hour), shadowNow.Add(-24*time.Hour)
	in.State.Trades = []league.Trade{again, first}
	r := mirror.Shadow(in)
	if r.Status != mirror.ReportClean {
		t.Fatalf("by order: status %s, failing %v", r.Status, notOK(r))
	}
	// Imported refs win over order: trd_2 is FPL's 5, trd_1 FPL's 6.
	in.State.Trades = []league.Trade{localTrade("trd_1", league.TradeProcessed), localTrade("trd_2", league.TradeVetoed)}
	in.State.Refs[league.RefKey{Entity: league.RefTrade, Provider: mirror.Provider, ExternalID: "5"}] = "trd_2"
	in.State.Refs[league.RefKey{Entity: league.RefTrade, Provider: mirror.Provider, ExternalID: "6"}] = "trd_1"
	r = mirror.Shadow(in)
	if r.Status != mirror.ReportClean {
		t.Fatalf("by ref: status %s, failing %v", r.Status, notOK(r))
	}
}

// A waiver run here with no claims still compares FPL's claims of that
// gameweek: FPL approving one we never saw is a mismatch.
func TestShadowComparesWaiversOfAnEmptyLocalRun(t *testing.T) {
	in := finalCase.input(t)
	in.State.Runs = map[league.RunKey]time.Time{{Kind: league.RunWaivers, Gameweek: 9}: shadowNow}
	in.Plan.Txns = []mirror.PlanTxn{{ExternalID: "77", Kind: league.TxnWaiver, Gameweek: 9, Seat: "1", In: "fa1", Out: "a15",
		Approved: true, Priority: 1}}
	it, ok := byKey(mirror.Shadow(in))["waiver:gw9:tA:fa1:a15"]
	if !ok || it.Status != mirror.ItemMismatch || it.Ours != "no claim" || it.Theirs != "won (priority 1)" {
		t.Fatalf("item %+v (present %v)", it, ok)
	}
}
