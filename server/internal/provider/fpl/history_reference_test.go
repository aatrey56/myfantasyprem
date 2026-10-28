package fpl

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// TestHistoryPastMatchesReferenceCSV compares decodeHistoryPast over the
// owner's local export of history_past (raw/{code}.json) with the flattened
// reference CSV from the same fetch, for every player-season either covers.
// A tracked stat must equal the CSV; an untracked (nil) one must be 0 in
// the CSV, which is what proves the first-tracked seasons in history.go.
// It reads local files only and is skipped unless FPL_HISTORY_REF_DIR
// points at the export (e.g. ~/Coding/Football/data/fpl_history_past).
func TestHistoryPastMatchesReferenceCSV(t *testing.T) {
	dir := os.Getenv("FPL_HISTORY_REF_DIR")
	if dir == "" {
		t.Skip("FPL_HISTORY_REF_DIR not set")
	}
	f, err := os.Open(filepath.Join(dir, "history_past.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	recs, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, h := range recs[0] {
		col[h] = i
	}
	csvRows := map[string]map[string][]string{} // code -> season -> row
	for _, r := range recs[1:] {
		code, season := r[col["code"]], r[col["season_name"]]
		if csvRows[code] == nil {
			csvRows[code] = map[string][]string{}
		}
		csvRows[code][season] = r
	}

	raws, err := filepath.Glob(filepath.Join(dir, "raw", "*.json"))
	if err != nil || len(raws) == 0 {
		t.Fatalf("no raw files: %v", err)
	}
	var compared, mismatches, nulled int
	report := func(format string, a ...any) {
		mismatches++
		if mismatches <= 50 {
			t.Errorf(format, a...)
		}
	}
	for _, p := range raws {
		code := filepath.Base(p[:len(p)-len(".json")])
		hp, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		var list []json.RawMessage
		if err := json.Unmarshal(hp, &list); err != nil {
			t.Fatal(err)
		}
		body := fmt.Appendf(nil, `{"history_past":%s}`, hp)
		seasons, err := decodeHistoryPast(body, code)
		if err != nil {
			report("%s: %v", code, err)
			continue
		}
		seen := map[string]bool{}
		for _, s := range seasons {
			seen[s.Season] = true
			row, ok := csvRows[code][s.Season]
			if !ok {
				report("%s %s: in history_past, not in CSV", code, s.Season)
				continue
			}
			compared++
			for name, v := range seasonFields(s) {
				want := row[col[name]]
				w, err := strconv.ParseFloat(want, 64)
				if err != nil {
					report("%s %s %s: CSV %q", code, s.Season, name, want)
					continue
				}
				if v == nil {
					nulled++
					if w != 0 {
						report("%s %s %s: nil (untracked) but CSV has %v", code, s.Season, name, w)
					}
				} else if *v != w {
					report("%s %s %s: %v, CSV %v", code, s.Season, name, *v, w)
				}
			}
		}
		for season := range csvRows[code] {
			if !seen[season] {
				report("%s %s: in CSV, not in history_past", code, season)
			}
		}
	}
	for code := range csvRows {
		if _, err := os.Stat(filepath.Join(dir, "raw", code+".json")); err != nil {
			report("%s: in CSV, no raw file", code)
		}
	}
	t.Logf("%d players, %d player-seasons compared, %d untracked values nulled, %d mismatches",
		len(raws), compared, nulled, mismatches)
}

func seasonFields(s provider.PlayerSeason) map[string]*float64 {
	i := func(p *int) *float64 {
		if p == nil {
			return nil
		}
		f := float64(*p)
		return &f
	}
	return map[string]*float64{
		"total_points": i(s.TotalPoints), "minutes": i(s.Minutes), "goals_scored": i(s.GoalsScored),
		"assists": i(s.Assists), "clean_sheets": i(s.CleanSheets), "goals_conceded": i(s.GoalsConceded),
		"own_goals": i(s.OwnGoals), "penalties_saved": i(s.PenaltiesSaved), "penalties_missed": i(s.PenaltiesMissed),
		"yellow_cards": i(s.YellowCards), "red_cards": i(s.RedCards), "saves": i(s.Saves), "bonus": i(s.Bonus),
		"bps": i(s.BPS), "start_cost": i(s.StartCost), "end_cost": i(s.EndCost),
		"influence": s.Influence, "creativity": s.Creativity, "threat": s.Threat, "ict_index": s.ICTIndex,
		"starts": i(s.Starts), "expected_goals": s.ExpectedGoals, "expected_assists": s.ExpectedAssists,
		"expected_goal_involvements": s.ExpectedGoalInvolvements, "expected_goals_conceded": s.ExpectedGoalsConceded,
		"clearances_blocks_interceptions": i(s.ClearancesBlocksInterceptions), "recoveries": i(s.Recoveries),
		"tackles": i(s.Tackles), "defensive_contribution": i(s.DefensiveContribution),
	}
}
