-- Importing an FPL Draft league, syncing it until the switch-over and
-- verifying it (docs/adr/0012).

-- +goose Up

-- One FPL league may be imported by several users, each into their own
-- league (ADR 0012), so these are no longer unique across the database.
ALTER TABLE leagues DROP CONSTRAINT leagues_fpl_league_id_key;
ALTER TABLE transactions DROP CONSTRAINT transactions_fpl_txn_id_key;

-- An imported league's sync: syncing (FPL is the source), switch_scheduled
-- (confirmed, flips at switch_gameweek), native (our engines own it).
-- NULL for a league created here.
ALTER TABLE leagues
    ADD COLUMN sync_state          text NULL,
    ADD COLUMN switch_gameweek     int NULL CHECK (switch_gameweek BETWEEN 1 AND 38),
    ADD COLUMN switch_confirmed_at timestamptz NULL,
    ADD COLUMN switch_confirmed_by text NULL REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN switched_at         timestamptz NULL;
CREATE INDEX leagues_fpl_idx ON leagues (fpl_league_id) WHERE fpl_league_id IS NOT NULL;

-- The FPL manager's name of an imported seat, so the commissioner knows
-- whom to send its claim link.
ALTER TABLE teams ADD COLUMN fpl_manager_name text NULL;

-- A claim invite gives its user that (manager-less) team instead of a new one.
ALTER TABLE invites ADD COLUMN team_id text NULL REFERENCES teams (id) ON DELETE CASCADE;

-- Upstream ids of a league's own rows (teams, matches, transactions,
-- trades), so a re-run of an import or sync finds what it made.
CREATE TABLE league_refs (
    league_id   text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    entity      text NOT NULL,               -- team | match | transaction | trade
    provider    text NOT NULL,
    external_id text NOT NULL,
    internal_id text NOT NULL,
    PRIMARY KEY (league_id, entity, provider, external_id),
    UNIQUE (league_id, entity, internal_id)
);

-- Import jobs (POST /v1/imports). league_id is minted with the row, so a
-- retried job creates exactly that league.
CREATE TABLE imports (
    id            text PRIMARY KEY CHECK (id ~ '^imp_[0-9a-hjkmnp-tv-z]{26}$'),
    user_id       text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    fpl_league_id bigint NOT NULL,
    seat          int NOT NULL,
    status        text NOT NULL,             -- queued | running | succeeded | failed
    done          int NOT NULL DEFAULT 0,
    total         int NOT NULL DEFAULT 0,
    error         jsonb NULL,                -- problem when failed
    league_id     text NOT NULL,
    created_at    timestamptz NOT NULL,
    updated_at    timestamptz NOT NULL
);
-- At most one active or successful import per user and FPL league.
CREATE UNIQUE INDEX imports_active_idx ON imports (user_id, fpl_league_id)
    WHERE status IN ('queued', 'running', 'succeeded');

-- Decoded picks of finished gameweeks (they never change), per provider
-- entry and gameweek: a re-run does not fetch them again.
CREATE TABLE mirror_picks (
    provider text NOT NULL,
    entry_id text NOT NULL,
    gameweek int NOT NULL CHECK (gameweek BETWEEN 1 AND 38),
    picks    jsonb NOT NULL,
    PRIMARY KEY (provider, entry_id, gameweek)
);

-- A synced league's schedule and last outcome.
CREATE TABLE mirror_sync (
    league_id            text PRIMARY KEY REFERENCES leagues (id) ON DELETE CASCADE,
    last_attempt_at      timestamptz NULL,
    last_synced_at       timestamptz NULL,
    next_sync_at         timestamptz NOT NULL,
    last_error           text NULL,
    consecutive_failures int NOT NULL DEFAULT 0,
    note                 text NULL               -- the switch-over's progress
);
CREATE INDEX mirror_sync_due_idx ON mirror_sync (next_sync_at);

-- Verification reports: every check of an import or sync.
CREATE TABLE mirror_reports (
    id         bigserial PRIMARY KEY,
    league_id  text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    checked_at timestamptz NOT NULL,
    status     text NOT NULL,                -- clean | mismatch | incomplete
    items      jsonb NOT NULL
);
CREATE INDEX mirror_reports_league_idx ON mirror_reports (league_id, checked_at DESC);

-- The commissioner's explanations of failing items: one holds only while
-- the item shows the same values.
CREATE TABLE mirror_explanations (
    league_id    text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    key          text NOT NULL,
    ours         text NOT NULL,
    theirs       text NOT NULL,
    note         text NOT NULL,
    explained_by text NULL REFERENCES users (id) ON DELETE SET NULL,
    explained_at timestamptz NOT NULL,
    PRIMARY KEY (league_id, key)
);

-- +goose Down
DROP TABLE mirror_explanations;
DROP TABLE mirror_reports;
DROP TABLE mirror_sync;
DROP TABLE mirror_picks;
DROP TABLE imports;
DROP TABLE league_refs;
ALTER TABLE invites DROP COLUMN team_id;
ALTER TABLE teams DROP COLUMN fpl_manager_name;
DROP INDEX leagues_fpl_idx;
ALTER TABLE leagues
    DROP COLUMN sync_state,
    DROP COLUMN switch_gameweek,
    DROP COLUMN switch_confirmed_at,
    DROP COLUMN switch_confirmed_by,
    DROP COLUMN switched_at;
ALTER TABLE transactions ADD CONSTRAINT transactions_fpl_txn_id_key UNIQUE (fpl_txn_id);
ALTER TABLE leagues ADD CONSTRAINT leagues_fpl_league_id_key UNIQUE (fpl_league_id);
