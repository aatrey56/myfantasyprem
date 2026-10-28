package httpapi

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// GET /v1/gameweeks/{gw}/fixtures: the Premier League fixtures of one
// gameweek with their scores, for the scores screen. GET
// /v1/fixtures/{fixtureId}: one fixture's box score, with the caller's
// leagues' owners.

func (s *Server) GetGameweekFixtures(ctx context.Context, r GetGameweekFixturesRequestObject) (GetGameweekFixturesResponseObject, error) {
	if d := badGameweek(r.Gw); d != "" {
		return GetGameweekFixtures400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	if _, ok, err := s.stats.Status(ctx); err != nil {
		return nil, err
	} else if !ok {
		return fail[GetGameweekFixturesdefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	// Only this gameweek's rows: the response needs the fixtures, clubs
	// and the rows' minutes (the match clock).
	w, err := league.LoadWorld(ctx, s.stats, r.Gw)
	if err != nil {
		return nil, err
	}
	out := []GameweekFixture{}
	for _, f := range gameweekFixtures(w.Fixtures, r.Gw) {
		out = append(out, gameweekFixture(w, f, s.now()))
	}
	state := w.State(r.Gw)
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return GetGameweekFixtures200JSONResponse{Data: out, Meta: meta}, nil
}

func gameweekFixture(w *league.World, f stats.Fixture, now time.Time) GameweekFixture {
	gf := GameweekFixture{
		FixtureId: f.ID, Gameweek: f.Gameweek, Status: fixtureStatus(f.Status),
		Home: clubRef(w, f.HomeClubID), Away: clubRef(w, f.AwayClubID),
		HomeScore: f.HomeScore, AwayScore: f.AwayScore,
	}
	if f.KickoffAt != nil {
		gf.KickoffAt = ptr(f.KickoffAt.UTC())
	}
	if f.Status == stats.FixtureLive {
		gf.Minute, gf.Period = matchClock(fixtureMinute(w, f), f.KickoffAt, now)
	}
	return gf
}

// fixtureMinute is the most minutes any player has played in the fixture
// so far: the match minute while it is live, 0 before any is reported.
func fixtureMinute(w *league.World, f stats.Fixture) int {
	most := 0
	for _, rows := range w.Rows[f.Gameweek] {
		for _, r := range rows {
			if r.FixtureID == f.ID {
				most = max(most, r.Stats.Minutes)
			}
		}
	}
	return most
}

// halfTimeAfter is how long after kickoff a minute standing at 45 means
// half time rather than first-half stoppage time.
const halfTimeAfter = 50 * time.Minute

// matchClock is a live fixture's minute and period (both nil until
// upstream reports minutes). Upstream minutes stop at 45 over half time,
// so 45 more than halfTimeAfter after kickoff is half time.
func matchClock(minute int, kickoff *time.Time, now time.Time) (*int, *MatchPeriod) {
	if minute <= 0 {
		return nil, nil
	}
	p := FirstHalf
	switch {
	case minute > 45:
		p = SecondHalf
	case minute == 45 && kickoff != nil && now.Sub(*kickoff) > halfTimeAfter:
		p = HalfTime
	}
	return &minute, &p
}

// gameweekFixtures is a gameweek's fixtures, postponed ones included, by
// kickoff (none last), then id.
func gameweekFixtures(all []stats.Fixture, gw int) []stats.Fixture {
	var out []stats.Fixture
	for _, f := range all {
		if f.Gameweek == gw {
			out = append(out, f)
		}
	}
	slices.SortFunc(out, func(a, b stats.Fixture) int {
		switch {
		case a.KickoffAt == nil && b.KickoffAt != nil:
			return 1
		case a.KickoffAt != nil && b.KickoffAt == nil:
			return -1
		case a.KickoffAt != nil && !a.KickoffAt.Equal(*b.KickoffAt):
			return a.KickoffAt.Compare(*b.KickoffAt)
		}
		return cmp.Compare(a.ID, b.ID)
	})
	return out
}

func (s *Server) GetFixture(ctx context.Context, r GetFixtureRequestObject) (GetFixtureResponseObject, error) {
	if d := badID("fixtureId", "fx", r.FixtureId); d != "" {
		return GetFixture400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	st, ok, err := s.stats.Status(ctx)
	if err != nil {
		return nil, err
	} else if !ok {
		return fail[GetFixturedefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	all, err := s.stats.Fixtures(ctx)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(all, func(f stats.Fixture) bool { return f.ID == r.FixtureId })
	if i < 0 {
		return GetFixture404ApplicationProblemPlusJSONResponse{NotFoundApplicationProblemPlusJSONResponse(notFound(ctx, "Fixture not found."))}, nil
	}
	f := all[i]
	// Owners as of the fixture's gameweek; a postponed one has none, so
	// the current gameweek's.
	gw := f.Gameweek
	if gw == 0 {
		gw = max(st.Current, 1)
		if cw, err := s.world(ctx); err == nil {
			gw = max(cw.Status.Current, 1)
		}
	}
	w, err := league.LoadWorld(ctx, s.stats, gw)
	if err != nil {
		return nil, err
	}
	owners, err := s.ownersFor(ctx, gw)
	if err != nil {
		return nil, err
	}
	body := FixtureDetail{
		Fixture: gameweekFixture(w, f, s.now()),
		Home:    fixtureSide(w, f, f.HomeClubID, owners),
		Away:    fixtureSide(w, f, f.AwayClubID, owners),
	}
	var meta Meta
	if f.Gameweek > 0 {
		state := w.State(f.Gameweek)
		meta, err = s.statsMeta(ctx, &state)
	} else {
		meta, err = s.statsMeta(ctx, nil)
	}
	if err != nil {
		return nil, err
	}
	return GetFixture200JSONResponse{Data: body, Meta: meta}, nil
}

// ownersFor maps player id to the teams owning him in gameweek gw, one per
// league of the caller's that has rosters, leagues in the caller's order.
func (s *Server) ownersFor(ctx context.Context, gw int) (map[string][]FixtureOwner, error) {
	uid := userFrom(ctx)
	ls, err := s.leagues.ForUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	n, err := s.namesFor(ctx, ls...)
	if err != nil {
		return nil, err
	}
	out := map[string][]FixtureOwner{}
	for _, l := range ls {
		if l.Status == league.StatusPreDraft {
			continue
		}
		g, err := s.leagues.Repo().Game(ctx, l.ID)
		if err != nil {
			return nil, err
		}
		if g == nil {
			continue
		}
		me, _ := l.TeamOf(uid)
		for playerID, teamID := range g.OwnerAt(gw) {
			t, ok := l.Team(teamID)
			if !ok {
				continue
			}
			out[playerID] = append(out[playerID], FixtureOwner{
				LeagueId: l.ID, LeagueName: l.Name, Team: teamRef(t, n), Mine: teamID == me.ID,
			})
		}
	}
	return out, nil
}

// fixtureSide is one club's players in the fixture: everyone with a row
// who played (or was booked on the bench), and owned players of the club
// who have not. Rows carry no club, so a row counts for the player's
// current club; a player who has since moved to a club outside the
// fixture is left out.
func fixtureSide(w *league.World, f stats.Fixture, clubID string, owners map[string][]FixtureOwner) FixtureSide {
	side := FixtureSide{Club: clubRef(w, clubID), Players: []FixturePlayer{}}
	seen := map[string]bool{}
	for playerID, rows := range w.Rows[f.Gameweek] {
		if w.Players[playerID].ClubID != clubID {
			continue
		}
		for _, r := range rows {
			if r.FixtureID != f.ID {
				continue
			}
			appeared := r.Stats.Minutes > 0 || r.Stats.YellowCards > 0 || r.Stats.RedCards > 0
			if !appeared && len(owners[playerID]) == 0 {
				continue
			}
			seen[playerID] = true
			side.Players = append(side.Players, fixturePlayer(w, playerID, &r, owners[playerID]))
		}
	}
	for playerID := range owners {
		if !seen[playerID] && w.Players[playerID].ClubID == clubID {
			side.Players = append(side.Players, fixturePlayer(w, playerID, nil, owners[playerID]))
		}
	}
	slices.SortFunc(side.Players, func(a, b FixturePlayer) int {
		return cmp.Or(cmp.Compare(b.Points, a.Points), cmp.Compare(b.Minutes, a.Minutes),
			cmp.Compare(a.Player.DisplayName, b.Player.DisplayName), cmp.Compare(a.Player.Id, b.Player.Id))
	})
	return side
}

func fixturePlayer(w *league.World, playerID string, r *stats.PlayerFixture, owners []FixtureOwner) FixturePlayer {
	fp := FixturePlayer{Player: playerRef(w, playerID), Breakdown: []PointsLine{}, Owners: owners}
	if fp.Owners == nil {
		fp.Owners = []FixtureOwner{}
	}
	if r == nil {
		return fp
	}
	st := r.Stats
	fp.Minutes, fp.Points, fp.BonusProvisional = st.Minutes, r.Points, r.BonusProvisional
	fp.Stats = FixturePlayerStats{
		GoalsScored: st.Goals, Assists: st.Assists, OwnGoals: st.OwnGoals, YellowCards: st.YellowCards,
		RedCards: st.RedCards, Saves: st.Saves, PenaltiesSaved: st.PenaltiesSaved,
		PenaltiesMissed: st.PenaltiesMissed, Bonus: st.Bonus,
	}
	lines := r.Breakdown
	if len(lines) == 0 {
		_, lines = scoring.StandardRules().Points(w.Players[playerID].Position, st)
	}
	for _, l := range lines {
		fp.Breakdown = append(fp.Breakdown, PointsLine{Stat: PointsStat(l.Stat), Value: l.Value, Points: l.Points})
	}
	return fp
}
