# 0015: Notifications (inbox, preferences, Web Push, outbox)

Status: accepted, 2026-10-17

## Context

Sleeper pings you when a trade lands, waivers clear or your player
scores. The owner wants the same, every type on by default with a switch
per type and league, and pushes on the web now (no Apple developer
account yet), native later.

## Decision

`server/internal/notifications` owns it. Producers turn domain events
into `Notification`s, each with a dedupe key unique per user; the store
ignores a key it has seen, so a re-poll, a retried job or a second
instance never sends twice.

- Every notification lands in the inbox (the FEED tab). Preferences
  (`notification_prefs`, a row only for a switch turned away from the
  default, so new types start on) decide only whether it is also queued
  for push.
- Push goes through an outbox (`push_outbox`, one row per notification
  and device). The `Dispatcher` claims due rows with a lease (`FOR UPDATE
SKIP LOCKED`), retries with backoff (30 s, 2 m, 10 m, 30 m, 2 h, then
  gives up) and drops a device the push service calls gone (404, 410).
- Deliverers: `notifications/webpush` (RFC 8291 encryption, RFC 8292
  VAPID, standard library only) and `Expo`, a stub that drops native
  deliveries until the iOS app ships.
- Producers:
  - league changes (`Server.Notify`, which the rule services, jobs and
    poller already call): trades by status, waiver results, "your pick";
  - the jobs tick (`Runner.Reminders`): draft starting, waivers due, free
    agency closing, an empty or invalid XI, each 1 hour ahead, keyed by
    the deadline;
  - the scorer (`league.LiveHook`): goals, assists, own goals, reds,
    defensive contribution points and clean sheets for the owner and the
    owner's opponent that gameweek;
  - the poller's bootstrap ingest (`poller.NewsHook`): availability or
    news text changes.

Live events are keyed by ordinal ("goal 2 of player P in fixture F"), so
a correction that removes a goal sends nothing, and giving it back sends
nothing either. Reds, DC points and clean sheets are once per match. The
stats feed has minutes, not substitutions, so a player whose minutes have
not moved for 5 minutes while his match is live, below 85 minutes and
without a red card, counts as off; with 60+ minutes and no goal conceded
while on (FPL's own goals_conceded), his clean sheet goes out then. Goals
after he went off do not count against him, so nothing is taken back.
Live events notify only within 6 hours of kickoff, so backfills and old
rescores stay quiet.

## Consequences

- One more table set and a background dispatcher per job instance.
- The "off" guess can wait until full time (a sub in stoppage time, or a
  quiet league whose scores did not move), never send early for a player
  still on, except a substitute who came on and played 60+ minutes
  before going quiet; documented, rare.
- Native push needs only an `Expo` implementation and the app's token
  registration (`kind: expo` is already accepted and stored).
