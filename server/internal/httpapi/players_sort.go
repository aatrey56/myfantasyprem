package httpapi

import (
	"cmp"
	"math"
	"slices"
	"strings"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// lineTotals are a player's season sums of the source's per-match stats
// (stats.PlayerGameweek). A pointer stays nil while no line publishes the
// stat; has is false for a player without any line.
type lineTotals struct {
	has                                    bool
	bps, cbi, recoveries, tackles, defCont int
	influence, creativity, threat, ict     *float64
	xg, xa, xgi, xgc                       *float64
	starts                                 *int
}

// withLineCounts corrects the season counts of double gameweeks. Their
// fixture rows are rebuilt from upstream's explain breakdown, which leaves
// out stats that earn no points (goals conceded for a midfielder, a save
// short of the threshold, a forward's clean sheet); the gameweek line
// carries upstream's complete totals, so a gameweek with more than one
// row counts the line instead of the rows. Points and bonus stay the
// rows' (the line's bonus is upstream's projection).
func withLineCounts(w *league.World, totals map[string]playerTotals, lines []stats.PlayerGameweek) map[string]playerTotals {
	for _, l := range lines {
		rows := w.Rows[l.Gameweek][l.PlayerID]
		if len(rows) < 2 {
			continue
		}
		t := totals[l.PlayerID]
		for _, r := range rows {
			t.minutes -= r.Stats.Minutes
			t.goals -= r.Stats.Goals
			t.assists -= r.Stats.Assists
			t.saves -= r.Stats.Saves
			t.goalsConceded -= r.Stats.GoalsConceded
			t.ownGoals -= r.Stats.OwnGoals
			t.pensSaved -= r.Stats.PenaltiesSaved
			t.pensMissed -= r.Stats.PenaltiesMissed
			t.yellows -= r.Stats.YellowCards
			t.reds -= r.Stats.RedCards
			if r.Stats.CleanSheet && r.Stats.Minutes >= 60 {
				t.cleanSheets--
			}
		}
		t.minutes += l.Minutes
		t.goals += l.GoalsScored
		t.assists += l.Assists
		t.saves += l.Saves
		t.goalsConceded += l.GoalsConceded
		t.ownGoals += l.OwnGoals
		t.pensSaved += l.PenaltiesSaved
		t.pensMissed += l.PenaltiesMissed
		t.yellows += l.YellowCards
		t.reds += l.RedCards
		// Upstream counts a clean sheet only for 60 minutes or more.
		t.cleanSheets += l.CleanSheets
		totals[l.PlayerID] = t
	}
	return totals
}

// sumLines sums gameweek lines per player.
func sumLines(lines []stats.PlayerGameweek) map[string]lineTotals {
	add := func(sum **float64, v *float64) {
		if v == nil {
			return
		}
		if *sum == nil {
			*sum = new(float64)
		}
		**sum += *v
	}
	out := map[string]lineTotals{}
	for _, l := range lines {
		t := out[l.PlayerID]
		t.has = true
		t.bps += l.BPS
		t.cbi += l.ClearancesBlocksInterceptions
		t.recoveries += l.Recoveries
		t.tackles += l.Tackles
		t.defCont += l.DefensiveContribution
		add(&t.influence, l.Influence)
		add(&t.creativity, l.Creativity)
		add(&t.threat, l.Threat)
		add(&t.ict, l.ICTIndex)
		add(&t.xg, l.ExpectedGoals)
		add(&t.xa, l.ExpectedAssists)
		add(&t.xgi, l.ExpectedGoalInvolvements)
		add(&t.xgc, l.ExpectedGoalsConceded)
		if l.Starts != nil {
			if t.starts == nil {
				t.starts = new(int)
			}
			*t.starts += *l.Starts
		}
		out[l.PlayerID] = t
	}
	return out
}

func roundN(v float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(v*p) / p
}

func intVal(n int) *float64 { return ptr(float64(n)) }

// lineInt is a count from the source's lines: null without any line.
func lineInt(l lineTotals, n int) *float64 {
	if !l.has {
		return nil
	}
	return intVal(n)
}

func round2(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return ptr(roundN(*v, 2))
}

// sortValue is the player's value of the sorted stat, rounded as the
// contract shows it (PlayerListItem.sortValue); nil when untracked.
func sortValue(sort ListLeaguePlayersParamsSort, t playerTotals, l lineTotals, gwPts, draftRank int) *float64 {
	switch sort {
	case ListLeaguePlayersParamsSortTotalPoints:
		return intVal(t.points)
	case ListLeaguePlayersParamsSortGameweekPoints:
		return intVal(gwPts)
	case ListLeaguePlayersParamsSortForm:
		if !t.hasForm {
			return nil
		}
		return ptr(roundN(t.form, 1))
	case ListLeaguePlayersParamsSortFormRise, ListLeaguePlayersParamsSortFormFall:
		if c := t.contractFormChange(); c != nil {
			return ptr(roundN(float64(*c), 1))
		}
		return nil
	case ListLeaguePlayersParamsSortPointsPerMatch:
		if t.appearances == 0 {
			return nil
		}
		return ptr(roundN(float64(t.points)/float64(t.appearances), 1))
	case ListLeaguePlayersParamsSortDraftRank:
		return intVal(draftRank)
	case ListLeaguePlayersParamsSortGoals:
		return intVal(t.goals)
	case ListLeaguePlayersParamsSortAssists:
		return intVal(t.assists)
	case ListLeaguePlayersParamsSortBonus:
		return intVal(t.bonus)
	case ListLeaguePlayersParamsSortCleanSheets:
		return intVal(t.cleanSheets)
	case ListLeaguePlayersParamsSortGoalsConceded:
		return intVal(t.goalsConceded)
	case ListLeaguePlayersParamsSortMinutes:
		return intVal(t.minutes)
	case ListLeaguePlayersParamsSortOwnGoals:
		return intVal(t.ownGoals)
	case ListLeaguePlayersParamsSortPenaltiesMissed:
		return intVal(t.pensMissed)
	case ListLeaguePlayersParamsSortPenaltiesSaved:
		return intVal(t.pensSaved)
	case ListLeaguePlayersParamsSortRedCards:
		return intVal(t.reds)
	case ListLeaguePlayersParamsSortYellowCards:
		return intVal(t.yellows)
	case ListLeaguePlayersParamsSortSaves:
		return intVal(t.saves)
	case ListLeaguePlayersParamsSortBps:
		return lineInt(l, l.bps)
	case ListLeaguePlayersParamsSortClearancesBlocksInterceptions:
		return lineInt(l, l.cbi)
	case ListLeaguePlayersParamsSortRecoveries:
		return lineInt(l, l.recoveries)
	case ListLeaguePlayersParamsSortTackles:
		return lineInt(l, l.tackles)
	case ListLeaguePlayersParamsSortDefensiveContribution:
		return lineInt(l, l.defCont)
	case ListLeaguePlayersParamsSortInfluence:
		return round2(l.influence)
	case ListLeaguePlayersParamsSortCreativity:
		return round2(l.creativity)
	case ListLeaguePlayersParamsSortThreat:
		return round2(l.threat)
	case ListLeaguePlayersParamsSortIctIndex:
		return round2(l.ict)
	case ListLeaguePlayersParamsSortExpectedGoals:
		return round2(l.xg)
	case ListLeaguePlayersParamsSortExpectedAssists:
		return round2(l.xa)
	case ListLeaguePlayersParamsSortExpectedGoalInvolvements:
		return round2(l.xgi)
	case ListLeaguePlayersParamsSortExpectedGoalsConceded:
		return round2(l.xgc)
	case ListLeaguePlayersParamsSortStarts:
		if l.starts == nil {
			return nil
		}
		return intVal(*l.starts)
	}
	return nil // name

}

func contractSortValue(sort ListLeaguePlayersParamsSort, v *float64) *float32 {
	if v == nil || sort == ListLeaguePlayersParamsSortName {
		return nil
	}
	return ptr(float32(*v))
}

// sortPlayers orders the list as the contract's sort and order say: nulls
// last either way, ties by total points (most first), then name, then id.
func sortPlayers(items []PlayerListItem, sort ListLeaguePlayersParamsSort, order ListLeaguePlayersParamsOrder, vals map[string]*float64) {
	// desc is biggest first, except where the natural order is smallest
	// first: draft rank 1 first, A to Z, the biggest fall first.
	smallFirst := sort == ListLeaguePlayersParamsSortDraftRank || sort == ListLeaguePlayersParamsSortName ||
		sort == ListLeaguePlayersParamsSortFormFall
	if order == Asc {
		smallFirst = !smallFirst
	}
	name := func(p PlayerListItem) string { return strings.ToLower(p.Player.DisplayName) }
	slices.SortFunc(items, func(a, b PlayerListItem) int {
		var c int
		if sort == ListLeaguePlayersParamsSortName {
			c = cmp.Compare(name(a), name(b))
		} else {
			va, vb := vals[a.Player.Id], vals[b.Player.Id]
			switch {
			case va == nil && vb == nil:
			case va == nil:
				return 1
			case vb == nil:
				return -1
			default:
				c = cmp.Compare(*va, *vb)
			}
		}
		if !smallFirst {
			c = -c
		}
		return cmp.Or(c, cmp.Compare(b.TotalPoints, a.TotalPoints), cmp.Compare(name(a), name(b)), cmp.Compare(a.Player.Id, b.Player.Id))
	})
}
