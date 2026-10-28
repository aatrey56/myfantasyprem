package scoring

import (
	"fmt"
	"testing"

	"pgregory.net/rapid"
)

// Property tests: invariants that must hold for any input. The oracles
// below are written straight from the rule tables (RESEARCH.md §7, ADR 0008
// "Scoring and rules") and deliberately share no code with the package.

// table is RESEARCH.md §7's scoring table, copied by hand.
var table = struct {
	goal, cleanSheet, concededPer2 map[Position]int
	dcThreshold                    map[Position]int
}{
	goal:         map[Position]int{GKP: 10, DEF: 6, MID: 5, FWD: 4},
	cleanSheet:   map[Position]int{GKP: 4, DEF: 4, MID: 1, FWD: 0},
	concededPer2: map[Position]int{GKP: -1, DEF: -1, MID: 0, FWD: 0},
	dcThreshold:  map[Position]int{DEF: 10, MID: 12, FWD: 12}, // GKP: never
}

// oraclePoints scores one fixture from the table.
func oraclePoints(pos Position, s FixtureStats) int {
	// ADR 0008 rule 1, from FPL's rules page: "Playing in a Gameweek means
	// making an appearance on the pitch or receiving a yellow / red card."
	// The table's card lines have no minutes condition and its appearance
	// points need minutes, so a player booked without a minute (on the
	// bench) scores the card alone: -3 for a red (rule 6), else -1.
	if s.Minutes <= 0 {
		switch {
		case s.RedCards > 0:
			return -3
		case s.YellowCards > 0:
			return -1
		}
		return 0
	}
	pts := 1
	if s.Minutes >= 60 {
		pts = 2
	}
	pts += s.Goals * table.goal[pos]
	pts += s.Assists * 3
	if s.CleanSheet && s.Minutes >= 60 {
		pts += table.cleanSheet[pos]
	}
	pts += (s.GoalsConceded / 2) * table.concededPer2[pos]
	pts += s.OwnGoals * -2
	pts += s.PenaltiesSaved * 5
	pts += s.PenaltiesMissed * -2
	switch {
	case s.RedCards > 0:
		pts -= 3 // ADR 0008 rule 6: includes any yellow
	case s.YellowCards > 0:
		pts--
	}
	// ADR 0008 rule 4: saves score for whoever keeps goal. An outfield
	// player in goal "will be able to collect points for saves, but will
	// score points as per their position classification" (FPL FAQ).
	pts += s.Saves / 3
	if th, ok := table.dcThreshold[pos]; ok {
		actions := s.ClearancesBlocksInterceptions + s.Tackles
		if pos != DEF {
			actions += s.Recoveries // CBIRT for MID and FWD
		}
		if actions >= th {
			pts += 2 // at most once per fixture
		}
	}
	return pts + s.Bonus
}

func drawPosition(t *rapid.T, label string) Position {
	return rapid.SampledFrom([]Position{GKP, DEF, MID, FWD}).Draw(t, label)
}

func drawStats(t *rapid.T) FixtureStats {
	return FixtureStats{
		Minutes:                       rapid.IntRange(0, 120).Draw(t, "minutes"),
		Goals:                         rapid.IntRange(0, 4).Draw(t, "goals"),
		Assists:                       rapid.IntRange(0, 4).Draw(t, "assists"),
		CleanSheet:                    rapid.Bool().Draw(t, "cs"),
		GoalsConceded:                 rapid.IntRange(0, 7).Draw(t, "conceded"),
		OwnGoals:                      rapid.IntRange(0, 2).Draw(t, "og"),
		PenaltiesSaved:                rapid.IntRange(0, 2).Draw(t, "ps"),
		PenaltiesMissed:               rapid.IntRange(0, 2).Draw(t, "pm"),
		YellowCards:                   rapid.IntRange(0, 2).Draw(t, "yellow"),
		RedCards:                      rapid.IntRange(0, 1).Draw(t, "red"),
		Saves:                         rapid.IntRange(0, 12).Draw(t, "saves"),
		ClearancesBlocksInterceptions: rapid.IntRange(0, 15).Draw(t, "cbi"),
		Tackles:                       rapid.IntRange(0, 8).Draw(t, "tackles"),
		Recoveries:                    rapid.IntRange(0, 12).Draw(t, "recoveries"),
		Bonus:                         rapid.IntRange(0, 3).Draw(t, "bonus"),
	}
}

// Any stat line scores what the table says, and the breakdown adds up to
// the total with no zero-point lines except minutes.
func TestPointsMatchTheTable(t *testing.T) {
	r := StandardRules()
	rapid.Check(t, func(rt *rapid.T) {
		pos, s := drawPosition(rt, "pos"), drawStats(rt)
		got, lines := r.Points(pos, s)
		if want := oraclePoints(pos, s); got != want {
			rt.Fatalf("Points(%s, %+v) = %d, table says %d", pos, s, got, want)
		}
		sum := 0
		for _, l := range lines {
			sum += l.Points
			if l.Points == 0 && l.Stat != StatMinutes {
				rt.Fatalf("zero-point line %+v", l)
			}
		}
		if sum != got {
			rt.Fatalf("lines sum to %d, total %d", sum, got)
		}
	})
}

// A double gameweek is the sum of its fixtures (ADR 0008 rule 9).
func TestGameweekPointsIsTheSumOfFixtures(t *testing.T) {
	r := StandardRules()
	rapid.Check(t, func(rt *rapid.T) {
		pos := drawPosition(rt, "pos")
		n := rapid.IntRange(0, 3).Draw(rt, "fixtures")
		var fx []FixtureStats
		want := 0
		for range n {
			s := drawStats(rt)
			fx = append(fx, s)
			want += oraclePoints(pos, s)
		}
		if got := r.GameweekPoints(pos, fx); got != want {
			rt.Fatalf("GameweekPoints = %d, want %d", got, want)
		}
	})
}

// Bonus, defined as standard competition ranking (ADR 0008 rule 8): a
// player's rank is 1 + the number of players with a strictly higher BPS;
// ranks 1, 2, 3 earn 3, 2, 1. This count-based definition is independent
// of the sort the code uses.
func TestBonusMatchesCompetitionRanking(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(0, 12).Draw(rt, "players")
		var in []BPS
		for i := range n {
			// A narrow range makes ties common.
			in = append(in, BPS{PlayerID: fmt.Sprintf("p%d", i), Score: rapid.IntRange(0, 6).Draw(rt, "bps")})
		}
		got := BonusFromBPS(in)
		total := 0
		for _, p := range in {
			rank := 1
			for _, q := range in {
				if q.Score > p.Score {
					rank++
				}
			}
			want := 0
			if rank <= 3 {
				want = 4 - rank
			}
			if got[p.PlayerID] != want {
				rt.Fatalf("%s (BPS %d, rank %d) got %d, want %d; all %v", p.PlayerID, p.Score, rank, got[p.PlayerID], want, in)
			}
			if _, listed := got[p.PlayerID]; listed != (want > 0) {
				rt.Fatalf("%s listed=%v with bonus %d", p.PlayerID, listed, want)
			}
			total += want
		}
		// FPL's tie table: with 3+ players at least 3+2+1 = 6 points go
		// out; ties only add (3,3,1 = 7; 3,2,2 = 7; 3,3,3 = 9; ...).
		if n >= 3 && total < 6 {
			rt.Fatalf("total bonus %d below 6 for %v", total, in)
		}
	})
}

// legalXI is ADR 0008 rule 11, written out: exactly 1 GKP, 3-5 DEF, 2-5
// MID, 1-3 FWD, 11 players.
func legalXI(c map[Position]int) bool {
	return c[GKP] == 1 && c[DEF] >= 3 && c[DEF] <= 5 && c[MID] >= 2 && c[MID] <= 5 &&
		c[FWD] >= 1 && c[FWD] <= 3 && c[GKP]+c[DEF]+c[MID]+c[FWD] == 11
}

// drawLineup draws a legal XI and bench from a 2/5/5/3 squad, with random
// minutes, fixture progress and points.
func drawLineup(t *rapid.T) (xi, bench []LineupPlayer) {
	var def, mid, fwd int
	for {
		def = rapid.IntRange(3, 5).Draw(t, "def")
		fwd = rapid.IntRange(1, 3).Draw(t, "fwd")
		mid = 10 - def - fwd
		if mid >= 2 && mid <= 5 {
			break
		}
	}
	player := func(id string, pos Position) LineupPlayer {
		played := rapid.Bool().Draw(t, id+" played")
		lp := LineupPlayer{PlayerID: id, Position: pos, FixturesDone: rapid.Bool().Draw(t, id+" done")}
		if played {
			lp.Minutes = rapid.IntRange(1, 90).Draw(t, id+" minutes")
			lp.Points = rapid.IntRange(-4, 20).Draw(t, id+" points")
		}
		return lp
	}
	xi = append(xi, player("g1", GKP))
	var rest []LineupPlayer
	for i := 1; i <= 5; i++ {
		p := player(fmt.Sprintf("d%d", i), DEF)
		if i <= def {
			xi = append(xi, p)
		} else {
			rest = append(rest, p)
		}
	}
	for i := 1; i <= 5; i++ {
		p := player(fmt.Sprintf("m%d", i), MID)
		if i <= mid {
			xi = append(xi, p)
		} else {
			rest = append(rest, p)
		}
	}
	for i := 1; i <= 3; i++ {
		p := player(fmt.Sprintf("f%d", i), FWD)
		if i <= fwd {
			xi = append(xi, p)
		} else {
			rest = append(rest, p)
		}
	}
	perm := rapid.Permutation(rest).Draw(t, "bench order")
	bench = append([]LineupPlayer{player("g2", GKP)}, perm...)
	return xi, bench
}

// For any legal lineup and any minutes, auto-subs (ADR 0008 rule 10):
// keep the XI legal, take each bench player at most once, replace only
// starters who cannot play any more, bring on only players who played,
// keeper for keeper; and the team total is exactly the points of the final
// XI.
func TestAutoSubsKeepTheLineupLegal(t *testing.T) {
	rules := StandardLineup()
	rapid.Check(t, func(rt *rapid.T) {
		xi, bench := drawLineup(rt)
		done := rapid.Bool().Draw(rt, "gameweek done")
		subs := rules.AutoSubs(xi, bench, done)

		outSlots, inSlots := map[int]bool{}, map[int]bool{}
		for _, s := range subs {
			if s.OutSlot < 1 || s.OutSlot > 11 || s.InSlot < 12 || s.InSlot > 15 {
				rt.Fatalf("slots out of range: %+v", s)
			}
			if outSlots[s.OutSlot] || inSlots[s.InSlot] {
				rt.Fatalf("slot used twice: %+v in %+v", s, subs)
			}
			outSlots[s.OutSlot], inSlots[s.InSlot] = true, true
			out, in := xi[s.OutSlot-1], bench[s.InSlot-12]
			if out.Minutes != 0 || !out.FixturesDone {
				rt.Fatalf("subbed out a starter who can still play: %+v", out)
			}
			if in.Minutes <= 0 {
				rt.Fatalf("subbed on a player who did not play: %+v", in)
			}
			if (out.Position == GKP) != (in.Position == GKP) {
				rt.Fatalf("keeper rule broken: %s for %s", in.Position, out.Position)
			}
			if want := map[bool]SubStatus{true: SubConfirmed, false: SubProjected}[done]; s.Status != want {
				rt.Fatalf("status %s, want %s", s.Status, want)
			}
		}

		counts := map[Position]int{}
		wantTotal, wantBench := 0, 0
		for i, p := range xi {
			if !outSlots[i+1] {
				counts[p.Position]++
				wantTotal += p.Points
			}
		}
		for j, p := range bench {
			if inSlots[12+j] {
				counts[p.Position]++
				wantTotal += p.Points
			} else {
				wantBench += p.Points
			}
		}
		if !legalXI(counts) {
			rt.Fatalf("final XI %v is not a legal formation (subs %+v)", counts, subs)
		}
		total, benchPts := TeamTotal(xi, bench, subs)
		if total != wantTotal || benchPts != wantBench {
			rt.Fatalf("TeamTotal = %d/%d, want %d/%d", total, benchPts, wantTotal, wantBench)
		}
	})
}

// Every legal formation ValidateLineup accepts is legal by the rule, and
// every one the rule allows is accepted.
func TestValidateLineupMatchesFormationRule(t *testing.T) {
	rules := StandardLineup()
	all := []string{"g1", "g2", "d1", "d2", "d3", "d4", "d5", "m1", "m2", "m3", "m4", "m5", "f1", "f2", "f3"}
	roster := squad()
	rapid.Check(t, func(rt *rapid.T) {
		perm := rapid.Permutation(all).Draw(rt, "order")
		xi, bench := perm[:11], perm[11:]
		counts := map[Position]int{}
		for _, id := range xi {
			counts[roster[id]]++
		}
		want := legalXI(counts) && roster[bench[0]] == GKP
		if got := len(rules.ValidateLineup(roster, xi, bench)) == 0; got != want {
			rt.Fatalf("XI %v bench %v: valid=%v, rule says %v (counts %v)", xi, bench, got, want, counts)
		}
	})
}
