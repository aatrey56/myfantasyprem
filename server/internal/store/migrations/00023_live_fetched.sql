-- Live fetch tickets per gameweek. A fetch takes the next ticket (issued)
-- before asking upstream; an ingest records the highest ticket applied
-- (ingested, NULL until one is) and is refused when it holds a lower one
-- (an admin backfill racing the poller), instead of overwriting newer
-- rows. Tickets order fetches across processes without clocks.

-- +goose Up
CREATE TABLE live_fetched (
    gameweek int PRIMARY KEY CHECK (gameweek BETWEEN 1 AND 38),
    issued   bigint NOT NULL DEFAULT 0,
    ingested bigint
);

-- +goose Down
DROP TABLE live_fetched;
