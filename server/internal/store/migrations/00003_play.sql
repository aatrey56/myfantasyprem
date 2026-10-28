-- Phase 2: the draft engine, waivers, trades, realtime and job bookkeeping
-- (docs/ARCHITECTURE.md "Data model", docs/adr/0010, docs/adr/0011).
-- River's own tables (river_job, ...) are applied by store.Migrate through
-- rivermigrate after these, so they follow the River version in go.mod.

-- +goose Up

-- Autopick is per team and can be set before the order exists.
ALTER TABLE draft_order DROP COLUMN autopick;
CREATE TABLE draft_autopick (
    draft_id text NOT NULL REFERENCES drafts (id) ON DELETE CASCADE,
    team_id  text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    PRIMARY KEY (draft_id, team_id)
);

-- Once-per-gameweek league work that already ran (waiver run, lineup lock
-- announcement): the idempotency marker for the jobs.
CREATE TABLE league_runs (
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    gameweek  int NOT NULL CHECK (gameweek BETWEEN 1 AND 38),
    kind      text NOT NULL,                 -- waivers | lineup_lock
    ran_at    timestamptz NOT NULL,
    PRIMARY KEY (league_id, gameweek, kind)
);

-- A player added or dropped stays locked (dropped: on waivers; added: not
-- droppable) until the waiver run of until_gw.
CREATE TABLE waiver_locks (
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    player_id text NOT NULL REFERENCES players (id),
    until_gw  int NOT NULL CHECK (until_gw BETWEEN 1 AND 39),
    reason    text NOT NULL CHECK (reason IN ('added', 'dropped')),
    PRIMARY KEY (league_id, player_id)
);

CREATE INDEX waiver_claims_team_idx ON waiver_claims (team_id, status);

ALTER TABLE trades
    ADD COLUMN effective_gw    int NULL CHECK (effective_gw BETWEEN 1 AND 38),
    ADD COLUMN review_mode     text NULL,
    ADD COLUMN decision_reason text NULL;
CREATE INDEX trades_open_idx ON trades (league_id) WHERE status IN ('offered', 'accepted', 'in_review');
-- The order players were listed in (offered[i] pairs with requested[i]).
ALTER TABLE trade_items ADD COLUMN ord int NOT NULL DEFAULT 0;

-- Realtime: a per-league sequence and the 5-minute replay buffer. payload
-- is the event as most members see it; per_team overrides it for a team
-- (trade viewer actions); audience limits delivery to those teams.
CREATE TABLE realtime_seq (
    league_id text PRIMARY KEY REFERENCES leagues (id) ON DELETE CASCADE,
    seq       bigint NOT NULL
);
ALTER TABLE realtime_events
    ADD COLUMN audience text[] NULL,
    ADD COLUMN per_team jsonb NULL;
CREATE INDEX realtime_events_sent_idx ON realtime_events (sent_at);

-- Single-use WebSocket tickets (only the hash is kept).
CREATE TABLE realtime_tickets (
    ticket_sha256 text PRIMARY KEY,
    user_id       text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id     text NULL REFERENCES leagues (id) ON DELETE CASCADE,
    expires_at    timestamptz NOT NULL,
    used_at       timestamptz NULL
);

-- Snapshots of a gameweek's final data are kept forever; others are pruned
-- to the newest N per kind and gameweek (cmd/prune, the prune job).
ALTER TABLE stat_snapshots ADD COLUMN final boolean NOT NULL DEFAULT false;

-- +goose Down
ALTER TABLE stat_snapshots DROP COLUMN final;
DROP TABLE IF EXISTS realtime_tickets, realtime_seq, waiver_locks, league_runs, draft_autopick;
DROP INDEX IF EXISTS realtime_events_sent_idx, trades_open_idx, waiver_claims_team_idx;
ALTER TABLE realtime_events DROP COLUMN audience, DROP COLUMN per_team;
ALTER TABLE trade_items DROP COLUMN ord;
ALTER TABLE trades DROP COLUMN effective_gw, DROP COLUMN review_mode, DROP COLUMN decision_reason;
ALTER TABLE draft_order ADD COLUMN autopick boolean NOT NULL DEFAULT false;
