package httpapi

import (
	"testing"
)

// The demo's data/element-summary/113.json (code 9000113) has thirteen
// seasons, 2013/14 .. 2025/26, sent the way FPL sends them: 0 where a stat
// was not tracked yet.
func TestPlayerDetailPreviousSeasons(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		var out PlayerDetailResponse
		rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+demoPlayer(t, f.srv, "9000113"), &out)
		if rec.Code != 200 {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		ps := out.Data.PreviousSeasons
		if len(ps) != 13 || ps[0].Season != "2025/26" || ps[12].Season != "2013/14" {
			t.Fatalf("%d seasons, %+v", len(ps), ps)
		}
		by := map[string]PlayerSeasonTotals{}
		for _, s := range ps {
			by[s.Season] = s
		}
		// 2025/26 row: 179 pts, 1720 min, 0 goals, 2 assists, 20 starts,
		// xG "7.22", DC 75; all tracked.
		s := by["2025/26"]
		if *s.Points != 179 || *s.Minutes != 1720 || *s.GoalsScored != 0 || *s.Assists != 2 || *s.Starts != 20 ||
			*s.ExpectedGoals != float32(7.22) || *s.DefensiveContribution != 75 {
			t.Errorf("2025/26 = %+v", s)
		}
		// 2021/22: ICT 258.9 tracked; starts, xG, xA, xGI and DC were 0 = untracked.
		s = by["2021/22"]
		if s.IctIndex == nil || *s.IctIndex != float32(258.9) {
			t.Errorf("2021/22 ict = %v", s.IctIndex)
		}
		if s.Starts != nil || s.ExpectedGoals != nil || s.ExpectedAssists != nil || s.ExpectedGoalInvolvements != nil || s.DefensiveContribution != nil {
			t.Errorf("2021/22 untracked stats = %+v", s)
		}
		// 2015/16: ICT untracked too; 6 goals, 7 assists real.
		s = by["2015/16"]
		if s.IctIndex != nil || *s.GoalsScored != 6 || *s.Assists != 7 {
			t.Errorf("2015/16 = %+v", s)
		}
		// 2024/25: DC 230 (FPL back-filled it).
		if s := by["2024/25"]; s.DefensiveContribution == nil || *s.DefensiveContribution != 230 {
			t.Errorf("2024/25 dc = %v", s.DefensiveContribution)
		}

		// 9000068 was fetched and has none; a player never fetched has none.
		for _, code := range []string{"9000068", "9000001"} {
			var o PlayerDetailResponse
			f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+demoPlayer(t, f.srv, code), &o)
			if o.Data.PreviousSeasons == nil || len(o.Data.PreviousSeasons) != 0 {
				t.Errorf("%s: %+v, want []", code, o.Data.PreviousSeasons)
			}
		}
	})
}
