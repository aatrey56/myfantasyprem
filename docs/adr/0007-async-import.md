# 0007. Asynchronous league import

Status: accepted. Deprecated for v1 by [0008](0008-native-leagues.md) (`x-status: later`); the endpoints and the fake job stay.

## Context

Mirroring an FPL Draft league reads league details, element status, draft
choices, transactions, trades and every entry's picks for every played
gameweek. That is about 85 upstream requests for a 12-team league at
gameweek 6, and about 650 for a 16-team league at gameweek 38. At a polite
rate of at most 2 requests per second the larger import takes over five
minutes, far longer than an HTTP request should stay open.

Other constraints:

- Draft and classic league ids overlap, so a classic link must be refused
  rather than silently importing a different league.
- FPL's `draft_status` is unreliable; a completed draft is detected via
  `drafts[].draft_completed`.
- Two members of a league may import it at the same moment.

## Decision

Import is a job:

- `GET /v1/imports/preview?draftLeagueUrl=…` (or `fplLeagueId`) fetches the
  league details only and returns name, team count and managers, so the user
  can confirm "Is this your league?".
- `POST /v1/imports` with `{draftLeagueUrl}` or `{fplLeagueId}` returns
  **202** with `Location: /v1/imports/{importId}`, `Retry-After` and the
  Import. If an import for the same FPL league is `queued`, `running` or
  `succeeded`, it returns **200** with that Import instead (idempotent). A
  `failed` import does not block a retry.
- `GET /v1/imports/{importId}` returns
  `{id, fplLeagueId, status: queued|running|succeeded|failed,
progress: {done, total}, error: Problem|null, leagueId|null}`.
- Classic links (fantasy.premierleague.com) get **422** with problem type
  `classic-league-url`.

Implementation (when the store lands): [River](https://riverqueue.com) on
the same Postgres.

- The API handler inserts the job with `InsertTx` in the same transaction
  that writes the `imports` row, so a job exists if and only if the row
  does.
- `UniqueOpts{ByArgs: true}` on the job args (the FPL league id) makes a
  second insert for the same league a no-op while the first is active.
- Each step (league, entries, draft, transactions, trades, picks per
  gameweek) is an idempotent upsert keyed on upstream ids, and progress is
  written after each step. A retried or duplicated job converges on the same
  rows.
- The FPL client is rate limited to at most 2 requests per second across all
  jobs, with backoff on errors and a clear User-Agent.

Until then the server fakes the job in memory
(`server/internal/httpapi/server.go`): it moves from queued to running to
failed with a "not implemented" problem, so clients can exercise the flow.
The MSW mocks fake a successful import.

## Consequences

- The client polls; the import screen shows progress and survives a slow
  upstream.
- One more moving part (the job runner), but it runs in the same binary and
  database (ADR 0002), with no extra infrastructure.
- Idempotency is enforced twice, by River's unique jobs and by the 200 path
  in the handler, so a double tap or two managers importing together create
  one mirror.
