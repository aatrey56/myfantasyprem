# 0011. Realtime over WebSocket with a Postgres event log

Status: accepted (2026-10-12). Implements the plan in
[docs/realtime.md](../realtime.md) and ADR 0008's realtime decision.

## Context

The draft room, live matchups and trades want pushes; REST stays the
source of truth. Browsers cannot send an `Authorization` header on a
WebSocket, the API may run as several instances, `cmd/api` sets a 15 s
`WriteTimeout` on every connection, and the app must keep working (by
polling) when the socket is down. Some events differ per member: a
trade's `viewerActions` and `myVote`, and offers that only the two teams
may see.

## Decision

- **Library: `github.com/coder/websocket`.** Small, maintained, context
  aware, safe for a concurrent close. The standard library has no
  WebSocket server; gorilla/websocket was the alternative.
- **Tickets.** `POST /v1/realtime/tickets` stores the SHA-256 of a random
  ticket (`realtime_tickets`, single use, 60 s) bound to the user and
  optionally a league; `GET /v1/realtime?ticket=` redeems it before the
  upgrade (401 problem otherwise). The URL is `REALTIME_URL` or the
  request's host with `ws`/`wss`.
- **Event log.** A change is reported after its transaction commits
  (`league.Notifier`); `httpapi` renders the contract's event from the
  state at that moment (so REST and the event agree) and appends it to
  `realtime_events` with the league's next `seq` (an upserted counter in
  `realtime_seq`, in the same transaction as the insert). A row holds the
  body most members get, per-team overrides (trades) and an audience
  (trades not yet accepted: the two teams). The log keeps five minutes
  for `afterSeq` replay (the prune job).
- **Fan-out across instances.** The append notifies channel `realtime`
  with `leagueId:seq`; every instance runs `realtime.Listen` (one connection,
  `LISTEN realtime`), reads the row and hands it to its hub. Without a
  database the publisher delivers to the local hub directly. Processes
  without sockets (`cmd/admin rescore`, the poller in any instance) publish
  through the same log.
- **Hub.** Per connection: subscriptions with the member's team, the last
  `seq` sent per league (duplicates dropped), a replay on subscribe that
  holds live events back until the missed ones are sent, and a send
  buffer of 64 frames; a client that fills it is closed (1013, "slow
  consumer") and reconnects with `afterSeq`.
- **Socket.** One writer goroutine (events, pongs, a WebSocket ping every
  25 s) and the reader: `subscribe` (membership checked; 4403 close
  otherwise), `unsubscribe`, `ping` (answered with a `pong` message),
  unknown ops ignored, a malformed message closes with 4400, 60 s of
  silence closes. The handler clears the connection's read and write
  deadlines (cmd/api's `ReadTimeout`/`WriteTimeout`) before the upgrade;
  `API.Close` ends every socket on shutdown, which `http.Server.Shutdown`
  does not wait for.
- **Contract change.** Browsers cannot see pong frames, so the heartbeat
  answer is a JSON message: `RealtimePong {type: pong, serverTime}`, with
  `RealtimeServerMessage` (event or pong) as `x-realtime.serverMessages`.

## Consequences

- Delivery is at most once per socket. A client that sees a `seq` gap, or
  reconnects after five minutes, refetches over REST. Events published
  while an instance's listener reconnects reach its clients only as a gap.
- Each event is rendered once (per team for trades), not per connection.
- Several picks in one commit (an autopick run, up to a whole draft) are
  sent as one `draft.updated` with the board, so the end of a draft cannot
  overflow the 64-frame buffer and drop every client.
- An event says what changed as of commit; clients that act on it refetch
  anyway, so a rendering race with a later change cannot mislead them.
- `matchups.updated` is sent whenever a rescore moves a league's scores or
  state (the poller rescores at most every 30 s), not throttled further.
