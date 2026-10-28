package mirror

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// Reset fails the open trades made here, never one imported from FPL
// (which Apply then keeps as FPL says), and cancels pending claims.
func TestResetKeepsImportedTrades(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	st := &league.State{
		Game: &league.Game{},
		Trades: []league.Trade{
			{ID: "trd_here", Status: league.TradeOffered},
			{ID: "trd_fpl", Status: league.TradeInReview},
		},
		Claims: []league.Claim{{ID: "wv_1", Status: league.ClaimPending}},
		Locks:  []league.WaiverLock{{PlayerID: "plr_1", UntilGW: 8}},
	}
	st.SetRef(league.RefTrade, Provider, "322640", "trd_fpl")
	c := Reset(st, &Plan{}, now)
	if c != (Cancelled{Trades: 1, Claims: 1}) {
		t.Errorf("cancelled = %+v", c)
	}
	if st.Trades[0].Status != league.TradeFailed || st.Trades[0].FailureReason != league.TradeFailResynced {
		t.Errorf("local trade = %+v", st.Trades[0])
	}
	if st.Trades[1].Status != league.TradeInReview {
		t.Errorf("imported trade = %+v", st.Trades[1])
	}
	if st.Claims[0].Status != league.ClaimCancelled || len(st.Locks) != 0 || !st.DropLocalMoves {
		t.Errorf("claims %+v, locks %v, drop %v", st.Claims, st.Locks, st.DropLocalMoves)
	}
	if got := (Cancelled{Trades: 2}).Note(); got != "Re-synced from FPL. 2 open trades were cancelled." {
		t.Errorf("note = %q", got)
	}
}
