// Package lineup sets a team's XI and bench order for a gameweek and every
// later one until changed (league.Game.SetLineup), with FPL's formation
// rules (scoring.LineupRules: exactly one goalkeeper, at least three
// defenders, two midfielders and one forward, the backup goalkeeper first
// on the bench) and the deadline lock: a gameweek's lineup cannot change
// once its deadline has passed. A locked gameweek keeps the lineup in force
// at the deadline; lineups carry forward because a lineup applies until the
// team's next one. Other teams' lineups stay private until the deadline
// (enforced by the read in httpapi). Depends on league, scoring and stats,
// never on net/http.
package lineup
