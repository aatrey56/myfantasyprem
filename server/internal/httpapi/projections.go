package httpapi

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"math"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/projection"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/statuspage"
)

// Fixture difficulty, Expected points, win odds and start/sit accuracy
// (ADR 0014). The rules are package projection; this gathers their inputs
// from the world and the league.

// oddsBucket is how long live win odds keep one seed.
const oddsBucket = 300 // seconds

// difficultyOf ranks the clubs over the completed gameweeks in window.
func difficultyOf(w *league.World, window []int) projection.Difficulty {
	var matches []projection.Match
	var lines []projection.Line
	for _, gw := range window {
		for _, f := range w.FixturesIn(gw) {
			if f.Over() {
				matches = append(matches, projection.Match{FixtureID: f.ID, HomeClubID: f.HomeClubID, AwayClubID: f.AwayClubID})
			}
		}
		for id, rows := range w.Rows[gw] {
			p := w.Players[id]
			for _, r := range rows {
				// A player who has since changed club is matched to neither
				// side and ignored (projection.Rank).
				lines = append(lines, projection.Line{FixtureID: r.FixtureID, ClubID: p.ClubID, Position: p.Position, Points: r.Points})
			}
		}
	}
	return projection.Rank(matches, lines)
}

func (s *Server) GetFixtureDifficulty(ctx context.Context, _ GetFixtureDifficultyRequestObject) (GetFixtureDifficultyResponseObject, error) {
	w, err := s.world(ctx)
	if errors.Is(err, errNoSeasonData) {
		return fail[GetFixtureDifficultydefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	if err != nil {
		return nil, err
	}
	formGW := formGameweek(w)
	window := projection.RecentGameweeks(formGW, w.Completed)
	d := difficultyOf(w, window)
	body := FixtureDifficulty{FormGameweek: formGW, RecentGameweeks: append([]int{}, window...), Positions: []PositionDifficulty{}}
	for _, pos := range []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD} {
		rows := slices.Collect(maps.Values(d[pos]))
		slices.SortFunc(rows, func(a, b projection.Ranking) int {
			return cmp.Or(cmp.Compare(a.Rank, b.Rank), cmp.Compare(w.Clubs[a.ClubID].Code, w.Clubs[b.ClubID].Code))
		})
		pd := PositionDifficulty{Position: Position(pos), Clubs: []ClubDifficulty{}}
		for _, r := range rows {
			pd.Clubs = append(pd.Clubs, ClubDifficulty{Club: clubRef(w, r.ClubID), Rank: r.Rank, PointsConceded: r.Conceded, Matches: r.Matches})
		}
		body.Positions = append(body.Positions, pd)
	}
	meta, err := s.statsMeta(ctx, nil)
	if err != nil {
		return nil, err
	}
	return GetFixtureDifficulty200JSONResponse{Data: body, Meta: meta}, nil
}

// formOf is a player's form for gameweek formGW (statuspage.Form).
func formOf(w *league.World, playerID string, formGW int) (float64, bool) {
	var matches []statuspage.MatchPoints
	for gw := max(formGW-statuspage.FormWeeks, 1); gw < formGW; gw++ {
		for _, r := range w.Rows[gw][playerID] {
			matches = append(matches, statuspage.MatchPoints{Gameweek: gw, Points: r.Points})
		}
	}
	return statuspage.Form(matches, formGW)
}

func pf32(x float64) *float32 { v := float32(x); return &v }

func (s *Server) GetLeagueProjections(ctx context.Context, r GetLeagueProjectionsRequestObject) (GetLeagueProjectionsResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), badGameweek(r.Gw)); d != "" {
		return fail[GetLeagueProjectionsdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[GetLeagueProjectionsdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	if v.l.Status != league.StatusInSeason && v.l.Status != league.StatusComplete {
		return failLeague[GetLeagueProjectionsdefaultApplicationProblemPlusJSONResponse](ctx, league.ErrSeasonNotStarted)
	}
	gw := r.Gw
	g, ok := v.w.Gameweeks[gw]
	if !ok {
		return fail[GetLeagueProjectionsdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "No such gameweek this season.")), nil
	}
	// Form as it stood for this gameweek, or now for a later one.
	formGW := min(gw, formGameweek(v.w))
	window := projection.RecentGameweeks(formGW, v.w.Completed)
	diff := difficultyOf(v.w, window)
	state := v.w.State(gw)
	body := LeagueProjections{
		Gameweek: gw, FormGameweek: formGW, RecentGameweeks: append([]int{}, window...),
		Players: []PlayerProjection{}, Teams: []TeamProjection{}, Matchups: []MatchupOdds{},
	}

	// Players: every one on a roster this gameweek.
	expected := map[string]*float64{}
	var rostered []string
	for _, teamID := range league.TeamsWithRosters(v.l, v.g, gw) {
		rostered = append(rostered, v.g.RosterAt(teamID, gw)...)
	}
	slices.Sort(rostered)
	for _, id := range slices.Compact(rostered) {
		pl := v.w.Players[id]
		pp := PlayerProjection{PlayerId: id, Fixtures: []FixtureProjection{}}
		var ranks []int
		for _, f := range v.w.ClubFixtures(pl.ClubID, gw) {
			opp, home := f.AwayClubID, true
			if f.AwayClubID == pl.ClubID {
				opp, home = f.HomeClubID, false
			}
			rank := diff.Rank(pl.Position, opp)
			fp := FixtureProjection{FixtureId: f.ID, Opponent: clubRef(v.w, opp), IsHome: home, Factor: float32(projection.Factor(rank))}
			if rank > 0 {
				fp.DifficultyRank = &rank
			}
			pp.Fixtures = append(pp.Fixtures, fp)
			ranks = append(ranks, rank)
		}
		if form, ok := formOf(v.w, id, formGW); ok {
			e := projection.Expected(form, ranks)
			expected[id] = &e
			pp.Form, pp.Expected = pf32(form), pf32(e)
		}
		body.Players = append(body.Players, pp)
	}

	// Teams: the XI of the gameweek's lineup; before the deadline another
	// team's is private, so its last locked lineup stands in.
	open := v.now.Before(g.DeadlineAt)
	var sims []projection.Team
	minutes := map[string]int{} // fixture id → match minute, for live fixtures
	for _, teamID := range league.TeamsWithRosters(v.l, v.g, gw) {
		team, _ := v.l.Team(teamID)
		slots, basis := league.EffectiveLineup(v.g, v.w, teamID, gw).Slots, LineupBasisLineup
		if open && team.OwnerID != userFrom(ctx) {
			onRoster := map[string]string{}
			for _, id := range v.g.RosterAt(teamID, gw) {
				onRoster[id] = ""
			}
			slots, basis = publicRosterOrder(v, teamID, onRoster), LineupBasisLastLocked
		}
		xi := slots[:min(len(slots), scoring.StandardLineup().Starters)]
		tp := TeamProjection{Team: v.team(teamID), LineupBasis: basis, Starters: append([]string{}, xi...)}
		tenths, has := 0, false
		sim := projection.Team{ID: teamID}
		for _, id := range xi {
			if e := expected[id]; e != nil {
				tenths += int(math.Round(*e * 10))
				has = true
			}
			st := projection.Starter{}
			st.Actual, _, _ = v.w.PlayerPoints(id, gw)
			for _, f := range v.w.ClubFixtures(v.w.Players[id].ClubID, gw) {
				switch {
				case !f.Started():
					st.Remaining++
				case !f.Over():
					m, ok := minutes[f.ID]
					if !ok {
						m = fixtureMinute(v.w, f)
						minutes[f.ID] = m
					}
					st.Live = append(st.Live, m)
				}
			}
			for _, h := range window {
				pts, _, _ := v.w.PlayerPoints(id, h)
				st.History = append(st.History, pts)
			}
			sim.Starters = append(sim.Starters, st)
		}
		if has {
			tp.Expected = pf32(float64(tenths) / 10)
		}
		body.Teams = append(body.Teams, tp)
		sims = append(sims, sim)
	}

	// Matchups: the result once every match is over, else resampled.
	_, matchups := league.ScoreGameweek(scoring.StandardLineup(), v.l, v.g, v.w, gw)
	mode, bucket := OddsModePreGame, int64(0)
	switch state {
	case scoring.StateProvisional, scoring.StateOfficial, scoring.StateFinal:
		mode = OddsModeFinal
	case scoring.StateLive:
		mode, bucket = OddsModeLive, v.now.Unix()/oddsBucket
	}
	for _, m := range matchups {
		mo := MatchupOdds{MatchupId: m.ID, Mode: mode}
		if m.HomeTeamID != "" {
			mo.HomeTeamId = ptr(m.HomeTeamID)
		}
		if m.AwayTeamID != "" {
			mo.AwayTeamId = ptr(m.AwayTeamID)
		}
		var o projection.Odds
		switch {
		case mode == OddsModeFinal:
			o = projection.Final(m.HomePoints, m.AwayPoints)
		case len(window) == 0:
			body.Matchups = append(body.Matchups, mo)
			continue
		default:
			o = projection.WinOdds(sims, m.HomeTeamID, m.AwayTeamID, projection.Seed(m.ID, gw, bucket), projection.Samples)
			mo.Samples = projection.Samples
		}
		mo.Odds = &WinOdds{Home: float32(o.Home), Draw: float32(o.Draw), Away: float32(o.Away)}
		body.Matchups = append(body.Matchups, mo)
	}

	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return GetLeagueProjections200JSONResponse{Data: body, Meta: meta}, nil
}

func (s *Server) GetStartSitAccuracy(ctx context.Context, r GetStartSitAccuracyRequestObject) (GetStartSitAccuracyResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return fail[GetStartSitAccuracydefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[GetStartSitAccuracydefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	if v.l.Status != league.StatusInSeason && v.l.Status != league.StatusComplete {
		return failLeague[GetStartSitAccuracydefaultApplicationProblemPlusJSONResponse](ctx, league.ErrSeasonNotStarted)
	}
	rules := scoring.StandardLineup()
	rows := map[string]*TeamStartSit{}
	accs := map[string][]float64{}
	var order []string
	for _, t := range v.l.Teams {
		rows[t.ID] = &TeamStartSit{Team: v.team(t.ID), Gameweeks: []StartSitWeek{}}
		order = append(order, t.ID)
	}
	for gw := 1; gw <= 38; gw++ {
		if !v.w.Completed(gw) {
			continue
		}
		results, _ := league.ScoreGameweek(rules, v.l, v.g, v.w, gw)
		for teamID, res := range results {
			row := rows[teamID]
			if row == nil {
				continue
			}
			var squad []projection.Scored
			for _, id := range v.g.RosterAt(teamID, gw) {
				pts, _, _ := v.w.PlayerPoints(id, gw)
				squad = append(squad, projection.Scored{PlayerID: id, Position: v.w.Players[id].Position, Points: pts})
			}
			_, optimal, ok := projection.OptimalXI(squad, rules)
			if !ok {
				continue
			}
			week := StartSitWeek{Gameweek: gw, Points: res.Points, OptimalPoints: optimal}
			if a, ok := projection.Accuracy(res.Points, optimal); ok {
				week.Accuracy = pf32(a)
				accs[teamID] = append(accs[teamID], a)
			}
			row.Gameweeks = append(row.Gameweeks, week)
		}
	}
	for id, row := range rows {
		if a, ok := projection.SeasonAccuracy(accs[id]); ok {
			row.SeasonAccuracy = pf32(a)
		}
	}
	slices.SortStableFunc(order, func(a, b string) int {
		x, y := rows[a].SeasonAccuracy, rows[b].SeasonAccuracy
		switch {
		case x == nil && y == nil:
			return 0
		case x == nil:
			return 1
		case y == nil:
			return -1
		}
		return cmp.Compare(*y, *x)
	})
	out := []TeamStartSit{}
	for _, id := range order {
		out = append(out, *rows[id])
	}
	meta, err := s.statsMeta(ctx, nil)
	if err != nil {
		return nil, err
	}
	return GetStartSitAccuracy200JSONResponse{Data: out, Meta: meta}, nil
}
