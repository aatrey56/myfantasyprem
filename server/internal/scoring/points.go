package scoring

// Position is a player's fantasy position.
type Position string

const (
	GKP Position = "GKP"
	DEF Position = "DEF"
	MID Position = "MID"
	FWD Position = "FWD"
)

// Valid reports whether p is one of the four positions.
func (p Position) Valid() bool {
	switch p {
	case GKP, DEF, MID, FWD:
		return true
	}
	return false
}

// Stat names a line of the points breakdown. The values are FPL's `explain`
// identifiers and match the PointsStat enum in api/openapi.yaml.
type Stat string

const (
	StatMinutes               Stat = "minutes"
	StatGoalsScored           Stat = "goals_scored"
	StatAssists               Stat = "assists"
	StatCleanSheets           Stat = "clean_sheets"
	StatGoalsConceded         Stat = "goals_conceded"
	StatOwnGoals              Stat = "own_goals"
	StatPenaltiesSaved        Stat = "penalties_saved"
	StatPenaltiesMissed       Stat = "penalties_missed"
	StatYellowCards           Stat = "yellow_cards"
	StatRedCards              Stat = "red_cards"
	StatSaves                 Stat = "saves"
	StatDefensiveContribution Stat = "defensive_contribution"
	StatBonus                 Stat = "bonus"
)

// FixtureStats is one player's raw stats in one fixture. A double gameweek
// has two of these per player.
type FixtureStats struct {
	Minutes int
	Goals   int
	Assists int
	// CleanSheet: the player's team conceded no goal while he was on the
	// pitch, nor after he was sent off. It scores only with 60 or more
	// minutes (see Points).
	CleanSheet bool
	// GoalsConceded by the player's team while he was on the pitch and,
	// after a red card, for the rest of the match (FPL: a sent-off player
	// "will continue to be penalised for goals conceded by their team").
	// FPL's goals_conceded stat already counts it that way.
	GoalsConceded   int
	OwnGoals        int
	PenaltiesSaved  int
	PenaltiesMissed int
	// YellowCards and RedCards count even without minutes: a player booked
	// on the bench is docked for it (see Points and Played).
	YellowCards int
	RedCards    int
	// Saves by the player while in goal: a goalkeeper, or an outfield
	// player who took over in goal (he scores them too).
	Saves int
	// Defensive actions. CBIT = clearances+blocks+interceptions + tackles;
	// CBIRT also adds recoveries.
	ClearancesBlocksInterceptions int
	Tackles                       int
	Recoveries                    int
	// Bonus is the bonus awarded (0-3): provisional from BPS during and right
	// after the match (see BonusFromBPS), official once FPL adds it.
	Bonus int
}

// Line is one row of a points breakdown: the raw value and the points it
// earned. Lines with zero points are omitted, except minutes.
type Line struct {
	Stat   Stat `json:"stat"`
	Value  int  `json:"value"`
	Points int  `json:"points"`
}

// Rules are the per-position scoring values. StandardRules holds FPL's
// 2026-27 values (docs/research/RESEARCH.md section 7).
type Rules struct {
	Goal                  map[Position]int
	Assist                int
	CleanSheet            map[Position]int
	CleanSheetMinMinutes  int
	ConcededPer           int // goals conceded per deduction
	ConcededPenalty       map[Position]int
	SavesPer              int // saves per SavesPoints (whoever keeps goal)
	SavesPoints           int // points for every SavesPer saves
	PenaltySaved          int
	PenaltyMissed         int
	YellowCard            int
	RedCard               int
	OwnGoal               int
	ShortAppearance       int // 1-59 minutes
	FullAppearance        int // 60+ minutes
	FullAppearanceMinutes int
	// DefensiveThreshold is the CBIT (DEF) or CBIRT (MID, FWD) count that
	// earns DefensiveContribution points, once per fixture. 0 = never.
	DefensiveThreshold    map[Position]int
	DefensiveContribution int
}

// StandardRules returns FPL's 2026-27 scoring.
func StandardRules() Rules {
	return Rules{
		Goal:                  map[Position]int{GKP: 10, DEF: 6, MID: 5, FWD: 4},
		Assist:                3,
		CleanSheet:            map[Position]int{GKP: 4, DEF: 4, MID: 1, FWD: 0},
		CleanSheetMinMinutes:  60,
		ConcededPer:           2,
		ConcededPenalty:       map[Position]int{GKP: -1, DEF: -1, MID: 0, FWD: 0},
		SavesPer:              3,
		SavesPoints:           1,
		PenaltySaved:          5,
		PenaltyMissed:         -2,
		YellowCard:            -1,
		RedCard:               -3,
		OwnGoal:               -2,
		ShortAppearance:       1,
		FullAppearance:        2,
		FullAppearanceMinutes: 60,
		DefensiveThreshold:    map[Position]int{GKP: 0, DEF: 10, MID: 12, FWD: 12},
		DefensiveContribution: 2,
	}
}

// Played reports whether the player played in FPL's sense: "making an
// appearance on the pitch or receiving a yellow / red card"
// (fantasy.premierleague.com/help/rules). It decides auto-subs.
func (s FixtureStats) Played() bool {
	return s.Minutes > 0 || s.YellowCards > 0 || s.RedCards > 0
}

// Points scores one fixture and returns the total and its breakdown.
//
// A player who did not play scores nothing, whatever else the stats say.
// One booked without playing a minute (on the bench) scores only the card:
// FPL's card deductions have no minutes condition, its appearance points
// do. A red card's -3 already includes any yellow in the same match (FPL:
// "Red card deductions include any points deducted for yellow cards"), so
// a yellow is not also deducted when there is a red. Saves score for any
// position: an outfield player who goes in goal "will be able to collect
// points for saves, but will score points as per their position
// classification" (FPL help).
func (r Rules) Points(pos Position, s FixtureStats) (int, []Line) {
	if !s.Played() {
		return 0, nil
	}
	var lines []Line
	add := func(stat Stat, value, points int) {
		if points != 0 || stat == StatMinutes {
			lines = append(lines, Line{Stat: stat, Value: value, Points: points})
		}
	}

	cards := func() {
		if s.RedCards > 0 {
			add(StatRedCards, s.RedCards, r.RedCard)
		} else {
			add(StatYellowCards, s.YellowCards, min(s.YellowCards, 1)*r.YellowCard)
		}
	}
	if s.Minutes <= 0 {
		add(StatMinutes, 0, 0)
		cards()
		return sum(lines), lines
	}

	appearance := r.ShortAppearance
	if s.Minutes >= r.FullAppearanceMinutes {
		appearance = r.FullAppearance
	}
	add(StatMinutes, s.Minutes, appearance)
	add(StatGoalsScored, s.Goals, s.Goals*r.Goal[pos])
	add(StatAssists, s.Assists, s.Assists*r.Assist)
	if s.CleanSheet && s.Minutes >= r.CleanSheetMinMinutes {
		add(StatCleanSheets, 1, r.CleanSheet[pos])
	}
	if r.ConcededPer > 0 {
		add(StatGoalsConceded, s.GoalsConceded, (s.GoalsConceded/r.ConcededPer)*r.ConcededPenalty[pos])
	}
	add(StatOwnGoals, s.OwnGoals, s.OwnGoals*r.OwnGoal)
	add(StatPenaltiesSaved, s.PenaltiesSaved, s.PenaltiesSaved*r.PenaltySaved)
	add(StatPenaltiesMissed, s.PenaltiesMissed, s.PenaltiesMissed*r.PenaltyMissed)
	cards()
	if r.SavesPer > 0 {
		add(StatSaves, s.Saves, (s.Saves/r.SavesPer)*r.SavesPoints)
	}
	if t := r.DefensiveThreshold[pos]; t > 0 {
		actions := s.ClearancesBlocksInterceptions + s.Tackles
		if pos != DEF {
			actions += s.Recoveries
		}
		if actions >= t {
			add(StatDefensiveContribution, actions, r.DefensiveContribution)
		}
	}
	add(StatBonus, s.Bonus, s.Bonus)
	return sum(lines), lines
}

func sum(lines []Line) int {
	total := 0
	for _, l := range lines {
		total += l.Points
	}
	return total
}

// GameweekPoints sums a player's fixtures in one gameweek: two in a double,
// none in a blank (0 points).
func (r Rules) GameweekPoints(pos Position, fixtures []FixtureStats) int {
	total := 0
	for _, f := range fixtures {
		p, _ := r.Points(pos, f)
		total += p
	}
	return total
}
