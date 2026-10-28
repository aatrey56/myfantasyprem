package httpapi

import (
	"net/url"
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

func sortItem(id, name string, total int) PlayerListItem {
	return PlayerListItem{Player: PlayerRef{Id: id, DisplayName: name}, TotalPoints: total}
}

// Hand-worked: xGI A 2.2 (10 pts), B 1.5 (12), C 2.2 (11), D untracked
// (30), E 2.2 (11, "Able"). desc: the 2.2s by points, C and E tied on 11
// go by name (Able before Charlie) -> E, C, A, then B, then D (null last).
// asc: B, then the 2.2s still by points then name -> E, C, A, then D.
func TestSortPlayersNullsLastTies(t *testing.T) {
	vals := map[string]*float64{"plr_a": ptr(2.2), "plr_b": ptr(1.5), "plr_c": ptr(2.2), "plr_d": nil, "plr_e": ptr(2.2)}
	mk := func() []PlayerListItem {
		return []PlayerListItem{
			sortItem("plr_d", "Delta", 30), sortItem("plr_a", "Alpha", 10), sortItem("plr_b", "Bravo", 12),
			sortItem("plr_c", "Charlie", 11), sortItem("plr_e", "Able", 11),
		}
	}
	ids := func(items []PlayerListItem) []string {
		out := []string{}
		for _, it := range items {
			out = append(out, it.Player.Id)
		}
		return out
	}
	for _, c := range []struct {
		order ListLeaguePlayersParamsOrder
		want  []string
	}{
		{Desc, []string{"plr_e", "plr_c", "plr_a", "plr_b", "plr_d"}},
		{Asc, []string{"plr_b", "plr_e", "plr_c", "plr_a", "plr_d"}},
	} {
		items := mk()
		sortPlayers(items, ListLeaguePlayersParamsSortExpectedGoalInvolvements, c.order, vals)
		if got := ids(items); !slices.Equal(got, c.want) {
			t.Errorf("%s: %v, want %v", c.order, got, c.want)
		}
	}

	// Draft rank: desc is the natural order (rank 1 first), asc reverses.
	ranks := map[string]*float64{"plr_a": ptr(3.0), "plr_b": ptr(1.0), "plr_c": ptr(2.0)}
	items := mk()[1:4]
	sortPlayers(items, ListLeaguePlayersParamsSortDraftRank, Desc, ranks)
	if got := ids(items); !slices.Equal(got, []string{"plr_b", "plr_c", "plr_a"}) {
		t.Errorf("draft_rank desc: %v", got)
	}
	sortPlayers(items, ListLeaguePlayersParamsSortDraftRank, Asc, ranks)
	if got := ids(items); !slices.Equal(got, []string{"plr_a", "plr_c", "plr_b"}) {
		t.Errorf("draft_rank asc: %v", got)
	}
	// Name: desc is A to Z.
	items = mk()
	sortPlayers(items, ListLeaguePlayersParamsSortName, Desc, nil)
	if got := ids(items); !slices.Equal(got, []string{"plr_e", "plr_a", "plr_b", "plr_c", "plr_d"}) {
		t.Errorf("name desc: %v", got)
	}
}

func TestSortValueRounding(t *testing.T) {
	val := func(v *float64) any {
		if v == nil {
			return nil
		}
		return *v
	}
	t23 := playerTotals{points: 23, appearances: 4, goals: 3, goalsConceded: 7, bonus: 5}
	lines := lineTotals{has: true, bps: 0, xgi: ptr(0.333 + 0.5), ict: ptr(12.345)}
	for _, c := range []struct {
		sort ListLeaguePlayersParamsSort
		t    playerTotals
		l    lineTotals
		want any
	}{
		{ListLeaguePlayersParamsSortPointsPerMatch, t23, lines, 5.8}, // 23 / 4 = 5.75 -> 5.8
		{ListLeaguePlayersParamsSortPointsPerMatch, playerTotals{points: -1}, lines, nil},
		{ListLeaguePlayersParamsSortGoals, t23, lines, 3.0},
		{ListLeaguePlayersParamsSortGoalsConceded, t23, lines, 7.0},
		{ListLeaguePlayersParamsSortBonus, t23, lines, 5.0},
		{ListLeaguePlayersParamsSortExpectedGoalInvolvements, t23, lines, 0.83}, // 0.833
		{ListLeaguePlayersParamsSortIctIndex, t23, lines, 12.35},                // 12.345 -> 12.35 (half away)
		{ListLeaguePlayersParamsSortExpectedGoals, t23, lines, nil},             // never published
		{ListLeaguePlayersParamsSortBps, t23, lines, 0.0},                       // has lines: a real 0
		{ListLeaguePlayersParamsSortBps, t23, lineTotals{}, nil},                // no line at all
		{ListLeaguePlayersParamsSortStarts, t23, lines, nil},                    // no line sent starts
		{ListLeaguePlayersParamsSortStarts, t23, lineTotals{has: true, starts: new(int)}, 0.0},
		{ListLeaguePlayersParamsSortDraftRank, t23, lines, 9.0},
		{ListLeaguePlayersParamsSortGameweekPoints, t23, lines, 6.0},
	} {
		if got := val(sortValue(c.sort, c.t, c.l, 6, 9)); got != c.want {
			t.Errorf("%s = %v, want %v", c.sort, got, c.want)
		}
	}
}

// Every sort pages consistently, keeps sortValue in order with nulls last,
// and the season counts match the player's own game log.
func TestPlayersSortEndpoint(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		base := "/v1/leagues/" + f.league + "/players?status=all"
		for _, bad := range []string{"&sort=nope", "&order=up"} {
			if rec := f.get(t, f.token, base+bad, nil); rec.Code != 400 {
				t.Errorf("%s = %d, want 400", bad, rec.Code)
			}
		}
		for _, sort := range []string{"goals", "expected_goal_involvements", "goals_conceded", "points_per_match", "bps", "minutes"} {
			for _, order := range []string{"desc", "asc"} {
				q := base + "&sort=" + sort + "&order=" + order
				var all, p1, p2 PlayerListResponse
				f.get(t, f.token, q+"&limit=100", &all)
				f.get(t, f.token, q+"&limit=7", &p1)
				if p1.Meta.NextCursor == nil {
					t.Fatalf("%s: no second page", q)
				}
				f.get(t, f.token, q+"&limit=7&cursor="+url.QueryEscape(*p1.Meta.NextCursor), &p2)
				paged := append(listIDs(p1), listIDs(p2)...)
				if !slices.Equal(paged, listIDs(all)[:14]) {
					t.Errorf("%s: pages %v, one page %v", q, paged, listIDs(all)[:14])
				}
				if all.Data[0].SortValue == nil {
					t.Errorf("%s: the first row has no value", q)
				}
				seenNull := false
				for i, it := range all.Data {
					if it.SortValue == nil {
						seenNull = true
						continue
					}
					if seenNull {
						t.Fatalf("%s: %s has a value after a null", q, it.Player.Id)
					}
					if i == 0 || all.Data[i-1].SortValue == nil {
						continue
					}
					prev, cur := *all.Data[i-1].SortValue, *it.SortValue
					if (order == "desc" && prev < cur) || (order == "asc" && prev > cur) {
						t.Fatalf("%s out of order at %d: %v then %v", q, i, prev, cur)
					}
					if prev == cur && all.Data[i-1].TotalPoints < it.TotalPoints {
						t.Fatalf("%s: tie at %d not by total points", q, i)
					}
				}
			}
		}
		// The top scorer's goals and goals conceded are his game log's sums.
		var top PlayerListResponse
		f.get(t, f.token, base+"&sort=goals&limit=1", &top)
		it := top.Data[0]
		var detail PlayerDetailResponse
		f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+it.Player.Id, &detail)
		goals, conceded := 0, 0
		for _, g := range detail.Data.Gameweeks {
			if g.Stats.GoalsScored != nil {
				goals += *g.Stats.GoalsScored
			}
			if g.Stats.GoalsConceded != nil {
				conceded += *g.Stats.GoalsConceded
			}
		}
		if it.SortValue == nil || *it.SortValue != float32(goals) || goals == 0 {
			t.Errorf("goals sortValue %v, game log %d", it.SortValue, goals)
		}
		var gc PlayerListResponse
		f.get(t, f.token, base+"&sort=goals_conceded&q="+url.QueryEscape(it.Player.DisplayName), &gc)
		for _, x := range gc.Data {
			if x.Player.Id == it.Player.Id && (x.SortValue == nil || *x.SortValue != float32(conceded)) {
				t.Errorf("goals_conceded sortValue %v, game log %d", x.SortValue, conceded)
			}
		}
		// The demo's lines carry starts (demo.FS: 60 minutes or more in a
		// fixture), so the sort and the card both have a value.
		var st PlayerListResponse
		f.get(t, f.token, base+"&sort=starts&limit=1", &st)
		if st.Data[0].SortValue == nil || *st.Data[0].SortValue < 1 || detail.Data.Starts == nil {
			t.Errorf("starts from the demo: sortValue %v, detail %v", st.Data[0].SortValue, detail.Data.Starts)
		}
		// name sorts carry no value.
		var byName PlayerListResponse
		f.get(t, f.token, base+"&sort=name&limit=1", &byName)
		if byName.Data[0].SortValue != nil {
			t.Errorf("name sortValue = %v, want null", *byName.Data[0].SortValue)
		}
	})
}

// Hand-worked starts over two gameweeks (nil: a line FPL sent without
// starts): A 1 + 2 (a double) = 3, B 1 + nil = 1, C nil + nil = null,
// D 0 + 0 = 0, E 1 + 1 = 2. desc: A, E, B, D, then C (null last); asc:
// D, B, E, A, then C.
func TestSortByStarts(t *testing.T) {
	n := func(v int) *int { return &v }
	line := func(id string, gw int, starts *int) stats.PlayerGameweek {
		return stats.PlayerGameweek{PlayerID: id, Gameweek: gw, Minutes: 90, Starts: starts}
	}
	sums := sumLines([]stats.PlayerGameweek{
		line("plr_a", 1, n(1)), line("plr_a", 2, n(2)),
		line("plr_b", 1, n(1)), line("plr_b", 2, nil),
		line("plr_c", 1, nil), line("plr_c", 2, nil),
		line("plr_d", 1, n(0)), line("plr_d", 2, n(0)),
		line("plr_e", 1, n(1)), line("plr_e", 2, n(1)),
	})
	vals := map[string]*float64{}
	for _, id := range []string{"plr_a", "plr_b", "plr_c", "plr_d", "plr_e"} {
		vals[id] = sortValue(ListLeaguePlayersParamsSortStarts, playerTotals{}, sums[id], 0, 0)
	}
	if vals["plr_c"] != nil || vals["plr_d"] == nil || *vals["plr_d"] != 0 || *vals["plr_a"] != 3 || *vals["plr_b"] != 1 {
		t.Fatalf("starts: a %v b %v c %v d %v", vals["plr_a"], vals["plr_b"], vals["plr_c"], vals["plr_d"])
	}
	for _, c := range []struct {
		order ListLeaguePlayersParamsOrder
		want  []string
	}{
		{Desc, []string{"plr_a", "plr_e", "plr_b", "plr_d", "plr_c"}},
		{Asc, []string{"plr_d", "plr_b", "plr_e", "plr_a", "plr_c"}},
	} {
		items := []PlayerListItem{sortItem("plr_c", "C", 50), sortItem("plr_b", "B", 1), sortItem("plr_e", "E", 2),
			sortItem("plr_d", "D", 3), sortItem("plr_a", "A", 4)}
		sortPlayers(items, ListLeaguePlayersParamsSortStarts, c.order, vals)
		got := []string{}
		for _, it := range items {
			got = append(got, it.Player.Id)
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("starts %s: %v, want %v", c.order, got, c.want)
		}
	}
}

// A double gameweek's fixture rows come from FPL's explain breakdown,
// which leaves out stats that earn nothing; the gameweek line has the full
// counts. Hand-worked: a midfielder's GW1 is a double, rows 90' + 90'
// with goals conceded 0 + 0 (MID: no points for conceding, so explain has
// none) and saves 0; the line says 3 conceded, 1 save, 1 clean sheet, 180'.
// A forward's clean sheet earns nothing either. GW2 is a single fixture
// with 2 conceded in both the row and the line. Season: MID conceded
// 3 + 2 = 5, saves 1, minutes 180 + 90 = 270; FWD clean sheets 1.
func TestLineCountsInDoubleGameweeks(t *testing.T) {
	row := func(id string, gw int, fid string, s scoring.FixtureStats) stats.PlayerFixture {
		return stats.PlayerFixture{PlayerID: id, FixtureID: fid, Gameweek: gw, Stats: s}
	}
	w := &league.World{Players: map[string]stats.Player{"plr_m": {}, "plr_f": {}}, Rows: map[int]map[string][]stats.PlayerFixture{
		1: {
			"plr_m": {row("plr_m", 1, "fx_1", scoring.FixtureStats{Minutes: 90}), row("plr_m", 1, "fx_2", scoring.FixtureStats{Minutes: 90})},
			"plr_f": {row("plr_f", 1, "fx_1", scoring.FixtureStats{Minutes: 90}), row("plr_f", 1, "fx_2", scoring.FixtureStats{Minutes: 90})},
		},
		2: {"plr_m": {row("plr_m", 2, "fx_3", scoring.FixtureStats{Minutes: 90, GoalsConceded: 2})}},
	}}
	lines := []stats.PlayerGameweek{
		{PlayerID: "plr_m", Gameweek: 1, Minutes: 180, GoalsConceded: 3, Saves: 1, CleanSheets: 1},
		{PlayerID: "plr_f", Gameweek: 1, Minutes: 180, CleanSheets: 1},
		{PlayerID: "plr_m", Gameweek: 2, Minutes: 90, GoalsConceded: 2},
	}
	totals := withLineCounts(w, seasonTotals(w), lines)
	m, f := totals["plr_m"], totals["plr_f"]
	if m.goalsConceded != 5 || m.saves != 1 || m.minutes != 270 || m.cleanSheets != 1 {
		t.Errorf("MID: conceded %d saves %d minutes %d clean sheets %d, want 5 1 270 1", m.goalsConceded, m.saves, m.minutes, m.cleanSheets)
	}
	if f.cleanSheets != 1 {
		t.Errorf("FWD clean sheets %d, want 1", f.cleanSheets)
	}
	if got := sortValue(ListLeaguePlayersParamsSortGoalsConceded, m, lineTotals{}, 0, 0); got == nil || *got != 5 {
		t.Errorf("goals_conceded sortValue %v, want 5", got)
	}
}
