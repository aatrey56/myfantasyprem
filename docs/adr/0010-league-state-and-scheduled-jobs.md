# 0010. League state aggregate and scheduled jobs on River

Status: accepted (2026-10-12). Extends 0002 and 0009. Amended 2026-10-14:
the tick no longer skips imported leagues (they are playable at once,
ADR 0012), and a draft starts with 2 managers and open seats.

## Context

Phase 2 makes the draft, lineup edits, waivers and trades real. Their
rules change several kinds of rows at once (a waiver run ends roster
slots, starts others, rewrites lineups, decides claims, rotates the
priority and writes the feed) and must be strict and safe under races:
two picks for one slot, two teams adding the same free agent, an accept
racing a withdraw. Several things also happen at a time rather than on a
request: pick clocks, scheduled draft starts, lineup locks, waiver runs
(deadline − 24 h), trade expiry (the next waiver run) and review windows.
Those deadlines must survive a restart and run once even with several API
instances. ADR 0002 named River as the job queue; ADR 0009 deferred it.

## Decision

**One State per league, loaded locked, changed in Go, saved as a diff.**
`league.State` holds the league, its game state (rosters, lineups,
matchups), the draft (order, autopick, picks, queues), the waiver
priority, pending claims, open trades (with votes), waiver locks, the
once-per-gameweek run markers and new feed entries. `Tx.LoadState` takes
the league row `FOR UPDATE`; the rule packages (`draft`, `lineup`,
`waivers`, `trades`) change the struct; `Tx.SaveState` writes only what
differs from what was loaded. Every change to a league therefore queues
behind the league lock and runs its rules on one consistent snapshot; the
unique constraints (`draft_picks (draft, overall)` and `(draft, player)`)
and the roster exclusion constraint stay as the database's backstop.
Persistence for these tables lives in `league` (Postgres and Memory, one
round-trip suite); the rules live in their packages. This keeps ADR
0009's "load, change in Go, save" pattern, at the cost of `league` owning
the SQL of the draft, waiver and trade tables.

**River on the same Postgres for jobs.** River
(`github.com/riverqueue/river`, its `riverpgxv5` driver) is
transactional (a job inserted with `InsertTx` exists exactly when the
change that scheduled it commits), runs periodic jobs from an elected
leader only, retries, and needs no other infrastructure. Its tables are
applied by `store.Migrate` through `rivermigrate`, so they follow the
River version in `go.mod`. Rejected: goroutine timers (lost on restart),
`pg_cron` (an extension most hosts lack, and SQL-only jobs), our own job
table (River already solves leader election, retries and uniqueness).

- **Precise jobs.** The draft schedules `draft_clock {league, overall,
deadline}` at each pick deadline and trades schedule `trade_review
{league, trade, endsAt}` at each review window's end, through
  `league.Tx.Schedule` in the same transaction (unique by arguments; the
  deadline is part of them so a resumed clock gets its own job).
- **The tick.** A periodic `tick` job (every `JOBS_TICK`, default 15 s,
  also on start) does every due thing across leagues: start scheduled
  drafts, settle expired clocks, announce lineup locks, run waivers whose
  deadline passed, expire offers and close review windows. It is the
  backstop that makes every deadline durable: state, not the job, says
  what is due, so a lost or late job is caught within one tick. A
  `prune` job (hourly) drops realtime events older than five minutes and
  stat snapshots beyond `SNAPSHOTS_KEEP` per kind and gameweek (final
  ones stay).
- **Idempotent work.** Each job re-reads the state under the league lock
  and does nothing if the work is done: a pick clock checks the pick and
  deadline, a waiver run and a lineup-lock announcement check a
  `league_runs (league, gameweek, kind)` marker, trade closing checks the
  status. Running a job twice, late or on two instances changes nothing.
- **Memory mode.** Without a database, `jobs.Memory` runs the same work
  with timers and a ticker (nothing survives a restart; the tick catches
  up on start).
- Every instance inserts jobs; `JOBS_ENABLED` instances (default all)
  work them. The stats poller stays a single goroutine (ADR 0009).

**Rule decisions** where the contract and ADR 0008 left room:

- The draft's first gameweek is the later of `startGameweek` and the
  first gameweek whose deadline has not passed when the draft completes;
  rosters, default lineups and the H2H schedule start there, and the
  draft records it as the league's `startGameweek` (amended 2026-10-13).
  Every view starts there: a drafted league's current gameweek is the
  season's but never before `startGameweek`, so a league drafted
  mid-season shows its first gameweek (upcoming) rather than an earlier
  one it never played, and gameweek pickers stop at it. Migration 8 sets
  it for leagues drafted before this from their first matchup. When no gameweek up to `endGameweek` is
  left, starting the draft fails with 409 `season-over` rather than
  backdating rosters to a played gameweek; the tick leaves such a
  scheduled draft alone. A draft already running when the last deadline
  passes still finishes: its last pick (manual or autopick) is recorded,
  rosters start after the season (from the first unplayable gameweek, 39
  at the latest), no lineups or matchups are made and the league goes
  straight to `complete`. Failing that pick instead would roll it back on
  every tick and leave the draft running forever. The waiver order starts as the reverse
  of the draft order. The draft starts once at least 2 teams have a
  manager (409 `not-enough-managers`; amended 2026-10-14, it needed every
  place filled and 4 teams): each empty place becomes an open seat, a team
  named "Open seat N" without a manager, appended to the draft order. A
  team without a manager drafts on autopick at once, with no clock wait,
  and whoever joins with an invite later takes over the first open seat
  with its squad (renamed to their team name), during or after the draft; a
  missed clock picks (queue, then rank) at the time the job runs, not at
  the deadline, so a long outage does not autopick every team in a row.
  A missed clock is never silent (amended 2026-10-13): the pick is marked
  `missedClock`, the draft room says who was autopicked and with which
  player, and the pick joins the activity feed (`draft_autopick`). Each
  manager has "autopick when my clock runs out", on by default: on, the
  missed clock also turns the team's autopick on for the rest of the draft
  (as before); off, autopick stays off and the next pick gets a full
  clock. Neither ADR leaves a draft waiting on a manager, so the missed
  pick itself is made either way and the draft never stalls.
- Waiver cycle per gameweek G (the first whose deadline has not passed):
  until G's waiver run every unowned player is on waivers; after it, until
  G's deadline, unowned players are free agents except those dropped
  since the run. A free-agent add or drop (and a waiver drop) locks the
  player until the next run. Claims made after G's waiver deadline go to
  G+1's, even before G's run has happened, and G's claims can no longer
  change (409 `waivers-processing`).
  Moves take effect from G (rosters are half-open gameweek ranges) and
  the saved lineup hands the dropped player's slot to the new one.
  A run missed altogether (no marker for the last gameweek whose
  deadline passed while its claims still wait) is caught up by the tick:
  its moves count from the first gameweek still open, and after the last
  deadline its claims lose with `roster_locked`.
- Trades count from G before G's trade deadline (deadline − 48 h) and from
  G+1 after it. Accepting a trade (and processing one) fails the other
  open offers with the same players (`failed`, `player_no_longer_on_roster`;
  the contract has no "cancelled" status). Offers and their outcomes stay
  private to the two teams unless the trade was accepted at some point.
- A trade accepted after the trade deadline is processed at once but
  counts from G+1, so its players stay on their old teams for G. Until
  then they are in a pending trade (read from the rosters: a slot that
  starts, or ends, after G): they cannot be dropped as a free agent, in a
  claim or at the waiver run (the claim loses with `roster_locked`), nor
  traded again (409 `player-in-trade`). Every roster move also checks the
  squad is 2/5/5/3 in each later gameweek where it changes.
- A saved lineup counts from its gameweek until the team's next saved one;
  saving a gameweek replaces only that gameweek's lineup. A lineup that no
  longer matches the roster is repaired: each departed player's slot goes
  to the arrival of his position, other slots stay; only when that cannot
  give a legal XI does the default lineup apply.
- A commissioner who is a party to a trade cannot review it (403
  `own-trade`; no `review` action). In `commissioner` mode such a trade is
  approved when the review window closes, like an undecided one; in
  `league_vote` mode the vote decides it without the commissioner's
  override.
- Processed and vetoed trades join the activity feed; every processed
  claim does, won or lost. `ClaimDeniedReason` gained `roster_locked`.

## Consequences

- One lock per league serialises its writes. At the expected size (≤ 20
  teams, one draft pick or roster move at a time) that is cheap and makes
  the races easy to reason about; a hot league cannot slow another.
- Loading the whole State per change reads more than one change needs
  (rosters, lineups, the draft board). Closed claims and trades and the
  feed are not loaded, so it stays bounded over a season.
- River adds a dependency and its own tables; operators query
  `river_job` (docs/RUNBOOK.md "Jobs").
- The tick interval bounds how late a deadline can be processed when its
  precise job is missing (15 s by default). Requests do not wait for it:
  each action checks the wall clock against its own deadline (an offer's
  `expiresAt`, a review window's end, the waiver deadline) and answers
  409 once it has passed, so nothing slips through in the gap.
