package scoring

// LineupPlayer is one lineup slot as the gameweek stands.
type LineupPlayer struct {
	PlayerID string
	Position Position
	// Minutes across all of the player's fixtures this gameweek so far.
	Minutes int
	// Booked: shown a yellow or red card this gameweek, even without
	// playing a minute (a booking on the bench). FPL counts it as playing.
	Booked bool
	// FixturesDone: every fixture of the player's club this gameweek is
	// finished, or the club has none (a blank). A postponed fixture does not
	// count: it is played in another gameweek.
	FixturesDone bool
	// Points this gameweek, used by TeamTotal.
	Points int
}

// played: an appearance or a card (FPL's definition, see AutoSubs).
func (p LineupPlayer) played() bool { return p.Minutes > 0 || p.Booked }

// SubStatus is whether an auto-sub is final.
type SubStatus string

const (
	// SubProjected: the gameweek is still running; the sub can still change.
	SubProjected SubStatus = "projected"
	// SubConfirmed: every fixture of the gameweek is finished.
	SubConfirmed SubStatus = "confirmed"
)

// Sub replaces the starter in OutSlot (1-11) with the bench player in InSlot
// (12-15).
type Sub struct {
	OutSlot     int
	InSlot      int
	OutPlayerID string
	InPlayerID  string
	Status      SubStatus
}

// AutoSubs applies FPL's auto-sub rules to a lineup. starters are slots 1-11
// and bench slots 12-15, both in order.
//
//   - Playing means minutes > 0 or a yellow or red card (Booked): FPL,
//     "Playing in a Gameweek means making an appearance on the pitch or
//     receiving a yellow / red card".
//   - A starter is replaced only once he cannot play any more: he has not
//     played and all his club's fixtures this gameweek are done (or none).
//   - Starters are handled in slot order; each takes the first bench player,
//     in bench order, who has played, is not already used, and
//     keeps the formation legal. Goalkeepers only replace goalkeepers.
//   - If an earlier eligible bench player has not played yet but still has a
//     fixture to come, the sub is undecided and nothing is projected for that
//     starter yet, so a projection never has to be taken back for a bench
//     player who later comes on.
//
// Subs are SubConfirmed when gameweekDone, else SubProjected.
func (r LineupRules) AutoSubs(starters, bench []LineupPlayer, gameweekDone bool) []Sub {
	status := SubProjected
	if gameweekDone {
		status = SubConfirmed
	}
	counts := Counts{}
	for _, s := range starters {
		counts[s.Position]++
	}
	used := make([]bool, len(bench))
	var subs []Sub
	for i, starter := range starters {
		if starter.played() || !starter.FixturesDone {
			continue
		}
	bench:
		for j, b := range bench {
			if used[j] || (starter.Position == GKP) != (b.Position == GKP) {
				continue
			}
			next := Counts{}
			for k, v := range counts {
				next[k] = v
			}
			next[starter.Position]--
			next[b.Position]++
			if !r.FormationLegal(next) {
				continue
			}
			switch {
			case b.played():
				used[j] = true
				counts = next
				subs = append(subs, Sub{
					OutSlot: i + 1, InSlot: len(starters) + j + 1,
					OutPlayerID: starter.PlayerID, InPlayerID: b.PlayerID,
					Status: status,
				})
				break bench
			case !b.FixturesDone:
				// Might still play: wait for him.
				break bench
			}
		}
	}
	return subs
}
