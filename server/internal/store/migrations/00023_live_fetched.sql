-- When the newest ingested live snapshot of each gameweek was fetched
-- upstream. An ingest of an older snapshot (an admin backfill racing the
-- poller) is refused instead of overwriting newer rows.

-- +goose Up
CREATE TABLE live_fetched (
    gameweek   int PRIMARY KEY CHECK (gameweek BETWEEN 1 AND 38),
    fetched_at timestamptz NOT NULL
);

-- +goose Down
DROP TABLE live_fetched;
