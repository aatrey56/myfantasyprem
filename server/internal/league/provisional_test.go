package league

import (
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Owner decision 2026-10-25: a gameweek counts in the standings as soon as
// every fixture has finished, with provisional bonus; bonus_added and
// data_checked are not waited for.
//
//	gw  A v B     fixtures                    counts
//	1   50 v 40   finished (event finished)    yes
//	2   30 v 45   all finished_provisional     yes (no bonus_added yet)
//	3   90 v 10   one still live               no
//
//	A: W L = 1W 0D 1L = 3 pts, PF 80, PA 85
//	B: L W = 1W 0D 1L = 3 pts, PF 85, PA 80  -> B first on points for
//
// Before gameweek 2's last match ends (a fixture still live), only
// gameweek 1 counts: A 3 pts PF 50 PA 40, B 0 pts PF 40 PA 50.
func TestStandingsCountFinishedProvisionalGameweek(t *testing.T) {
	l := &League{Teams: []Team{{ID: "A"}, {ID: "B"}}}
	g := &Game{Matchups: []Matchup{
		{Gameweek: 1, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 50, AwayPoints: 40},
		{Gameweek: 2, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 30, AwayPoints: 45},
		{Gameweek: 3, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 90, AwayPoints: 10},
	}}
	world := func(gw2Last string) *World {
		w := &World{Gameweeks: map[int]stats.Gameweek{
			1: {Number: 1, Finished: true, BonusAdded: true, DataChecked: true},
			2: {Number: 2}, 3: {Number: 3},
		}}
		w.Fixtures = []stats.Fixture{
			{ID: "f1", Gameweek: 1, Status: stats.FixtureFinished},
			{ID: "f2a", Gameweek: 2, Status: stats.FixtureFinishedProvisional},
			{ID: "f2b", Gameweek: 2, Status: gw2Last},
			{ID: "f3a", Gameweek: 3, Status: stats.FixtureFinishedProvisional},
			{ID: "f3b", Gameweek: 3, Status: stats.FixtureLive},
		}
		w.IndexFixtures()
		return w
	}
	type row struct {
		id                 string
		rank, w, d, l, pld int
		pf, pa, pts        int
	}
	check := func(name string, w *World, want []row) {
		t.Helper()
		rows := Standings(l, g, w)
		for i, wr := range want {
			r := rows[i]
			got := row{r.TeamID, r.Rank, r.Wins, r.Draws, r.Losses, r.Played, r.PointsFor, r.PointsAgainst, r.LeaguePoints()}
			if got != wr {
				t.Errorf("%s: row %d = %+v, want %+v", name, i, got, wr)
			}
		}
	}
	check("gameweek 2 still live", world(stats.FixtureLive), []row{
		{"A", 1, 1, 0, 0, 1, 50, 40, 3},
		{"B", 2, 0, 0, 1, 1, 40, 50, 0},
	})
	check("gameweek 2 all finished_provisional", world(stats.FixtureFinishedProvisional), []row{
		{"B", 1, 1, 0, 1, 2, 85, 80, 3},
		{"A", 2, 1, 0, 1, 2, 80, 85, 3},
	})
}

// A gameweek whose matches are all over, bonus provisional: the result
// counts at once. A later bonus change that flips the result reaches the
// standings through the normal rescore, and is reported as a correction.
func TestProvisionalBonusCorrectionFlipsStandings(t *testing.T) {
	scoringImpls(t, func(t *testing.T, repo Repo, statsRepo stats.Repo) {
		l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup {
			return []Matchup{{ID: ids.New("mu"), Gameweek: 1, HomeTeamID: a, AwayTeamID: b, State: scoring.StateUpcoming}}
		})
		a, b := l.Teams[0].ID, l.Teams[1].ID
		// Turn gameweek 1 back to "all matches over, bonus not added".
		gws, _ := statsRepo.Gameweeks(ctx)
		var gw1 []stats.Gameweek
		for _, g := range gws {
			if g.Number == 1 {
				g.Finished, g.BonusAdded, g.DataChecked = false, false, false
				gw1 = append(gw1, g)
			}
		}
		fixtures, _ := statsRepo.Fixtures(ctx)
		var fs []stats.Fixture
		for _, f := range fixtures {
			if f.Gameweek == 1 {
				f.Status = stats.FixtureFinishedProvisional
				fs = append(fs, f)
			}
		}
		if err := statsRepo.Apply(ctx, stats.Batch{Gameweeks: gw1, Fixtures: fs}); err != nil {
			t.Fatal(err)
		}
		n := &notes{}
		sc := &Scorer{Leagues: repo, Stats: statsRepo, Rules: scoring.StandardLineup(), Notify: n}
		table := func() (map[string]StandingRow, scoring.ScoreState) {
			t.Helper()
			if _, err := sc.Recompute(ctx, 1, nil, true); err != nil {
				t.Fatal(err)
			}
			g, err := repo.Game(ctx, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			w, err := LoadWorld(ctx, statsRepo, 1)
			if err != nil {
				t.Fatal(err)
			}
			out := map[string]StandingRow{}
			for _, r := range Standings(l, g, w) {
				out[r.TeamID] = r
			}
			return out, g.MatchupsIn(1)[0].State
		}
		// Same stats as the golden test: A 46, B 35 (TestGoldenTeamScores).
		rows, state := table()
		if state != scoring.StateProvisional {
			t.Fatalf("state = %s, want provisional", state)
		}
		if r := rows[a]; r.Wins != 1 || r.Played != 1 || r.PointsFor != 46 || r.PointsAgainst != 35 || r.Rank != 1 {
			t.Fatalf("A before correction = %+v", r)
		}
		if r := rows[b]; r.Losses != 1 || r.PointsFor != 35 || r.Rank != 2 {
			t.Fatalf("B before correction = %+v", r)
		}

		// Bonus moves: four of B's starters who played gain 3 bonus each.
		// B 35 + 4*3 = 47 > A 46, so B now wins.
		live, err := statsRepo.PlayerFixtures(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		played := map[string]bool{}
		for _, r := range live {
			if r.Stats.Minutes > 0 {
				played[r.PlayerID] = true
			}
		}
		var bumped []string
		for _, num := range lineupB[:11] {
			if len(bumped) < 4 && played[pid[num]] {
				bumped = append(bumped, pid[num])
			}
		}
		if len(bumped) != 4 {
			t.Fatalf("only %d of B's starters played", len(bumped))
		}
		for i := range live {
			for _, id := range bumped {
				if live[i].PlayerID == id {
					live[i].Stats.Bonus += 3
					live[i].Points += 3
				}
			}
		}
		if err := statsRepo.Apply(ctx, stats.Batch{Live: &stats.LiveRows{Gameweek: 1, Rows: live}}); err != nil {
			t.Fatal(err)
		}
		n.changes = nil
		res, err := sc.Recompute(ctx, 1, bumped, false)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Corrections) != 1 || res.Corrections[0].TeamID != b || res.Corrections[0].Previous != 35 || res.Corrections[0].Points != 47 {
			t.Fatalf("corrections = %+v", res.Corrections)
		}
		rows, _ = table()
		if r := rows[b]; r.Wins != 1 || r.Losses != 0 || r.PointsFor != 47 || r.PointsAgainst != 46 || r.Rank != 1 {
			t.Errorf("B after correction = %+v", r)
		}
		if r := rows[a]; r.Wins != 0 || r.Losses != 1 || r.PointsFor != 46 || r.PointsAgainst != 47 || r.Rank != 2 {
			t.Errorf("A after correction = %+v", r)
		}
	})
}

// Production, 2026-11: the server was down for all of gameweek 9, so its
// matchups were never scored and stayed upcoming at 0-0 while FPL marked
// the event finished. A matchup that was never scored is not a result:
//
//	gw  A v B   matchup state   counts
//	8   60 v 50 final           yes
//	9    0 v 0  upcoming        no (FPL: finished; we have no stats)
//	10  40 v 70 live            no (not over)
//
//	A: 1W 0D 0L, PF 60, PA 50; B: 0W 0D 1L, PF 50, PA 60. Nobody has a draw.
func TestStandingsSkipUnscoredMatchupOfFinishedGameweek(t *testing.T) {
	l := &League{Teams: []Team{{ID: "A"}, {ID: "B"}}}
	g := &Game{Matchups: []Matchup{
		{Gameweek: 8, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 60, AwayPoints: 50, State: scoring.StateFinal},
		{Gameweek: 9, HomeTeamID: "A", AwayTeamID: "B", State: scoring.StateUpcoming},
		{Gameweek: 10, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 40, AwayPoints: 70, State: scoring.StateLive},
	}}
	w := &World{Gameweeks: map[int]stats.Gameweek{
		8:  {Number: 8, Finished: true, BonusAdded: true, DataChecked: true},
		9:  {Number: 9, Finished: true, BonusAdded: true, DataChecked: true},
		10: {Number: 10, Finished: true, BonusAdded: true, DataChecked: true},
	}}
	w.IndexFixtures()
	rows := Standings(l, g, w)
	type row struct {
		id           string
		w, d, l, pld int
		pf, pa       int
		form         int
	}
	want := []row{{"A", 1, 0, 0, 1, 60, 50, 1}, {"B", 0, 0, 1, 1, 50, 60, 1}}
	for i, wr := range want {
		r := rows[i]
		if got := (row{r.TeamID, r.Wins, r.Draws, r.Losses, r.Played, r.PointsFor, r.PointsAgainst, len(r.Form)}); got != wr {
			t.Errorf("row %d = %+v, want %+v", i, got, wr)
		}
	}
}

// A finished gameweek with no fixture left in it counts as a blank only
// once its live data was read (stats.Gameweek.LiveIngested). Then every
// player scored 0 (RESEARCH.md §7), the matchup is final 0-0 and a draw.
// Unread, its fixtures may simply not be known yet (a past gameweek's come
// only from its live data): it stays upcoming and does not count.
//
//	gw  A v B  fixtures                     read  counts
//	1   50 v 40 finished                    yes   yes: A W, B L
//	2    0 v 0  one, postponed               yes   yes: draw each
//	3    0 v 0  none known                   no    no
//
//	A: 1W 1D 0L = 4 pts, PF 50, PA 40, played 2
//	B: 0W 1D 1L = 1 pt,  PF 40, PA 50, played 2
func TestVerifiedBlankGameweekCountsAsDraw(t *testing.T) {
	l := &League{Teams: []Team{{ID: "A"}, {ID: "B"}}}
	g := &Game{Matchups: []Matchup{
		{Gameweek: 1, HomeTeamID: "A", AwayTeamID: "B", HomePoints: 50, AwayPoints: 40, State: scoring.StateFinal},
		{Gameweek: 2, HomeTeamID: "A", AwayTeamID: "B", State: scoring.StateUpcoming},
		{Gameweek: 3, HomeTeamID: "A", AwayTeamID: "B", State: scoring.StateUpcoming},
	}}
	w := &World{Gameweeks: map[int]stats.Gameweek{
		1: {Number: 1, Finished: true, LiveIngested: true},
		2: {Number: 2, Finished: true, LiveIngested: true},
		3: {Number: 3, Finished: true},
	}}
	w.Fixtures = []stats.Fixture{
		{ID: "f1", Gameweek: 1, Status: stats.FixtureFinished},
		{ID: "f2", Gameweek: 2, Status: stats.FixturePostponed},
	}
	w.IndexFixtures()
	if s := w.State(2); s != scoring.StateFinal {
		t.Errorf("read blank gameweek 2: state %s, want final", s)
	}
	if s := w.State(3); s != scoring.StateUpcoming {
		t.Errorf("unread gameweek 3 with no fixtures known: state %s, want upcoming", s)
	}
	// The rescore stores ScoreGameweek's state; nobody has a roster, so
	// both sides score 0.
	for gw := 2; gw <= 3; gw++ {
		_, ms := ScoreGameweek(scoring.StandardLineup(), l, g, w, gw)
		for _, m := range ms {
			for i := range g.Matchups {
				if g.Matchups[i].Gameweek == gw {
					g.Matchups[i] = m
				}
			}
		}
	}
	want := map[string]StandingRow{
		"A": {Standing: scoring.Standing{TeamID: "A", Wins: 1, Draws: 1, PointsFor: 50, PointsAgainst: 40}, Played: 2},
		"B": {Standing: scoring.Standing{TeamID: "B", Draws: 1, Losses: 1, PointsFor: 40, PointsAgainst: 50}, Played: 2},
	}
	for _, r := range Standings(l, g, w) {
		wr := want[r.TeamID]
		if r.Standing != wr.Standing || r.Played != wr.Played {
			t.Errorf("team %s: %+v played %d, want %+v played %d", r.TeamID, r.Standing, r.Played, wr.Standing, wr.Played)
		}
	}
}
