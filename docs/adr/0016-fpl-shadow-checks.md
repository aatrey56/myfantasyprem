# 0016: Automatic read-only checks against FPL (shadow checks)

Status: accepted, 2026-10-24

## Context

The owner's league is imported from FPL Draft (ADR 0012) and played in
both places until the switch-over in December or January. Before the
league leaves FPL, trades, waivers and scoring here must be shown to come
out the same as FPL's. "Check against FPL" does that on request and
"Re-sync from FPL" resets the league (destructive); neither runs by
itself, and a check during a live match shows differences that are only
FPL lagging.

## Decision

A shadow check (`mirror.Service.ShadowCheck`) fetches FPL like a check
(league details, element status, transactions, trades, each entry's picks
up to the current gameweek, cached once finished) plus the gameweek's
live points, and diffs it with the league as stored, without writing
league state. `mirror.Shadow` is pure. It reuses `Verify` (points,
results, auto-subs, standings, squads, ownership, waiver order, pending
trades) and adds:

- `waiver`: for each gameweek our engine ran waivers, every FPL claim's
  outcome (won/lost) against ours by team and players, and our decided
  claims FPL has no record of. While the league is still played on FPL
  (before `FPL_SHADOW_UNTIL`) its claims are made there, so for a
  gameweek with no claims made here (pending or decided) each FPL claim's
  outcome (its transaction's result) is checked against the transaction
  the sync imported from it (same FPL id, team, players and gameweek),
  not against our rosters: a later move in the same gameweek (the player
  dropped again, a chained claim, the player out dropped by another move)
  changes those. A claim not imported is a mismatch;
- `trade`: each closed FPL trade's status against the same trade here;
- `lineup`: each locked lineup, slot by slot (auto-subs take starters in
  XI order), against the FPL picks. Once FPL has made its auto-subs its
  picks come back reordered and the plan rebuilds the order its subs
  imply, which the next re-sync stores. A lineup with FPL's final XI in
  another order and the bench in the same order (it decides auto-sub
  priority) is rescored in FPL's order, without writing. Only if that
  gives FPL's auto-subs and points are the lineup, subs and points
  `pending` ("re-sync stores FPL's final order"), and a result or our
  standing only if the same rescore gives FPL's. Anything else, such as a
  wrong stats row or another bench order, stays a `mismatch`;
- `player_points`: each starter's points in the current gameweek against
  FPL's live points, and the team totals against FPL's match points.

**Lag.** A difference on data FPL has not finalized is `pending`: a
gameweek is final once every fixture is over and bonus is added (or the
data checked); a player once his club's fixtures are finished and his
bonus confirmed. Only differences on final data are `mismatch`.

**Schedule.** An hourly River job (`fpl_shadow_plan`) schedules
`fpl_shadow_check` jobs for the next 36 hours from the stored FPL
calendar (`mirror.ShadowTimes`): waiver deadline +10 min, deadline −15
and +10 min, each kickoff, +55 min (half time), +115 min (full time) and
+175 min (bonus), and once after a gameweek is marked data checked
(`DataCheckedSlot`, fixed at its last kickoff so it is scheduled once).
Slots at the same moment run once (River's unique-by-arguments; the
service also remembers what it scheduled for the in-memory scheduler).
A typical gameweek has 20–30 slots; each costs about 6 requests plus one
per seat for the current picks, at the FPL client's pace (MinInterval),
so a few hundred requests a gameweek per league. `FPL_SHADOW_UNTIL`
ends them.

**Reports and alerts.** Each report goes to `mirror_shadow_checks`
(memory and Postgres stores, one storetest suite; newest 200 per league).
`GET /v1/leagues/{id}/sync` gives the commissioner the latest as
`shadow`. A report with mismatches sends the commissioner one
`fpl_check_mismatch` notification, keyed by the set of mismatches and
their values, so finding the same differences again sends nothing. The
type has no settings switch. `npm run admin -- shadow-check -league <id>`
runs one now.

## Consequences

- The comparison is only as good as members mirroring their moves in
  both places: a claim made in only one place is a `waiver` mismatch, by
  design (it says the two leagues diverged).
- A failed shadow check is logged and not retried; the next slot covers it.
- Pending differences are recomputed each run, not tracked over time: a
  lag that never resolves shows as a mismatch once the data is final.
