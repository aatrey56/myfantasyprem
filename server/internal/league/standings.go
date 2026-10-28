package league

import (
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// StandingRow is one real team's row. The league-average side never has
// one; its results count for the team it played.
type StandingRow struct {
	scoring.Standing
	Rank   int
	Played int
	// Form is the last five results, oldest first.
	Form []scoring.Result
}

// Completed reports whether gw counts in the standings: every fixture has
// finished, provisional bonus or not (owner decision 2026-10-25: FPL's
// provisional bonus comes from BPS and is final in practice; a later change
// rescores the matchups and the derived table follows).
func (w *World) Completed(gw int) bool {
	if w.Gameweeks[gw].Finished {
		return true
	}
	switch w.State(gw) {
	case scoring.StateProvisional, scoring.StateOfficial, scoring.StateFinal:
		return true
	}
	return false
}

// Standings derives the table from the materialised matchups of completed
// gameweeks: win 3, draw 1, then points for (ADR 0009: derived, not
// stored). Every real team has a row, played or not.
func Standings(l *League, g *Game, w *World) []StandingRow { return StandingsThrough(l, g, w, 38) }

// StandingsThrough is the table as it stood after gameweek last: only the
// completed gameweeks up to it count.
func StandingsThrough(l *League, g *Game, w *World, last int) []StandingRow {
	rows := map[string]*StandingRow{}
	var order []string
	for _, t := range l.Teams {
		rows[t.ID] = &StandingRow{Standing: scoring.Standing{TeamID: t.ID}}
		order = append(order, t.ID)
	}
	ms := slices.Clone(g.Matchups)
	slices.SortStableFunc(ms, func(a, b Matchup) int { return a.Gameweek - b.Gameweek })
	record := func(teamID string, mine, theirs int) {
		r, ok := rows[teamID]
		if !ok {
			return
		}
		res := scoring.H2H(mine, theirs)
		switch res {
		case scoring.Win:
			r.Wins++
		case scoring.Draw:
			r.Draws++
		default:
			r.Losses++
		}
		r.Played++
		r.PointsFor += mine
		r.PointsAgainst += theirs
		r.Form = append(r.Form, res)
	}
	for _, m := range ms {
		if m.Gameweek > last || !w.Completed(m.Gameweek) {
			continue
		}
		if m.HomeTeamID != "" {
			record(m.HomeTeamID, m.HomePoints, m.AwayPoints)
		}
		if m.AwayTeamID != "" {
			record(m.AwayTeamID, m.AwayPoints, m.HomePoints)
		}
	}
	plain := make([]scoring.Standing, 0, len(order))
	for _, id := range order {
		plain = append(plain, rows[id].Standing)
	}
	scoring.SortStandings(plain)
	out := make([]StandingRow, 0, len(plain))
	for i, s := range plain {
		r := *rows[s.TeamID]
		r.Rank = i + 1
		if len(r.Form) > 5 {
			r.Form = r.Form[len(r.Form)-5:]
		}
		if r.Form == nil {
			r.Form = []scoring.Result{}
		}
		out = append(out, r)
	}
	return out
}

// RoundRobin builds an H2H schedule for gameweeks start..end: the circle
// method, repeated. With an odd number of teams one team per gameweek
// plays the league average (an empty team id). Home and away swap every
// other cycle so each pairing alternates.
func RoundRobin(teamIDs []string, start, end int) []Matchup {
	sides := slices.Clone(teamIDs)
	if len(sides)%2 == 1 {
		sides = append(sides, "")
	}
	n := len(sides)
	if n < 2 || end < start {
		return nil
	}
	var out []Matchup
	for gw := start; gw <= end; gw++ {
		round := (gw - start) % (n - 1)
		cycle := (gw - start) / (n - 1)
		// Rotate everyone but the first side by `round` places.
		rot := make([]string, n)
		rot[0] = sides[0]
		for i := 1; i < n; i++ {
			rot[i] = sides[1+(i-1+round)%(n-1)]
		}
		for i := 0; i < n/2; i++ {
			home, away := rot[i], rot[n-1-i]
			if (round+i+cycle)%2 == 1 {
				home, away = away, home
			}
			if home == "" { // the average side is always listed away
				home, away = away, home
			}
			out = append(out, Matchup{ID: ids.New("mu"), Gameweek: gw, HomeTeamID: home, AwayTeamID: away, State: scoring.StateUpcoming})
		}
	}
	return out
}
