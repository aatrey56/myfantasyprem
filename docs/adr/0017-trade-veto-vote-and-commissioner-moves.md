# 0017: Trades decided by a league veto vote, and the commissioner's moves

Status: accepted, 2026-11-16. Builds on 0010 (State, jobs) and 0015
(notifications).

## Context

Trades already had three review modes (`none`, `commissioner`,
`league_vote`, ADR 0008 and 0010), but a new league started on `none`:
accepted trades moved players at once and nobody could object. The owner
wants leagues to decide trades together by default, and wants managers to
see when the commissioner used a power the other managers do not have.

## Decision

### A league veto vote is the default

- A new league starts on `league_vote`, a 24 hour review and a veto
  threshold of a third (`league.DefaultSettings`). The threshold is a
  share of the managers **not in the trade**, rounded up, at least one: 4
  of 10. The product is rounded to 6 places before the ceiling
  (`trades.VotesNeeded`), because the contract's number is a float32 and
  `0.33333334 × 9` must stay 3.
- **Migration 24** gives each league whose current settings have no trade
  review (the old default, `none`) a new settings version with
  `league_vote`, 24 hours and a third. Leagues that chose `commissioner`
  review or a vote keep their settings, and so do imported FPL leagues,
  which mirror FPL's commissioner review. Offers made before the
  migration are untouched: they enter the review of whatever the league's
  setting is when they are accepted, and a trade already in review keeps
  the `review_mode` and window it was accepted with.
- Accepting an offer puts it `in_review` for `reviewHours` and schedules
  the existing `trade_review` job at the window's end (ADR 0010). Every
  manager outside the trade may **veto once and withdraw the veto** while
  the window is open (`POST .../votes`, `veto` | `withdraw`; `allow` stays
  accepted for older clients and does not count). The veto that reaches
  the threshold vetoes the trade at once. Otherwise the trade executes by
  itself when the window ends.
- Nothing new was needed for the timing and the races. The job and the
  15 s tick both call `trades.CloseDue`, which loads the league `FOR
UPDATE`, looks at the trade's status and does nothing if it is not
  `in_review` with a closed window, so a trade executes once however many
  instances run it (`TestReviewEndIsIdempotentUnderConcurrency`). Voting,
  withdrawing and executing queue behind the same lock.
- **Execution re-checks the rosters** (`trades.invalid`): every offered
  player still on the offering team, every requested one on the receiving
  team, both squads still 2/5/5/3. A trade that no longer holds becomes
  `failed` with `player_no_longer_on_roster` or `roster_invalid`, moves
  nobody and tells both managers why.
- Notifications (ADR 0015): `league_trade_in_review` to every other
  manager when a trade enters a league vote (with the hours left and the
  vetoes needed), `league_trade_vetoed` when one is vetoed (besides
  `trade_reviewed` for the two teams), `trade_failed` to the two teams
  when an accepted trade cannot execute. Processing keeps
  `trade_processed` and `league_trade_processed`.
- The commissioner's early approval or veto (`POST .../review`) still
  works in every mode, and a commissioner in the trade still cannot review
  it (ADR 0010).

### The review can change at any time

- `trades.review`, `trades.reviewHours` and `trades.vetoThreshold` are no
  longer locked once the draft starts: the commissioner can change them
  in-season. Team count, the pick clock, the draft start and the start
  gameweek stay locked (`setting-locked`). This is how an imported FPL
  league, left on `commissioner` by migration 24, moves to a league vote:
  the commissioner switches it by hand. Nothing switches a league
  automatically.
- The change is a commissioner move: `settings_changed`, with the review
  before and after, written in the transaction of the update like every
  other setting. Saving the same values writes nothing.
- A trade already `in_review` keeps the `review_mode`, `review_ends_at`
  and, in a league vote, `veto_threshold` it was accepted with:
  `trades.Accept` stores them on the trade, and the vote, the review and
  `CloseDue` read the trade's own, not the league's, so changing the
  threshold mid-vote cannot veto or save a trade already in a vote. The
  managers are still counted when the votes are (a seat emptied or filled
  meanwhile counts). Only offers accepted after the change use the new
  mode, period and threshold. Open offers (not yet accepted) take whichever
  is set when they are accepted. Migration 24 gives trades already in a
  vote the league's threshold at the time; a trade without one counts
  against the league's.
- The Settings screen keeps the Trade review control enabled after the
  draft, with a hint that trades already in review are not affected.

### Commissioner moves

- An entry is written whenever the commissioner uses a power a manager
  does not have, **in the transaction of the action**: `league.Tx.AddAudit`
  (Postgres insert, memory append) is called by the rule code as its last
  step, so a refused action leaves nothing and a committed one always
  leaves its entry. It lives in `league` because `league.Tx` is what the
  rule packages already share (ADR 0010).
- What is logged (the `action` open enum): `settings_changed` and
  `league_renamed` (with before and after values), `draft_updated`
  (order, timer, start time), `draft_started`, `draft_paused`,
  `draft_resumed`, `draft_pick_for_team` (the commissioner picking on
  another team's clock), `member_removed`, `commissioner_transferred`,
  `invite_created`, `invite_revoked`, `seat_link_issued`,
  `signin_link_issued`, `trade_vetoed`, `trade_approved` and
  `league_resynced`. A commissioner moving their own team as a manager
  (their picks, lineup, claims, trades, votes) is never logged, nor is a
  request that changes nothing or fails (revoking a revoked invite is
  nothing). Two moves cannot share the transaction entirely: the sign-in
  link lives in the auth store, so it is issued first and its code is
  returned only once the entry committed (a link whose entry failed is
  revoked, and nobody saw its code); the re-sync's pending mark lives in
  the mirror store, and its job is queued in the transaction of the entry
  (`Tx.ScheduleOn`), which says the re-sync was requested.
- Not in this repo today, so not logged: editing another team's roster or
  lineup, renaming another manager's team and correcting points. When one
  of them is added it records an entry with `league.Record` like the
  others; `npm run admin` rescoring is an operator tool, not a commissioner
  power.
- Each entry has a public id (`cmv_`), the actor, the action, a sentence
  written when it happened, the affected teams, optional before and after
  values and the time. Read it with `GET .../commissioner-moves` (members
  only, newest first, offset cursor).

### The Status banner

- `commissioner_moves_seen (league, user, seen_seq)` holds each member's
  seen-up-to marker, so the banner follows them across sessions and
  devices. `GET .../commissioner-moves/summary` returns the unseen count
  and the latest unseen move; `POST .../commissioner-moves/seen` moves
  the marker to `throughId` (or the newest), never backwards.
- A move counts as unseen when it is after the marker, someone else made
  it, and it was made after the member joined: a new manager does not
  inherit the setup history, and the commissioner is not told about their
  own moves. A manager who takes over an existing seat (an open seat after
  the draft, a claim link) keeps the seat's first `joined_at`, so joining
  also starts their marker at the league's newest move, in the
  transaction that gives them the seat (`Tx.StartAuditSeen`).
- The Status screen shows "Commissioner moves · N new" over the latest
  move's sentence until it is tapped. The tap marks the moves seen and
  opens League settings scrolled to the "Commissioner Moves" section under
  League Rules.

## Consequences

- Existing leagues that never chose a review get one. That is the owner's
  intent (the old default was the absence of a decision), but a league that
  did choose `none` explicitly cannot be told apart in the stored settings
  and is switched too; its commissioner can change the kind of review,
  the review period and the threshold at any time, in-season included.
- The log grows with commissioner activity only, a handful of rows per
  league; the list is paged by offset like the other lists.
- `Seq` is an identity column, drawn when the row is inserted, not when it
  commits. `Tx.AddAudit` takes the league's row lock before inserting
  (already held when the action loaded the league), so a league's entries
  are numbered in commit order and the seen-up-to marker never skips an
  entry that commits late (`TestAuditSeqFollowsCommitOrder`).
