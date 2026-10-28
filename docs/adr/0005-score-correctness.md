# 0005. Score correctness

Status: accepted

## Context

Scores are the product. Users compare against FPL and against livefpl.net.
Competitors lost trust by showing stale data and by results changing a day
later. FPL data arrives provisionally (bonus is added after matches) and can
be corrected afterwards.

## Decision

- **Recompute from scratch.** A team's score is a pure function of the roster
  that gameweek, the lineup and the latest points data. We never patch scores
  incrementally.
- **Golden tests.** FPL's `explain` data from `event/{gw}/live` is the
  expected answer. The scoring code must reproduce it exactly.
- **Score states:** `live` (matches in progress) to `provisional` (matches
  done, bonus not yet added) to `official` (bonus added) to `final` (FPL has
  checked the data). The state is in `meta.scoreState` and per matchup.
- **Standings count a gameweek once every match is over** (owner decision
  2026-10-25). A `provisional` gameweek counts with provisional bonus, which
  FPL computes from BPS and is final in practice; the app shows it as final
  and does not mark provisional bonus. A later bonus or stats change rescores
  the matchups (reported as `score.corrected`), and the derived standings
  follow. The FPL shadow checks still treat pre-bonus data as pending.
- **Bonus is provisional until FPL confirms it** (2026-27 notes, added
  2026-10-12). FPL now adds its own projected bonus to live points after 20
  minutes of a match and revises it until confirmation, so a bonus value
  upstream proves nothing. A match's bonus is confirmed when FPL marks the
  fixture `finished`, `/pl/event-status` has `bonus_added` for its day, or
  the gameweek's bonus is added. Until then we project it ourselves from
  each player's BPS in that match (the fixture's `stats`, so doubles rank
  per match) with FPL's tie rules, from 20 minutes in, and mark it
  provisional. If a player who played has no known BPS in that match (a
  double without per-fixture data that the gameweek total cannot settle),
  no ranking of it is complete, so the match keeps FPL's own projection,
  still provisional.
- **The 09:00 lock.** FPL locks a gameweek's scores at 09:00 UK time on the
  day after its last match. `final` still follows FPL's `data_checked`
  flag; the poller wakes at the lock and polls every 10 minutes for two
  hours after it so `official` turns `final` promptly. FPL publishes
  `data_checked` only in bootstrap, so every poll in this mode refetches
  bootstrap.
- **FPL Draft: finished is final** (added 2026-10-13). Draft's events have
  no `data_checked`, so with that flag alone a Draft gameweek never turned
  `final` and the poller stayed in unchecked mode for good (bootstrap
  every 10 minutes for two hours after the lock, then every 6 hours). For
  the Draft source an event's `finished: true` (bootstrap's event, or
  `/game`'s `current_event_finished` for the current one) counts as
  `data_checked`. The gameweek turns `final` and the poller goes idle, but
  still fetches the gameweek's live data each cycle; a later change to it
  is ingested, rescored and reported as `score.corrected` like any other
  correction.
- **Corrections** recompute and emit a "score corrected" event.
- **Bad data never zeroes a score.** Keep the last good snapshot. After 3
  minutes without a good poll, mark responses `meta.stale = true`.
- **Idempotent jobs.** Polls, recomputes and syncs can run twice without
  changing the result.
- Auto-subs are projected while fixtures are running and confirmed at the end
  of the gameweek, in bench order, keeping the formation legal. A fixture
  counts as done only when all of a club's fixtures are done, so double
  gameweeks work. A fixture's status change rescores every league with a
  player from either club on a roster, so a 0-minute starter is projected
  out as soon as his match ends.

## Consequences

- Recomputing costs more CPU than patching. At league scale this is cheap, and
  correctness is worth it.
- We need a replay test harness over past gameweeks before running a league
  natively.
- Clients must handle `stale` and changing `scoreState`. Each screen shows
  one freshness banner from its own query: offline, server unreachable, or
  FPL data delayed (from `meta.stale` or a serverTime-adjusted age past
  3 minutes). The data stays on screen.
- With an odd team count one side of a matchup is the league average
  (floor of the mean of all real teams' points, including its opponent). It
  has a score but no lineup and never appears in standings.
