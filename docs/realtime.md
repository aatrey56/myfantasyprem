# Realtime channel

Live draft picks, scores and trade updates are pushed over one WebSocket
per client. REST stays the source of truth: an event says what changed and
usually carries the new resource, and a client that misses events refetches.
The message schemas live in `api/openapi.yaml` (`RealtimeEvent`,
`RealtimePong`, `RealtimeServerMessage`, `RealtimeClientMessage` and the
`x-realtime` extension), so the TypeScript types are generated with the
rest of the contract. Design: [ADR 0011](adr/0011-realtime-websocket.md).

Status: built. The Go server serves tickets and the socket; the app opens
one socket per league screen stack (`apps/app/src/api/realtime.ts`) and
turns events into query invalidations, keeping its polling as the
fallback. The MSW mocks return a ticket but serve no socket, so the app
leaves realtime off with the mocks (and under Jest) and polls.

## Connecting

1. `POST /v1/realtime/tickets` with the bearer token, optionally
   `{"leagueId": "lg_..."}` (403 if the caller is not in it). The answer is
   `{ticket, url, expiresAt}`. Browsers cannot set an `Authorization`
   header on a WebSocket, so the ticket stands in for it: single use,
   valid for 60 seconds, bound to the user. `url` is
   `REALTIME_URL?ticket=...` when the server sets `REALTIME_URL`,
   otherwise the request's own host with `ws://` or `wss://`.
2. Open `url` (`wss://<host>/v1/realtime?ticket=...`). A missing, used or
   expired ticket gets HTTP 401 (problem+json) instead of the upgrade.
3. Send `{"op":"subscribe","leagueId":"lg_..."}` for each league the screen
   shows (the ticket's league is subscribed already). The server checks
   membership and closes the socket with code 4403 for a league the user is
   not in. `{"op":"unsubscribe","leagueId":"lg_..."}` stops one.
4. Send `{"op":"ping"}` every 25 seconds; the server answers with
   `{"type":"pong","serverTime":"..."}` (a message, because browsers cannot
   see WebSocket pong frames) and drops sockets silent for 60 seconds. It
   also sends WebSocket ping frames every 25 seconds.

Reconnect with exponential backoff (1 s doubling to 30 s, with jitter), a
new ticket each time, and `afterSeq` on `subscribe` to replay what was
missed.

Close codes: 4403 not (or no longer) a member of a league; 4401 the
session ended; 4400 a message that is not a JSON object with an `op`;
1013 the client fell behind (its send buffer of 64 messages filled); 1001
the server is shutting down. Unknown `op`s are ignored.

## Re-authorization

A ticket belongs to the session (bearer token) that asked for it, and a
socket keeps only what it was authorized with while that still holds:

- leaving a league or being removed from it closes the user's sockets
  subscribed to that league with 4403 (their cached team, which decides
  who sees private trade frames, is no longer theirs);
- logging out, or reusing a rotated-out refresh token (which revokes the
  session), closes that session's sockets with 4401; other devices keep
  theirs;
- deleting the account closes every socket of the user with 4401;
- no socket lives longer than an access token (`auth.AccessTTL`, one
  hour): it closes with 4401 and the client reconnects with a new ticket,
  which needs a valid session.

Unused tickets covered by a revocation are dropped too. On Postgres the
revocation is announced on the `realtime_control` NOTIFY channel, so every
instance closes its own sockets; a subscribe that races a revocation
fails with 4403.

## Ordering and gaps

Every event carries `leagueId`, `seq` and `sentAt`. `seq` increases by one
per league. The server keeps the last 5 minutes of events per league:

- `subscribe` with `afterSeq: n` replays events after `n` if they are still
  kept, in order, before any live event;
- otherwise, or when a client sees a gap in `seq`, it refetches the REST
  resources for that league and continues from the newest `seq`.

Delivery is at most once over a socket; correctness comes from `seq` plus
REST, never from the socket alone.

## Events

| `type`                | When                                                                                                                              | Who gets it                                         | Carries                                       |
| --------------------- | --------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------- | --------------------------------------------- |
| `draft.pick_made`     | A pick is made (manual, queue, rank or commissioner); several in one change (an autopick run) come as one `draft.updated` instead | League members                                      | The `DraftPick` and the next `currentPick`    |
| `draft.updated`       | Draft order, clock or settings set; starts, pauses, resumes, completes; autopick set; several picks at once                       | League members                                      | The whole `Draft`                             |
| `matchups.updated`    | A rescore moved the league's scores or score state (each poll, at most every 30 s)                                                | League members                                      | `gameweek`, `scoreState`, `MatchupSummary[]`  |
| `score.corrected`     | A stats correction changed an official or final team score (poller, `cmd/admin rescore`)                                          | League members                                      | The team, previous and new points             |
| `trade.updated`       | Any trade state change, review vote or decision                                                                                   | The two teams; the whole league once it is accepted | The `Trade` (with the receiver's own actions) |
| `waivers.processed`   | The waiver run for a gameweek finished                                                                                            | League members                                      | The gameweek; refetch claims and the feed     |
| `lineup.locked`       | The gameweek deadline passed (announced by the jobs' tick, within `JOBS_TICK`)                                                    | League members                                      | The gameweek                                  |
| `league.updated`      | Settings, members or names changed                                                                                                | League members                                      | Nothing; refetch the league                   |
| `trade_block.updated` | A team put a player on the trade block, changed or removed his entry, or he left the team (drop, waiver run, processed trade)     | League members                                      | Nothing; refetch the trade block              |

The trade block, player news and tags (migration 00015, see
[ARCHITECTURE](ARCHITECTURE.md#trade-block-player-news-and-tags)) add one
event, `trade_block.updated`. A player who leaves his team in a waiver
run or a processed trade comes off the block in the same save, so the app
also refetches the block on `trade.updated` and `waivers.processed`, and
every team's news (`['league', id, 'news']`), since the news follows the
roster. A re-sync from FPL sends `league.updated`, which refetches the
whole league, block and news included. Tags are private and news is
global stats data: neither has an event of its own.

Clients ignore event types they do not know (open enum), as with every enum
in the contract. SCORES (the gameweek's fixtures and a fixture's box score)
belongs to no league and has no socket of its own: `matchups.updated` and
`score.corrected` on any open league socket also refresh its queries, and
otherwise it polls while the gameweek is live and the screen has focus. Free-agent moves send no event; refetch the players and
the feed on focus.

## Server

- `server/internal/realtime`: the `Store` (per-league `seq` in
  `realtime_seq`, the replay buffer in `realtime_events`, tickets in
  `realtime_tickets`; memory and Postgres), the `Hub` (subscriptions,
  replay, per-team frames, backpressure) and `Listen`. It moves opaque
  JSON and never imports net/http.
- `server/internal/httpapi`: the ticket operation, the upgrade
  (`GET /v1/realtime`, authenticated by the ticket, not a bearer token)
  and the socket loop, and `Server.Notify`, which renders committed
  league changes (`league.Change` from the rule services, the jobs and the
  poller's scorer) into the events above and publishes them.
- Publishing appends the event (next `seq` and the row in one
  transaction) and runs `pg_notify('realtime', 'leagueId:seq')`; every API
  instance's `realtime.Listen` loop reads the row and fans it out to its
  sockets. Without a database the publisher delivers to the local hub.
- Trade events are rendered once per team (each with its own
  `viewerActions` and `myVote`) and, until the trade is accepted, go only
  to the two teams.
- The handler clears the connection's read and write deadlines before the
  upgrade (`cmd/api` sets a 10 s `ReadTimeout` and 15 s `WriteTimeout`),
  and `cmd/api` closes every socket when it shuts down.
- `Hub.Revoke` closes the local sockets a `realtime.Revocation` covers;
  `httpapi` calls it (and `Store.Revoke`, which notifies the other
  instances) after `LeaveLeague`, `RemoveMember`, `DeleteMe`, `Logout`
  and a refresh that revoked its session. A socket registers with the hub
  (close callback included) before the handler re-checks its session, so
  a revocation that lands between redeeming the ticket and registering
  still closes it.
- The prune job drops events older than 5 minutes and expired tickets.
