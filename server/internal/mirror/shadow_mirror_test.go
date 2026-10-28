package mirror_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// noShow gives a player 0 minutes and 0 points in gameweek 9 (ours and
// FPL's live points).
func noShow(in mirror.ShadowInput, ids ...string) {
	for _, id := range ids {
		in.World.Rows[9][id] = []stats.PlayerFixture{{PlayerID: id, FixtureID: "fx1", Gameweek: 9}}
		in.Live[id] = 0
	}
}

// The real gameweek 8 shape (2026-27), on the shadow fixture: Ash's
// defenders a2, a3 and a5 did not play, a13 (DEF) and a14 (MID) came on.
// During the gameweek FPL served Ash's picks as a1, a3, a5, a4, a2, ...
// and the sync stored that; FPL's subs (a13 for a2, a14 for a3) imply the
// XI order a1, a2, a3, a4, a5, which the plan rebuilds from its final
// picks. Until the next sync stores it, our subs (a13 for a3, a14 for a5)
// differ: pending, with the reason, not a mismatch. Ash scores 28 either
// way.
func TestShadowSubsAwaitResyncedLineup(t *testing.T) {
	c := finalCase
	c.fplHome = 28
	in := c.input(t)
	noShow(in, "a2", "a3", "a5", "a15")
	in.State.Game.Matchups[0].HomePoints = 28
	final := []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11", "a12", "a13", "a14", "a15"}
	served := []string{"a1", "a3", "a5", "a4", "a2", "a6", "a7", "a8", "a9", "a10", "a11", "a12", "a13", "a14", "a15"}
	in.Plan.Lineups[0] = mirror.PlanLineup{Seat: "1", Gameweek: 9, Slots: final,
		Subs: []mirror.Sub{{Out: "a2", In: "a13"}, {Out: "a3", In: "a14"}}}
	in.State.Game.Lineups[0].Slots = served

	r := mirror.Shadow(in)
	if ms := r.Mismatches(); len(ms) != 0 {
		t.Fatalf("mismatches %v", notOK(r))
	}
	items := byKey(r)
	subs := items["subs:gw9:tA"]
	if subs.Status != mirror.ItemPending || subs.Ours != "A13 for A3; A14 for A5" || subs.Theirs != "A13 for A2; A14 for A3" ||
		!strings.Contains(subs.Note, "Re-sync stores FPL's final order") {
		t.Errorf("subs = %+v", subs)
	}
	lu := items["lineup:gw9:tA"]
	if lu.Status != mirror.ItemPending || !strings.HasPrefix(lu.Ours, "XI A1, A3, A5, A4, A2,") || !strings.HasPrefix(lu.Theirs, "XI A1, A2, A3, A4, A5,") {
		t.Errorf("lineup = %+v", lu)
	}
	if it := items["points:gw9:tA"]; it.Status != mirror.ItemOK || it.Ours != "28" {
		t.Errorf("points = %+v", it)
	}

	// The sync stores FPL's final order: the same subs, all clean.
	in.State.Game.Lineups[0].Slots = slices.Clone(final)
	r = mirror.Shadow(in)
	if r.Status != mirror.ReportClean {
		t.Fatalf("after sync: %v", notOK(r))
	}
	if it := byKey(r)["subs:gw9:tA"]; it.Ours != "A13 for A2; A14 for A3" {
		t.Errorf("after sync, subs = %+v", it)
	}
}

// reorderCase is TestShadowSubsAwaitResyncedLineup's gameweek: Ash's
// stored lineup in FPL's mid-gameweek order (XI a1, a3, a5, a4, a2), FPL's
// final picks in the order its subs imply.
func reorderCase(t *testing.T) mirror.ShadowInput {
	c := finalCase
	c.fplHome = 28
	in := c.input(t)
	noShow(in, "a2", "a3", "a5", "a15")
	in.State.Game.Matchups[0].HomePoints = 28
	in.Plan.Lineups[0] = mirror.PlanLineup{Seat: "1", Gameweek: 9,
		Slots: []string{"a1", "a2", "a3", "a4", "a5", "a6", "a7", "a8", "a9", "a10", "a11", "a12", "a13", "a14", "a15"},
		Subs:  []mirror.Sub{{Out: "a2", In: "a13"}, {Out: "a3", In: "a14"}}}
	in.State.Game.Lineups[0].Slots = []string{"a1", "a3", "a5", "a4", "a2", "a6", "a7", "a8", "a9", "a10", "a11", "a12", "a13", "a14", "a15"}
	return in
}

// A reordered lineup does not hide a real difference: with a6's stored
// stats row corrupted to 5 points (FPL: 2), FPL's order gives FPL's subs
// but 31 points (a1 10 + a4 2 + a6 5 + a7..a11 5x2 + a13 2 + a14 2), not
// FPL's 28. The order does not explain it: points, subs and lineup are
// mismatches, not pending.
func TestShadowReorderKeepsCorruptedPoints(t *testing.T) {
	in := reorderCase(t)
	in.World.Rows[9]["a6"][0].Points = 5
	in.State.Game.Matchups[0].HomePoints = 31 // what the rescore wrote
	items := byKey(mirror.Shadow(in))
	for k, want := range map[string]string{"points:gw9:tA": "31", "subs:gw9:tA": "A13 for A3; A14 for A5", "lineup:gw9:tA": ""} {
		if it := items[k]; it.Status != mirror.ItemMismatch || (want != "" && it.Ours != want) {
			t.Errorf("%s = %+v", k, it)
		}
	}
	if it := items["result:gw9:mu1"]; it.Status != mirror.ItemMismatch || it.Ours != "31-22" {
		t.Errorf("result = %+v", it)
	}
}

// The bench order decides auto-sub priority, so a bench in another order
// than FPL's stays a mismatch even with the same players. Bench a12, a14,
// a13, a15: a3 (first no-show in our XI order) gets a14 (MID; 3 DEF
// left), a5 gets a13, a2 nobody (a15 did not play); listed by player.
// 28 points either way.
func TestShadowReorderBenchPermutation(t *testing.T) {
	in := reorderCase(t)
	in.State.Game.Lineups[0].Slots = []string{"a1", "a3", "a5", "a4", "a2", "a6", "a7", "a8", "a9", "a10", "a11", "a12", "a14", "a13", "a15"}
	items := byKey(mirror.Shadow(in))
	if it := items["lineup:gw9:tA"]; it.Status != mirror.ItemMismatch {
		t.Errorf("lineup = %+v", it)
	}
	if it := items["subs:gw9:tA"]; it.Status != mirror.ItemMismatch || it.Ours != "A13 for A5; A14 for A3" {
		t.Errorf("subs = %+v", it)
	}
}

// An XI in another order than FPL's, with no FPL subs to explain it, is a
// lineup mismatch: the order decides auto-subs (the XI used to be compared
// as a set).
func TestShadowLineupOrderMismatch(t *testing.T) {
	in := finalCase.input(t)
	slots := slices.Clone(in.State.Game.Lineups[0].Slots)
	slots[1], slots[2] = slots[2], slots[1]
	in.State.Game.Lineups[0].Slots = slots
	it := byKey(mirror.Shadow(in))["lineup:gw9:tA"]
	if it.Status != mirror.ItemMismatch || !strings.HasPrefix(it.Ours, "XI A1, A3, A2,") {
		t.Errorf("lineup = %+v", it)
	}
}

// While the league is played on FPL its claims are made there: with no
// claim of ours, a waiver item compares FPL's outcome (its transaction
// history) with the transaction the sync imported from it (by FPL id),
// never with our rosters, which a later move in the same gameweek changes.
// Once claims are made here (or the league switched over), a missing
// claim of ours is a mismatch again.
func TestShadowMirroredWaiverOutcome(t *testing.T) {
	type txn struct {
		id, seat, in, out string
		kind              string
		approved          bool
	}
	w := func(id, seat, in, out string, approved bool) txn {
		return txn{id, seat, in, out, league.TxnWaiver, approved}
	}
	fa := func(id, seat, in, out string) txn { return txn{id, seat, in, out, league.TxnFreeAgent, true} }
	// roster moves in gameweek 9: player -> team (in, from 9) or "" (out, to 9)
	type move struct{ team, player, via string }
	for _, tc := range []struct {
		name   string
		fpl    []txn
		rec    map[string]bool // what our import recorded, when it differs
		skip   map[string]bool // not imported
		moves  []move
		mirror bool
		claim  string // a claim of Birch's made here: its status
		want   map[string]string
	}{
		{name: "won, then dropped for a free agent the same gameweek", mirror: true,
			// fa1's zero-length interval (in and out in gameweek 9) is not stored
			fpl:   []txn{w("1", "1", "fa1", "a15", true), fa("2", "1", "fa3", "fa1")},
			moves: []move{{"tA", "a15", ""}, {"tA", "fa3", league.TxnFreeAgent}},
			want:  map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemOK + " won (imported)"}},
		{name: "chained claims", mirror: true,
			fpl:   []txn{w("1", "1", "fa1", "a15", true), w("2", "1", "fa2", "fa1", true)},
			moves: []move{{"tA", "a15", ""}, {"tA", "fa2", league.TxnWaiver}},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemOK + " won (imported)",
				"waiver:gw9:tA:fa2:fa1": mirror.ItemOK + " won (imported)"}},
		{name: "lost claim, its player out dropped by another move", mirror: true,
			// fa1 for a15 lost, fa1 for a14 won, then a15 dropped for fa3
			fpl: []txn{w("1", "1", "fa1", "a15", false), w("2", "1", "fa1", "a14", true), fa("3", "1", "fa3", "a15")},
			moves: []move{{"tA", "a14", ""}, {"tA", "fa1", league.TxnWaiver}, {"tA", "a15", ""},
				{"tA", "fa3", league.TxnFreeAgent}},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemOK + " lost (imported)",
				"waiver:gw9:tA:fa1:a14": mirror.ItemOK + " won (imported)"}},
		{name: "two teams claim one player", mirror: true,
			fpl:   []txn{w("1", "1", "fa1", "a15", true), w("2", "2", "fa1", "b15", false)},
			moves: []move{{"tA", "a15", ""}, {"tA", "fa1", league.TxnWaiver}},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemOK + " won (imported)",
				"waiver:gw9:tB:fa1:b15": mirror.ItemOK + " lost (imported)"}},
		{name: "a free-agent move after waivers is no claim", mirror: true,
			fpl:   []txn{w("1", "1", "fa1", "a15", true), fa("2", "1", "fa3", "a14")},
			moves: []move{{"tA", "a15", ""}, {"tA", "fa1", league.TxnWaiver}, {"tA", "a14", ""}, {"tA", "fa3", league.TxnFreeAgent}},
			want:  map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemOK + " won (imported)", "waiver:gw9:tA:fa3:a14": ""}},
		{name: "imported with another outcome", mirror: true,
			fpl: []txn{w("1", "1", "fa1", "a15", true)}, rec: map[string]bool{"1": false},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemMismatch + " lost (imported)"}},
		{name: "not imported", mirror: true,
			fpl: []txn{w("1", "1", "fa1", "a15", true)}, skip: map[string]bool{"1": true},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemMismatch + " not imported"}},
		{name: "switched over: claims are ours",
			fpl:  []txn{w("1", "1", "fa1", "a15", true)},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemMismatch + " no claim"}},
		{name: "claims decided here this gameweek", mirror: true, claim: league.ClaimLost,
			fpl:  []txn{w("1", "1", "fa1", "a15", true)},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemMismatch + " no claim"}},
		{name: "claims pending here this gameweek", mirror: true, claim: league.ClaimPending,
			fpl:  []txn{w("1", "1", "fa1", "a15", true)},
			want: map[string]string{"waiver:gw9:tA:fa1:a15": mirror.ItemMismatch + " no claim"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := finalCase.input(t)
			in.Mirroring = tc.mirror
			st, g := in.State, in.State.Game
			st.Runs = map[league.RunKey]time.Time{{Kind: league.RunWaivers, Gameweek: 9}: shadowNow}
			team := map[string]string{"1": "tA", "2": "tB"}
			for _, x := range tc.fpl {
				in.Plan.Txns = append(in.Plan.Txns, mirror.PlanTxn{ExternalID: x.id, Kind: x.kind, Gameweek: 9, Seat: x.seat, In: x.in, Out: x.out,
					Approved: x.approved, Priority: 2})
				if tc.skip[x.id] {
					continue
				}
				rec, ok := tc.rec[x.id]
				if !ok {
					rec = x.approved
				}
				in.Txns = append(in.Txns, league.Txn{ID: "txn_" + x.id, Kind: x.kind, Gameweek: 9, TeamID: team[x.seat], PlayerIn: x.in,
					PlayerOut: x.out, Approved: rec})
				st.SetRef(league.RefTransaction, mirror.Provider, x.id, "txn_"+x.id)
			}
			for _, id := range []string{"fa1", "fa2", "fa3"} {
				in.World.Players[id] = stats.Player{ID: id, DisplayName: strings.ToUpper(id), Position: scoring.FWD, ClubID: "c1"}
			}
			for _, m := range tc.moves {
				if m.via == "" {
					for i := range g.Rosters {
						if g.Rosters[i].PlayerID == m.player {
							g.Rosters[i].ToGW = 9
						}
					}
					continue
				}
				g.Rosters = append(g.Rosters, league.RosterSlot{TeamID: m.team, PlayerID: m.player, FromGW: 9, AcquiredVia: m.via})
			}
			if tc.claim != "" { // another claim of Birch's, made here
				st.Claims = []league.Claim{{ID: "clm_2", TeamID: "tB", AddPlayerID: "fa2", DropPlayerID: "b15", Order: 1, Gameweek: 9,
					Status: tc.claim}}
			}
			items := byKey(mirror.Shadow(in))
			for k, want := range tc.want {
				it, ok := items[k]
				if got := it.Status + " " + it.Ours; want == "" && ok || want != "" && got != want {
					t.Errorf("%s = %+v, want %q", k, it, want)
				}
			}
		})
	}
}
