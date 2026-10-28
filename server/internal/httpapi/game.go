package httpapi

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"math"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/statuspage"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// The read side of a league: the global stats (tier 1) joined with the
// league's game state, scored from scratch on every read with the same
// functions the poller uses to materialise scores (ADR 0005).

// errNoSeasonData means nothing has been ingested yet (a fresh database
// before the first poll or seed): 503, the client retries.
var errNoSeasonData = errors.New("no season data")

// errUnexpected is a response type a handler did not expect from another.
var errUnexpected = errors.New("httpapi: unexpected response")

func unavailable(ctx context.Context) Problem {
	return newProblem(503, "season-data-unavailable", "Season data unavailable",
		"Player and fixture data has not been loaded yet. Try again shortly.", RequestIDFrom(ctx))
}

func notFound(ctx context.Context, detail string) Problem {
	return newProblem(404, "not-found", "Not found", detail, RequestIDFrom(ctx))
}

// statsMeta is the envelope meta for stats-derived data: dataAsOf and
// stale from the poller's health (stats.Freshness), and the score state
// when one applies.
func (s *Server) statsMeta(ctx context.Context, state *scoring.ScoreState) (Meta, error) {
	ps, err := s.stats.PollState(ctx)
	if err != nil {
		return Meta{}, err
	}
	now := s.now().UTC()
	asOf, stale := stats.Freshness(ps, now)
	m := Meta{DataAsOf: asOf.UTC(), ServerTime: now, Stale: stale}
	if at := stats.StaleAt(ps); !at.IsZero() {
		at = at.UTC()
		m.StaleAt = &at
	}
	if state != nil {
		st := ScoreState(*state)
		m.ScoreState = &st
	}
	return m, nil
}

func pageMeta(m Meta, next *string) PageMeta {
	return PageMeta{DataAsOf: m.DataAsOf, ServerTime: m.ServerTime, Stale: m.Stale, StaleAt: m.StaleAt, ScoreState: m.ScoreState, NextCursor: next}
}

// world loads tier-1 data or returns errNoSeasonData.
func (s *Server) world(ctx context.Context) (*league.World, error) {
	if _, ok, err := s.stats.Status(ctx); err != nil {
		return nil, err
	} else if !ok {
		return nil, errNoSeasonData
	}
	w, err := league.LoadWorld(ctx, s.stats)
	if err != nil {
		return nil, err
	}
	w.SyncCalendar(s.now())
	return w, nil
}

// view is one league as a member sees it.
type view struct {
	l     *league.League
	g     *league.Game
	st    *league.State
	w     *league.World
	names names
	now   time.Time
}

// loadView checks membership and loads the league, its game state and the
// world. A *Problem return is the response to send.
func (s *Server) loadView(ctx context.Context, leagueID string) (*view, *Problem, error) {
	l, err := s.leagues.Get(ctx, userFrom(ctx), leagueID)
	if err != nil {
		p, err := leagueProblem(ctx, err)
		if err != nil {
			return nil, nil, err
		}
		return nil, &p, nil
	}
	w, err := s.world(ctx)
	if errors.Is(err, errNoSeasonData) {
		p := unavailable(ctx)
		return nil, &p, nil
	}
	if err != nil {
		return nil, nil, err
	}
	st, err := s.leagues.Repo().State(ctx, leagueID)
	if err != nil {
		return nil, nil, err
	}
	if st == nil {
		p := notFound(ctx, "League not found.")
		return nil, &p, nil
	}
	g := st.Game
	n, err := s.namesFor(ctx, l)
	if err != nil {
		return nil, nil, err
	}
	return &view{l: l, g: g, st: st, w: w, names: n, now: s.now()}, nil, nil
}

// current is the season's current gameweek.
func (v *view) current() int { return v.w.Status.Current }

// leagueGW is the league's current gameweek: the season's, but never
// before the league's first scoring gameweek once it has drafted
// (startGameweek, ADR 0010), so a league drafted mid-season shows its
// first gameweek's pairings, not an earlier gameweek it never played.
func (v *view) leagueGW() int {
	if v.l.Status != league.StatusInSeason && v.l.Status != league.StatusComplete {
		return v.current()
	}
	return min(max(v.current(), v.l.Settings.StartGameweek), 38)
}

// upcomingGW is the first gameweek whose deadline has not passed: the one
// lineups are being set for. After the last deadline it is the last one.
func upcomingGW(w *league.World, now time.Time) int {
	for gw := 1; gw <= 38; gw++ {
		if g, ok := w.Gameweeks[gw]; ok && now.Before(g.DeadlineAt) {
			return gw
		}
	}
	return max(w.Status.Current, 1)
}

func (v *view) team(id string) TeamRef {
	t, _ := v.l.Team(id)
	return teamRef(t, v.names)
}

// ---------------------------------------------------------------- mapping

func clubRef(w *league.World, clubID string) ClubRef {
	c := w.Clubs[clubID]
	return ClubRef{Code: c.Code, Name: c.Name}
}

// availability maps our values onto the contract's enum; anything new
// renders as unavailable rather than breaking clients.
func availability(a string) PlayerAvailability {
	if v := PlayerAvailability(a); v.Valid() {
		return v
	}
	return PlayerAvailabilityUnavailable
}

func playerRef(w *league.World, id string) PlayerRef {
	p := w.Players[id]
	ref := PlayerRef{
		Id: p.ID, DisplayName: p.DisplayName, Club: clubRef(w, p.ClubID),
		Position: Position(p.Position), Availability: availability(p.Availability),
	}
	if p.News != "" {
		n := p.News
		ref.News = &n
		ref.NewsAt = p.NewsAt
	}
	return ref
}

func gameweekBody(g stats.Gameweek) Gameweek {
	return Gameweek{
		Number: g.Number, Name: g.Name, DeadlineAt: g.DeadlineAt.UTC(), WaiverDeadlineAt: g.WaiverDeadlineAt.UTC(),
		TradeDeadlineAt: g.TradeDeadlineAt.UTC(), Finished: g.Finished,
	}
}

func fixtureStatus(s string) FixtureStatus {
	if v := FixtureStatus(s); v.Valid() {
		return v
	}
	return FixtureStatusScheduled
}

// upcomingFixtures maps a club's fixtures, at most limit; ones without a
// kickoff are skipped.
func upcomingFixtures(w *league.World, clubID string, fixtures []stats.Fixture, limit int) []UpcomingFixture {
	out := []UpcomingFixture{}
	for _, f := range fixtures {
		if f.KickoffAt == nil || len(out) == limit {
			continue
		}
		out = append(out, upcomingFixture(w, clubID, f))
	}
	return out
}

// remainingFixtures is every fixture of a club not yet kicked off this
// season, postponed (TBC) ones included, in kickoff order with undated ones
// last (then by gameweek and id, so the order is stable).
func remainingFixtures(w *league.World, clubID string) []UpcomingFixture {
	remainingScans.Add(1)
	var fs []stats.Fixture
	for _, f := range w.Fixtures {
		if (f.HomeClubID == clubID || f.AwayClubID == clubID) && !f.Started() {
			fs = append(fs, f)
		}
	}
	slices.SortStableFunc(fs, func(a, b stats.Fixture) int {
		switch {
		case a.KickoffAt == nil && b.KickoffAt == nil:
		case a.KickoffAt == nil:
			return 1
		case b.KickoffAt == nil:
			return -1
		default:
			if c := a.KickoffAt.Compare(*b.KickoffAt); c != 0 {
				return c
			}
		}
		return cmp.Or(cmp.Compare(a.Gameweek, b.Gameweek), cmp.Compare(a.ID, b.ID))
	})
	out := make([]UpcomingFixture, 0, len(fs))
	for _, f := range fs {
		out = append(out, upcomingFixture(w, clubID, f))
	}
	return out
}

// remainingMemo is a row's compact remainingFixtures, memoised per club
// for one request: every row of a club shares its schedule.
func remainingMemo(w *league.World) func(clubID string) []RowFixture {
	by := map[string][]RowFixture{}
	return func(clubID string) []RowFixture {
		fs, ok := by[clubID]
		if !ok {
			fs = []RowFixture{}
			for _, u := range remainingFixtures(w, clubID) {
				fs = append(fs, rowFixture(u))
			}
			by[clubID] = fs
		}
		return fs
	}
}

// rowFixture is the compact row form of a fixture: the opponent's code,
// and no status while scheduled.
func rowFixture(u UpcomingFixture) RowFixture {
	r := RowFixture{FixtureId: u.FixtureId, Gameweek: u.Gameweek, IsHome: u.IsHome, KickoffAt: u.KickoffAt,
		Opponent: u.Opponent.Code}
	if u.Status != nil && *u.Status != FixtureStatusScheduled {
		r.Status = u.Status
	}
	return r
}

// remainingScans counts remainingFixtures calls (tests check the work
// follows the page).
var remainingScans atomic.Int64

// upcomingFixture maps one of a club's fixtures. A postponed fixture has no
// gameweek (it counts in the one it is replayed in), and an undated one no
// kickoff.
func upcomingFixture(w *league.World, clubID string, f stats.Fixture) UpcomingFixture {
	home := f.HomeClubID == clubID
	opp := f.AwayClubID
	if !home {
		opp = f.HomeClubID
	}
	st := fixtureStatus(f.Status)
	u := UpcomingFixture{FixtureId: f.ID, IsHome: home, Opponent: clubRef(w, opp), Status: &st}
	if f.Gameweek > 0 && f.Status != stats.FixturePostponed {
		u.Gameweek = ptr(f.Gameweek)
	}
	if f.KickoffAt != nil {
		u.KickoffAt = ptr(f.KickoffAt.UTC())
	}
	return u
}

// playerFixtures is a player's line in each of his club's fixtures of a
// gameweek, with the points breakdown.
func playerFixtures(w *league.World, playerID string, gw int) []PlayerFixture {
	p := w.Players[playerID]
	rows := map[string]stats.PlayerFixture{}
	for _, r := range w.Rows[gw][playerID] {
		rows[r.FixtureID] = r
	}
	out := []PlayerFixture{}
	for _, f := range w.ClubFixtures(p.ClubID, gw) {
		if f.KickoffAt == nil {
			continue
		}
		home := f.HomeClubID == p.ClubID
		opp := f.AwayClubID
		if !home {
			opp = f.HomeClubID
		}
		pf := PlayerFixture{
			FixtureId: f.ID, Opponent: clubRef(w, opp), IsHome: home, KickoffAt: f.KickoffAt.UTC(),
			Status: fixtureStatus(f.Status), HomeScore: f.HomeScore, AwayScore: f.AwayScore,
			Breakdown: []PointsLine{},
		}
		if r, ok := rows[f.ID]; ok {
			pf.Minutes, pf.Points, pf.BonusProvisional = r.Stats.Minutes, r.Points, r.BonusProvisional
			// The row's own breakdown, scored with the same table as its
			// points. Rows stored before breakdowns were kept have none
			// until the next ingest; they fall back to the standard table.
			lines := r.Breakdown
			if len(lines) == 0 {
				_, lines = scoring.StandardRules().Points(p.Position, r.Stats)
			}
			for _, l := range lines {
				pf.Breakdown = append(pf.Breakdown, PointsLine{Stat: PointsStat(l.Stat), Value: l.Value, Points: l.Points})
			}
		}
		out = append(out, pf)
	}
	return out
}

func averageSide(points int) AverageOpponent {
	return AverageOpponent{Kind: AverageOpponentKindAverage, Points: points, Basis: FloorMeanAllTeams}
}

func opponent(v *view, teamID string) (Opponent, error) {
	var o Opponent
	if teamID == "" {
		return o, o.FromAverageOpponentRef(AverageOpponentRef{Kind: AverageOpponentRefKindAverage})
	}
	return o, o.FromTeamOpponent(TeamOpponent{Kind: TeamOpponentKindTeam, Team: v.team(teamID)})
}

// summaries scores a gameweek's matchups from scratch.
func (v *view) summaries(gw int) ([]MatchupSummary, error) {
	_, matchups := league.ScoreGameweek(scoring.StandardLineup(), v.l, v.g, v.w, gw)
	out := []MatchupSummary{}
	for _, m := range matchups {
		ms := MatchupSummary{Id: m.ID, Gameweek: gw, State: ScoreState(m.State)}
		for _, side := range []struct {
			team   string
			points int
			dst    *MatchupSide
		}{{m.HomeTeamID, m.HomePoints, &ms.Home}, {m.AwayTeamID, m.AwayPoints, &ms.Away}} {
			var err error
			if side.team == "" {
				err = side.dst.FromAverageOpponent(averageSide(side.points))
			} else {
				err = side.dst.FromTeamScore(TeamScore{Kind: TeamScoreKindTeam, Team: v.team(side.team), Points: side.points})
			}
			if err != nil {
				return nil, err
			}
		}
		out = append(out, ms)
	}
	return out, nil
}

func (v *view) standings() []StandingRow {
	out := []StandingRow{}
	for _, r := range league.Standings(v.l, v.g, v.w) {
		row := StandingRow{
			Rank: r.Rank, Team: v.team(r.TeamID), Played: r.Played, Wins: r.Wins, Draws: r.Draws, Losses: r.Losses,
			PointsFor: r.PointsFor, PointsAgainst: r.PointsAgainst, LeaguePoints: r.LeaguePoints(), Form: []FormResult{},
		}
		for _, f := range r.Form {
			row.Form = append(row.Form, FormResult(f))
		}
		out = append(out, row)
	}
	return out
}

func recordOf(rows []StandingRow, teamID string) Record {
	for _, r := range rows {
		if r.Team.Id == teamID {
			return Record{Rank: r.Rank, Played: r.Played, Wins: r.Wins, Draws: r.Draws, Losses: r.Losses,
				PointsFor: r.PointsFor, PointsAgainst: r.PointsAgainst, LeaguePoints: r.LeaguePoints}
		}
	}
	return Record{Rank: len(rows) + 1}
}

// ---------------------------------------------------------------- public

func (s *Server) ListClubs(ctx context.Context, _ ListClubsRequestObject) (ListClubsResponseObject, error) {
	clubs, err := s.stats.Clubs(ctx)
	if err != nil {
		return nil, err
	}
	meta, err := s.statsMeta(ctx, nil)
	if err != nil {
		return nil, err
	}
	out := []ClubRef{}
	for _, c := range clubs {
		out = append(out, ClubRef{Code: c.Code, Name: c.Name})
	}
	return ListClubs200JSONResponse{Data: out, Meta: meta}, nil
}

func (s *Server) GetCurrentGameweek(ctx context.Context, _ GetCurrentGameweekRequestObject) (GetCurrentGameweekResponseObject, error) {
	w, err := s.world(ctx)
	if errors.Is(err, errNoSeasonData) {
		return fail[GetCurrentGameweekdefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	if err != nil {
		return nil, err
	}
	cur, ok := w.Gameweeks[w.Status.Current]
	if !ok {
		return fail[GetCurrentGameweekdefaultApplicationProblemPlusJSONResponse](unavailable(ctx)), nil
	}
	data := GameweekStatus{
		Current: gameweekBody(cur), BonusAdded: cur.BonusAdded, DataChecked: cur.DataChecked,
		WaiversProcessed: w.Status.WaiversProcessed,
	}
	if next, ok := w.Gameweeks[w.Status.Next]; ok {
		g := gameweekBody(next)
		data.Next = &g
	}
	state := w.State(w.Status.Current)
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return GetCurrentGameweek200JSONResponse{Data: data, Meta: meta}, nil
}

// ---------------------------------------------------------------- league

func (s *Server) GetLeague(ctx context.Context, req GetLeagueRequestObject) (GetLeagueResponseObject, error) {
	if d := badID("leagueId", "lg", req.LeagueId); d != "" {
		return GetLeague400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[GetLeaguedefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	gw := v.leagueGW()
	matchups, err := v.summaries(gw)
	if err != nil {
		return nil, err
	}
	state := v.w.State(gw)
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	var phase *TransactionPhase
	if v.l.Status == league.StatusInSeason {
		tp := v.transactionPhase()
		phase = &tp
	}
	return GetLeague200JSONResponse{Data: LeagueHome{
		TransactionPhase: phase,
		League:           leagueSummary(v.l),
		Settings:         leagueSettings(v.l.Settings),
		Viewer:           leagueViewer(v.l, userFrom(ctx), v.names),
		CurrentGameweek:  gameweekBody(v.w.Gameweeks[gw]),
		Standings:        v.standings(),
		Matchups:         matchups,
	}, Meta: meta}, nil
}

// deref returns *p, or a zero Problem when p is nil (the caller then
// returns an error and the zero response is never written).
func deref(p *Problem) Problem {
	if p == nil {
		return Problem{}
	}
	return *p
}

func (s *Server) ListStandings(ctx context.Context, r ListStandingsRequestObject) (ListStandingsResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return fail[ListStandingsdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[ListStandingsdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	meta, err := s.statsMeta(ctx, nil)
	if err != nil {
		return nil, err
	}
	return ListStandings200JSONResponse{Data: v.standings(), Meta: meta}, nil
}

func (s *Server) GetSchedule(ctx context.Context, r GetScheduleRequestObject) (GetScheduleResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return fail[GetScheduledefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[GetScheduledefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	weeks := []ScheduleWeek{}
	for gw := 1; gw <= 38; gw++ {
		ms := v.g.MatchupsIn(gw)
		if len(ms) == 0 {
			continue
		}
		week := ScheduleWeek{Gameweek: gw, Matchups: []ScheduledMatchup{}}
		for _, m := range ms {
			home, err := opponent(v, m.HomeTeamID)
			if err != nil {
				return nil, err
			}
			away, err := opponent(v, m.AwayTeamID)
			if err != nil {
				return nil, err
			}
			week.Matchups = append(week.Matchups, ScheduledMatchup{Id: m.ID, Gameweek: gw, Home: home, Away: away})
		}
		weeks = append(weeks, week)
	}
	return GetSchedule200JSONResponse{Data: weeks, Meta: metaAt(s.now())}, nil
}

func (s *Server) ListMatchups(ctx context.Context, req ListMatchupsRequestObject) (ListMatchupsResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badGameweek(req.Gw)); d != "" {
		return ListMatchups400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[ListMatchupsdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	out, err := v.summaries(req.Gw)
	if err != nil {
		return nil, err
	}
	state := v.w.State(req.Gw)
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return ListMatchups200JSONResponse{Data: out, Meta: meta}, nil
}

// lineupSlots maps a scored team to the contract's slots and auto-subs.
func lineupSide(v *view, r league.TeamResult) TeamLineup {
	tl := TeamLineup{
		Kind: TeamLineupKindTeam, Team: v.team(r.TeamID), Formation: r.Formation,
		TotalPoints: r.Points, BenchPoints: r.BenchPoints,
		Starters: []LineupSlot{}, Bench: []LineupSlot{}, AutoSubs: []AutoSub{},
	}
	for _, sl := range r.Slots {
		ls := LineupSlot{
			Slot: sl.Slot, Player: playerRef(v.w, sl.PlayerID), Points: sl.Points, Minutes: sl.Minutes,
			AutoSub: AutoSubState(sl.AutoSub), Fixtures: playerFixtures(v.w, sl.PlayerID, r.Gameweek),
		}
		if sl.Slot <= 11 {
			tl.Starters = append(tl.Starters, ls)
		} else {
			tl.Bench = append(tl.Bench, ls)
		}
	}
	for _, sub := range r.Subs {
		tl.AutoSubs = append(tl.AutoSubs, AutoSub{
			Status: AutoSubStatus(sub.Status), SlotOut: sub.OutSlot, SlotIn: sub.InSlot,
			PlayerOutId: sub.OutPlayerID, PlayerInId: sub.InPlayerID,
		})
	}
	return tl
}

func (s *Server) GetMatchup(ctx context.Context, req GetMatchupRequestObject) (GetMatchupResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badGameweek(req.Gw), badID("matchupId", "mu", req.MatchupId)); d != "" {
		return GetMatchup400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[GetMatchupdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	results, matchups := league.ScoreGameweek(scoring.StandardLineup(), v.l, v.g, v.w, req.Gw)
	i := slices.IndexFunc(matchups, func(m league.Matchup) bool { return m.ID == req.MatchupId })
	if i < 0 {
		return fail[GetMatchupdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Matchup not found in this gameweek.")), nil
	}
	// Lineups are private until the deadline, so the detail opens then.
	if g := v.w.Gameweeks[req.Gw]; v.now.Before(g.DeadlineAt) {
		return fail[GetMatchupdefaultApplicationProblemPlusJSONResponse](
			newProblem(404, "matchup-not-started", "Not started", "Matchup lineups open at the gameweek deadline.", RequestIDFrom(ctx))), nil
	}
	m := matchups[i]
	detail := MatchupDetail{Id: m.ID, Gameweek: req.Gw, State: ScoreState(m.State)}
	for _, side := range []struct {
		team   string
		points int
		dst    *MatchupDetailSide
	}{{m.HomeTeamID, m.HomePoints, &detail.Home}, {m.AwayTeamID, m.AwayPoints, &detail.Away}} {
		var err error
		if side.team == "" {
			err = side.dst.FromAverageOpponent(averageSide(side.points))
		} else {
			err = side.dst.FromTeamLineup(lineupSide(v, results[side.team]))
		}
		if err != nil {
			return nil, err
		}
	}
	meta, err := s.statsMeta(ctx, &m.State)
	if err != nil {
		return nil, err
	}
	return GetMatchup200JSONResponse{Data: detail, Meta: meta}, nil
}

// ---------------------------------------------------------------- lineup

func (s *Server) GetLineup(ctx context.Context, r GetLineupRequestObject) (GetLineupResponseObject, error) {
	if d := cmp.Or(lg(r.LeagueId), tm(r.TeamId), optGameweek(r.Params.Gameweek)); d != "" {
		return fail[GetLineupdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[GetLineupdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	team, ok := v.l.Team(r.TeamId)
	if !ok {
		return fail[GetLineupdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Team not found.")), nil
	}
	gw := upcomingGW(v.w, v.now)
	if r.Params.Gameweek != nil {
		gw = *r.Params.Gameweek
	}
	g, ok := v.w.Gameweeks[gw]
	if !ok {
		return fail[GetLineupdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "No such gameweek this season.")), nil
	}
	locked := !v.now.Before(g.DeadlineAt)
	if !locked && team.OwnerID != userFrom(ctx) {
		return fail[GetLineupdefaultApplicationProblemPlusJSONResponse](newProblem(403, "lineup-private", "Lineup private",
			"Other teams' lineups are private until the gameweek deadline.", RequestIDFrom(ctx))), nil
	}
	if len(v.g.RosterAt(team.ID, gw)) == 0 {
		return fail[GetLineupdefaultApplicationProblemPlusJSONResponse](
			newProblem(404, "no-roster", "No roster", "This team has no players for that gameweek yet (the draft has not run).", RequestIDFrom(ctx))), nil
	}
	res := league.ScoreTeam(scoring.StandardLineup(), v.g, v.w, team.ID, gw)
	started := v.w.Started(gw)
	totals, clubMatches := seasonTotals(v.w), clubMatchesOver(v.w)
	body := Lineup{
		Team: teamRef(team, v.names), Gameweek: gw, Locked: locked, LocksAt: g.DeadlineAt.UTC(),
		Formation: res.Formation, Starters: []LineupEntry{}, Bench: []LineupEntry{},
		UpdatedAt: res.Lineup.UpdatedAt,
	}
	if body.UpdatedAt.IsZero() {
		body.UpdatedAt = v.l.CreatedAt
	}
	if started {
		state := ScoreState(res.State)
		body.TotalPoints, body.BenchPoints, body.ScoreState = &res.Points, &res.BenchPoints, &state
	}
	for _, sl := range res.Slots {
		p := v.w.Players[sl.PlayerID]
		e := LineupEntry{
			Slot: sl.Slot, Player: playerRef(v.w, sl.PlayerID),
			Fixtures: upcomingFixtures(v.w, p.ClubID, v.w.ClubFixtures(p.ClubID, gw), 3),
			Season: &LineupEntrySeason{
				Form: totals[sl.PlayerID].contractForm(), TotalPoints: totals[sl.PlayerID].points,
				Appearances: totals[sl.PlayerID].appearances, ClubMatches: clubMatches[p.ClubID],
			},
			ChanceOfPlaying: statuspage.ChanceOfPlaying(p.Availability, p.News),
		}
		if started {
			pts, sub, prov := sl.Points, AutoSubState(sl.AutoSub), sl.BonusProvisional
			e.Points, e.AutoSub, e.BonusProvisional = &pts, &sub, &prov
		}
		if sl.Slot <= 11 {
			body.Starters = append(body.Starters, e)
		} else {
			body.Bench = append(body.Bench, e)
		}
	}
	var state *scoring.ScoreState
	if started {
		state = &res.State
	}
	meta, err := s.statsMeta(ctx, state)
	if err != nil {
		return nil, err
	}
	return GetLineup200JSONResponse{Data: body, Meta: meta}, nil
}

// ---------------------------------------------------------------- team

func (s *Server) GetTeam(ctx context.Context, req GetTeamRequestObject) (GetTeamResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("teamId", "tm", req.TeamId)); d != "" {
		return GetTeam400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[GetTeamdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	team, ok := v.l.Team(req.TeamId)
	if !ok {
		return fail[GetTeamdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Team not found.")), nil
	}
	cur, next := v.current(), upcomingGW(v.w, v.now)
	totals := seasonTotals(v.w)
	detail := TeamDetail{
		Team: teamRef(team, v.names), Record: recordOf(v.standings(), team.ID),
		Roster: []RosterEntry{}, Results: []TeamResult{},
	}

	via := map[string]string{}
	for _, rs := range v.g.Rosters {
		if rs.TeamID == team.ID && rs.OnRosterAt(next) {
			via[rs.PlayerID] = rs.AcquiredVia
		}
	}
	if len(via) > 0 {
		order := league.EffectiveLineup(v.g, v.w, team.ID, next).Slots
		if team.OwnerID != userFrom(ctx) {
			// The next gameweek's lineup is private until its deadline
			// (GetLineup's lineup-private): others see the roster in the
			// order of the last locked lineup.
			order = publicRosterOrder(v, team.ID, via)
		}
		remaining := remainingMemo(v.w)
		for i, id := range order {
			pts, _, _ := v.w.PlayerPoints(id, cur)
			acquired := RosterEntryAcquiredVia(via[id])
			if !acquired.Valid() {
				acquired = RosterEntryAcquiredViaDraft
			}
			club := v.w.Players[id].ClubID
			detail.Roster = append(detail.Roster, RosterEntry{
				Slot: i + 1, Player: playerRef(v.w, id), TotalPoints: totals[id].points, GameweekPoints: pts,
				AcquiredVia: acquired, NextFixtures: remaining(club),
			})
		}
	}

	for gw := 38; gw >= 1; gw-- {
		if !v.w.Completed(gw) {
			continue
		}
		for _, m := range v.g.MatchupsIn(gw) {
			mine, theirs, opp := m.HomePoints, m.AwayPoints, m.AwayTeamID
			switch team.ID {
			case m.HomeTeamID:
			case m.AwayTeamID:
				mine, theirs, opp = m.AwayPoints, m.HomePoints, m.HomeTeamID
			default:
				continue
			}
			o, err := opponent(v, opp)
			if err != nil {
				return nil, err
			}
			detail.Results = append(detail.Results, TeamResult{
				Gameweek: gw, MatchupId: m.ID, Opponent: o, PointsFor: mine, PointsAgainst: theirs,
				Result: FormResult(scoring.H2H(mine, theirs)),
			})
		}
	}

	for _, m := range v.g.MatchupsIn(next) {
		if m.HomeTeamID != team.ID && m.AwayTeamID != team.ID || !v.now.Before(v.w.Gameweeks[next].DeadlineAt) {
			continue
		}
		opp := m.AwayTeamID
		if m.AwayTeamID == team.ID {
			opp = m.HomeTeamID
		}
		o, err := opponent(v, opp)
		if err != nil {
			return nil, err
		}
		detail.NextOpponent = &NextOpponent{Gameweek: next, MatchupId: m.ID, Opponent: o, DeadlineAt: v.w.Gameweeks[next].DeadlineAt.UTC()}
	}
	meta, err := s.statsMeta(ctx, nil)
	if err != nil {
		return nil, err
	}
	return GetTeam200JSONResponse{Data: detail, Meta: meta}, nil
}

// publicRosterOrder orders a team's roster (the keys of onRoster) for
// someone other than its owner, using only what is already public: the
// lineup of the last gameweek whose deadline has passed, then players
// added since in DefaultLineup's order (position, then draft rank).
// Before the first deadline it is DefaultLineup of the whole roster.
func publicRosterOrder(v *view, teamID string, onRoster map[string]string) []string {
	locked := 0
	for gw := 1; gw <= 38; gw++ {
		if g, ok := v.w.Gameweeks[gw]; ok && !v.now.Before(g.DeadlineAt) {
			locked = gw
		}
	}
	var order []string
	seen := map[string]bool{}
	if locked > 0 {
		for _, id := range league.EffectiveLineup(v.g, v.w, teamID, locked).Slots {
			if _, ok := onRoster[id]; ok && !seen[id] {
				order = append(order, id)
				seen[id] = true
			}
		}
	}
	var rest []string
	for id := range onRoster {
		if !seen[id] {
			rest = append(rest, id)
		}
	}
	return append(order, league.DefaultLineup(rest, v.w.Players)...)
}

// ---------------------------------------------------------------- players

type playerTotals struct {
	points, minutes int
	// appearances counts the matches he played in (scoring.FixtureStats.Played).
	appearances int
	// form is statuspage.Form; hasForm is false before any gameweek is
	// completed.
	form    float64
	hasForm bool
	// prevForm is the same mean for the two gameweeks before form's;
	// hasPrev is false until one of them is completed.
	prevForm float64
	hasPrev  bool
	// The stat strip's season counts, from the scored rows.
	goals, assists, cleanSheets, saves, dcPoints int
	// The rest of the sortable counts, from the scored rows.
	goalsConceded, ownGoals, pensSaved, pensMissed, yellows, reds, bonus int
}

// contractFormChange is form minus prevForm, one decimal, or null.
func (t playerTotals) contractFormChange() *float32 {
	if !t.hasForm || !t.hasPrev {
		return nil
	}
	return ptr(float32(math.Round((t.form-t.prevForm)*10) / 10))
}

// seasonStats is the list row's PlayerSeasonStats; xgi is the player's
// season xGI from upstream's lines, nil when none was published.
func (t playerTotals) seasonStats(xgi *float64) PlayerSeasonStats {
	out := PlayerSeasonStats{
		Goals: t.goals, Assists: t.assists, CleanSheets: t.cleanSheets, Saves: t.saves,
		DefensiveContributionPoints: t.dcPoints,
	}
	if xgi != nil {
		out.ExpectedGoalInvolvements = ptr(float32(math.Round(*xgi*100) / 100))
	}
	return out
}

// contractForm is form as the contract has it: null when there is none.
func (t playerTotals) contractForm() *float32 {
	if !t.hasForm {
		return nil
	}
	return ptr(float32(t.form))
}

// formGameweek is the gameweek form is for (statuspage.FormGameweek): the
// season's current gameweek while it is played, the next once it has
// finished.
func formGameweek(w *league.World) int {
	return statuspage.FormGameweek(w.Status.Current, w.Completed(w.Status.Current))
}

// seasonTotals sums every player's season and computes his form
// (statuspage.Form: the mean points over the two completed gameweeks
// before formGameweek, a gameweek he did not play counting as 0).
func seasonTotals(w *league.World) map[string]playerTotals {
	out := map[string]playerTotals{}
	matches := map[string][]statuspage.MatchPoints{}
	for id := range w.Players {
		out[id] = playerTotals{} // a player without a row still has a form
	}
	for gw, players := range w.Rows {
		for id, rows := range players {
			t := out[id]
			for _, r := range rows {
				t.points += r.Points
				t.minutes += r.Stats.Minutes
				if r.Stats.Played() {
					t.appearances++
				}
				t.goals += r.Stats.Goals
				t.assists += r.Stats.Assists
				t.saves += r.Stats.Saves
				t.goalsConceded += r.Stats.GoalsConceded
				t.ownGoals += r.Stats.OwnGoals
				t.pensSaved += r.Stats.PenaltiesSaved
				t.pensMissed += r.Stats.PenaltiesMissed
				t.yellows += r.Stats.YellowCards
				t.reds += r.Stats.RedCards
				t.bonus += r.Stats.Bonus
				if r.Stats.CleanSheet && r.Stats.Minutes >= 60 {
					t.cleanSheets++
				}
				for _, l := range r.Breakdown {
					if l.Stat == scoring.StatDefensiveContribution {
						t.dcPoints += l.Points
					}
				}
				matches[id] = append(matches[id], statuspage.MatchPoints{Gameweek: gw, Points: r.Points})
			}
			out[id] = t
		}
	}
	formGW := formGameweek(w)
	for id, t := range out {
		t.form, t.hasForm = statuspage.Form(matches[id], formGW)
		t.prevForm, t.hasPrev = statuspage.Form(matches[id], formGW-statuspage.FormWeeks)
		out[id] = t
	}
	return out
}

// clubMatchesOver counts each club's finished (or provisionally finished)
// matches this season: the most a player of the club could have played.
func clubMatchesOver(w *league.World) map[string]int {
	out := map[string]int{}
	for _, f := range w.Fixtures {
		if f.Over() {
			out[f.HomeClubID]++
			out[f.AwayClubID]++
		}
	}
	return out
}

// positionRank is the player's rank by season points among players of his
// position, 1 = most (ties share a rank).
func positionRank(w *league.World, totals map[string]playerTotals, playerID string) int {
	p := w.Players[playerID]
	rank := 1
	for id, o := range w.Players {
		if o.Position == p.Position && totals[id].points > totals[playerID].points {
			rank++
		}
	}
	return rank
}

// rosterStatus is a player's availability in the league (the waiver
// cycle, package waivers): on a team, on waivers until the next run, or a
// free agent.
// transactionPhase is the league's transaction window now (waivers.PhaseAt).
func (v *view) transactionPhase() TransactionPhase {
	p := waivers.CycleAt(v.st, v.w, v.now).Phase
	return TransactionPhase{Phase: TransactionPhasePhase(p.Phase), Gameweek: min(p.Gameweek, 38), Until: p.Until}
}

func rosterStatus(v *view, owners map[string]string, playerID string) (RosterStatus, *time.Time, *TeamRef) {
	status, clears, team := waivers.PlayerStatus(v.st, v.w, waivers.CycleAt(v.st, v.w, v.now), owners, playerID)
	if team != "" {
		ref := v.team(team)
		return RosterStatusRostered, nil, &ref
	}
	return RosterStatus(status), clears, nil
}

// owners maps each player on a roster for the gameweek roster moves are
// for (the upcoming one; the last after the final deadline) to his team.
// While the draft runs (rosters only exist once it completes) the picks
// made so far count as owned.
func owners(v *view) map[string]string {
	own := v.g.OwnerAt(min(v.w.UpcomingGW(v.now), 38))
	if d := v.st.Draft; d != nil && d.Status != league.DraftComplete {
		for _, p := range d.Picks {
			own[p.PlayerID] = p.TeamID
		}
	}
	return own
}

func encodeCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte("o:" + strconv.Itoa(offset)))
}

func decodeCursor(c *string) (int, bool) {
	if c == nil || *c == "" {
		return 0, true
	}
	b, err := base64.RawURLEncoding.DecodeString(*c)
	if err != nil {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(string(b), "o:"))
	if err != nil || !strings.HasPrefix(string(b), "o:") || n < 0 {
		return 0, false
	}
	return n, true
}

func limitOf(l *int) (int, string) {
	if l == nil {
		return 50, ""
	}
	if *l < 1 || *l > 100 {
		return 0, "limit must be between 1 and 100."
	}
	return *l, ""
}

func (s *Server) ListLeaguePlayers(ctx context.Context, req ListLeaguePlayersRequestObject) (ListLeaguePlayersResponseObject, error) {
	q := req.Params
	status := ListLeaguePlayersParamsStatusAll
	if q.Status != nil {
		status = *q.Status
	} else if q.Available != nil && *q.Available {
		status = ListLeaguePlayersParamsStatusAvailable
	}
	sort := ListLeaguePlayersParamsSortTotalPoints
	if q.Sort != nil {
		sort = *q.Sort
	}
	order := Desc
	if q.Order != nil {
		order = *q.Order
	}
	limit, limitErr := limitOf(q.Limit)
	offset, cursorOK := decodeCursor(q.Cursor)
	var d string
	switch {
	case badID("leagueId", "lg", req.LeagueId) != "":
		d = badID("leagueId", "lg", req.LeagueId)
	case !status.Valid():
		d = "status must be all, available, free_agent, waivers or rostered."
	case !sort.Valid():
		d = "sort is not a known sort; see the contract's list."
	case !order.Valid():
		d = "order must be desc or asc."
	case q.Position != nil && !q.Position.Valid():
		d = "position must be GKP, DEF, MID or FWD."
	case q.TeamId != nil && tm(*q.TeamId) != "":
		d = tm(*q.TeamId)
	case q.Club != nil && (len(*q.Club) < 2 || len(*q.Club) > 4):
		d = "club must be a 2 to 4 letter club code."
	case q.Q != nil && (len(*q.Q) < 1 || len(*q.Q) > 64):
		d = "q must be 1 to 64 characters."
	case q.Tag != nil && !q.Tag.Valid():
		d = "tag must be one of hot, cold, target, sell and avoid."
	case limitErr != "":
		d = limitErr
	case !cursorOK:
		d = "cursor is not valid; pass meta.nextCursor back unchanged."
	}
	if d != "" {
		return ListLeaguePlayers400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[ListLeaguePlayersdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	cur := v.current()
	raw, err := s.stats.PlayerGameweeks(ctx, 0)
	if err != nil {
		return nil, err
	}
	totals := withLineCounts(v.w, seasonTotals(v.w), raw)
	lines := sumLines(raw)
	own := owners(v)
	var watched map[string]bool
	if q.Watchlist != nil && *q.Watchlist {
		entries, err := s.watchlist.List(ctx, userFrom(ctx), req.LeagueId)
		if err != nil {
			return nil, err
		}
		watched = map[string]bool{}
		for _, e := range entries {
			watched[e.PlayerID] = true
		}
	}
	var tagged map[string]bool
	if q.Tag != nil {
		entries, err := s.tags.List(ctx, userFrom(ctx), req.LeagueId)
		if err != nil {
			return nil, err
		}
		tagged = map[string]bool{}
		for _, e := range entries {
			if e.Tag == string(*q.Tag) {
				tagged[e.PlayerID] = true
			}
		}
	}
	var items []PlayerListItem
	sortVals := map[string]*float64{}
	for id, pl := range v.w.Players {
		if watched != nil && !watched[id] {
			continue
		}
		if tagged != nil && !tagged[id] {
			continue
		}
		if q.Position != nil && string(pl.Position) != string(*q.Position) {
			continue
		}
		if q.Club != nil && !strings.EqualFold(v.w.Clubs[pl.ClubID].Code, *q.Club) {
			continue
		}
		if q.TeamId != nil && own[id] != *q.TeamId {
			continue
		}
		if q.Q != nil {
			needle := strings.ToLower(*q.Q)
			if !strings.Contains(strings.ToLower(pl.DisplayName), needle) &&
				!strings.Contains(strings.ToLower(pl.FirstName+" "+pl.LastName), needle) {
				continue
			}
		}
		rs, clears, owner := rosterStatus(v, own, id)
		switch status {
		case ListLeaguePlayersParamsStatusAvailable:
			if rs == RosterStatusRostered {
				continue
			}
		case ListLeaguePlayersParamsStatusFreeAgent, ListLeaguePlayersParamsStatusWaivers, ListLeaguePlayersParamsStatusRostered,
			ListLeaguePlayersParamsStatusLocked:
			if string(rs) != string(status) {
				continue
			}
		}
		gwPts, _, _ := v.w.PlayerPoints(id, cur)
		t := totals[id]
		sortVals[id] = sortValue(sort, t, lines[id], gwPts, pl.DraftRank)
		items = append(items, PlayerListItem{
			Player: playerRef(v.w, id), Owner: owner, RosterStatus: rs, WaiverClearsAt: clears,
			TotalPoints: t.points, GameweekPoints: gwPts, Form: t.contractForm(), DraftRank: pl.DraftRank, Minutes: t.minutes,
			FormChange: t.contractFormChange(), SeasonStats: t.seasonStats(lines[id].xgi),
		})
	}
	for i := range items {
		items[i].SortValue = contractSortValue(sort, sortVals[items[i].Player.Id])
	}
	sortPlayers(items, sort, order, sortVals)
	page := []PlayerListItem{}
	if offset < len(items) {
		page = items[offset:min(offset+limit, len(items))]
	}
	// Only the page's rows get their fixtures (each club's built once).
	remaining := remainingMemo(v.w)
	for i := range page {
		page[i].NextFixtures = remaining(v.w.Players[page[i].Player.Id].ClubID)
	}
	var next *string
	if offset+limit < len(items) {
		c := encodeCursor(offset + limit)
		next = &c
	}
	state := v.w.State(cur)
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return ListLeaguePlayers200JSONResponse{Data: page, Meta: pageMeta(meta, next)}, nil
}

func (s *Server) GetLeaguePlayer(ctx context.Context, req GetLeaguePlayerRequestObject) (GetLeaguePlayerResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badID("playerId", "plr", req.PlayerId)); d != "" {
		return GetLeaguePlayer400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[GetLeaguePlayerdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	pl, ok := v.w.Players[req.PlayerId]
	if !ok {
		return fail[GetLeaguePlayerdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Player not found.")), nil
	}
	cur := v.current()
	totals := seasonTotals(v.w)
	t := totals[pl.ID]
	rs, clears, owner := rosterStatus(v, owners(v), pl.ID)
	lines, err := s.stats.PlayerGameweeksOf(ctx, pl.ID)
	if err != nil {
		return nil, err
	}
	lineOf := map[int]stats.PlayerGameweek{}
	for _, l := range lines {
		lineOf[l.Gameweek] = l
	}
	starts := sumLines(lines)[pl.ID].starts
	detail := PlayerDetail{
		Player: playerRef(v.w, pl.ID), Season: stats.Season, Owner: owner, RosterStatus: rs, WaiverClearsAt: clears,
		TotalPoints: t.points, Form: t.contractForm(), DraftRank: pl.DraftRank, Minutes: t.minutes,
		Starts: starts, PositionRank: ptr(positionRank(v.w, totals, pl.ID)),
		Gameweeks: []PlayerGameweek{}, Upcoming: []UpcomingFixture{}, PreviousSeasons: []PlayerSeasonTotals{},
	}
	for gw := cur; gw >= 1; gw-- {
		if !v.w.Started(gw) || len(v.w.ClubFixtures(pl.ClubID, gw)) == 0 {
			continue
		}
		pts, mins, _ := v.w.PlayerPoints(pl.ID, gw)
		fixtures := playerFixtures(v.w, pl.ID, gw)
		var line *stats.PlayerGameweek
		if l, ok := lineOf[gw]; ok {
			line = &l
		}
		detail.Gameweeks = append(detail.Gameweeks, PlayerGameweek{
			Gameweek: gw, Points: pts, Minutes: mins, Fixtures: fixtures, Stats: gameStats(fixtures, line, v.w.Gameweeks[gw].Finished || v.w.Gameweeks[gw].DataChecked),
		})
	}
	detail.Upcoming = remainingFixtures(v.w, pl.ClubID)
	if detail.PreviousSeasons, err = s.previousSeasons(ctx, pl.ID); err != nil {
		return nil, err
	}
	meta, err := s.statsMeta(ctx, nil)
	if err != nil {
		return nil, err
	}
	return GetLeaguePlayer200JSONResponse{Data: detail, Meta: meta}, nil
}

// ListTransactions reads the activity feed (waiver runs, free-agent moves,
// processed and vetoed trades), newest first.
func (s *Server) ListTransactions(ctx context.Context, req ListTransactionsRequestObject) (ListTransactionsResponseObject, error) {
	q := req.Params
	limit, limitErr := limitOf(q.Limit)
	offset, cursorOK := decodeCursor(q.Cursor)
	d := cmp.Or(badID("leagueId", "lg", req.LeagueId), limitErr)
	switch {
	case d != "":
	case q.Kind != nil && !q.Kind.Valid():
		d = "kind must be waiver, free_agent, trade or draft_autopick."
	case !cursorOK:
		d = "cursor is not valid; pass meta.nextCursor back unchanged."
	}
	if d != "" {
		return ListTransactions400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
	}
	l, err := s.leagues.Get(ctx, userFrom(ctx), req.LeagueId)
	if err != nil {
		return failLeague[ListTransactionsdefaultApplicationProblemPlusJSONResponse](ctx, err)
	}
	all, err := s.leagues.Repo().Transactions(ctx, req.LeagueId)
	if err != nil {
		return nil, err
	}
	var items []league.Txn
	for _, x := range all {
		if q.Kind == nil || string(*q.Kind) == x.Kind {
			items = append(items, x)
		}
	}
	page := []Transaction{}
	if offset < len(items) {
		w, err := league.SeasonWorld(ctx, s.stats)
		if err != nil {
			return failLeague[ListTransactionsdefaultApplicationProblemPlusJSONResponse](ctx, err)
		}
		n, err := s.namesFor(ctx, l)
		if err != nil {
			return nil, err
		}
		for _, x := range items[offset:min(offset+limit, len(items))] {
			body, err := transactionBody(l, w, n, x)
			if err != nil {
				return nil, err
			}
			page = append(page, body)
		}
	}
	var next *string
	if offset+limit < len(items) {
		c := encodeCursor(offset + limit)
		next = &c
	}
	return ListTransactions200JSONResponse{Data: page, Meta: pageMeta(metaAt(s.now()), next)}, nil
}

// ---------------------------------------------------------------- preview

// previewSide is a scheduled side's squad as it stands: the viewer's own
// team in the lineup set for gw, any other team in its latest locked lineup
// (lineups stay private until their deadline, and none locked before the
// league's first gameweek), both on the roster for gw.
// A lineup that no longer matches that roster falls back to the default
// order. Fixtures are the club's in gw, kickoffs as currently scheduled.
func previewSide(v *view, viewer, teamID string, gw int) PreviewSide {
	if teamID == "" {
		return PreviewSide{Kind: PreviewSideKindAverage, Starters: []PreviewSlot{}, Bench: []PreviewSlot{}}
	}
	team, _ := v.l.Team(teamID)
	ref := teamRef(team, v.names)
	side := PreviewSide{Kind: PreviewSideKindTeam, Team: &ref, Starters: []PreviewSlot{}, Bench: []PreviewSlot{}}
	roster := v.g.RosterAt(teamID, gw)
	if len(roster) == 0 {
		return side
	}
	src := gw
	if team.OwnerID == "" || team.OwnerID != viewer {
		// The last gameweek whose deadline has passed (0 before the first
		// deadline: nothing has locked, so nobody else's lineup shows).
		src = min(gw, upcomingGW(v.w, v.now)-1)
		if g, ok := v.w.Gameweeks[gw]; ok && !v.now.Before(g.DeadlineAt) {
			src = gw
		}
	}
	// Before the league's first gameweek no lineup has locked: the default
	// order, and no lineup gameweek.
	lineupGW := &src
	var slots []string
	if src >= v.l.Settings.StartGameweek {
		slots = league.EffectiveLineup(v.g, v.w, teamID, src).Slots
	} else {
		lineupGW = nil
	}
	if !sameIDs(slots, roster) {
		slots = league.DefaultLineup(roster, v.w.Players)
	}
	rules := scoring.StandardLineup()
	counts := scoring.Counts{}
	for i, id := range slots {
		p := v.w.Players[id]
		ps := PreviewSlot{Slot: i + 1, Player: playerRef(v.w, id), Fixtures: []PreviewFixture{}}
		for _, f := range v.w.ClubFixtures(p.ClubID, gw) {
			if f.KickoffAt == nil {
				continue
			}
			home := f.HomeClubID == p.ClubID
			opp := f.AwayClubID
			if !home {
				opp = f.HomeClubID
			}
			ps.Fixtures = append(ps.Fixtures, PreviewFixture{
				FixtureId: f.ID, Opponent: clubRef(v.w, opp), IsHome: home, KickoffAt: f.KickoffAt.UTC(),
			})
		}
		if i < rules.Starters {
			counts[p.Position]++
			side.Starters = append(side.Starters, ps)
		} else {
			side.Bench = append(side.Bench, ps)
		}
	}
	formation := scoring.Formation(counts)
	side.Formation, side.LineupGameweek = &formation, lineupGW
	return side
}

func sameIDs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

func (s *Server) GetMatchupPreview(ctx context.Context, req GetMatchupPreviewRequestObject) (GetMatchupPreviewResponseObject, error) {
	if d := cmp.Or(badID("leagueId", "lg", req.LeagueId), badGameweek(req.Gw), badID("matchupId", "mu", req.MatchupId)); d != "" {
		return fail[GetMatchupPreviewdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, req.LeagueId)
	if p != nil || err != nil {
		return fail[GetMatchupPreviewdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	ms := v.g.MatchupsIn(req.Gw)
	i := slices.IndexFunc(ms, func(m league.Matchup) bool { return m.ID == req.MatchupId })
	if i < 0 {
		return fail[GetMatchupPreviewdefaultApplicationProblemPlusJSONResponse](notFound(ctx, "Matchup not found in this gameweek.")), nil
	}
	m, viewer := ms[i], userFrom(ctx)
	out := MatchupPreview{
		Id: m.ID, Gameweek: req.Gw,
		Home: previewSide(v, viewer, m.HomeTeamID, req.Gw),
		Away: previewSide(v, viewer, m.AwayTeamID, req.Gw),
	}
	return GetMatchupPreview200JSONResponse{Data: out, Meta: metaAt(s.now())}, nil
}
