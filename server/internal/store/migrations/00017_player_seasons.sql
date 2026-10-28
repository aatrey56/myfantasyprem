-- A player's totals for each finished Premier League season, from FPL's
-- element-summary history_past (the player card's "Previous seasons").
-- Keyed by FPL `code`, the permanent player id (player_external_id with
-- provider 'fpl'), not by our plr_ id: history is fetched for every code
-- and outlives a season's players table. A stat FPL did not track that
-- season is NULL, never 0. history_past has no club; an optional club
-- column can be added later without changing the key.

-- +goose Up
CREATE TABLE player_seasons (
    code             text NOT NULL CHECK (code ~ '^[0-9]+$'),
    season           text NOT NULL CHECK (season ~ '^[0-9]{4}/[0-9]{2}$'),
    total_points     integer NULL,
    minutes          integer NULL,
    goals_scored     integer NULL,
    assists          integer NULL,  -- FPL's assists, not Opta's
    clean_sheets     integer NULL,
    goals_conceded   integer NULL,
    own_goals        integer NULL,
    penalties_saved  integer NULL,
    penalties_missed integer NULL,
    yellow_cards     integer NULL,
    red_cards        integer NULL,
    saves            integer NULL,
    bonus            integer NULL,
    bps              integer NULL,
    start_cost       integer NULL,
    end_cost         integer NULL,
    influence        double precision NULL,
    creativity       double precision NULL,
    threat           double precision NULL,
    ict_index        double precision NULL,
    starts           integer NULL,
    expected_goals             double precision NULL,
    expected_assists           double precision NULL,
    expected_goal_involvements double precision NULL,
    expected_goals_conceded    double precision NULL,
    clearances_blocks_interceptions integer NULL,
    recoveries             integer NULL,
    tackles                integer NULL,
    defensive_contribution integer NULL,
    fetched_at       timestamptz NOT NULL,
    PRIMARY KEY (code, season)
);

-- When each code's history was last fetched, including players with no
-- past seasons, so a run fetches only new players or a new season.
CREATE TABLE player_history_fetches (
    code       text PRIMARY KEY CHECK (code ~ '^[0-9]+$'),
    fetched_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE player_history_fetches;
DROP TABLE player_seasons;
