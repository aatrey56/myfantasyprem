package httpapi

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

func TestGameStatsFromBreakdown(t *testing.T) {
	blank, err := json.Marshal(gameStatsFromBreakdown(nil))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(blank, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 24 {
		t.Fatalf("blank gameweek has %d fields, want all 24", len(fields))
	}
	for k, v := range fields {
		if v != nil {
			t.Errorf("blank gameweek %s = %v, want null", k, v)
		}
	}

	s := gameStatsFromBreakdown([]PlayerFixture{
		{Breakdown: []PointsLine{
			{Stat: "minutes", Value: 90, Points: 2},
			{Stat: "goals_scored", Value: 1, Points: 6},
			{Stat: "yellow_cards", Value: 1, Points: -1},
			{Stat: "defensive_contribution", Value: 10, Points: 2},
		}},
		{Breakdown: []PointsLine{
			{Stat: "goals_scored", Value: 1, Points: 6},
			{Stat: "bonus", Value: 2, Points: 2},
		}},
	})
	for name, got := range map[string]*int{
		"goals": s.GoalsScored, "assists": s.Assists, "yellow": s.YellowCards,
		"red": s.RedCards, "dc": s.DefensiveContributionPoints, "bonus": s.Bonus,
	} {
		want := map[string]int{"goals": 2, "assists": 0, "yellow": 1, "red": 0, "dc": 2, "bonus": 2}[name]
		if got == nil || *got != want {
			t.Errorf("%s = %v, want %d", name, got, want)
		}
	}
	if s.CleanSheets != nil || s.Bps != nil || s.ExpectedGoals != nil || s.Cbit != nil {
		t.Error("stats the breakdown cannot tell should stay null")
	}
}

func TestGameStats(t *testing.T) {
	x := func(v float64) *float64 { return &v }
	line := &stats.PlayerGameweek{
		Minutes: 150, GoalsScored: 1, Assists: 2, CleanSheets: 1, GoalsConceded: 3, OwnGoals: 1, PenaltiesSaved: 1,
		PenaltiesMissed: 1, YellowCards: 1, RedCards: 0, Saves: 4, Bonus: 1, BPS: 31,
		ClearancesBlocksInterceptions: 7, Tackles: 4, Recoveries: 6, DefensiveContribution: 11,
		Influence: x(40.2), Creativity: x(12.5), Threat: x(9), ICTIndex: x(6.2),
		ExpectedGoals: x(0.42), ExpectedAssists: x(0.31), ExpectedGoalInvolvements: x(0.73), ExpectedGoalsConceded: nil,
	}
	played := []PlayerFixture{
		{Status: FixtureStatusFinished, Breakdown: []PointsLine{
			{Stat: "goals_scored", Value: 1, Points: 6}, {Stat: "defensive_contribution", Value: 11, Points: 2},
		}},
		{Status: FixtureStatusLive, Breakdown: []PointsLine{{Stat: "bonus", Value: 2, Points: 2}}},
	}

	// The gameweek line, with bonus and DC points from the breakdown (our
	// bonus, here 2, can differ from upstream's projection, 1).
	s := gameStats(played, line, true)
	got, _ := json.Marshal(s)
	want := `{"goalsScored":1,"assists":2,"cleanSheets":1,"goalsConceded":3,"ownGoals":1,"penaltiesSaved":1,` +
		`"penaltiesMissed":1,"yellowCards":1,"redCards":0,"saves":4,"defensiveContributionPoints":2,"bonus":2,"bps":31,` +
		`"influence":40.2,"creativity":12.5,"threat":9,"ictIndex":6.2,"expectedGoals":0.42,"expectedAssists":0.31,` +
		`"expectedGoalInvolvements":0.73,"expectedGoalsConceded":null,"cbit":11,"recoveries":6,"tackles":4}`
	if !jsonEqual(t, got, want) {
		t.Errorf("stats = %s\nwant    %s", got, want)
	}

	// Nothing kicked off yet: every field null, line or not.
	for _, l := range []*stats.PlayerGameweek{line, nil} {
		blank, _ := json.Marshal(gameStats([]PlayerFixture{{Status: FixtureStatusScheduled}}, l, true))
		var fields map[string]any
		_ = json.Unmarshal(blank, &fields)
		for k, v := range fields {
			if v != nil {
				t.Errorf("unstarted gameweek %s = %v, want null", k, v)
			}
		}
	}

	// Started but no line stored: what the breakdown can tell.
	s = gameStats(played, nil, true)
	if s.GoalsScored == nil || *s.GoalsScored != 1 || s.Bonus == nil || *s.Bonus != 2 || s.Bps != nil || s.Influence != nil {
		t.Errorf("without a line = %+v", s)
	}
}

// FPL's live feed sends ICT and expected stats as 0.0 until it processes
// them: in an unsettled gameweek an all-zero group for a player who played
// is null, while a settled gameweek keeps the zeros.
func TestGameStatsUnpublishedDecimals(t *testing.T) {
	zero := func() *float64 { v := 0.0; return &v }
	line := &stats.PlayerGameweek{
		Minutes: 90, Saves: 3, BPS: 22,
		Influence: zero(), Creativity: zero(), Threat: zero(), ICTIndex: zero(),
		ExpectedGoals: zero(), ExpectedAssists: zero(), ExpectedGoalInvolvements: zero(), ExpectedGoalsConceded: zero(),
	}
	played := []PlayerFixture{{Status: FixtureStatusFinishedProvisional}}

	live := gameStats(played, line, false)
	if live.Influence != nil || live.Creativity != nil || live.Threat != nil || live.IctIndex != nil ||
		live.ExpectedGoals != nil || live.ExpectedAssists != nil || live.ExpectedGoalInvolvements != nil ||
		live.ExpectedGoalsConceded != nil {
		t.Errorf("unsettled all-zero decimals = %+v, want null", live)
	}
	if live.Saves == nil || *live.Saves != 3 || live.Bps == nil || *live.Bps != 22 {
		t.Errorf("counting stats lost: %+v", live)
	}

	settled := gameStats(played, line, true)
	if settled.Influence == nil || *settled.Influence != 0 || settled.ExpectedGoalsConceded == nil {
		t.Errorf("settled zeros = %+v, want 0", settled)
	}

	// Published values (decimal strings upstream) pass through while live.
	v := 23.4
	line.Influence = &v
	live = gameStats(played, line, false)
	if live.Influence == nil || *live.Influence != float32(23.4) || live.IctIndex == nil || live.ExpectedGoals != nil {
		t.Errorf("published ICT while live = %+v", live)
	}

	// A player who did not play keeps his zeros.
	bench := &stats.PlayerGameweek{Influence: zero(), ICTIndex: zero()}
	if s := gameStats(played, bench, false); s.Influence == nil || s.IctIndex == nil {
		t.Errorf("unused player ICT = %+v, want 0", s)
	}
}

func TestRemainingFixtures(t *testing.T) {
	at := func(d int) *time.Time {
		v := time.Date(2026, 10, d, 15, 0, 0, 0, time.FixedZone("BST", 3600))
		return &v
	}
	w := &league.World{
		Clubs: map[string]stats.Club{"new": {Code: "NEW", Name: "Newcastle"}, "ars": {Code: "ARS", Name: "Arsenal"},
			"che": {Code: "CHE", Name: "Chelsea"}},
		Fixtures: []stats.Fixture{
			{ID: "fx_1", Gameweek: 7, HomeClubID: "new", AwayClubID: "ars", KickoffAt: at(4), Status: stats.FixtureFinished},
			{ID: "fx_2", Gameweek: 7, HomeClubID: "che", AwayClubID: "new", KickoffAt: at(5), Status: stats.FixtureLive},
			{ID: "fx_3", Gameweek: 8, HomeClubID: "ars", AwayClubID: "che", KickoffAt: at(18), Status: stats.FixtureScheduled},
			{ID: "fx_4", Gameweek: 8, HomeClubID: "ars", AwayClubID: "new", KickoffAt: at(18), Status: stats.FixtureScheduled},
			{ID: "fx_5", Gameweek: 8, HomeClubID: "new", AwayClubID: "che", KickoffAt: at(21), Status: stats.FixtureScheduled},
			{ID: "fx_6", Gameweek: 9, HomeClubID: "che", AwayClubID: "new", KickoffAt: at(25), Status: stats.FixtureScheduled},
			{ID: "fx_7", Gameweek: 10, HomeClubID: "new", AwayClubID: "che", KickoffAt: at(28), Status: stats.FixtureScheduled},
			{ID: "fx_8", Gameweek: 11, HomeClubID: "new", AwayClubID: "ars", KickoffAt: at(30), Status: stats.FixtureScheduled},
			{ID: "fx_9", Gameweek: 0, HomeClubID: "che", AwayClubID: "new", Status: stats.FixturePostponed},
		},
	}
	got := remainingFixtures(w, "new")
	var ids []string
	for _, f := range got {
		ids = append(ids, f.FixtureId)
	}
	if want := "[fx_4 fx_5 fx_6 fx_7 fx_8 fx_9]"; fmt.Sprint(ids) != want {
		t.Fatalf("remaining = %v, want %s", ids, want)
	}
	if f := got[0]; f.Gameweek == nil || *f.Gameweek != 8 || f.IsHome || f.Opponent.Code != "ARS" ||
		f.KickoffAt == nil || f.KickoffAt.Location() != time.UTC {
		t.Errorf("first = %+v", f)
	}
	tbc := got[len(got)-1]
	if tbc.Gameweek != nil || tbc.KickoffAt != nil || tbc.Status == nil || *tbc.Status != FixtureStatusPostponed {
		t.Errorf("TBC fixture = %+v", tbc)
	}
	b, _ := json.Marshal(tbc)
	if !jsonEqual(t, b, `{"fixtureId":"fx_9","gameweek":null,"isHome":false,"kickoffAt":null,`+
		`"opponent":{"code":"CHE","name":"Chelsea"},"status":"postponed"}`) {
		t.Errorf("TBC json = %s", b)
	}
	// Kickoff order whatever order the world holds them in (TBC still last).
	slices.Reverse(w.Fixtures)
	var again []string
	for _, f := range remainingFixtures(w, "new") {
		again = append(again, f.FixtureId)
	}
	if fmt.Sprint(again) != fmt.Sprint(ids) {
		t.Errorf("reversed world = %v, want %v", again, ids)
	}
}

func jsonEqual(t *testing.T, a []byte, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatal(err)
	}
	ax, _ := json.Marshal(x)
	by, _ := json.Marshal(y)
	return string(ax) == string(by)
}
