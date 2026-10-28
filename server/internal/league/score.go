package league

import (
	"cmp"
	"context"
	"log/slog"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// World is the tier-1 data that scoring and the league views read,
// indexed for lookups. Load it once per request or recompute.
type World struct {
	Status    stats.Status
	Clubs     map[string]stats.Club
	Players   map[string]stats.Player
	Gameweeks map[int]stats.Gameweek
	Fixtures  []stats.Fixture
	// Rows are player-fixture rows by gameweek, then player.
	Rows map[int]map[string][]stats.PlayerFixture

	byGW map[int][]stats.Fixture
}

// LoadWorld reads tier-1 data. With gameweeks given only their rows are
// loaded; otherwise every row is.
func LoadWorld(ctx context.Context, repo stats.Repo, gameweeks ...int) (*World, error) {
	w := &World{
		Clubs: map[string]stats.Club{}, Players: map[string]stats.Player{}, Gameweeks: map[int]stats.Gameweek{},
		Rows: map[int]map[string][]stats.PlayerFixture{}, byGW: map[int][]stats.Fixture{},
	}
	st, _, err := repo.Status(ctx)
	if err != nil {
		return nil, err
	}
	w.Status = st
	if w.Status.Current == 0 {
		w.Status.Current = 1
	}
	clubs, err := repo.Clubs(ctx)
	if err != nil {
		return nil, err
	}
	for _, c := range clubs {
		w.Clubs[c.ID] = c
	}
	players, err := repo.Players(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range players {
		w.Players[p.ID] = p
	}
	gws, err := repo.Gameweeks(ctx)
	if err != nil {
		return nil, err
	}
	for _, g := range gws {
		w.Gameweeks[g.Number] = g
	}
	if w.Fixtures, err = repo.Fixtures(ctx); err != nil {
		return nil, err
	}
	w.IndexFixtures()
	if len(gameweeks) == 0 {
		gameweeks = []int{0}
	}
	for _, gw := range gameweeks {
		rows, err := repo.PlayerFixtures(ctx, gw)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			m := w.Rows[r.Gameweek]
			if m == nil {
				m = map[string][]stats.PlayerFixture{}
				w.Rows[r.Gameweek] = m
			}
			m[r.PlayerID] = append(m[r.PlayerID], r)
		}
	}
	return w, nil
}

// IndexFixtures rebuilds the per-gameweek fixture index from Fixtures
// (LoadWorld does it; a World built by hand calls it).
func (w *World) IndexFixtures() {
	w.byGW = map[int][]stats.Fixture{}
	for _, f := range w.Fixtures {
		if f.Gameweek > 0 && f.Status != stats.FixturePostponed {
			w.byGW[f.Gameweek] = append(w.byGW[f.Gameweek], f)
		}
	}
}

// FixturesIn returns a gameweek's fixtures in kickoff order (postponed
// ones excluded: they count in the gameweek they are replayed).
func (w *World) FixturesIn(gw int) []stats.Fixture { return w.byGW[gw] }

// ClubFixtures returns a club's fixtures in a gameweek: none in a blank,
// two in a double.
func (w *World) ClubFixtures(clubID string, gw int) []stats.Fixture {
	var out []stats.Fixture
	for _, f := range w.byGW[gw] {
		if f.HomeClubID == clubID || f.AwayClubID == clubID {
			out = append(out, f)
		}
	}
	return out
}

// State is the gameweek's score state (ADR 0005).
func (w *World) State(gw int) scoring.ScoreState {
	var progress []scoring.FixtureProgress
	for _, f := range w.byGW[gw] {
		progress = append(progress, scoring.FixtureProgress{Started: f.Started(), Finished: f.Over()})
	}
	g := w.Gameweeks[gw]
	return scoring.StateOf(progress, g.BonusAdded, g.DataChecked)
}

// Started reports whether any fixture of the gameweek has kicked off.
func (w *World) Started(gw int) bool { return w.State(gw) != scoring.StateUpcoming }

// PlayerPoints sums a player's points and minutes in a gameweek.
func (w *World) PlayerPoints(playerID string, gw int) (points, minutes int, provisional bool) {
	for _, r := range w.Rows[gw][playerID] {
		points += r.Points
		minutes += r.Stats.Minutes
		provisional = provisional || r.BonusProvisional
	}
	return points, minutes, provisional
}

// Booked reports whether a player was shown a card in a gameweek, which
// counts as playing for auto-subs even with 0 minutes.
func (w *World) Booked(playerID string, gw int) bool {
	for _, r := range w.Rows[gw][playerID] {
		if r.Stats.YellowCards > 0 || r.Stats.RedCards > 0 {
			return true
		}
	}
	return false
}

// AutoSubState values (AutoSubState in the contract).
const (
	AutoSubNone         = "none"
	AutoSubProjectedOut = "projected_out"
	AutoSubProjectedIn  = "projected_in"
	AutoSubConfirmedOut = "confirmed_out"
	AutoSubConfirmedIn  = "confirmed_in"
)

// SlotResult is one lineup slot scored for a gameweek.
type SlotResult struct {
	Slot             int
	PlayerID         string
	Points           int
	Minutes          int
	BonusProvisional bool
	AutoSub          string
}

// TeamResult is a team's gameweek scored from scratch.
type TeamResult struct {
	TeamID      string
	Gameweek    int
	Lineup      Lineup // the lineup in force (possibly the default)
	Slots       []SlotResult
	Subs        []scoring.Sub
	Points      int
	BenchPoints int
	Formation   string
	State       scoring.ScoreState
}

// DefaultLineup orders a roster into a legal XI by draft rank: the best
// goalkeeper, four defenders, four midfielders and two forwards start; the
// other goalkeeper is first on the bench, then the rest by rank. It is
// used until the team saves a lineup, and when a saved one no longer
// matches the roster.
func DefaultLineup(roster []string, players map[string]stats.Player) []string {
	byPos := map[scoring.Position][]string{}
	for _, id := range roster {
		pos := players[id].Position
		byPos[pos] = append(byPos[pos], id)
	}
	for _, ids := range byPos {
		slices.SortFunc(ids, func(a, b string) int {
			return cmp.Or(cmp.Compare(players[a].DraftRank, players[b].DraftRank), cmp.Compare(a, b))
		})
	}
	take := func(pos scoring.Position, n int) []string {
		n = min(n, len(byPos[pos]))
		out := byPos[pos][:n]
		byPos[pos] = byPos[pos][n:]
		return out
	}
	starters := slices.Concat(take(scoring.GKP, 1), take(scoring.DEF, 4), take(scoring.MID, 4), take(scoring.FWD, 2))
	bench := take(scoring.GKP, len(byPos[scoring.GKP]))
	var rest []string
	for _, pos := range []scoring.Position{scoring.DEF, scoring.MID, scoring.FWD} {
		rest = append(rest, byPos[pos]...)
	}
	slices.SortFunc(rest, func(a, b string) int {
		return cmp.Or(cmp.Compare(players[a].DraftRank, players[b].DraftRank), cmp.Compare(a, b))
	})
	return slices.Concat(starters, bench, rest)
}

// EffectiveLineup is the team's lineup in force for gw: its saved lineup
// when it holds exactly the roster, else that lineup repaired for the
// roster (RepairLineup) when the result is legal, otherwise DefaultLineup.
func EffectiveLineup(g *Game, w *World, teamID string, gw int) Lineup {
	roster := g.RosterAt(teamID, gw)
	l, ok := g.LineupAt(teamID, gw)
	if ok && sameSet(l.Slots, roster) {
		return l
	}
	if ok {
		if slots, fixed := RepairLineup(l.Slots, roster, w.Players); fixed && legalLineup(w, roster, slots) {
			return Lineup{TeamID: teamID, FromGW: l.FromGW, Slots: slots, UpdatedAt: l.UpdatedAt}
		}
	}
	return Lineup{TeamID: teamID, FromGW: gw, Slots: DefaultLineup(roster, w.Players)}
}

func legalLineup(w *World, roster, slots []string) bool {
	rules := scoring.StandardLineup()
	if len(slots) < rules.Starters {
		return false
	}
	pos := map[string]scoring.Position{}
	for _, id := range roster {
		pos[id] = w.Players[id].Position
	}
	return len(rules.ValidateLineup(pos, slots[:rules.Starters], slots[rules.Starters:])) == 0
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// ScoreTeam scores a team's gameweek from scratch: per-player points from
// the stats, auto-subs (projected while the gameweek runs, confirmed once
// every fixture is over), the total and the bench points.
func ScoreTeam(rules scoring.LineupRules, g *Game, w *World, teamID string, gw int) TeamResult {
	lineup := EffectiveLineup(g, w, teamID, gw)
	state := w.State(gw)
	res := TeamResult{TeamID: teamID, Gameweek: gw, Lineup: lineup, State: state}
	var starters, bench []scoring.LineupPlayer
	for i, id := range lineup.Slots {
		pts, mins, prov := w.PlayerPoints(id, gw)
		done := true
		for _, f := range w.ClubFixtures(w.Players[id].ClubID, gw) {
			done = done && f.Over()
		}
		lp := scoring.LineupPlayer{PlayerID: id, Position: w.Players[id].Position, Minutes: mins, Booked: w.Booked(id, gw),
			FixturesDone: done, Points: pts}
		if i < rules.Starters {
			starters = append(starters, lp)
		} else {
			bench = append(bench, lp)
		}
		res.Slots = append(res.Slots, SlotResult{Slot: i + 1, PlayerID: id, Points: pts, Minutes: mins, BonusProvisional: prov, AutoSub: AutoSubNone})
	}
	gwDone := state == scoring.StateProvisional || state == scoring.StateOfficial || state == scoring.StateFinal
	if state != scoring.StateUpcoming {
		res.Subs = rules.AutoSubs(starters, bench, gwDone)
	}
	res.Points, res.BenchPoints = scoring.TeamTotal(starters, bench, res.Subs)
	counts := scoring.Counts{}
	for _, s := range starters {
		counts[s.Position]++
	}
	for _, sub := range res.Subs {
		// The formation is the XI that scores, after auto-subs.
		counts[starters[sub.OutSlot-1].Position]--
		counts[bench[sub.InSlot-len(starters)-1].Position]++
		out, in := AutoSubProjectedOut, AutoSubProjectedIn
		if sub.Status == scoring.SubConfirmed {
			out, in = AutoSubConfirmedOut, AutoSubConfirmedIn
		}
		res.Slots[sub.OutSlot-1].AutoSub = out
		res.Slots[sub.InSlot-1].AutoSub = in
	}
	res.Formation = scoring.Formation(counts)
	return res
}

// TeamsWithRosters returns the ids of the league's teams that have players
// in gameweek gw: the real teams that score.
func TeamsWithRosters(l *League, g *Game, gw int) []string {
	var out []string
	for _, t := range l.Teams {
		if len(g.RosterAt(t.ID, gw)) > 0 {
			out = append(out, t.ID)
		}
	}
	return out
}

// ScoreGameweek scores every team and matchup of a league's gameweek.
// The average side of a matchup gets floor(mean of every real team's
// points), including its opponent's (ADR 0005).
func ScoreGameweek(rules scoring.LineupRules, l *League, g *Game, w *World, gw int) (map[string]TeamResult, []Matchup) {
	results := map[string]TeamResult{}
	var totals []int
	for _, id := range TeamsWithRosters(l, g, gw) {
		r := ScoreTeam(rules, g, w, id, gw)
		results[id] = r
		totals = append(totals, r.Points)
	}
	avg := scoring.AveragePoints(totals)
	side := func(teamID string) int {
		if teamID == "" {
			return avg
		}
		return results[teamID].Points
	}
	var matchups []Matchup
	for _, m := range g.MatchupsIn(gw) {
		m.HomePoints, m.AwayPoints, m.State = side(m.HomeTeamID), side(m.AwayTeamID), w.State(gw)
		matchups = append(matchups, m)
	}
	return results, matchups
}

// Scorer recomputes and stores league scores from tier-1 stats.
type Scorer struct {
	Leagues Repo
	Stats   stats.Repo
	Rules   scoring.LineupRules
	Log     *slog.Logger
	// Notify, if set, hears matchups.updated for every league whose scores
	// moved and score.corrected for every settled score that changed.
	Notify Notifier
	// Live, if set, hears every league it rescored, with the stats, to
	// turn live events into notifications.
	Live LiveHook
}

// LiveHook hears a league's rescored gameweek. It runs under the
// gameweek's scoring lock, after the scores are saved.
type LiveHook interface {
	LiveScored(ctx context.Context, l *League, g *Game, w *World, gw int)
}

// RecomputeResult summarises one recompute.
type RecomputeResult struct {
	Leagues      int          // leagues rescored
	Skipped      int          // leagues with nothing that moved
	ChangedTeams []string     // teams whose stored score changed
	Corrections  []Correction // settled scores that changed
}

// Correction is a settled team score (every match played: provisional,
// official or final) that a later stats or bonus correction changed.
type Correction struct {
	LeagueID, TeamID   string
	Gameweek           int
	Previous, Points   int
	PreviousState, Now scoring.ScoreState
}

// settled reports whether a score state counts in the standings: every
// match is played (provisional, official, final).
func settled(s scoring.ScoreState) bool {
	return s == scoring.StateProvisional || s == scoring.StateOfficial || s == scoring.StateFinal
}

// Recompute rescores gameweek gw in every in-season league, from scratch.
// A league is skipped when none of changedPlayers is on its rosters and
// its matchups' state has not moved; pass all = true to rescore anyway
// (corrections, a new gameweek, seeding). It is idempotent. It holds the
// gameweek's scoring lock (Repo.LockScores) from reading the stats to
// saving the scores.
func (s *Scorer) Recompute(ctx context.Context, gw int, changedPlayers []string, all bool) (RecomputeResult, error) {
	var res RecomputeResult
	err := s.Leagues.LockScores(ctx, gw, func() error {
		res = RecomputeResult{}
		w, err := LoadWorld(ctx, s.Stats, gw)
		if err != nil {
			return err
		}
		leagueIDs, err := s.Leagues.InSeasonLeagues(ctx)
		if err != nil {
			return err
		}
		changed := map[string]bool{}
		for _, id := range changedPlayers {
			changed[id] = true
		}
		for _, id := range leagueIDs {
			teams, fixes, err := s.recomputeLeague(ctx, id, gw, w, changed, all)
			if err != nil {
				return err
			}
			if teams == nil {
				res.Skipped++
				continue
			}
			res.Leagues++
			res.ChangedTeams = append(res.ChangedTeams, teams...)
			res.Corrections = append(res.Corrections, fixes...)
		}
		return nil
	})
	return res, err
}

// RescoreLeague rescores one league's gameweek from the stored stats,
// under the gameweek's scoring lock like Recompute. It returns the teams
// whose score changed and the settled scores that changed.
func (s *Scorer) RescoreLeague(ctx context.Context, leagueID string, gw int) (teams []string, fixes []Correction, err error) {
	err = s.Leagues.LockScores(ctx, gw, func() error {
		w, err := LoadWorld(ctx, s.Stats, gw)
		if err != nil {
			return err
		}
		teams, fixes, err = s.recomputeLeague(ctx, leagueID, gw, w, nil, true)
		return err
	})
	return teams, fixes, err
}

// recomputeLeague rescores one league's gameweek from w; the caller holds
// the gameweek's scoring lock. It returns nil teams when the league was
// skipped, otherwise the (possibly empty) changed teams and the settled
// scores that changed. With Notify set it reports matchups.updated when
// any score or state moved and score.corrected for each correction.
func (s *Scorer) recomputeLeague(ctx context.Context, leagueID string, gw int, w *World, changed map[string]bool, all bool) ([]string, []Correction, error) {
	l, err := s.Leagues.Get(ctx, leagueID)
	if err != nil || l == nil {
		return nil, nil, err
	}
	g, err := s.Leagues.Game(ctx, leagueID)
	if err != nil {
		return nil, nil, err
	}
	if !all && !touched(g, w, gw, changed) {
		return nil, nil, nil
	}
	results, matchups := ScoreGameweek(s.Rules, l, g, w, gw)
	scores := make([]TeamScore, 0, len(results))
	for _, r := range results {
		scores = append(scores, TeamScore{
			TeamID: r.TeamID, Gameweek: gw, Points: r.Points, BenchPoints: r.BenchPoints, AutoSubs: r.Subs, State: r.State,
		})
	}
	slices.SortFunc(scores, func(a, b TeamScore) int { return cmp.Compare(a.TeamID, b.TeamID) })
	teams, err := s.Leagues.SaveScores(ctx, leagueID, gw, scores, matchups)
	if err != nil {
		return nil, nil, err
	}
	if teams == nil {
		teams = []string{}
	}
	var fixes []Correction
	for _, sc := range scores {
		prev, ok := g.Score(sc.TeamID, gw)
		if ok && settled(prev.State) && prev.Points != sc.Points {
			fixes = append(fixes, Correction{LeagueID: leagueID, TeamID: sc.TeamID, Gameweek: gw,
				Previous: prev.Points, Points: sc.Points, PreviousState: prev.State, Now: sc.State})
		}
	}
	if s.Live != nil {
		s.Live.LiveScored(ctx, l, g, w, gw)
	}
	if s.Notify != nil {
		var changes []Change
		if len(teams) > 0 || matchupsMoved(g, matchups) {
			changes = append(changes, Change{LeagueID: leagueID, Kind: ChangeMatchups, Gameweek: gw})
		}
		for _, f := range fixes {
			changes = append(changes, Change{LeagueID: leagueID, Kind: ChangeScoreCorrected, Gameweek: gw,
				TeamID: f.TeamID, Previous: f.Previous, Points: f.Points})
		}
		if len(changes) > 0 {
			s.Notify.Notify(ctx, changes...)
		}
	}
	if s.Log != nil {
		for _, f := range fixes {
			s.Log.InfoContext(ctx, "score corrected", "league", leagueID, "team", f.TeamID, "gameweek", gw,
				"previous", f.Previous, "points", f.Points)
		}
	}
	return teams, fixes, nil
}

// matchupsMoved reports whether a recomputed matchup differs from the
// stored one.
func matchupsMoved(g *Game, now []Matchup) bool {
	for _, m := range now {
		for _, o := range g.Matchups {
			if o.ID == m.ID && (o.HomePoints != m.HomePoints || o.AwayPoints != m.AwayPoints || o.State != m.State) {
				return true
			}
		}
	}
	return false
}

// touched reports whether a changed player is on one of the league's
// rosters in gw, or the gameweek's state differs from the stored one.
func touched(g *Game, w *World, gw int, changed map[string]bool) bool {
	for _, r := range g.Rosters {
		if r.OnRosterAt(gw) && changed[r.PlayerID] {
			return true
		}
	}
	state := w.State(gw)
	for _, m := range g.MatchupsIn(gw) {
		if m.State != state {
			return true
		}
	}
	return false
}
