# AGENTS.md

Guidance for anyone (human or agent) changing this repo. Details live in
README.md, docs/ARCHITECTURE.md and docs/adr/.

## Layout and tooling

- npm workspaces (`apps/*`, `packages/*`) plus a Go module in `server/`.
  Use plain npm. No pnpm, yarn, bun or corepack.
- `apps/app`: Expo (Expo Router) app, web first. `packages/api-client`:
  generated TS types + openapi-fetch client. `server`: Go API.
- Add Expo packages from `apps/app` with `npx expo install <pkg>`, then pin
  the exact version (the repo uses `save-exact`).

## Root scripts

| Script                                                             | Use                                                                                    |
| ------------------------------------------------------------------ | -------------------------------------------------------------------------------------- |
| `npm run gen`                                                      | Regenerate TS types and Go server from `api/openapi.yaml`.                             |
| `npm run check`                                                    | lint, format check, typecheck, Jest, script tests, Go.                                 |
| `npm run build:web`                                                | Production SPA into `apps/app/dist` (no mocks).                                        |
| `npm run verify:prod-bundle`                                       | Fails if mock code reached `apps/app/dist`.                                            |
| `npm run build:web:mocks`                                          | Mock SPA into `apps/app/dist-mocks` (Playwright uses it).                              |
| `npx playwright test`                                              | e2e against `dist-mocks`.                                                              |
| `npm run dev:web` / `npm run dev:web:mocks` / `npm run dev:server` | Local development.                                                                     |
| `npm run dev:stack`                                                | Postgres + migrate + demo seed + API (demo data, no FPL).                              |
| `npm run test:go:db`                                               | Go tests incl. Postgres suites (Docker, own container).                                |
| `npm run admin -- <cmd>`                                           | `cmd/admin`: rescore, prune-snapshots, tick, run-waivers, fetch-history, shadow-check. |
| `npm run test:e2e:server`                                          | Playwright smoke against the real API (Docker; not gated).                             |

`.office/checks` lists the gate: every line must pass from the repo root.

Several worktrees run tests on one machine at once. Never kill processes by
name or pattern (`pkill -f "go test"`, `killall node`): that stops every
worktree's run, and `npm run test:go:db` then ends after whichever package
last reported. Kill only the PIDs you started.

## Conventions

- **Contract first.** After editing `api/openapi.yaml`, run `npm run gen`
  and commit the generated files. CI fails on drift.
- Every success body is `{data, meta}`; errors are problem+json. Public ids
  are opaque prefixed ULIDs (`lg_`, `tm_`, `plr_`, ...), never FPL integers.
  Enums are open: handle unknown values.
- Read env vars only as `process.env.EXPO_PUBLIC_X` (dot form). Guard any
  mock import with an inline `process.env.EXPO_PUBLIC_USE_MOCKS === 'true'`.
- Server: std `net/http` and `log/slog`; no `internal/` package except `httpapi` imports
  `net/http`, besides upstream HTTP clients (`provider/fpl`, `idp`); `scoring` does no I/O.
- Persistence (ADR 0009): pgx v5, goose migrations in `server/internal/store/migrations`
  (add a file; never edit one that ran). Each domain package owns its SQL and keeps a
  memory implementation; one test suite runs on both (`storetest`, `TEST_DATABASE_URL`).
- Dates and times on screen go through `apps/app/src/lib/time.ts` (its header
  lists the rules: scheduled moments, kickoffs, time left, past events).
- Tests next to code (`*.test.ts(x)`, `*_test.go`); e2e in `e2e/`.
- Conventional Commits, small and logical.

## Hard rules

- Official club crests, kit images and player photos are allowed only behind
  the branded-assets switch (server `BRANDED_ASSETS`, build-time
  `EXPO_PUBLIC_BRANDED_ASSETS`; both must allow it). It is on for private
  builds and must be off for App Store and other public builds (Apple's
  intellectual-property guideline). Production must set it explicitly. The
  app never hotlinks upstream: images come only through `/v1/assets/*`. No
  Premier League or FPL logos anywhere.
- No in-app "Not affiliated" disclaimer or FPL/Premier League descriptive
  copy: the owner had it removed on 2026-10-14. A legal notice belongs in the
  store listing and the terms when we publish. Functional labels for the FPL
  Draft import (e.g. "Import from FPL Draft", the league ID field) stay.
- Never collect FPL logins.
- Never call FPL servers from tests or scripts; use the mocks and fixtures.
