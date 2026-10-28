package mirror

import (
	"fmt"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// Cancelled counts the open moves a re-sync cancelled.
type Cancelled struct {
	Trades, Claims int
}

// Note is the re-sync's notice for the league's members.
func (c Cancelled) Note() string {
	if c.Trades == 0 && c.Claims == 0 {
		return "Re-synced from FPL. No open trades or waiver claims needed cancelling."
	}
	plural := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return fmt.Sprintf("%d %s", n, many)
	}
	var parts []string
	if c.Trades > 0 {
		parts = append(parts, plural(c.Trades, "open trade", "open trades"))
	}
	if c.Claims > 0 {
		parts = append(parts, plural(c.Claims, "waiver claim", "waiver claims"))
	}
	verb := " were cancelled."
	if c.Trades+c.Claims == 1 {
		verb = " was cancelled."
	}
	if len(parts) == 2 {
		return "Re-synced from FPL. " + parts[0] + " and " + parts[1] + verb
	}
	return "Re-synced from FPL. " + parts[0] + verb
}

// Reset clears what was played here since FPL's state was last applied,
// so that Apply then puts FPL's state back exactly (a re-sync,
// docs/adr/0012). Open trades made here fail (TradeFailResynced) and
// pending claims are cancelled, and both stay listed as the notice; open
// trades imported from FPL stay as FPL has them. The moves made here
// (closed claims and trades, their feed entries) are dropped when the
// State is saved; waiver locks go; lineups FPL did not set are removed,
// so each team plays FPL's last lineup until its manager saves a new one.
// Rosters, the waiver order, names and settings are Apply's. Owners and
// claim links are untouched. It is idempotent: a second Reset finds
// nothing to cancel.
func Reset(st *league.State, p *Plan, now time.Time) Cancelled {
	var c Cancelled
	for i := range st.Claims {
		if cl := &st.Claims[i]; cl.Status == league.ClaimPending {
			cl.Status, cl.ProcessedAt = league.ClaimCancelled, &now
			c.Claims++
		}
	}
	imported := map[string]bool{}
	for k, id := range st.Refs {
		if k.Entity == league.RefTrade && k.Provider == Provider {
			imported[id] = true
		}
	}
	for i := range st.Trades {
		// A trade imported from FPL is FPL's: Apply keeps it as FPL says.
		if tr := &st.Trades[i]; league.TradeOpen(tr.Status) && !imported[tr.ID] {
			tr.Status, tr.FailureReason = league.TradeFailed, league.TradeFailResynced
			c.Trades++
		}
	}
	st.Locks = nil
	st.DropLocalMoves = true

	type key struct {
		team string
		gw   int
	}
	team := Seats(st)
	fpl := map[key]bool{}
	for _, lu := range p.Lineups {
		fpl[key{team[lu.Seat], lu.Gameweek}] = true
	}
	st.Game.Lineups = slices.DeleteFunc(st.Game.Lineups, func(l league.Lineup) bool { return !fpl[key{l.TeamID, l.FromGW}] })
	return c
}
