package scoring

import (
	"reflect"
	"testing"
)

// p builds a lineup player. played: minutes > 0; done: fixtures finished.
func p(id string, pos Position, minutes int, done bool, points int) LineupPlayer {
	return LineupPlayer{PlayerID: id, Position: pos, Minutes: minutes, FixturesDone: done, Points: points}
}

// xi442 is a 4-4-2 where everyone played; tests overwrite slots.
func xi442() []LineupPlayer {
	return []LineupPlayer{
		p("g1", GKP, 90, true, 2),
		p("d1", DEF, 90, true, 2), p("d2", DEF, 90, true, 2), p("d3", DEF, 90, true, 2), p("d4", DEF, 90, true, 2),
		p("m1", MID, 90, true, 2), p("m2", MID, 90, true, 2), p("m3", MID, 90, true, 2), p("m4", MID, 90, true, 2),
		p("f1", FWD, 90, true, 2), p("f2", FWD, 90, true, 2),
	}
}

func bench(players ...LineupPlayer) []LineupPlayer { return players }

// booked marks a lineup player as shown a card this gameweek.
func booked(lp LineupPlayer) LineupPlayer { lp.Booked = true; return lp }

type subPair struct{ out, in int }

func pairs(subs []Sub) []subPair {
	out := []subPair{}
	for _, s := range subs {
		out = append(out, subPair{s.OutSlot, s.InSlot})
	}
	return out
}

func TestAutoSubs(t *testing.T) {
	rules := StandardLineup()
	tests := []struct {
		name   string
		xi     func([]LineupPlayer)
		bench  []LineupPlayer
		done   bool
		want   []subPair
		status SubStatus
	}{
		{
			name:  "everyone played: no subs",
			xi:    func([]LineupPlayer) {},
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 90, true, 1), p("m5", MID, 90, true, 1), p("f3", FWD, 90, true, 1)),
			done:  true, want: []subPair{},
		},
		{
			name:  "first bench player who played comes on",
			xi:    func(xi []LineupPlayer) { xi[5] = p("m1", MID, 0, true, 0) },
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 0, true, 0), p("m5", MID, 45, true, 3), p("f3", FWD, 90, true, 5)),
			done:  true, want: []subPair{{6, 14}}, status: SubConfirmed,
		},
		// FPL: "Playing in a Gameweek means making an appearance on the
		// pitch or receiving a yellow / red card."
		{
			name:  "a starter booked on the bench played: not subbed",
			xi:    func(xi []LineupPlayer) { xi[5] = booked(p("m1", MID, 0, true, -1)) },
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 90, true, 2), p("m5", MID, 90, true, 2), p("f3", FWD, 90, true, 2)),
			done:  true, want: []subPair{},
		},
		{
			name:  "a bench player booked without playing played: he comes on",
			xi:    func(xi []LineupPlayer) { xi[5] = p("m1", MID, 0, true, 0) },
			bench: bench(p("g2", GKP, 0, true, 0), booked(p("d5", DEF, 0, true, -1)), p("m5", MID, 90, true, 2), p("f3", FWD, 90, true, 2)),
			done:  true, want: []subPair{{6, 13}}, status: SubConfirmed,
		},
		{
			name:  "keeper only replaces keeper",
			xi:    func(xi []LineupPlayer) { xi[0] = p("g1", GKP, 0, true, 0) },
			bench: bench(p("g2", GKP, 90, true, 3), p("d5", DEF, 90, true, 2), p("m5", MID, 90, true, 2), p("f3", FWD, 90, true, 2)),
			done:  true, want: []subPair{{1, 12}},
		},
		{
			name:  "bench keeper never replaces an outfielder",
			xi:    func(xi []LineupPlayer) { xi[9] = p("f1", FWD, 0, true, 0) },
			bench: bench(p("g2", GKP, 90, true, 3), p("d5", DEF, 0, true, 0), p("m5", MID, 0, true, 0), p("f3", FWD, 20, true, 1)),
			done:  true, want: []subPair{{10, 15}},
		},
		{
			name: "formation stays legal: last forward needs a forward",
			xi: func(xi []LineupPlayer) {
				// 5-4-1: replace a midfielder with d-bench to get 5-4-1, then the lone forward does not play.
				xi[5] = p("d5", DEF, 90, true, 2)
				xi[10] = p("f2", FWD, 0, true, 0)
				xi[9] = p("m5", MID, 90, true, 2)
			},
			bench: bench(p("g2", GKP, 0, true, 0), p("m1", MID, 90, true, 2), p("d6", DEF, 90, true, 2), p("f3", FWD, 30, true, 1)),
			done:  true, want: []subPair{{11, 15}},
		},
		{
			name: "formation stays legal: third defender needs a defender",
			xi: func(xi []LineupPlayer) {
				// 3-5-2 with d1 not playing.
				xi[4] = p("m5", MID, 90, true, 2)
				xi[1] = p("d1", DEF, 0, true, 0)
			},
			bench: bench(p("g2", GKP, 0, true, 0), p("m9", MID, 90, true, 2), p("f3", FWD, 90, true, 2), p("d5", DEF, 10, true, 1)),
			done:  true, want: []subPair{{2, 15}},
		},
		{
			name:  "starter with a fixture still to play is not subbed",
			xi:    func(xi []LineupPlayer) { xi[5] = p("m1", MID, 0, false, 0) },
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 90, true, 2), p("m5", MID, 90, true, 2), p("f3", FWD, 90, true, 2)),
			done:  false, want: []subPair{},
		},
		{
			name:  "blank gameweek starter is subbed",
			xi:    func(xi []LineupPlayer) { xi[9] = p("f1", FWD, 0, true, 0) },
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 60, true, 2), p("m5", MID, 0, true, 0), p("f3", FWD, 0, true, 0)),
			done:  false, want: []subPair{{10, 13}}, status: SubProjected,
		},
		{
			name:  "earlier bench player still to play: wait",
			xi:    func(xi []LineupPlayer) { xi[5] = p("m1", MID, 0, true, 0) },
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 0, false, 0), p("m5", MID, 90, true, 2), p("f3", FWD, 90, true, 2)),
			done:  false, want: []subPair{},
		},
		{
			name: "each bench player is used once, in bench order",
			xi: func(xi []LineupPlayer) {
				xi[5] = p("m1", MID, 0, true, 0)
				xi[6] = p("m2", MID, 0, true, 0)
			},
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 90, true, 2), p("m5", MID, 0, true, 0), p("f3", FWD, 90, true, 2)),
			done:  true, want: []subPair{{6, 13}, {7, 15}},
		},
		{
			name:  "nobody on the bench played",
			xi:    func(xi []LineupPlayer) { xi[5] = p("m1", MID, 0, true, 0) },
			bench: bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 0, true, 0), p("m5", MID, 0, true, 0), p("f3", FWD, 0, true, 0)),
			done:  true, want: []subPair{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			xi := xi442()
			tt.xi(xi)
			subs := rules.AutoSubs(xi, tt.bench, tt.done)
			if got := pairs(subs); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("subs = %v, want %v", got, tt.want)
			}
			for _, s := range subs {
				if tt.status != "" && s.Status != tt.status {
					t.Errorf("status = %q, want %q", s.Status, tt.status)
				}
				if s.OutPlayerID != xi[s.OutSlot-1].PlayerID || s.InPlayerID != tt.bench[s.InSlot-12].PlayerID {
					t.Errorf("player ids do not match slots: %+v", s)
				}
			}
		})
	}
}

func TestTeamTotal(t *testing.T) {
	xi := xi442()
	xi[5] = p("m1", MID, 0, true, 0)
	b := bench(p("g2", GKP, 0, true, 0), p("d5", DEF, 90, true, 6), p("m5", MID, 90, true, 3), p("f3", FWD, 90, true, 9))
	subs := StandardLineup().AutoSubs(xi, b, true)
	total, benchPts := TeamTotal(xi, b, subs)
	// 10 starters x 2 + d5 (6) subbed in; bench keeps m5 (3) + f3 (9).
	if total != 26 || benchPts != 12 {
		t.Errorf("total, bench = %d, %d", total, benchPts)
	}
}

func TestAveragePoints(t *testing.T) {
	tests := []struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{40, 41}, 40},
		{[]int{50, 61, 33, 47, 59, 28, 44, 52, 39}, 45}, // 413 / 9 = 45.9
		{[]int{-3, 0}, -2}, // floor, not truncation
	}
	for _, tt := range tests {
		if got := AveragePoints(tt.in); got != tt.want {
			t.Errorf("AveragePoints(%v) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestH2HAndStandings(t *testing.T) {
	if H2H(50, 40) != Win || H2H(40, 50) != Loss || H2H(45, 45) != Draw {
		t.Fatal("H2H")
	}
	if LeaguePoints(Win) != 3 || LeaguePoints(Draw) != 1 || LeaguePoints(Loss) != 0 {
		t.Fatal("LeaguePoints")
	}
	rows := []Standing{
		{TeamID: "a", Wins: 2, PointsFor: 100},
		{TeamID: "b", Wins: 2, PointsFor: 120},
		{TeamID: "c", Wins: 1, Draws: 3, PointsFor: 200},
		{TeamID: "d", Wins: 2, PointsFor: 100},
	}
	SortStandings(rows)
	var got []string
	for _, r := range rows {
		got = append(got, r.TeamID)
	}
	if !reflect.DeepEqual(got, []string{"c", "b", "a", "d"}) {
		t.Errorf("order = %v", got)
	}
}

func TestStateOf(t *testing.T) {
	notStarted := FixtureProgress{}
	live := FixtureProgress{Started: true}
	done := FixtureProgress{Started: true, Finished: true}
	tests := []struct {
		fx             []FixtureProgress
		bonus, checked bool
		want           ScoreState
	}{
		{nil, false, false, StateUpcoming},
		{[]FixtureProgress{notStarted, notStarted}, false, false, StateUpcoming},
		{[]FixtureProgress{done, live, notStarted}, false, false, StateLive},
		{[]FixtureProgress{done, notStarted}, false, false, StateLive},
		{[]FixtureProgress{done, done}, false, false, StateProvisional},
		{[]FixtureProgress{done, done}, true, false, StateOfficial},
		{[]FixtureProgress{done, done}, true, true, StateFinal},
	}
	for i, tt := range tests {
		if got := StateOf(tt.fx, tt.bonus, tt.checked); got != tt.want {
			t.Errorf("case %d: got %q, want %q", i, got, tt.want)
		}
	}
}

// A real gameweek 8 (2026-27), ids anonymised: a 4-5-1 whose defenders
// d1, d2 and d4 did not play (0 minutes, no card, fixtures finished), with
// two outfield bench players who did: dB (DEF, 6 points) then fB (FWD, 1).
// Starters are taken in XI order, so the order decides which two of the
// three defenders go off; the total is the same either way, because a
// starter who did not play scores 0 and the same two come on.
func TestAutoSubsXIOrderDecidesWhoGoesOff(t *testing.T) {
	rules := StandardLineup()
	pl := map[string]LineupPlayer{
		"g1": p("g1", GKP, 90, true, 7),
		"d1": p("d1", DEF, 0, true, 0), "d2": p("d2", DEF, 0, true, 0), "d3": booked(p("d3", DEF, 90, true, 1)), "d4": p("d4", DEF, 0, true, 0),
		"m1": p("m1", MID, 0, true, 0), "m2": p("m2", MID, 76, true, 8), "m3": p("m3", MID, 79, true, -1), "m4": p("m4", MID, 79, true, 3),
		"m5": p("m5", MID, 90, true, 0), "f1": p("f1", FWD, 13, true, 1),
		"g2": p("g2", GKP, 0, true, 0), "dB": p("dB", DEF, 64, true, 6), "fB": p("fB", FWD, 2, true, 1), "f2": p("f2", FWD, 0, true, 0),
	}
	lineup := func(ids ...string) (xi, b []LineupPlayer) {
		for i, id := range ids {
			if i < 11 {
				xi = append(xi, pl[id])
			} else {
				b = append(b, pl[id])
			}
		}
		return xi, b
	}
	for _, tc := range []struct {
		name  string
		order []string
		want  []string // out-in, in sub order
	}{
		// FPL's picks as served during the gameweek.
		{"d2 d4 d3 d1", []string{"g1", "d2", "d4", "d3", "d1", "m1", "m2", "m3", "m4", "m5", "f1", "g2", "dB", "fB", "f2"}, []string{"d2-dB", "d4-fB"}},
		// The order FPL's subs imply (d1 for dB, then d2 for fB): rebuilt
		// from its final picks, d1 and d2 subbed out first.
		{"d1 d2 d3 d4", []string{"g1", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "m5", "f1", "g2", "dB", "fB", "f2"}, []string{"d1-dB", "d2-fB"}},
	} {
		xi, b := lineup(tc.order...)
		subs := rules.AutoSubs(xi, b, true)
		var got []string
		for _, s := range subs {
			got = append(got, s.OutPlayerID+"-"+s.InPlayerID)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: subs %v, want %v", tc.name, got, tc.want)
		}
		// 7 + 1 + 8 - 1 + 3 + 1 (XI) + 6 + 1 (subs) = 26, FPL's total.
		if pts, _ := TeamTotal(xi, b, subs); pts != 26 {
			t.Errorf("%s: total %d, want 26", tc.name, pts)
		}
	}
}
