# 0009. Persistence: pgx, goose migrations and hand-written SQL

Status: accepted (2026-10-12).

## Context

ADR 0002 chose one Postgres 17 database. Phase 1 of the real backend
moves accounts and leagues off in-memory fakes, stores the global stats
feed (tier 1) and materialises scores. We need to decide three things:
the driver, how the schema evolves, and how Go code talks to it.

Constraints:

- Waivers, trades and draft picks (phase 2) are multi-row changes under
  rules that must be strict transactions with row locks (RESEARCH.md §9.3).
- Some rules belong in the database: one owner per player per gameweek
  (an exclusion constraint over `int4range`), one pick per player per
  draft, `UNIQUE NULLS NOT DISTINCT` for the player id map (ADR 0006).
- `go test ./...` and `npm run check` must keep running on a laptop or CI
  runner without Postgres; the contract test must keep validating every
  response.
- AGENTS.md: small, boring dependencies; `npm run gen` must stay cheap and
  reproducible.

## Decision

**Driver: pgx v5** (`github.com/jackc/pgx/v5`, `pgxpool`). It is the
de-facto Postgres driver for Go, speaks the native protocol (no
`database/sql` overhead), supports arrays, `jsonb`, batches and `COPY`,
and its `BeginTxFunc` gives us scoped transactions.
`store.InTx` retries serialization failures and deadlocks three times.

**Migrations: goose v3** (`github.com/pressly/goose/v3`), SQL files
embedded in the binary (`server/internal/store/migrations/*.sql`) and
applied with goose's `Provider` API. Chosen over golang-migrate (needs
paired up/down files and a separate CLI install, weaker embedding story)
and tern (pgx-native but much smaller user base). goose takes an advisory
lock, so `cmd/migrate up`, `cmd/seed` and `cmd/api` (with
`MIGRATE_ON_START=true`) can all run it safely. It reaches the database
through pgx's `stdlib` adapter.

**Query layer: hand-written SQL inside each domain package**, not sqlc.

- Each domain package owns its persistence behind a small repository
  interface: `auth.Service`, `league.Repo`, `stats.Repo`. Each has a
  `Postgres` implementation (pgx, SQL next to the Go that uses it) and a
  `Memory` implementation.
- The domain logic (league rules, scoring, ingest mapping) is written
  once, above the repository, so the two implementations only store and
  load. The league aggregate is loaded `FOR UPDATE`, changed in Go and
  saved, which keeps the rules in one place.
- One test suite per package runs against both implementations: Memory
  always, Postgres when `TEST_DATABASE_URL` is set (`storetest` clones a
  migrated template database per test). CI runs both.
- `httpapi` tests, including the contract test, use the Memory
  implementations, so they run anywhere.

sqlc was the alternative. It would type-check queries at generate time,
but: it builds `pg_query_go` with cgo (minutes on a clean CI runner, a C
toolchain for every contributor) or needs Docker inside `npm run gen`; most
of our queries are bulk loads into domain structs or upserts with
`IS DISTINCT FROM` guards, which sqlc maps no better than hand-written
scans; and the conformance suite already exercises every query against a
real database. We will revisit sqlc if the query count grows past what
the suites cover comfortably (phase 2 adds draft, waiver and trade
queries).

**Materialised vs derived.**

- `player_fixture_stats.points` and `team_gw_scores` are materialised by
  the poller's recompute (ADR 0005: recomputed from scratch, `rev` goes up
  only when a value changes). `matchups` carry the materialised points and
  score state.
- Standings are derived on read from `matchups` of finished gameweeks
  (at most 38 × 10 rows per league); there is no standings table to drift.
- Raw upstream payloads go to `stat_snapshots`, deduplicated by SHA-256, as
  the audit trail for score disputes.

**Ids.** Every public id column has a `CHECK` on its pattern (`lg_`,
`tm_`, ...). Clubs get an internal `clb_` id (the contract identifies clubs
by code). Provider ids live only in `player_external_id` and
`external_ref`.

## Consequences

- Two implementations per repository is more code than one; the shared
  suites are what keep them honest, and the Memory versions double as the
  database-less dev server (`npm run dev:server` without `DATABASE_URL`).
- Schema changes are new numbered SQL files; editing an applied migration
  is not allowed. `npm run test:go:db` starts a throwaway Postgres
  container (own name and free port, removed afterwards) and runs every suite against it; CI does
  the same with a service container.
- The schema already contains the phase-2 tables (drafts, waivers, trades,
  realtime events), reviewed once with the rest.
- River (ADR 0002's job queue) was not added in phase 1: the only job was
  the poller, a goroutine in `cmd/api` behind `POLLER_ENABLED` (it still
  is). River landed with the phase-2 jobs; see ADR 0010, which also moves
  the draft, waiver and trade tables' SQL into `league` (the State
  aggregate).
