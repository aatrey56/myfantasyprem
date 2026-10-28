-- Gameweeks whose stats changed but whose league scores are not yet
-- recomputed. The live ingest inserts a row in the same transaction as the
-- changed stats; the poller deletes it only after a successful rescore, so a
-- failed rescore survives a restart.
--
-- Every mark takes a new version from a sequence (never reused, even after a
-- delete). A rescore deletes the row only if its version is the one it read
-- before recomputing, so a gameweek marked again meanwhile stays marked.

-- +goose Up
CREATE SEQUENCE rescore_version_seq;

CREATE TABLE rescore_pending (
    gameweek  int PRIMARY KEY,
    version   bigint NOT NULL DEFAULT nextval('rescore_version_seq'),
    marked_at timestamptz NOT NULL DEFAULT now()
);

ALTER SEQUENCE rescore_version_seq OWNED BY rescore_pending.version;

-- +goose Down
DROP TABLE IF EXISTS rescore_pending;
DROP SEQUENCE IF EXISTS rescore_version_seq;
