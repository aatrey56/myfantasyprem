// Package waivers runs waiver claims and free-agent moves (ADR 0008 rule
// 12, ADR 0010).
//
// The cycle, per gameweek G (the first gameweek whose deadline has not
// passed): from the previous deadline until G's waiver run (G's deadline −
// 24 h, processed by the jobs) every unowned player is on waivers and
// managers put in claims; after the run, until G's deadline, unowned
// players are free agents, first come first served, except players dropped
// since the run, who stay on waivers until the next one. An imported FPL
// Draft league's run is FPL's: its waiver time (events' waivers_time) ends
// the waiver period whether or not our jobs stamped the run, once claims
// made here are settled. A player added or
// dropped by a free-agent move (or dropped by a waiver claim) is locked
// until the next run. Every move swaps like for like (same position), so
// squads always stay 2/5/5/3, and takes effect from the gameweek it is for,
// with the saved lineup's slot handed to the new player.
//
// The run is deterministic and idempotent: teams in the order the league's
// rule gives (league.WaiverOrder: reverse standings or rolling) each get
// their first claim that can still succeed; a team that wins moves to the
// back for the rest of the run and the round restarts; claims that cannot
// succeed lose with a reason. Every processed claim (won or lost) joins the activity feed.
// Depends on league, scoring and stats, never on net/http.
package waivers
