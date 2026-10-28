// Package projection holds the pure rules behind the app's forward-looking
// numbers (docs/adr/0014-projections.md): fixture difficulty, Expected
// points, win probability and start/sit accuracy. Nothing here is a
// forecast model: every number is arithmetic on real past points.
//
//   - Difficulty ranks the 20 clubs, per position, by the points they
//     conceded to that position over the last RecentWeeks completed
//     gameweeks (per match, so double and blank gameweeks compare fairly):
//     1st concedes least (hardest), 20th most (easiest).
//   - Expected is a player's form (statuspage.Form) times the fixture
//     factor of each of his matches in the gameweek (Factor).
//   - WinOdds resamples each starter's own recent gameweek points.
//   - OptimalXI is the best legal XI from a squad by actual points.
//
// The package does no I/O and reads no clock; httpapi gathers the inputs.
package projection
