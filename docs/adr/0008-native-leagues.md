# 0008. Native leagues: the app runs the league

Status: accepted (2026-10-12). Amends 0004 and 0007. Rule 15 amended
2026-10-14; rule 12 amended 2026-10-15.

## Context

v1 was planned as a read-only mirror of FPL Draft leagues (RESEARCH.md
§9.1 level A): import a league, show live matchups, never write back. On
2026-10-12 the owner decided to build a standalone, Sleeper-style fantasy
app for the Premier League instead (level C):

- Leagues, drafts, lineups, waivers and trades live in this app.
- Scoring applies FPL's official 2026-27 points rules (RESEARCH.md §7) to
  player match stats.
- The only outside data is the global player, club, fixture and live-stats
  feed: FPL's public API now, a licensed feed later.

A mirror needed FPL Draft's league endpoints for everything; the pivot
leaves only the stats feed as a dependency, which a licensed feed can
replace (ADR 0004).

## Decision

**What stays**

- One Expo codebase, web first (0001); one Go modular monolith on Postgres
  (0002); the contract first (0003).
- `StatsSource` and the single live poller for all leagues. A poll is
  idempotent, keeps the last good snapshot, never zeroes a score, and marks
  data stale after 3 minutes (0005).
- Scoring is pure (`server/internal/scoring`, no I/O) and recomputes from
  scratch. Score states `live → provisional → official → final`, plus
  `upcoming` before the first kickoff.
- The `{data, meta}` envelope, RFC 9457 problems, open enums, cursor
  pagination and opaque prefixed ULID ids (0006). New prefixes: `usr_`,
  `drf_`, `pk_`, `wv_`, `trd_`, `inv_`.
- The league-average opponent for odd team counts, with the existing
  `AverageOpponent` shape: the floor of the mean of every real team's
  gameweek points, including its opponent's.
- Text-only branding, the "Not affiliated" disclaimer, no FPL logins.

**What changes**

- **Accounts.** Sign in with Apple, Google or an email magic link. The
  server issues opaque bearer tokens (1-hour access, rotating single-use
  refresh with reuse detection), stores only their SHA-256, and deletes
  accounts on request (App Store 5.1.1(v)). Every operation needs a token
  unless the contract says `security: []`.
- **League state is ours.** Create a league (2–20 teams, FPL standard
  scoring, 15-man squad of 2 GKP/5 DEF/5 MID/3 FWD, XI of 1 GKP, 3–5 DEF,
  2–5 MID, 1–3 FWD, H2H schedule, waiver order (reverse standings or
  rolling, rule 12), trade review
  none / commissioner / league vote), invite by code or link, join, leave,
  commissioner settings. Draft-time settings lock when the draft starts.
- **Draft.** Snake order, a pick clock, autopick from the team's queue and
  then by `draftRank`, a board, live updates over the realtime channel.
- **Lineups.** Set XI and bench order per gameweek (carried over), locked
  at the deadline; no captain. Other teams' lineups are private until the
  deadline. Auto-subs follow FPL's rules.
- **Waivers and free agents.** Claims with a same-position drop (squads
  always stay 2/5/5/3), private until processed; free agents first come,
  first served after the waiver run.
- **Trades.** Every state is real: offered, accepted, in review, processed,
  declined, withdrawn, expired, vetoed, failed.
- **Realtime.** A WebSocket channel with a short-lived ticket
  (`POST /v1/realtime/tickets`); events are `Realtime*` schemas in the
  contract and described in [docs/realtime.md](../realtime.md). REST stays
  the source of truth.
- **Idempotency.** Mutating POSTs accept an `Idempotency-Key`.
- **Import is later.** `/v1/imports*` stay in the contract and keep working
  (fake job) but are `deprecated: true` and `x-status: later`.
  `LeagueMirrorSource` stays as an optional, later interface; nothing in v1
  depends on it.

New server modules: `auth`, `league`, `draft`, `lineup`, `waivers`,
`trades`, `realtime` (see ARCHITECTURE.md). Data falls into three tiers:
global stats shared by every league, per-league state, and per-user private
data (pending lineups, the draft queue, pending waiver claims).

## Rule assumptions

Where RESEARCH.md §7 is ambiguous or silent, the scoring package and the
contract follow FPL's documented rule, as below.

1. **Playing** means minutes on the pitch or a yellow or red card (FPL
   rules: "Playing in a Gameweek means making an appearance on the pitch
   or receiving a yellow / red card"). A player who did not play scores
   0, whatever else the stats say. One booked without a minute (on the
   bench) scores only the card, −1 or −3: the card lines have no minutes
   condition and appearance points need minutes. Settled 2026-10-12, see
   "Rule sources" below.
2. **Clean sheet** needs 60+ minutes and no goal conceded while the player
   was on the pitch: 4 GKP/DEF, 1 MID, 0 FWD. Goals conceded after a red
   card cost it (rule 3); goals after a substitution do not.
3. **Goals conceded:** −1 per 2 for GKP and DEF, nothing for MID and FWD,
   counting goals while on the pitch and, after a red card, for the rest
   of the match (FPL: "If a player receives a red card, they will continue
   to be penalised for goals conceded by their team"). FPL's
   `goals_conceded` stat already counts them; our input means the same.
4. **Saves** score +1 per 3 for whoever keeps goal: an outfield player who
   goes in goal "will be able to collect points for saves, but will score
   points as per their position classification" (FPL help), so the rest
   of his scoring stays his position's.
5. **Penalty saved +5 / missed −2** for any position, as the table says.
6. **Red card −3 includes any yellow** in the same match (a second yellow
   is a red), so a sent-off player is not also docked −1.
7. **Defensive contributions** score at most +2 per fixture: DEF on 10+
   CBIT (clearances, blocks, interceptions, tackles; recoveries do not
   count), MID and FWD on 12+ CBIRT (CBIT plus recoveries), never GKP.
8. **Bonus ties** use FPL's rule (standard competition ranking): tie for 1st
   3/3/1, tie for 2nd 3/2/2, tie for 3rd 3/2/1/1, three-way tie for 1st
   3/3/3. Only players who played are ranked, each match on its own BPS
   (a double gameweek is ranked per fixture). FPL ranks "the three best
   performing players in each match" whatever the sign, so a known BPS of
   0 or below ranks like any other. An unknown BPS (a double gameweek
   without per-fixture data; NULL in the store) is never ranked and takes
   no place. We read BPS from FPL and
   never recompute it (FPL changed its weights for 2026-27, RESEARCH.md
   §7).
9. **Double gameweeks** add both fixtures; bonus and defensive
   contributions are per fixture. A blank scores 0 and can be auto-subbed.
   Bonus timing (2026-27): projected bonus appears after 20 minutes of a
   match and stays provisional until FPL confirms the match's bonus; scores
   lock at 09:00 UK time on the day after the gameweek's last match (ADR
   0005).
10. **Auto-subs** are FPL's: a starter who has not played (rule 1: no
    minutes and no card) and whose club's fixtures are all done is
    replaced by the first bench player in order who played (minutes or a
    card),
    goalkeeper for goalkeeper, keeping the formation legal. While the
    gameweek runs they are `projected`; a projection waits when an earlier
    eligible bench player still has a fixture to play, so it never has to
    be taken back. The final result is the same as FPL's end-of-gameweek
    processing.
11. **Formation:** shown for the XI in force after auto-subs. The owner's
    "≥3 DEF, ≥2 MID, ≥1 FWD" plus FPL's maxima
    (5/5/3, implied by the squad) and exactly one GKP; bench slot 12 is the
    backup goalkeeper.
12. **Waiver order is a league setting, `waiverOrder`** (amended
    2026-10-15; it was rolling only). `reverse_standings`, FPL Draft's
    rule, is the default; `rolling` remains. The run time is FPL's:
    deadline − 24 h. See "Rule 12 amendment" below for the exact rule and
    its evidence.
13. **Trade shape** is FPL's: equal numbers of players with matching
    positions, so both squads stay legal. Unanswered offers expire at the
    next waiver run. Accepting a trade cancels other offers with the same
    players.
14. **League vote:** a trade is vetoed when veto votes reach
    `ceil(vetoThreshold × managers not in the trade)` within the review
    window (default 24 h, threshold 0.5). FPL counts "50% of managers"
    without saying whether the two trading managers count; we exclude
    them. In `commissioner` mode an undecided trade is approved when the
    window closes.
15. **Team count 2–20** (amended 2026-10-14 from 4–20, so small test
    leagues work; FPL allows 2–16). The draft starts once at least 2 teams
    have a manager; every empty place becomes an open seat ("Open seat
    N", no manager) that autopicks without waiting for the clock, and the
    next person to join with an invite takes it over with its squad
    (ADR 0010). The game logic needs nothing more for small leagues: the
    round robin gives a 2-team league the same pairing every gameweek
    (home and away alternating) and an odd count one bye per gameweek
    against the league average; 2 × 15 players is far inside the pool; in
    `league_vote` mode a 2-team league has no voters besides the two
    trading teams, so no veto can reach the threshold and the trade is
    approved when the window closes.
16. **Deadlines** are FPL's: first kickoff − 90 minutes (lineups lock),
    waivers at deadline − 24 h, trades by deadline − 48 h.
17. **Scoring values** come from FPL's published `settings.scoring`, with
    the standard 2026-27 table as the fallback; a difference from the
    standard table is logged as a warning.

### Rule 12 amendment: waiver order (2026-10-15)

The owner decided waivers should work like FPL Draft: reverse standings.
Each league has `waiverOrder` (contract: `settings.waivers.order`):

- **`reverse_standings`** (FPL Draft): every run starts from the
  standings after the league's finished gameweeks before it, worst
  first. Ties: league points, then fewer points for, then the team that
  joined the league first. Before any of the league's gameweeks has
  finished: the reverse of the draft's first round.
- **`rolling`** (the old rule): starts as the reverse of the draft's
  first round; the order a run ends with is the next run's start, so a
  winner stays at the back.
- **Within a run**, under both: teams go in that order, each tries its
  claims in its own order until one wins (the ones that cannot win lose
  on the way), then the next team; a team that wins moves to the back
  for the rest of the run, so nobody wins a second claim until every
  other team still claiming has had its turn.
- **Which leagues.** New leagues and imported FPL leagues (new imports
  and, by migration 13, existing ones) are `reverse_standings`; native
  leagues created before keep `rolling`. A re-sync keeps the
  commissioner's choice.
- **Changing it.** The commissioner can change it before the season or
  at any time in it; it applies from the next waiver run (the waivers
  screen and settings name its gameweek and time). While a run is due
  (its deadline has passed, it has not run) the change answers 409
  `waivers-processing`, so it is never unclear which rule a run used. A
  switch to `rolling` starts from the order that next run would have
  used under reverse standings.

**Evidence.** The real league's capture (`provider/fpl/testdata/real-gw6`,
taken after gameweek 6) has every waiver claim of gameweeks 1–6
(`transactions`: `event` is the run's gameweek, `index` the processing
order across the run, `priority` the claim's place in its team's own
list, `result` a / di / do) and today's `waiver_pick` per league entry.
Working the standings out from `matches` for each run:

1. **Before any result: the reverse of the draft's first round.** The
   gameweek 1 run processed BB, CC, II, EE, LL, KK, FF, GG (the eight
   teams with claims; teams by their fixture initials). The first round
   went GG, FF, DD, KK, LL, EE, JJ, II, CC, AA, BB, HH; reversed and
   without the four teams that made no claim, that is exactly the run's
   order. Join order would have been BB, CC, EE, FF, GG, II, KK, LL.
2. **The standings at the time of the run, after the previous
   gameweek.** The runs of gameweeks 2–6 follow the standings after
   gameweeks 1–5, and today's `waiver_pick` (after gameweek 6, before
   gameweek 7's run) is exactly the reverse of the table after gameweek 6
   (CC, HH, AA, II, DD, LL, FF, JJ, KK, EE, BB, GG). Only finished
   gameweeks count; FPL had finished each previous gameweek by its run.
3. **Ties: league points, then points for, then join order.** Fewer
   points for picks first on equal league points (for example the
   gameweek 6 run: KK, JJ, EE, FF and LL all on 9 points go LL 202, FF
   206, EE 254, JJ 259, KK 265). Four runs had teams level on both;
   every time the team with the lower FPL league-entry id, which is the
   earlier join, picked first: gameweek 2 JJ before LL (0 pts, 38 for),
   gameweek 3 CC before II (0, 46) and BB before GG (6, 79), gameweek 4
   DD before JJ (6, 153). Points against does not explain them (DD
   conceded more than JJ, BB fewer than GG, and both picked first), nor
   does the draft (DD drafted before JJ and picked first, BB drafted
   after GG and also picked first) or the previous run's order (gameweek
   2 had GG before BB). None of the four pairs had
   played each other yet, so head-to-head cannot be told apart, and FPL
   documents no tie-break: join order rests on these four cases.
4. **Within a run, a winner goes to the back.** The claims run in
   passes: in the gameweek 1 run CC won at index 2 and its next claim
   came at index 13, after the other seven teams; each team's failed
   claims (`di`, `do`) sit just before its winning one. "Moves to the
   back for the rest of the run" (RESEARCH.md §7) and "keeps its place
   for the next pass" give the same sequence, since every team still
   claiming after a pass won in it; we implement the former. Nothing
   carries over: the next run starts from the standings again.

`mirror.TestRealWaiverRunsReplayWithZeroDiffs` replays all six runs with
our engine (the squads before each run, FPL's claims in each team's
order, the start order from `league.WaiverOrder` on our own scores): all
177 claims (18, 45, 51, 23, 27, 13) are decided as FPL decided them, in
FPL's `index` order. Turning the join-order tie-break round makes it
find 86 differences, turning points for round 123. The import's
verification works the order out the same way and must equal FPL's
`waiver_pick` (`TestRealCaptureVerifiesWithZeroDiffs`; a flipped stored
result is caught, `TestVerifierCatchesWrongReverseStandings`). The
hand-worked cases are in `league/waiverorder_test.go`,
`waivers/order_test.go` and `waivers/switch_test.go`.

### Rule sources

Rules 1 to 4 and 10 were checked on 2026-10-12 against FPL's own pages
(the text ships in the sites' JavaScript bundles; the pages need a
browser):

- Playing and auto-subs, FPL rules "Prioritising Your Bench For Automatic
  Substitutions" (https://fantasy.premierleague.com/help/rules) and FPL
  Draft rules "Managing your squad" (https://draft.premierleague.com/help):
  "Playing in a Gameweek means making an appearance on the pitch or
  receiving a yellow / red card." The Draft FAQ "I think my automatic
  substitutions are wrong." says "A player is considered to have played if
  they appeared in the match for any length of time"; it does not mention
  cards, and we follow the rules text.
- Cards off the pitch: the scoring table lists "For each yellow card −1"
  and "For each red card −3" with no minutes condition, while appearance
  points are "For playing up to 60 minutes" / "60 minutes or more". No page
  states the bench case in words; scoring only the card is our reading of
  the table (an assumption until a golden test with such a player).
- Red cards, FPL rules "Clean sheets" (both sites): "If a player receives
  a red card, they will continue to be penalised for goals conceded by
  their team. Red card deductions include any points deducted for yellow
  cards."
- Outfield goalkeepers, FPL FAQ "How does FPL decide the player
  positions?" (https://fantasy.premierleague.com/help/faqs and the Draft
  help): "if an outfield player takes over goalkeeping responsibility (due
  to no substitute goalkeeper being available), they will be able to
  collect points for saves, but will score points as per their position
  classification (e.g. if a midfielder assumes goalkeeping
  responsibilities, he will only receive one point for a clean sheet)".

## Consequences

- The API is mostly writes now: the store needs strict transactions for the
  draft, waiver runs and trades (RESEARCH.md §9.3), and the one-owner-per-
  player rule becomes a database constraint.
- Golden tests against FPL's `explain` data remain the check on scoring:
  the engine must reproduce FPL's per-player points from the same stats.
- Without a mirror the app no longer depends on FPL Draft's league
  endpoints, but it still depends on the stats feed; a public App Store
  release still needs a licensed feed (ADR 0004, RESEARCH.md §8).
- Account data makes the app a data controller (UK GDPR): minimal profile
  data, deletion on request, no tracking.
- When this was decided the server faked accounts and league management
  in memory and answered the draft, lineup, waiver and trade operations
  with 501, while the MSW mocks implemented every operation so the screens
  could be built first. Backend phases 1 and 2 made all of it real (ADRs
  0009 to 0011).
