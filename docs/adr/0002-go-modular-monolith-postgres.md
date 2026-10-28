# 0002. Go modular monolith and Postgres

Status: accepted

## Context

The backend polls FPL data, mirrors leagues, computes scores and serves a REST
API. Later phases add WebSockets, push notifications, waivers and drafts, which
need strict database transactions (RESEARCH.md section 9.2). The team is small.
Go packages from an earlier project can be reused.

## Decision

One Go service, one Postgres database, one deployable. Code is split into
internal packages with one-way dependencies:

- `internal/httpapi`: generated routes and handlers. No business logic.
- `internal/provider`: the `StatsProvider` interface (ADR 0004).
- `internal/league`: league domain logic.
- `internal/scoring`: pure score and auto-sub functions.
- `internal/store`: Postgres access.

Standard library `net/http` and `log/slog`. Postgres 17 (docker-compose for
local use). Background jobs will run inside the same binary.

## Consequences

- One thing to deploy, observe and debug. No network hops between modules.
- Boundaries are enforced by package imports and review, not by processes, so
  they need care. `scoring` must never import `store` or `net/http`.
- Scaling is vertical first. If the poller or WebSocket hub needs to scale
  separately, the packages already mark where to cut.
- Not Supabase or Firebase: waivers, trades and picks change many rows under
  complex rules and need Go-side transactions.
