package scoring

import (
	"reflect"
	"testing"
)

func TestPoints(t *testing.T) {
	r := StandardRules()
	tests := []struct {
		name  string
		pos   Position
		stats FixtureStats
		want  int
		lines []Line // nil: do not check lines
	}{
		{name: "did not play", pos: MID, stats: FixtureStats{Minutes: 0, Goals: 1, CleanSheet: true}, want: 0, lines: []Line{}},

		// Booked without playing (FPL: "Playing in a Gameweek means making
		// an appearance on the pitch or receiving a yellow / red card"; the
		// card lines carry no minutes condition, appearance points need
		// minutes): the card counts, nothing else does.
		{name: "booked on the bench", pos: DEF, stats: FixtureStats{Minutes: 0, YellowCards: 1}, want: -1,
			lines: []Line{{StatMinutes, 0, 0}, {StatYellowCards, 1, -1}}},
		{name: "sent off from the bench", pos: MID, stats: FixtureStats{Minutes: 0, RedCards: 1}, want: -3,
			lines: []Line{{StatMinutes, 0, 0}, {StatRedCards, 1, -3}}},
		{name: "booked on the bench scores no clean sheet or conceded", pos: GKP,
			stats: FixtureStats{Minutes: 0, YellowCards: 1, CleanSheet: true, GoalsConceded: 2, Saves: 3}, want: -1},
		{name: "cameo", pos: FWD, stats: FixtureStats{Minutes: 1}, want: 1,
			lines: []Line{{StatMinutes, 1, 1}}},
		{name: "59 minutes", pos: DEF, stats: FixtureStats{Minutes: 59}, want: 1},
		{name: "60 minutes", pos: DEF, stats: FixtureStats{Minutes: 60}, want: 2},

		// Goals by position.
		{name: "GKP goal", pos: GKP, stats: FixtureStats{Minutes: 90, Goals: 1}, want: 12},
		{name: "DEF goal", pos: DEF, stats: FixtureStats{Minutes: 90, Goals: 1}, want: 8},
		{name: "MID brace", pos: MID, stats: FixtureStats{Minutes: 90, Goals: 2}, want: 12},
		{name: "FWD hat-trick", pos: FWD, stats: FixtureStats{Minutes: 90, Goals: 3}, want: 14},
		{name: "assists", pos: FWD, stats: FixtureStats{Minutes: 90, Assists: 2}, want: 8},

		// Clean sheets need 60 minutes and depend on position.
		{name: "GKP clean sheet", pos: GKP, stats: FixtureStats{Minutes: 90, CleanSheet: true}, want: 6},
		{name: "DEF clean sheet", pos: DEF, stats: FixtureStats{Minutes: 60, CleanSheet: true}, want: 6},
		{name: "MID clean sheet", pos: MID, stats: FixtureStats{Minutes: 90, CleanSheet: true}, want: 3},
		{name: "FWD clean sheet scores nothing", pos: FWD, stats: FixtureStats{Minutes: 90, CleanSheet: true}, want: 2,
			lines: []Line{{StatMinutes, 90, 2}}},
		{name: "clean sheet under 60 minutes", pos: DEF, stats: FixtureStats{Minutes: 59, CleanSheet: true}, want: 1},

		// Goals conceded: -1 per 2 for GKP and DEF only.
		{name: "DEF conceded 1", pos: DEF, stats: FixtureStats{Minutes: 90, GoalsConceded: 1}, want: 2},
		{name: "DEF conceded 2", pos: DEF, stats: FixtureStats{Minutes: 90, GoalsConceded: 2}, want: 1},
		{name: "GKP conceded 5", pos: GKP, stats: FixtureStats{Minutes: 90, GoalsConceded: 5}, want: 0},
		{name: "MID conceded 4", pos: MID, stats: FixtureStats{Minutes: 90, GoalsConceded: 4}, want: 2},

		// Saves: 1 per 3, for whoever keeps goal.
		{name: "GKP 2 saves", pos: GKP, stats: FixtureStats{Minutes: 90, Saves: 2}, want: 2},
		{name: "GKP 7 saves", pos: GKP, stats: FixtureStats{Minutes: 90, Saves: 7}, want: 4,
			lines: []Line{{StatMinutes, 90, 2}, {StatSaves, 7, 2}}},
		// An outfield player in goal (FPL help, "How does FPL decide the
		// player positions?": "they will be able to collect points for
		// saves, but will score points as per their position
		// classification"): 2 + 2 for 6 saves + MID's 1 for the clean sheet.
		{name: "MID in goal: saves, MID clean sheet", pos: MID, stats: FixtureStats{Minutes: 90, Saves: 6, CleanSheet: true}, want: 5,
			lines: []Line{{StatMinutes, 90, 2}, {StatCleanSheets, 1, 1}, {StatSaves, 6, 2}}},
		{name: "DEF in goal: saves and conceded", pos: DEF, stats: FixtureStats{Minutes: 90, Saves: 3, GoalsConceded: 2}, want: 2 + 1 - 1},
		{name: "FWD in goal: 5 saves", pos: FWD, stats: FixtureStats{Minutes: 90, Saves: 5}, want: 3},

		// Penalties, cards and own goals.
		{name: "penalty saved", pos: GKP, stats: FixtureStats{Minutes: 90, PenaltiesSaved: 1}, want: 7},
		{name: "penalty missed", pos: FWD, stats: FixtureStats{Minutes: 90, PenaltiesMissed: 1}, want: 0},
		{name: "yellow", pos: MID, stats: FixtureStats{Minutes: 90, YellowCards: 1}, want: 1},
		{name: "straight red", pos: MID, stats: FixtureStats{Minutes: 40, RedCards: 1}, want: -2},
		{name: "second yellow is a red, not red plus yellow", pos: DEF, stats: FixtureStats{Minutes: 70, YellowCards: 1, RedCards: 1}, want: -1,
			lines: []Line{{StatMinutes, 70, 2}, {StatRedCards, 1, -3}}},
		{name: "own goal", pos: DEF, stats: FixtureStats{Minutes: 90, OwnGoals: 1}, want: 0},
		// FPL: "If a player receives a red card, they will continue to be
		// penalised for goals conceded by their team." GoalsConceded counts
		// the goals after the dismissal: off at 50 minutes, 4 conceded in
		// the match (3 after he went): 1 - 3 - 2 = -4.
		{name: "sent-off DEF still pays for goals conceded after", pos: DEF,
			stats: FixtureStats{Minutes: 50, RedCards: 1, GoalsConceded: 4}, want: 1 - 3 - 2,
			lines: []Line{{StatMinutes, 50, 1}, {StatGoalsConceded, 4, -2}, {StatRedCards, 1, -3}}},
		// Off at 70 with the score 0-0, 2 conceded after: no clean sheet,
		// 2 - 1 - 3 = -2.
		{name: "sent-off GKP loses the clean sheet to later goals", pos: GKP,
			stats: FixtureStats{Minutes: 70, RedCards: 1, GoalsConceded: 2}, want: 2 - 1 - 3},

		// Defensive contributions: DEF on CBIT >= 10, MID/FWD on CBIRT >= 12, GKP never.
		{name: "DEF 10 CBIT", pos: DEF, stats: FixtureStats{Minutes: 90, ClearancesBlocksInterceptions: 6, Tackles: 4}, want: 4,
			lines: []Line{{StatMinutes, 90, 2}, {StatDefensiveContribution, 10, 2}}},
		{name: "DEF 9 CBIT, recoveries do not count", pos: DEF, stats: FixtureStats{Minutes: 90, ClearancesBlocksInterceptions: 5, Tackles: 4, Recoveries: 8}, want: 2},
		{name: "MID 12 CBIRT", pos: MID, stats: FixtureStats{Minutes: 90, ClearancesBlocksInterceptions: 2, Tackles: 3, Recoveries: 7}, want: 4},
		{name: "MID 11 CBIRT", pos: MID, stats: FixtureStats{Minutes: 90, Tackles: 4, Recoveries: 7}, want: 2},
		{name: "FWD 12 CBIRT", pos: FWD, stats: FixtureStats{Minutes: 90, Recoveries: 12}, want: 4},
		{name: "DC capped at 2", pos: DEF, stats: FixtureStats{Minutes: 90, ClearancesBlocksInterceptions: 25, Tackles: 5}, want: 4},
		{name: "GKP never gets DC", pos: GKP, stats: FixtureStats{Minutes: 90, ClearancesBlocksInterceptions: 20, Tackles: 5, Recoveries: 10}, want: 2},

		{name: "bonus", pos: MID, stats: FixtureStats{Minutes: 90, Bonus: 3}, want: 5},

		// A full haul, with the breakdown in explain order.
		{name: "DEF haul", pos: DEF, stats: FixtureStats{
			Minutes: 90, Goals: 1, Assists: 1, CleanSheet: true, YellowCards: 1,
			ClearancesBlocksInterceptions: 8, Tackles: 3, Bonus: 3,
		}, want: 2 + 6 + 3 + 4 - 1 + 2 + 3, lines: []Line{
			{StatMinutes, 90, 2}, {StatGoalsScored, 1, 6}, {StatAssists, 1, 3}, {StatCleanSheets, 1, 4},
			{StatYellowCards, 1, -1}, {StatDefensiveContribution, 11, 2}, {StatBonus, 3, 3},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, lines := r.Points(tt.pos, tt.stats)
			if got != tt.want {
				t.Errorf("points = %d, want %d (lines %+v)", got, tt.want, lines)
			}
			sum := 0
			for _, l := range lines {
				sum += l.Points
			}
			if sum != got {
				t.Errorf("lines sum to %d, total is %d", sum, got)
			}
			if tt.lines != nil {
				if len(tt.lines) == 0 && len(lines) == 0 {
					return
				}
				if !reflect.DeepEqual(lines, tt.lines) {
					t.Errorf("lines = %+v, want %+v", lines, tt.lines)
				}
			}
		})
	}
}

func TestGameweekPointsDoubleAndBlank(t *testing.T) {
	r := StandardRules()
	if got := r.GameweekPoints(FWD, nil); got != 0 {
		t.Errorf("blank = %d", got)
	}
	dgw := []FixtureStats{{Minutes: 90, Goals: 1}, {Minutes: 30}}
	if got := r.GameweekPoints(FWD, dgw); got != 6+1 {
		t.Errorf("double = %d", got)
	}
}

func TestBonusFromBPS(t *testing.T) {
	k := func(id string, score int) BPS { return BPS{PlayerID: id, Score: score} }
	tests := []struct {
		name   string
		scores []BPS
		want   map[string]int
	}{
		{"clear top three", []BPS{k("a", 30), k("b", 25), k("c", 20), k("d", 10)}, map[string]int{"a": 3, "b": 2, "c": 1}},
		{"order of input does not matter", []BPS{k("d", 10), k("c", 20), k("a", 30), k("b", 25)}, map[string]int{"a": 3, "b": 2, "c": 1}},
		{"tie for first", []BPS{k("a", 30), k("b", 30), k("c", 20), k("d", 10)}, map[string]int{"a": 3, "b": 3, "c": 1}},
		{"tie for second", []BPS{k("a", 30), k("b", 25), k("c", 25), k("d", 10)}, map[string]int{"a": 3, "b": 2, "c": 2}},
		{"tie for third", []BPS{k("a", 30), k("b", 25), k("c", 20), k("d", 20), k("e", 5)}, map[string]int{"a": 3, "b": 2, "c": 1, "d": 1}},
		{"three-way tie for first", []BPS{k("a", 30), k("b", 30), k("c", 30), k("d", 29)}, map[string]int{"a": 3, "b": 3, "c": 3}},
		{"fewer than three players", []BPS{k("a", 5)}, map[string]int{"a": 3}},
		{"nobody", nil, map[string]int{}},
		// FPL: "The three best performing players in each match will be
		// awarded bonus points" (fantasy.premierleague.com/help/rules). The
		// sign of a known BPS does not matter: 0 and negative scores rank.
		{"zero and negative scores rank", []BPS{k("a", 12), k("b", 0), k("c", -3)}, map[string]int{"a": 3, "b": 2, "c": 1}},
		{"tie at zero for third", []BPS{k("a", 30), k("b", 20), k("c", 0), k("d", 0)}, map[string]int{"a": 3, "b": 2, "c": 1, "d": 1}},
		{"all negative", []BPS{k("a", -9), k("b", -1), k("c", -5), k("d", -2)}, map[string]int{"b": 3, "d": 2, "c": 1}},
		// An unknown BPS (missing per-fixture data) is not a score: it is
		// left out and never takes or shares a place.
		{"unknown is not ranked", []BPS{k("a", 12), {PlayerID: "b", Unknown: true}, k("c", -3)}, map[string]int{"a": 3, "c": 2}},
		{"only unknowns", []BPS{{PlayerID: "a", Unknown: true}, {PlayerID: "b", Unknown: true}}, map[string]int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BonusFromBPS(tt.scores); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
