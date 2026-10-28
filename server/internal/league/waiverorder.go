package league

import (
	"cmp"
	"slices"
	"time"
)

// Waiver order rules (WaiverOrder in the contract; ADR 0008 rule 12 as
// amended 2026-10-15).
const (
	// WaiverReverseStandings is FPL Draft's rule: every run starts from
	// the reverse of the standings after the last finished gameweek (the
	// reverse of the draft's first round before any), worst team first.
	WaiverReverseStandings = "reverse_standings"
	// WaiverRolling: the order starts as the reverse of the draft's first
	// round and a team that wins a claim stays at the back after the run.
	WaiverRolling = "rolling"
)

// WaiverRule is the league's waiver order rule. Leagues stored before the
// setting existed have none and keep the rolling order they were made with.
func (s Settings) WaiverRule() string { return cmp.Or(s.WaiverOrder, WaiverRolling) }

// WaiverOrder is the order gameweek gw's waiver run starts from, team ids
// first pick to last. Within the run a team that wins a claim moves to the
// back for the rest of it (the waivers package).
func WaiverOrder(st *State, w *World, gw int) []string {
	if st.League.Settings.WaiverRule() == WaiverReverseStandings {
		return ReverseStandings(st, w, gw)
	}
	return withEveryTeam(st.League, st.Priority)
}

// ReverseStandings is FPL Draft's order for gameweek gw's run: the
// standings after the finished gameweeks before gw, worst first. Ties go
// on league points, then points for (fewer picks first), then join order
// (the team that joined the league first picks first; FPL breaks them on
// its league-entry id, which follows join order). Before any of the
// league's gameweeks has finished it is the reverse of the draft's first
// round. Evidence: ADR 0008, rule 12 amendment.
func ReverseStandings(st *State, w *World, gw int) []string {
	l := st.League
	rows := StandingsThrough(l, st.Game, w, gw-1)
	if !slices.ContainsFunc(rows, func(r StandingRow) bool { return r.Played > 0 }) {
		return PreStandingsOrder(st)
	}
	joined := map[string]int{}
	for i, t := range l.Teams {
		joined[t.ID] = i
	}
	slices.SortStableFunc(rows, func(a, b StandingRow) int {
		return cmp.Or(cmp.Compare(a.LeaguePoints(), b.LeaguePoints()), cmp.Compare(a.PointsFor, b.PointsFor),
			cmp.Compare(joined[a.TeamID], joined[b.TeamID]))
	})
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.TeamID)
	}
	return out
}

// PreStandingsOrder is the order before any result: the reverse of the
// draft's first round (the order the draft's completion stores when the
// league has no draft row), then any other team in join order.
func PreStandingsOrder(st *State) []string {
	if st.Draft != nil && len(st.Draft.Order) > 0 {
		order := slices.Clone(st.Draft.Order)
		slices.Reverse(order)
		return withEveryTeam(st.League, order)
	}
	return withEveryTeam(st.League, st.Priority)
}

// withEveryTeam is order without teams that left the league, then the
// league's other teams in join order.
func withEveryTeam(l *League, order []string) []string {
	var out []string
	for _, id := range order {
		if _, ok := l.Team(id); ok && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, t := range l.Teams {
		if !slices.Contains(out, t.ID) {
			out = append(out, t.ID)
		}
	}
	return out
}

// NextWaiverRun is the gameweek of the league's next waiver run at now
// and whether it is due (its deadline passed, the run not done yet).
func NextWaiverRun(st *State, w *World, now time.Time) (gw int, due bool) {
	gw = w.UpcomingGW(now)
	if _, ran := st.Ran(RunWaivers, gw); ran {
		return gw + 1, false
	}
	g, ok := w.Gameweeks[gw]
	return gw, ok && !now.Before(g.WaiverDeadlineAt)
}

// errWaiversDue: the order cannot change while a run is due, so it is
// clear which rule that run used.
var errWaiversDue = errorf(KindConflict, "waivers-processing", "Waivers processing",
	"This gameweek's waiver run is due; change the waiver order after it has run.")

// switchWaiverOrder completes a change of the league's waiver order rule
// from prev to the one its settings now hold. The change takes effect at
// the next waiver run: in season it cannot happen while a run is due, and
// a switch to rolling starts from the order that run would have used
// under reverse standings.
func switchWaiverOrder(st *State, w *World, now time.Time, prev string) error {
	next := st.League.Settings.WaiverRule()
	if next == prev || st.League.Status != StatusInSeason {
		return nil
	}
	gw, due := NextWaiverRun(st, w, now)
	if due {
		return errWaiversDue
	}
	if next == WaiverRolling {
		st.Priority = ReverseStandings(st, w, gw)
	}
	return nil
}
