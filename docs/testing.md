# Testing

What each test layer proves, how strong the rule tests are, and the one rule
every new test follows.

## The rule

**Expected values come from the rules or the spec, never from the
implementation.** The sources are `docs/research/RESEARCH.md` §7 (FPL Draft
rules), ADR 0008 "Scoring and rules", ADR 0010 and `api/openapi.yaml`. Work
the number out by hand and put the arithmetic in a comment:

```go
// Priority A, B, C. pass 1: A's first claim wins -> B, C, A ...
```

Do not compute `want` by calling the function under test, or a helper that
copies its algorithm. Do not snapshot whatever the code returns. Do not
assert only "no error" when the rule gives an exact answer. Do not use a
bound such as `>= n` when the rule gives an exact count. A property test
may use an oracle, but the oracle is written from the rule table and shares
no code with the package (see `scoring/property_test.go`).

## Layers

| Layer                  | Where                                                                                                                                               | Proves                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ---------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Rule unit tests        | `server/internal/{scoring,draft,lineup,waivers,trades,league,projection}/*_test.go`                                                                 | Each FPL Draft rule at its boundaries: points table, bonus ties, auto-subs, formation, snake order, pick clock, waiver priority and locks, veto threshold, trade shape and deadlines, standings and schedule; fixture difficulty, Expected and start/sit from hand-worked tables (ADR 0014).                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Property tests (rapid) | `scoring/property_test.go`, `draft/rules_test.go`, `league/standings_rules_test.go`, `leaguetest/invariants_test.go`, `projection/property_test.go` | Invariants for any input. Points match the table. Bonus matches competition ranking. Auto-subs keep a legal XI and the total is the XI's points. The snake gives one pick per team per round, reversing each round. The optimal XI is legal and equals the best of every legal XI (brute force); win odds stay in [0, 1], sum to 1 and swap exactly with the sides; a match at minute 0 is the pre-kickoff draw; a side whose worst case beats the other's best wins for certain. The round-robin has every pair once per cycle, with home and away swapping. After any sequence of picks, claims, free-agent moves, waiver runs and trades, every player has one owner at most and every roster is 2/5/5/3. |
| Golden scoring         | `league/score_test.go`, `stats/ingest_test.go`                                                                                                      | FPL testdata reproduces FPL's own `explain` totals and the team totals worked out by hand.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Store and API          | `league/state_test.go`, `httpapi/*_test.go`, `test:go:db`                                                                                           | Memory and Postgres agree. Handlers follow the contract (`{data, meta}`, problem+json, ids).                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Client helpers (Jest)  | `apps/app/src/lib/*.test.ts`                                                                                                                        | The UI's copies of the rules (trade shape, formation hints, snake board, standings sorts) and their edge cases.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                              |
| Screens and e2e        | `apps/app/src/__tests__`, `e2e/`                                                                                                                    | Screens render on the mocks; the main flows work in a browser. Every spec imports `test` from `e2e/fixtures.ts`, which fails any request outside localhost.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |

## Mutation scores

A mutation tool makes small changes to the code, such as `>=` → `>`,
`&&` → `||`, or `+` → `-`, and reruns the tests. A test suite that still
passes has not pinned that line down. The score is killed / (killed +
survived), counting only mutants that a test reaches.

Go (gremlins v0.6.0, default mutators plus `INVERT_LOGICAL`), measured on
2026-10-12:

| Package              | Before          | After           | Survivors left, and why                                                                                                                                                                    |
| -------------------- | --------------- | --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `scoring`            | 116/129 (89.9%) | 121/129 (93.8%) | 8, all equivalent: sort comparators where ties cannot happen, divisors that are never 0, and `min(y,1)*-1` ≡ `/-1`.                                                                        |
| `draft`              | 171/214 (79.9%) | 198/216 (91.7%) | 18: race and error paths in `ensureDraft` and `Expire`; tie-breaks in autopick ranking that are equivalent; a pre-check repeated inside the tx.                                            |
| `lineup`             | 13/16 (81%)\*   | 26/32 (81%)     | 6: error and early-return paths in `LockDue`, and the wording of the lock message. \*Before, the 16 mutants in `LockDue` had no test at all.                                               |
| `waivers`            | 103/125 (82.4%) | 122/126 (96.8%) | 4: error-message wording, an error path, and one equivalent guard.                                                                                                                         |
| `trades`             | 136/170 (80.0%) | 157/171 (91.8%) | 14: early exits that only save work (`due`, `CloseDue`), shape guards made redundant by the position check, and close-time re-checks that votes already decide.                            |
| `league` (all files) | 246/308 (79.9%) | 260/311 (83.6%) | Mostly the Postgres code (not covered without a DB) and the recompute and notify plumbing. Rotation-direction mutants in `RoundRobin` are equivalent: any rotation is a valid round robin. |

The totals move slightly between runs, because a mutant that times out under
load is not counted.

App (StrykerJS on `apps/app/src/lib/{draft,lineup,matchup,standings,trade,waivers}.ts`):

| Before            | After                                         |
| ----------------- | --------------------------------------------- |
| 75.0% (667 / 889) | 81.8% (727 / 889); covered-only 81.3% → 87.2% |

Most of the app's remaining survivors are display strings, such as a label
or a pill tone. They are not rules.

## Rerunning

```sh
npm run test:mutation              # both (about 30 minutes)
npm run test:mutation:go           # gremlins on the six packages
npm run test:mutation:go -- trades # one package
npm run test:mutation:app          # StrykerJS, config in apps/app/stryker.config.json
```

These runs are slow and advisory. They are **not** part of `.office/checks`.
Run them after changing a rule package, and look at every new survivor.
Either add a rule-derived test that kills it, or note here why it is
equivalent. Run them one at a time: Jest's 5-second test timeout trips when
gremlins is also using every core.

## Known bugs pinned by skipped tests

None. The late-trade bug that `trades.TestFreeAgentDropOfAPlayerTradedForNextGameweek`
and `trades.TestClaimDropOfAPlayerTradedForNextGameweek` pinned is fixed
(`c80ff2a`): a player in a trade processed after gameweek N's trade
deadline, which moves him from N+1, cannot be dropped in N's free agency
(409 `player-in-trade`) or at N's waiver run (the claim loses with
`roster_locked`). Both tests run, and `leaguetest.TestRosterInvariants` no
longer steers around the case: claims, free-agent drops and trades draw
from the whole roster. It runs on the memory stores and, with
`TEST_DATABASE_URL`, on Postgres (one database, a new league per check).
On 2026-10-13, without the filters: 10,000 checks passed on memory
(`-rapid.checks=10000`, 18 m 39 s), and 2,318 checks over five Postgres
runs (seeds 1 to 5) passed before they were stopped to save time (about
0.9 s a check on Postgres; 10,000 would take 2.5 h on one process).

The property test orders players by draft rank, not by id. Ids are random
ULIDs, so an id order gave the same seed different players on each run, and
a rapid fail file could not replay the failure.
