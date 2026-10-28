# 0012. Importing an in-progress FPL Draft league, playing it here, re-syncing on demand

Status: accepted (2026-10-14). Revives and replaces 0007; amends 0008
("Import is later"). Amended the same day (owner decision 2026-10-14, see
"Amendment: playable at once" below): an imported league is playable
here immediately; the automatic sync, the read-only state and the
switch-over are replaced by a commissioner's re-sync. Amended again
the same day for trades pending on FPL ("Trade states" below).

## Amendment: playable at once (2026-10-14)

The owner will test the whole app with real users, their friends, on the
imported real league. A league that stays read-only until a switch-over
in December or January cannot be tested, and an automatic sync would
overwrite every move the testers make. So:

- **Playable at once.** From the moment it is imported, an imported
  league behaves exactly like a native one: waiver claims, free agents,
  trades (offer, respond, review, vote), lineups, league and team renames
  and settings all work, and the tick runs its waivers, lineup locks and
  trade windows. There is no `league-synced` error and no sync state.
- **No automatic sync.** The `fpl_sync` / `fpl_sync_due` jobs are gone.
  The global stats feed and our scoring are unaffected: the poller keeps
  ingesting and rescoring imported leagues like native ones.
- **Re-sync from FPL, on demand.** The commissioner may re-sync (`POST
/sync {confirm: true}`, a River job `fpl_resync`). It fetches FPL again
  and resets the league to FPL's current state: squads and player
  ownership (the replay), the waiver order, the activity feed, trades and
  lineups, and FPL's names and settings; scores, results and standings
  are recomputed. Every move made here since the last import or re-sync
  is replaced: pending claims are cancelled and open trades made here
  fail with `league_resynced` (both stay listed so their managers see
  what happened); a trade imported from FPL (named by a `'trade'` ref,
  such as one pending on FPL) is FPL's and stays as FPL has it. The
  closed claims, trades and feed entries made here are
  deleted, waiver locks go, and lineups FPL did not set are removed so
  each team plays FPL's last lineup until its manager saves a new one.
  The sync status carries a notice for every member ("Re-synced from
  FPL. 1 open trade and 2 waiver claims were cancelled."). The app shows
  the warning "This replaces all moves made here since …" and needs a
  confirmation. Owners and claim links are untouched, so claimed teams
  stay claimed. A re-sync is idempotent (a second one finds nothing to
  cancel and changes nothing) and one re-sync or check runs at a time per
  league (409 `sync-pending`; a request older than 30 minutes, well past
  the job timeout, gives way so a lost job cannot block the league).
- **Check against FPL, informational.** Verification stays as a manual
  check (`POST /sync/check`, job `fpl_check`) and runs after the import
  and every re-sync. It changes nothing and blocks nothing: after moves
  made here the league differs from FPL by design. Explanations remain
  as annotations.
- **No switch-over.** With the league already ours, the switch-over has
  nothing left to switch: its purpose (FPL's history complete before our
  engines take over) is served by a final re-sync when the owner moves
  over for real, after which nobody presses re-sync again. The endpoint,
  `switch_*` columns and `sync_state` are dropped (migration 12).

The sections below describe the import, the data model and verification,
which are unchanged, and keep the original sync and switch-over design
for the record, marked as superseded.

## Context

The owner's top priority is to move their real FPL Draft league, already
in its season (2026-27, gameweek 6 finished), into this app; keep it in
step with FPL while the league still plays there; and, at a gameweek the
commissioner picks (planned for December or January), let our engines run
it. The switch-over must be flawless: no wrong trade, waiver, squad, score
or standing.

Constraints:

- No FPL logins, ever (RESEARCH.md §6.2). Only the public league
  endpoints, called from the server: `league/{id}/details`,
  `league/{id}/element-status`, `draft/{id}/choices`,
  `draft/league/{id}/transactions`, `draft/league/{id}/trades`,
  `entry/{id}/public` and `entry/{id}/event/{gw}`. A gameweek's picks are
  private until its deadline (the body is the JSON string
  `"No pick history"`), so are pending waiver claims and trade offers.
- What a one-time capture of the real league (14 October 2026, 12 teams,
  H2H, gameweek 7 next) taught us about the real shapes:
  - `league_entries[].id` (the league entry) is what matches and
    standings name; `entry_id` is what choices, transactions, trades,
    element-status `owner` and the `entry/` endpoints name. Both map to
    one seat.
  - `entry/{id}/event/{gw}` lists the picks _after_ auto-subs (positions
    1–11 are the XI that scored) and `subs` lists `{element_in,
element_out}`. `multiplier` is 1 for every pick and `entry_history`
    is `{}`: neither carries information in Draft.
  - The XI after subs sums to FPL's match points for every team in
    every finished gameweek (1–6, all with captured live data).
  - Replaying the draft, then every accepted transaction (`result` `a`)
    and processed trade (`state` `p`) in gameweek (`event`) then time
    order, rebuilds every team's 15 for gameweeks 1–6 and today's
    element-status exactly. A trade item's `element_out` leaves the
    offering entry and `element_in` arrives there. A move's `event` is the
    gameweek it counts from.
  - `standings[].matches_played` says 38 after six gameweeks: unusable.
    `rank` breaks ties on points for, like our `SortStandings`.
  - `waiver_pick` is reverse standings (1 = first pick), reset every
    gameweek; ours was rolling then. Since 2026-10-15 an imported league
    uses reverse standings too (ADR 0008 rule 12 amendment, which derives
    FPL's exact rule from this capture).
  - `league.trades` is `m`, which the owner confirms is commissioner
    review; `transaction_mode` is `waivers`; `scoring` is `h`.
  - A second capture that evening (`real-gw6-pending`) showed a 5-for-5
    trade in state `a`: accepted by the receiver at 19:00 and waiting for
    the commissioner's approval and processing. The public trades feed
    lists it, and element-status marks its ten players
    `in_accepted_trade`, each still with the team giving him.
- The deprecated `/v1/imports*` endpoints are in the contract with a fake
  job (ADR 0007).

## Decision

### Reuse `/v1/imports*`, make them real

The three operations stay and lose `deprecated` / `x-status: later`:
the shapes were right (preview, then a 202 job with progress). Changes:
the preview also returns the current gameweek, the scoring mode, whether
the draft is complete, whether the league can be imported and every seat
(`seat`, team name, manager name); `POST /v1/imports` also takes `seat`,
the importer's own team; the fake job and its 501 are gone. New
operations live under the league: `GET /v1/leagues/{id}/sync`,
`POST /v1/leagues/{id}/sync` (re-sync, amended), `POST
/v1/leagues/{id}/sync/check` (amended), `POST
/v1/leagues/{id}/sync/explanations`, `GET /v1/leagues/{id}/seats` and
`POST /v1/leagues/{id}/seats/{teamId}/invite`. (`POST
/v1/leagues/{id}/switch-over` existed until the amendment.)

Idempotency is per importer and FPL league, not per FPL league: a
second user who imports the same FPL league gets a separate league.
Rationale: nothing proves who controls an FPL league without a login, so
a global "first importer owns it" rule would let anyone who knows a
league id squat on it and lock the real commissioner out. A mirror is only
as shared as its claim links; `leagues.fpl_league_id` and
`transactions.fpl_txn_id` lose their global UNIQUE.

### Data model

| FPL                     | Ours                                                                                                      |
| ----------------------- | --------------------------------------------------------------------------------------------------------- |
| league                  | `leagues` row, `source` `fpl_import`, `fpl_league_id`; settings mapped below                              |
| league entry (seat)     | `teams` row; `league_refs (league, 'team', 'fpl', league-entry id)`; entry id and manager name on `teams` |
| match                   | `matchups` row; ref `'match'` keyed `gw:entry1:entry2`; entry 1 is home; the AV entry is our average side |
| draft choice            | `draft_picks` (`overall` = `index`; `was_auto` → method `rank`, `missedClock`)                            |
| transaction             | `transactions` feed entry (`sourceState` = `kind/result`); ref `'transaction'`                            |
| trade (`p`, `v`, `a`)   | `trades` row (below), review mode commissioner; a feed entry once processed or vetoed; ref `'trade'`      |
| ownership over time     | `roster_slots` rebuilt by the replay above, `[event, next event)`                                         |
| `entry/{id}/event/{gw}` | `lineups (team, gw)` with the subs undone (below); `updated_at` = that gameweek's deadline                |
| `waiver_pick`           | `waiver_priority`                                                                                         |
| re-sync, check          | `mirror_sync`: last applied, last attempt and error, the pending request, the last re-sync's notice       |

`league_refs (league_id, entity, provider, external_id, internal_id)` is
unique on the first four and on `(league_id, entity, internal_id)`. It is
part of the league `State` (loaded and saved with it), so a ref commits
with the row it names: a crash can never leave a feed entry without its
ref, which is what makes re-running an import safe.

Trade states. FPL's help documents proposed, withdrawn, rejected,
accepted (waiting for approval), invalid (a player was in another
accepted trade) and vetoed, plus processed; offers also expire at the
waiver deadline. Only three codes have been seen in the real league, so
only those are mapped; the provider keeps a state for each of the others
in case a code is ever confirmed:

| FPL code       | FPL state                                 | `provider.TradeState` | Here                                                                                                   |
| -------------- | ----------------------------------------- | --------------------- | ------------------------------------------------------------------------------------------------------ |
| `p`            | processed                                 | `processed`           | `processed`; the players move from the trade's `event` (the replay); feed entry                        |
| `v`            | vetoed                                    | `vetoed`              | `vetoed`; nobody moves; feed entry                                                                     |
| `a`            | accepted, waiting for approval/processing | `accepted`            | `in_review`, no review window (`review_ends_at` null): FPL decides it; nobody moves; no feed entry yet |
| not seen       | proposed                                  | `offered`             | not imported (an open offer, private in FPL)                                                           |
| not seen       | rejected / withdrawn / expired / invalid  | `rejected` …          | not imported; closes a trade imported while `a` (`declined`, `withdrawn`, `expired`, `failed`)         |
| any other code | —                                         | `unknown`             | not imported; an **unverified** `setting` item (`trade-state:{id}`) until explained                    |

A trade imported while `a` is **pending on FPL**: the whole league sees
it in the trades screen with both teams and every player ("Pending on
FPL"), nobody can act on it here (`viewerActions` is empty and a review
answers 409 `pending-on-fpl`: FPL decides it, and a decision here would
give the trade two outcomes), and the trade engine never processes it
(no review window). A re-sync keeps it as it is while FPL still waits (Reset cancels only the open trades made here);
the re-sync that sees it `p` or `v` closes it the way FPL did and writes
its feed entry, and the replay moves the players for `p`. The rest of
the imported league stays playable.

Settings: `teamCount` = real seats (FPL allows 2–16; imports may have
fewer than our native minimum of 4); `pickTimerSeconds` =
`draft_pick_time_limit`; `draftStartsAt` = `draft_dt`; `startGameweek` /
`endGameweek` = `start_event` / `stop_event`; trade review `m` →
`commissioner` (24 h window); any other code (`n`, no trades, included)
→ `commissioner`, so nothing processes without a human, and an
**unverified** item in the report until the commissioner explains it.
Only `scoring` `h` (head-to-head) and a completed draft can be imported:
classic-scoring leagues and undrafted leagues are refused with 422
(`unsupported-league`, `draft-not-complete`).

Lineups: the picks are after subs, so the locked lineup is rebuilt
(`mirror.lineup`). FPL lists the bench as it was, except that each
player who came on left the player he replaced in his bench slot, and
lists the XI that scored sorted by position (GKP, DEF, MID, FWD). So the
bench is rebuilt by swapping back in place, and the XI by position with
the subbed-out players first within their position, in FPL's sub order
(FPL takes the starters in XI order; once one is subbed, a later
non-playing starter of the same position finds no bench player). The
order of starters who played does not matter to the auto-subs. A plain
"swap back in place" was tried first and got four of the capture's
gameweek-1 and -6 sub pairings wrong (same points, different pairing):
it put a subbed-out defender after a non-playing midfielder who stayed.
Our engine then replays the auto-subs from the rebuilt lineup;
verification compares the result with FPL's `subs` and points, which is
what proves the reconstruction.

Scores are always ours: the poller's recompute scores imported leagues
like native ones, from the imported rosters and lineups. FPL's numbers
are the oracle verification compares against; they are never copied into
`team_gw_scores` or the matchups. A disagreement is therefore visible
instead of hidden.

### Import

`POST /v1/imports` writes the `imports` row (with the league id it will
create, minted now) and inserts a River `fpl_import {importId}` job in the
same transaction (`UniqueOpts{ByArgs}`). The job:

1. checks the FPL league (H2H, draft complete) and fetches the league
   endpoints and every seat's picks for every gameweek from the start up
   to the current one, at most 2 requests per second, writing progress
   after each request. Decoded picks of finished gameweeks are cached
   (`mirror_picks`), so a retried or later run does not fetch them again;
2. builds the whole desired state from the fetched data (a pure
   function, `mirror.Plan`);
3. applies it in one transaction under the league lock (`mirror.Apply`):
   creates the league on first run, then reconciles teams, settings,
   draft, rosters, lineups, matchups, priority, feed and trades by their
   refs (an idempotent upsert: applying the same plan twice changes
   nothing);
4. rescores every gameweek up to the current one, verifies (below) and
   stores the report; marks the import `succeeded` with its `leagueId`.

A failure marks the import `failed` with a problem (the FPL client has
already retried with backoff; a job cut off by a shutdown is retried by
River); importing again starts from step 1 and converges on the same
rows, and a failed import never leaves a league behind (the apply is
one transaction). The importer becomes commissioner and owner of the seat
they chose.

### Unclaimed seats and claim links

Every other seat starts without a manager. Each gets a **claim invite**:
an `invites` row with `team_id` set, single use, no expiry. It uses the
existing invite preview and accept flow: the preview says which team it
claims (team and FPL manager name) and is joinable even though the league
has drafted; accepting it sets the team's owner instead of adding a team
(`teamName` is optional and ignored: the seat keeps its FPL name).
A user who already has a team in the league gets `already-member`; a used
or revoked link gets 410 `invite-expired`. The commissioner sees every
seat with its state and link (`GET /seats`) and can "resend": that revokes
the seat's current link and issues a new one (we have no email address
for the friends, so resending means sharing the new link). A manager who
leaves orphans the seat again; the commissioner issues a new link. While
`CLAIM_LINK_SIGNIN` is on, the link also signs a signed-out person in
with only a name ([0013](0013-claim-link-sign-in.md)).

### Sync until switch-over (superseded by the amendment)

A synced league (`sync_state` `syncing` or `switch_scheduled`) is
**read-only for everything FPL owns**: waiver claims, free-agent moves,
trades (offer, respond, review, vote), **lineups**, league and team
renames and settings answer 409 `league-synced` ("This league is synced
from FPL Draft until the switch-over. Make this change in FPL."). The
scheduled jobs skip it (no waiver run, lineup-lock announcement or trade
expiry). Lineups are read-only too because the pending lineup is private
in FPL: a lineup saved here would change nothing in FPL and would give
the league two truths. Claims, invites, leaving and the commissioner
role stay ours.

A periodic job (`fpl_sync_due`, every minute, River's leader) enqueues
`fpl_sync {league}` for each synced league whose `next_sync_at` has
passed. The sync is the import's steps 1–4 on the existing league: FPL
wins on everything it owns (names, settings, rosters, lineups of locked
gameweeks, schedule, feed, trades, waiver order), and nothing it does not
own (owners, invites, scores) is touched. Cadence (`mirror.NextSync`):
every 15 minutes, plus 5 minutes after each deadline (the new lineups),
15 minutes after each waiver deadline (the waiver run) and when a
gameweek is marked finished. That is 5 requests per sync plus one per
seat after each deadline, well inside the 2-per-second budget. Failures
back off (15 min doubling to 2 h) and keep the last good state; the sync
status shows the last error. The commissioner may sync now (at most once
a minute).

### Verification

After every import and re-sync, and on the commissioner's check
(amended: originally after every sync), `mirror.Verify` recomputes everything from
the imported data with our engines (`league.ScoreGameweek`,
`StandingsThrough`, the stored stats) and diffs it against FPL:

- **points**: each team's gameweek points vs FPL's match points (and
  `entry_history.points` when FPL fills it), for every finished gameweek;
- **subs**: our auto-subs vs FPL's `subs`;
- **results**: each finished H2H match: both scores as stored and shown
  (what the rescore wrote, so a score saved wrong is caught even when the
  engine is right) and W/D/L;
- **standings**: W, D, L, points for, points against, league points and
  rank (not FPL's `matches_played`), twice: our table from our scores
  (`unverified` while a finished gameweek has no stats) and our table
  logic fed FPL's match points (checks the schedule mapping, W/D/L and
  tie-breaks on its own);
- **squads**: each team's 15 per gameweek vs its FPL picks, and the
  current owner of every player vs element-status;
- **waiver order** vs `waiver_pick`; unmapped settings;
- **pending trades**: each trade FPL has accepted but not processed or
  vetoed (`a`) must be imported as the same trade in review (same teams,
  same players), and FPL must agree with itself: element-status flags
  exactly its players `in_accepted_trade`, each on the team giving him.
  Then the item is `pending`; otherwise `mismatch`. A flagged player in
  no such trade, or a trade in review here that FPL no longer shows as
  waiting, is a `mismatch`.

Each item has a stable key (`points:gw6:tm_…`) and is `ok`, `mismatch`,
`unverified` (could not be checked, e.g. no stats for that gameweek) or
`pending` (a trade waiting for FPL, imported as such). `pending` is not
failing: it does not make the report `mismatch` and cannot be explained;
the sync status lists it apart (`verification.pending`,
`counts.pending`) and the next re-sync applies what FPL decides. The
report (`mirror_reports`) is `clean`, `mismatch` or `incomplete`. The
commissioner sees it with every failing item; members see only "Synced
from FPL · last synced …". A commissioner may **explain** a failing item
with a note; the explanation holds only while the item shows the same
values (a different mismatch under the same key needs a new one).
Originally the switch-over needed a report with no unexplained failing
item; since the amendment the report is informational and blocks nothing.

### Switch-over at gameweek X (superseded by the amendment)

The commissioner picks X and confirms (`POST /switch-over {gameweek,
confirm: true}`, commissioner only). X must be after the league's start,
at most its end, and X's waiver deadline must be at least an hour away;
the latest report must have no unexplained failing item. Confirmation is
**irreversible**: there is no cancel. From then on the league is
`switch_scheduled` and keeps syncing.

The flip happens at the first sync after FPL has marked gameweek X−1
finished (at once when that is already so): by then nothing FPL owns can
still change for gameweeks before X (their lineups locked at their
deadlines, their moves ran, their results are settled), so FPL's history
is complete. That sync must verify with no unexplained failing item;
then, in the same transaction:

- the waiver order becomes FPL's `waiver_pick` order at that moment
  (reverse standings after X−1, which is what FPL would use for X); from
  then on it is rolling (ADR 0008 rule 12): a winner moves to the back.
  The switch-over panel says so;
- FPL moves that count from X (free agents after X−1's deadline) are
  part of that final sync, since FPL was still the source;
- pending FPL claims and trade offers cannot be seen and do not carry
  over: managers re-enter claims and re-propose trades here. An accepted
  FPL trade still waiting for approval (element-status
  `in_accepted_trade`) is a failing item: the commissioner waits for FPL
  to decide it, or explains it ("will re-propose");
- each team's lineup for X is its FPL lineup for X−1 carried over
  (repaired for roster moves), as FPL itself would carry it;
- `sync_state` becomes `native`, `switched_at` is set, and syncing stops.
  From X our engines own the league: X's waiver run, lineup lock and trade
  deadline happen here.

If X's waiver deadline passes before the flip could happen (FPL late to
finish X−1, a failing item not yet explained), the switch-over moves to
X+1 by itself and the commissioner sees why. It never half-happens.

### Failure modes

As amended (the original table below it described the automatic sync):

| Failure                                        | Behaviour                                                                                                   |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| FPL down, 429, 5xx, timeouts during a re-sync  | Client retries with backoff; the re-sync fails, the league keeps its state, the sync status shows the error |
| A re-sync or check job is lost                 | Its request stops blocking after 30 minutes; the commissioner asks again                                    |
| Two re-syncs at once                           | One pending per league (409 `sync-pending`); River unique jobs and the league lock serialise; both converge |
| Moves made here when the commissioner re-syncs | Replaced by FPL's state; open trades made here and pending claims are cancelled with a notice               |
| A trade is accepted on FPL, not processed      | Imported in review, no actions here; `pending` item; a re-sync keeps it until FPL processes or vetoes it    |
| An FPL trade code we do not know               | Not imported; an unverified item until the commissioner explains it                                         |
| FPL changes a past move or result              | Seen at the next re-sync (FPL wins then); a check shows it before                                           |

Original:

| Failure                                       | Behaviour                                                                                                         |
| --------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| FPL down, 429, 5xx, timeouts                  | Client retries with backoff; the import fails (start it again), a sync backs off; the league keeps its last state |
| A response breaks the shape or its references | `ErrInvalid`: nothing is written; the sync records the error                                                      |
| A player FPL names is unknown to our stats    | The sync fails with that player; the next bootstrap ingest maps it, the retry succeeds                            |
| Picks still private (`"No pick history"`)     | That gameweek is not imported yet; the previous lineup carries over                                               |
| Crash mid-import                              | The apply is one transaction; refs commit with rows; the retry converges                                          |
| Two syncs of one league at once               | River unique jobs; the league lock serialises the applies; both converge                                          |
| Our scores disagree with FPL                  | A `mismatch` item for the commissioner; switch-over blocked until explained or fixed                              |
| Missing stats for a gameweek                  | `unverified` items (`incomplete`); the poller backfills; switch-over waits                                        |
| FPL changes a past move or result             | FPL wins while syncing; the replay rebuilds rosters; verification shows it                                        |
| A seat's manager leaves FPL / the app         | FPL keeps the entry; here the seat is orphaned and a new claim link can be issued                                 |
| Someone imports a league that is not theirs   | They get their own mirror; nobody else's league is touched                                                        |

## Consequences

- One more package, `mirror` (plan, apply, verify, the jobs' work, its
  tables and their memory twins); `provider/fpl` implements
  `LeagueMirrorSource`; `jobs` gains three job kinds.
- Originally the rule packages and the tick checked `League.Synced()`;
  the amendment removed it, so imported and native leagues run the same
  code.
- The stored names include the friends' real FPL names (team and manager
  names), shown only to the league's members; the test fixtures are
  anonymised (invented names, real ids and numbers).
- The waiver order changes rule at the switch-over (reset by standings
  in FPL, rolling here). A per-league "reset to reverse standings" option
  would remove that difference; it is not built. (Built since: the
  `waiverOrder` setting, ADR 0008 rule 12 amendment, and imported leagues
  default to reverse standings, so the rule no longer changes. The
  verification works the order out from our standings and compares it
  with `waiver_pick`.)
- Verification proves the import only where stats exist. The capture has
  live data for every finished gameweek (1–6), and the replay test
  (`mirror.TestRealCaptureVerifiesWithZeroDiffs`) requires all of it: 72
  points, 72 auto-sub and 36 result items, both tables, 72 squads,
  ownership and the waiver order, every one `ok` and the report `clean`.
  Mutation tests corrupt one stored value of each kind (a player's
  minutes, a stored match score, a squad member, the waiver order) and
  require a mismatch of that kind and a refused switch-over, so the
  verifier is shown to catch errors, not only to bless a right import.
  In production the poller backfills every gameweek, so every finished
  gameweek is checked; one without stats yet is `unverified`.
- The later capture (`real-gw6-pending`) replays a trade pending on FPL
  (`mirror/pending_test.go`): zero mismatches, the 5-for-5 imported in
  review across the right teams with nobody moved, one `pending` item;
  a re-sync while FPL waits keeps it unchanged and cancels a trade made
  here; one after FPL processes it moves the players from gameweek 7 and
  the squads match; one after a veto moves nobody; FPL showing other
  players, or no longer showing the trade, is a mismatch.
