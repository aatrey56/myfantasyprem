-- The whole v1 schema (docs/ARCHITECTURE.md "Data model", docs/adr/0009).
-- Keys are our public ids (text, prefixed ULID) unless noted. Tables for
-- phase-2 features (draft, waivers, trades, realtime) exist now so the
-- schema is reviewed once; nothing writes them yet.

-- +goose Up

-- btree_gist lets the roster exclusion constraint mix text equality with
-- range overlap.
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- ------------------------------------------------------------- tier 1: stats
-- Written only by the poller (and cmd/seed); shared by every league.

CREATE TABLE seasons (
    id                text PRIMARY KEY CHECK (id ~ '^\d{4}-\d{2}$'),
    current_gameweek  int NOT NULL DEFAULT 1 CHECK (current_gameweek BETWEEN 1 AND 38),
    next_gameweek     int NULL CHECK (next_gameweek BETWEEN 1 AND 38),
    waivers_processed boolean NOT NULL DEFAULT false,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE clubs (
    id     text PRIMARY KEY,                 -- clb_ + ULID, internal only
    season text NOT NULL REFERENCES seasons (id),
    code   text NOT NULL CHECK (length(code) BETWEEN 2 AND 4),
    name   text NOT NULL,
    UNIQUE (season, code)
);

CREATE TABLE players (
    id           text PRIMARY KEY CHECK (id ~ '^plr_[0-9a-hjkmnp-tv-z]{26}$'),
    first_name   text NOT NULL DEFAULT '',
    last_name    text NOT NULL DEFAULT '',
    display_name text NOT NULL,
    position     text NOT NULL,              -- open enum: GKP DEF MID FWD
    club_id      text NOT NULL REFERENCES clubs (id),
    availability text NOT NULL,              -- open enum
    news         text NOT NULL DEFAULT '',
    draft_rank   int NOT NULL CHECK (draft_rank >= 1),
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX players_club_idx ON players (club_id);

-- ADR 0006: providers' player ids map to ours. season NULL = permanent id.
CREATE TABLE player_external_id (
    player_id    text NOT NULL REFERENCES players (id) ON DELETE CASCADE,
    provider     text NOT NULL,
    external_id  text NOT NULL,
    season       text NULL,
    match_method text NOT NULL CHECK (match_method IN ('exact_key', 'dob_name', 'manual')),
    confidence   real NOT NULL CHECK (confidence BETWEEN 0 AND 1),
    verified_at  timestamptz NULL,
    UNIQUE NULLS NOT DISTINCT (provider, external_id, season)
);
CREATE INDEX player_external_id_player_idx ON player_external_id (player_id);

-- The same map for clubs and fixtures, which only ever match on exact keys.
CREATE TABLE external_ref (
    entity      text NOT NULL CHECK (entity IN ('club', 'fixture')),
    provider    text NOT NULL,
    external_id text NOT NULL,
    season      text NULL,
    internal_id text NOT NULL,
    UNIQUE NULLS NOT DISTINCT (entity, provider, external_id, season)
);

CREATE TABLE gameweeks (
    season             text NOT NULL REFERENCES seasons (id),
    number             int NOT NULL CHECK (number BETWEEN 1 AND 38),
    name               text NOT NULL,
    deadline_at        timestamptz NOT NULL,
    waiver_deadline_at timestamptz NOT NULL,
    trade_deadline_at  timestamptz NOT NULL,
    finished           boolean NOT NULL DEFAULT false,
    bonus_added        boolean NOT NULL DEFAULT false,
    data_checked       boolean NOT NULL DEFAULT false,
    PRIMARY KEY (season, number)
);

CREATE TABLE fixtures (
    id           text PRIMARY KEY CHECK (id ~ '^fx_[0-9a-hjkmnp-tv-z]{26}$'),
    season       text NOT NULL REFERENCES seasons (id),
    gameweek     int NULL CHECK (gameweek BETWEEN 1 AND 38),   -- NULL while postponed
    home_club_id text NOT NULL REFERENCES clubs (id),
    away_club_id text NOT NULL REFERENCES clubs (id),
    kickoff_at   timestamptz NULL,
    status       text NOT NULL,              -- open enum (FixtureStatus)
    home_score   int NULL,
    away_score   int NULL,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fixtures_gameweek_idx ON fixtures (season, gameweek);

-- One player in one fixture. points is our engine's score from these stats
-- (scoring.Rules.Points), bonus included; rev counts real changes.
CREATE TABLE player_fixture_stats (
    player_id         text NOT NULL REFERENCES players (id),
    fixture_id        text NOT NULL REFERENCES fixtures (id),
    gameweek          int NOT NULL CHECK (gameweek BETWEEN 1 AND 38),
    minutes           int NOT NULL DEFAULT 0,
    goals             int NOT NULL DEFAULT 0,
    assists           int NOT NULL DEFAULT 0,
    clean_sheet       boolean NOT NULL DEFAULT false,
    goals_conceded    int NOT NULL DEFAULT 0,
    own_goals         int NOT NULL DEFAULT 0,
    pens_saved        int NOT NULL DEFAULT 0,
    pens_missed       int NOT NULL DEFAULT 0,
    yellow            int NOT NULL DEFAULT 0,
    red               int NOT NULL DEFAULT 0,
    saves             int NOT NULL DEFAULT 0,
    cbi               int NOT NULL DEFAULT 0,
    tackles           int NOT NULL DEFAULT 0,
    recoveries        int NOT NULL DEFAULT 0,
    bps               int NOT NULL DEFAULT 0,
    bonus             int NOT NULL DEFAULT 0,
    bonus_provisional boolean NOT NULL DEFAULT false,
    points            int NOT NULL DEFAULT 0,
    rev               int NOT NULL DEFAULT 1,
    updated_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (player_id, fixture_id)
);
CREATE INDEX player_fixture_stats_gameweek_idx ON player_fixture_stats (gameweek);

-- Raw upstream payloads, deduplicated by hash: the audit trail and replay
-- source for score disputes.
CREATE TABLE stat_snapshots (
    id             bigserial PRIMARY KEY,
    kind           text NOT NULL,            -- bootstrap | live | status
    gameweek       int NULL,
    fetched_at     timestamptz NOT NULL,
    payload_sha256 text NOT NULL,
    payload        jsonb NOT NULL
);
CREATE INDEX stat_snapshots_kind_idx ON stat_snapshots (kind, gameweek, fetched_at DESC);

-- The single poller's health, read by every API instance for meta.stale.
CREATE TABLE poll_state (
    id                   smallint PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    mode                 text NOT NULL,
    gameweek             int NOT NULL DEFAULT 0,
    last_attempt_at      timestamptz NULL,
    last_good_at         timestamptz NULL,
    next_poll_at         timestamptz NULL,
    last_error           text NOT NULL DEFAULT '',
    consecutive_failures int NOT NULL DEFAULT 0,
    updated_at           timestamptz NOT NULL DEFAULT now()
);

-- ------------------------------------------------------------- accounts

CREATE TABLE users (
    id           text PRIMARY KEY CHECK (id ~ '^usr_[0-9a-hjkmnp-tv-z]{26}$'),
    display_name text NOT NULL,
    email        text NULL UNIQUE CHECK (email = lower(email)),
    avatar_color text NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE auth_identities (
    provider            text NOT NULL CHECK (provider IN ('apple', 'google', 'email')),
    subject             text NOT NULL,
    user_id             text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    apple_refresh_token text NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (provider, subject)
);
CREATE INDEX auth_identities_user_idx ON auth_identities (user_id);

-- Only SHA-256 hashes of tokens are stored.
CREATE TABLE sessions (
    id                 text PRIMARY KEY,
    user_id            text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    access_sha256      text NOT NULL UNIQUE,
    access_expires_at  timestamptz NOT NULL,
    refresh_sha256     text NOT NULL UNIQUE,
    refresh_expires_at timestamptz NOT NULL,
    created_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX sessions_user_idx ON sessions (user_id);

-- Rotated-out refresh tokens: presenting one again revokes the session.
CREATE TABLE used_refresh_tokens (
    refresh_sha256 text PRIMARY KEY,
    session_id     text NOT NULL REFERENCES sessions (id) ON DELETE CASCADE
);

CREATE TABLE magic_links (
    token_sha256 text PRIMARY KEY,
    email        text NOT NULL,
    expires_at   timestamptz NOT NULL,
    used_at      timestamptz NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE idempotency_keys (
    user_id          text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    key              text NOT NULL,
    fingerprint      text NOT NULL,
    status           text NOT NULL CHECK (status IN ('in_flight', 'done')),
    response_status  int NULL,
    response_headers jsonb NULL,
    response_body    bytea NULL,
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

-- ------------------------------------------------------------- tier 2: leagues

CREATE TABLE leagues (
    id              text PRIMARY KEY CHECK (id ~ '^lg_[0-9a-hjkmnp-tv-z]{26}$'),
    name            text NOT NULL,
    season          text NOT NULL REFERENCES seasons (id),
    status          text NOT NULL,           -- open enum (LeagueStatus)
    source          text NOT NULL DEFAULT 'native',
    fpl_league_id   bigint NULL UNIQUE,
    commissioner_id text NULL REFERENCES users (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Versioned settings: a new row per change; the highest version is current.
CREATE TABLE league_settings (
    league_id         text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    version           int NOT NULL,
    settings          jsonb NOT NULL,
    effective_from_gw int NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (league_id, version)
);

CREATE TABLE teams (
    id           text PRIMARY KEY CHECK (id ~ '^tm_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id    text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    name         text NOT NULL,
    owner_id     text NULL REFERENCES users (id) ON DELETE SET NULL,  -- NULL: orphaned
    joined_at    timestamptz NOT NULL,
    fpl_entry_id bigint NULL,
    UNIQUE (league_id, owner_id)
);
CREATE INDEX teams_owner_idx ON teams (owner_id);

CREATE TABLE invites (
    id         text PRIMARY KEY CHECK (id ~ '^inv_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id  text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    code       text NOT NULL UNIQUE,
    max_uses   int NULL,
    uses       int NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL,
    expires_at timestamptz NULL,
    revoked_at timestamptz NULL
);
CREATE INDEX invites_league_idx ON invites (league_id);

CREATE TABLE drafts (
    id               text PRIMARY KEY CHECK (id ~ '^drf_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id        text NOT NULL UNIQUE REFERENCES leagues (id) ON DELETE CASCADE,
    status           text NOT NULL,
    starts_at        timestamptz NULL,
    pick_timer_s     int NOT NULL,
    clock_started_at timestamptz NULL,
    paused_at        timestamptz NULL,
    completed_at     timestamptz NULL
);

CREATE TABLE draft_order (
    draft_id text NOT NULL REFERENCES drafts (id) ON DELETE CASCADE,
    slot     int NOT NULL,
    team_id  text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    autopick boolean NOT NULL DEFAULT false,
    PRIMARY KEY (draft_id, slot)
);

CREATE TABLE draft_picks (
    id        text PRIMARY KEY CHECK (id ~ '^pk_[0-9a-hjkmnp-tv-z]{26}$'),
    draft_id  text NOT NULL REFERENCES drafts (id) ON DELETE CASCADE,
    overall   int NOT NULL,
    team_id   text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    player_id text NOT NULL REFERENCES players (id),
    method    text NOT NULL,
    picked_at timestamptz NOT NULL,
    UNIQUE (draft_id, overall),
    UNIQUE (draft_id, player_id)
);

CREATE TABLE draft_queue (
    team_id   text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    rank      int NOT NULL,
    player_id text NOT NULL REFERENCES players (id),
    PRIMARY KEY (team_id, player_id)
);

-- Ownership over half-open gameweek ranges [from_gw, to_gw); to_gw NULL =
-- still on the roster. The exclusion constraint is the "one owner per
-- player per league per gameweek" rule.
CREATE TABLE roster_slots (
    id           bigserial PRIMARY KEY,
    league_id    text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    team_id      text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    player_id    text NOT NULL REFERENCES players (id),
    from_gw      int NOT NULL CHECK (from_gw BETWEEN 1 AND 39),
    to_gw        int NULL CHECK (to_gw IS NULL OR to_gw > from_gw),
    acquired_via text NOT NULL,              -- draft | waiver | free_agent | trade
    txn_id       text NULL,
    EXCLUDE USING gist (league_id WITH =, player_id WITH =, int4range(from_gw, to_gw) WITH &&)
);
CREATE INDEX roster_slots_team_idx ON roster_slots (team_id);

-- One H2H pairing. A NULL side is the league-average opponent. Points and
-- state are materialised by the score recompute.
CREATE TABLE matchups (
    id           text PRIMARY KEY CHECK (id ~ '^mu_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id    text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    gameweek     int NOT NULL CHECK (gameweek BETWEEN 1 AND 38),
    home_team_id text NULL REFERENCES teams (id) ON DELETE CASCADE,
    away_team_id text NULL REFERENCES teams (id) ON DELETE CASCADE,
    home_points  int NOT NULL DEFAULT 0,
    away_points  int NOT NULL DEFAULT 0,
    state        text NOT NULL DEFAULT 'upcoming',
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (home_team_id IS NOT NULL OR away_team_id IS NOT NULL)
);
CREATE INDEX matchups_league_gw_idx ON matchups (league_id, gameweek);

CREATE TABLE team_gw_scores (
    team_id      text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    gameweek     int NOT NULL CHECK (gameweek BETWEEN 1 AND 38),
    points       int NOT NULL,
    bench_points int NOT NULL,
    autosubs     jsonb NOT NULL DEFAULT '[]',
    state        text NOT NULL,
    rev          int NOT NULL DEFAULT 1,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, gameweek)
);

CREATE TABLE waiver_priority (
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    team_id   text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    rank      int NOT NULL,
    as_of     timestamptz NOT NULL,
    PRIMARY KEY (league_id, team_id)
);

CREATE TABLE transactions (
    id          text PRIMARY KEY CHECK (id ~ '^txn_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id   text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    kind        text NOT NULL,               -- waiver | free_agent | trade
    gameweek    int NOT NULL,
    payload     jsonb NOT NULL,
    occurred_at timestamptz NOT NULL,
    fpl_txn_id  bigint NULL UNIQUE
);
CREATE INDEX transactions_league_idx ON transactions (league_id, occurred_at DESC);

CREATE TABLE trades (
    id                text PRIMARY KEY CHECK (id ~ '^trd_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id         text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    offering_team_id  text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    receiving_team_id text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    status            text NOT NULL,
    message           text NULL,
    created_at        timestamptz NOT NULL,
    expires_at        timestamptz NULL,
    responded_at      timestamptz NULL,
    review_ends_at    timestamptz NULL,
    processed_at      timestamptz NULL,
    decided_by        text NULL,
    failure_reason    text NULL
);
CREATE INDEX trades_league_idx ON trades (league_id, created_at DESC);

CREATE TABLE trade_items (
    trade_id     text NOT NULL REFERENCES trades (id) ON DELETE CASCADE,
    player_id    text NOT NULL REFERENCES players (id),
    from_team_id text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    PRIMARY KEY (trade_id, player_id)
);

CREATE TABLE trade_votes (
    trade_id text NOT NULL REFERENCES trades (id) ON DELETE CASCADE,
    team_id  text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    vote     text NOT NULL,
    voted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (trade_id, team_id)
);

CREATE TABLE realtime_events (
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    seq       bigint NOT NULL,
    type      text NOT NULL,
    payload   jsonb NOT NULL,
    sent_at   timestamptz NOT NULL,
    PRIMARY KEY (league_id, seq)
);

-- ------------------------------------------------------------- tier 3: per user

-- A lineup applies from from_gw until the team's next lineup row.
CREATE TABLE lineups (
    team_id    text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    from_gw    int NOT NULL CHECK (from_gw BETWEEN 1 AND 38),
    slots      text[] NOT NULL CHECK (cardinality(slots) = 15),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (team_id, from_gw)
);

CREATE TABLE waiver_claims (
    id             text PRIMARY KEY CHECK (id ~ '^wv_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id      text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    team_id        text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    add_player_id  text NOT NULL REFERENCES players (id),
    drop_player_id text NOT NULL REFERENCES players (id),
    pref_order     int NOT NULL,
    gameweek       int NOT NULL,
    status         text NOT NULL,
    failure_reason text NULL,
    processed_at   timestamptz NULL,
    created_at     timestamptz NOT NULL
);
CREATE INDEX waiver_claims_league_idx ON waiver_claims (league_id, gameweek);

-- +goose Down
DROP TABLE IF EXISTS waiver_claims, lineups, realtime_events, trade_votes, trade_items, trades,
    transactions, waiver_priority, team_gw_scores, matchups, roster_slots, draft_queue,
    draft_picks, draft_order, drafts, invites, teams, league_settings, leagues,
    idempotency_keys, magic_links, used_refresh_tokens, sessions, auth_identities, users,
    poll_state, stat_snapshots, player_fixture_stats, fixtures, gameweeks, external_ref,
    player_external_id, players, clubs, seasons;
