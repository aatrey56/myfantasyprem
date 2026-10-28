-- Each player's match stats per gameweek as upstream publishes them
-- (gameweek totals, a double gameweek's fixtures summed): the per-game
-- stats card and the awards. They score nothing; points stay in
-- player_fixture_stats. The ICT index and expected stats are NULL when the
-- provider does not publish them.

-- +goose Up
CREATE TABLE player_gameweek_stats (
    gameweek                        int NOT NULL CHECK (gameweek BETWEEN 1 AND 38),
    player_id                       text NOT NULL REFERENCES players (id),
    minutes                         int NOT NULL DEFAULT 0,
    goals                           int NOT NULL DEFAULT 0,
    assists                         int NOT NULL DEFAULT 0,
    clean_sheets                    int NOT NULL DEFAULT 0,
    goals_conceded                  int NOT NULL DEFAULT 0,
    own_goals                       int NOT NULL DEFAULT 0,
    pens_saved                      int NOT NULL DEFAULT 0,
    pens_missed                     int NOT NULL DEFAULT 0,
    yellow                          int NOT NULL DEFAULT 0,
    red                             int NOT NULL DEFAULT 0,
    saves                           int NOT NULL DEFAULT 0,
    bonus                           int NOT NULL DEFAULT 0,
    bps                             int NOT NULL DEFAULT 0,
    cbi                             int NOT NULL DEFAULT 0,
    tackles                         int NOT NULL DEFAULT 0,
    recoveries                      int NOT NULL DEFAULT 0,
    defensive_contribution          int NOT NULL DEFAULT 0,
    influence                       double precision NULL,
    creativity                      double precision NULL,
    threat                          double precision NULL,
    ict_index                       double precision NULL,
    expected_goals                  double precision NULL,
    expected_assists                double precision NULL,
    expected_goal_involvements      double precision NULL,
    expected_goals_conceded         double precision NULL,
    updated_at                      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (gameweek, player_id)
);
CREATE INDEX player_gameweek_stats_player_idx ON player_gameweek_stats (player_id);

-- +goose Down
DROP TABLE player_gameweek_stats;
