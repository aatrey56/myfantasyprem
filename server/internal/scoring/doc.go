// Package scoring turns player match stats into fantasy points with FPL's
// 2026-27 rules, validates lineups and applies auto-subs. It is pure: no I/O,
// no clocks, no globals that change.
//
// Rules to uphold, from docs/adr/0005 and docs/adr/0008:
//   - Recompute from scratch whenever upstream data changes. A team score is a
//     pure function of the roster that gameweek, the lineup and the latest
//     stats. Never patch incrementally.
//   - Never produce zero scores because of a bad upstream response; the
//     caller keeps the last good snapshot and never calls in here with it.
//   - Score states move live, provisional, official, final.
//   - Golden tests compare against FPL's own explain data (when the poller
//     lands); the table tests here pin the documented rules.
//
// Files: points.go (per-fixture points), bonus.go (bonus from BPS),
// lineup.go (formation rules), autosub.go (auto-subs), team.go (team totals,
// H2H results and the league-average opponent).
package scoring
