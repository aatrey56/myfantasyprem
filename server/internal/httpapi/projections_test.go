package httpapi

import (
	"math"
	"slices"
	"testing"
)

// Projections (ADR 0014) against the demo world (gameweeks 1-6 final, 7
// live, 8 with a double and a blank), on memory and Postgres. The rules
// themselves are hand-worked in package projection; these check that the
// handlers feed them the right data and keep the contract's promises,
// with each number rebuilt from another endpoint and the ADR's formulas.

// factorOf is ADR 0014's fixture factor, written out again.
func factorOf(rank *int) float64 {
	if rank == nil {
		return 1
	}
	return 0.8 + 0.4*float64(*rank-1)/19
}

func TestFixtureDifficultyOnTheDemoWorld(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		f.clock.t = beforeGW8
		var res FixtureDifficultyResponse
		if rec := f.get(t, f.token, "/v1/fixture-difficulty", &res); rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		d := res.Data
		// Gameweek 7 is live, so form (and the window) is for 7: the six
		// completed gameweeks before it are 1-6.
		if d.FormGameweek != 7 || !slices.Equal(d.RecentGameweeks, []int{1, 2, 3, 4, 5, 6}) {
			t.Fatalf("form gameweek %d, window %v; want 7 and 1-6", d.FormGameweek, d.RecentGameweeks)
		}
		clubs := len(f.srv.mustWorld(t).Clubs)
		if len(d.Positions) != 4 {
			t.Fatalf("%d positions", len(d.Positions))
		}
		for _, p := range d.Positions {
			if len(p.Clubs) != clubs {
				t.Errorf("%s ranks %d clubs, the world has %d", p.Position, len(p.Clubs), clubs)
			}
			for i, c := range p.Clubs {
				if c.Rank < 1 || c.Rank > clubs || c.Matches != 6 {
					t.Errorf("%s %s: rank %d, %d matches (every club played once a gameweek)", p.Position, c.Club.Code, c.Rank, c.Matches)
				}
				if i == 0 {
					if c.Rank != 1 {
						t.Errorf("%s: first rank %d", p.Position, c.Rank)
					}
					continue
				}
				prev := p.Clubs[i-1]
				// Hardest first; equal per match shares the better rank,
				// otherwise the rank is the position in the list.
				switch {
				case prev.PointsConceded*c.Matches > c.PointsConceded*prev.Matches:
					t.Errorf("%s: %s concedes less than %s but ranks after it", p.Position, c.Club.Code, prev.Club.Code)
				case prev.PointsConceded*c.Matches == c.PointsConceded*prev.Matches && c.Rank != prev.Rank:
					t.Errorf("%s: %s ties %s but ranks %d, not %d", p.Position, c.Club.Code, prev.Club.Code, c.Rank, prev.Rank)
				case prev.PointsConceded*c.Matches < c.PointsConceded*prev.Matches && c.Rank != i+1:
					t.Errorf("%s: %s ranks %d at place %d", p.Position, c.Club.Code, c.Rank, i+1)
				}
			}
		}
	})
}

func TestProjectionsBeforeTheDeadline(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		f.clock.t = beforeGW8
		target := "/v1/leagues/" + f.league + "/gameweeks/8/projections"
		var res LeagueProjectionsResponse
		if rec := f.get(t, f.token, target, &res); rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		p := res.Data
		if p.Gameweek != 8 || p.FormGameweek != 7 || !slices.Equal(p.RecentGameweeks, []int{1, 2, 3, 4, 5, 6}) {
			t.Fatalf("gameweek %d, form gameweek %d, window %v", p.Gameweek, p.FormGameweek, p.RecentGameweeks)
		}

		// Difficulty: every fixture's rank is the opponent's rank for the
		// player's position in GET /fixture-difficulty.
		var diff FixtureDifficultyResponse
		f.get(t, f.token, "/v1/fixture-difficulty", &diff)
		rankOf := map[string]map[string]int{}
		for _, pd := range diff.Data.Positions {
			rankOf[string(pd.Position)] = map[string]int{}
			for _, c := range pd.Clubs {
				rankOf[string(pd.Position)][c.Club.Code] = c.Rank
			}
		}
		// Form: as the players list reports it, team by team.
		form := map[string]*float32{}
		position := map[string]string{}
		teams := map[string]bool{}
		for _, tp := range p.Teams {
			teams[tp.Team.Id] = true
		}
		for id := range teams {
			var list PlayerListResponse
			f.get(t, f.token, "/v1/leagues/"+f.league+"/players?teamId="+id+"&limit=100", &list)
			for _, it := range list.Data {
				form[it.Player.Id] = it.Form
				position[it.Player.Id] = string(it.Player.Position)
			}
		}

		expected := map[string]float64{}
		blanks, doubles := 0, 0
		for _, pp := range p.Players {
			sum := 0.0
			for _, fx := range pp.Fixtures {
				want := rankOf[position[pp.PlayerId]][fx.Opponent.Code]
				if fx.DifficultyRank == nil || *fx.DifficultyRank != want {
					t.Errorf("%s v %s: rank %v, difficulty says %d", pp.PlayerId, fx.Opponent.Code, valueOf(fx.DifficultyRank), want)
				}
				if math.Abs(float64(fx.Factor)-factorOf(fx.DifficultyRank)) > 1e-6 {
					t.Errorf("%s: factor %v for rank %v", pp.PlayerId, fx.Factor, valueOf(fx.DifficultyRank))
				}
				sum += factorOf(fx.DifficultyRank)
			}
			switch len(pp.Fixtures) {
			case 0:
				blanks++
			case 2:
				doubles++
			}
			if !equalJSON(t, pp.Form, form[pp.PlayerId]) {
				t.Errorf("%s: form %v, the players list says %v", pp.PlayerId, valueOf(pp.Form), valueOf(form[pp.PlayerId]))
			}
			if pp.Form == nil || pp.Expected == nil {
				t.Errorf("%s: form or expected null in gameweek 8", pp.PlayerId)
				continue
			}
			// Expected = form x the factors' sum, one decimal, halves up.
			want := math.Floor(float64(*pp.Form)*sum*10+0.5+1e-6) / 10
			if math.Abs(float64(*pp.Expected)-want) > 1e-4 {
				t.Errorf("%s: expected %v, form %v x %v = %v", pp.PlayerId, *pp.Expected, *pp.Form, sum, want)
			}
			if len(pp.Fixtures) == 0 && *pp.Expected != 0 {
				t.Errorf("%s: blank gameweek but expected %v", pp.PlayerId, *pp.Expected)
			}
			expected[pp.PlayerId] = float64(*pp.Expected)
		}
		// Gameweek 8 has a postponed match (a blank for two clubs) and a
		// double for two others.
		if blanks == 0 || doubles == 0 {
			t.Errorf("%d players blank, %d double: the demo gameweek 8 has both", blanks, doubles)
		}

		// Teams: the caller's own XI is its lineup; others' are private
		// until the deadline and come from the last locked lineup.
		var lineup LineupResponse
		f.get(t, f.token, "/v1/leagues/"+f.league+"/teams/"+f.mine.ID+"/lineup?gameweek=8", &lineup)
		for _, tp := range p.Teams {
			sum := 0.0
			for _, id := range tp.Starters {
				sum += expected[id]
			}
			if tp.Expected == nil || math.Abs(float64(*tp.Expected)-sum) > 1e-3 {
				t.Errorf("%s: expected %v, starters sum to %v", tp.Team.Name, valueOf(tp.Expected), sum)
			}
			if len(tp.Starters) != 11 {
				t.Errorf("%s: %d starters", tp.Team.Name, len(tp.Starters))
			}
			if tp.Team.Id == f.mine.ID {
				var want []string
				for _, e := range lineup.Data.Starters {
					want = append(want, e.Player.Id)
				}
				if tp.LineupBasis != LineupBasisLineup || !slices.Equal(tp.Starters, want) {
					t.Errorf("own team: basis %s, starters %v; lineup %v", tp.LineupBasis, tp.Starters, want)
				}
				continue
			}
			if tp.LineupBasis != LineupBasisLastLocked {
				t.Errorf("%s before the deadline: basis %s", tp.Team.Name, tp.LineupBasis)
			}
			// The same order GET /teams/{id} shows others.
			var order []string
			for _, e := range teamOf(t, f, f.token, tp.Team.Id).Roster[:11] {
				order = append(order, e.Player.Id)
			}
			if !slices.Equal(tp.Starters, order) {
				t.Errorf("%s: starters %v, the team page's first 11 %v", tp.Team.Name, tp.Starters, order)
			}
		}

		// Odds: pre-game, resampled, summing to 1, and the same on the
		// next request and later the same day (one bucket before kickoff).
		var schedule ScheduleResponse
		f.get(t, f.token, "/v1/leagues/"+f.league+"/schedule", &schedule)
		pairings := 0
		for _, w := range schedule.Data {
			if w.Gameweek == 8 {
				pairings = len(w.Matchups)
			}
		}
		if len(p.Matchups) != pairings || pairings == 0 {
			t.Fatalf("%d odds for %d pairings", len(p.Matchups), pairings)
		}
		for _, m := range p.Matchups {
			if m.Mode != OddsModePreGame || m.Odds == nil || m.Samples != 10000 {
				t.Fatalf("%s: mode %s, odds %v, samples %d", m.MatchupId, m.Mode, m.Odds, m.Samples)
			}
			if s := m.Odds.Home + m.Odds.Draw + m.Odds.Away; math.Abs(float64(s)-1) > 1e-5 {
				t.Errorf("%s: odds sum to %v", m.MatchupId, s)
			}
		}
		f.clock.t = beforeGW8.Add(3 * 3600e9)
		var again LeagueProjectionsResponse
		f.get(t, f.token, target, &again)
		if !equalJSON(t, again.Data.Matchups, p.Matchups) {
			t.Errorf("odds moved between requests before kickoff:\n%s\n%s", mustJSON(t, p.Matchups), mustJSON(t, again.Data.Matchups))
		}
	})
}

func TestProjectionsLiveAndFinal(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		f.clock.t = beforeGW8
		// Gameweek 7 is live: points so far plus the rest resampled.
		var live LeagueProjectionsResponse
		if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/7/projections", &live); rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		for _, m := range live.Data.Matchups {
			if m.Mode != OddsModeLive || m.Odds == nil || m.Samples != 10000 {
				t.Errorf("gw7 %s: mode %s, odds %v", m.MatchupId, m.Mode, m.Odds)
			}
		}
		for _, tp := range live.Data.Teams {
			if tp.LineupBasis != LineupBasisLineup {
				t.Errorf("gw7 is past its deadline: %s basis %s", tp.Team.Name, tp.LineupBasis)
			}
		}
		// Gameweek 6 is over: the odds are the result.
		var final LeagueProjectionsResponse
		f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/6/projections", &final)
		var list MatchupListResponse
		f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/6/matchups", &list)
		points := func(s MatchupSide) int {
			if ts, err := s.AsTeamScore(); err == nil && ts.Kind == TeamScoreKindTeam {
				return ts.Points
			}
			av, _ := s.AsAverageOpponent()
			return av.Points
		}
		for _, m := range list.Data {
			i := slices.IndexFunc(final.Data.Matchups, func(o MatchupOdds) bool { return o.MatchupId == m.Id })
			if i < 0 {
				t.Fatalf("no odds for %s", m.Id)
			}
			o := final.Data.Matchups[i]
			h, a := points(m.Home), points(m.Away)
			want := WinOdds{}
			switch {
			case h > a:
				want.Home = 1
			case h < a:
				want.Away = 1
			default:
				want.Draw = 1
			}
			sideID := func(side MatchupSide) *string {
				if ts, err := side.AsTeamScore(); err == nil && ts.Kind == TeamScoreKindTeam {
					return &ts.Team.Id
				}
				return nil
			}
			if !equalJSON(t, o.HomeTeamId, sideID(m.Home)) || !equalJSON(t, o.AwayTeamId, sideID(m.Away)) {
				t.Errorf("gw6 %s: sides %v v %v", m.Id, valueOf(o.HomeTeamId), valueOf(o.AwayTeamId))
			}
			if o.Mode != OddsModeFinal || o.Odds == nil || *o.Odds != want || o.Samples != 0 {
				t.Errorf("gw6 %s (%d-%d): %+v %v", m.Id, h, a, o, valueOf(o.Odds))
			}
		}
	})
}

func TestStartSitOnTheDemoWorld(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		f.clock.t = beforeGW8
		var res StartSitResponse
		if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/start-sit", &res); rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		if len(res.Data) == 0 {
			t.Fatal("no teams")
		}
		var prev *float32
		for i, row := range res.Data {
			// The team's points are its results on the team page.
			results := map[int]int{}
			for _, r := range teamOf(t, f, f.token, row.Team.Id).Results {
				results[r.Gameweek] = r.PointsFor
			}
			var gws []int
			sum := 0.0
			for _, w := range row.Gameweeks {
				gws = append(gws, w.Gameweek)
				if pts, ok := results[w.Gameweek]; !ok || pts != w.Points {
					t.Errorf("%s gw%d: points %d, team page %d", row.Team.Name, w.Gameweek, w.Points, pts)
				}
				if w.OptimalPoints < w.Points {
					t.Errorf("%s gw%d: optimal %d below the %d scored", row.Team.Name, w.Gameweek, w.OptimalPoints, w.Points)
				}
				if w.Accuracy == nil || math.Abs(float64(*w.Accuracy)-float64(w.Points)/float64(w.OptimalPoints)) > 1e-6 {
					t.Errorf("%s gw%d: accuracy %v for %d/%d", row.Team.Name, w.Gameweek, valueOf(w.Accuracy), w.Points, w.OptimalPoints)
				}
				if w.Accuracy != nil {
					sum += float64(*w.Accuracy)
				}
			}
			// Gameweeks 1-6 are completed; 7 is live.
			if !slices.Equal(gws, []int{1, 2, 3, 4, 5, 6}) {
				t.Errorf("%s: gameweeks %v, want 1-6", row.Team.Name, gws)
			}
			if row.SeasonAccuracy == nil || math.Abs(float64(*row.SeasonAccuracy)-sum/6) > 1e-6 {
				t.Errorf("%s: season %v, mean %v", row.Team.Name, valueOf(row.SeasonAccuracy), sum/6)
			}
			if i > 0 && *row.SeasonAccuracy > *prev {
				t.Errorf("%s is above %s but more accurate", res.Data[i-1].Team.Name, row.Team.Name)
			}
			prev = row.SeasonAccuracy
		}
	})
}
