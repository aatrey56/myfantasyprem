-- A player's BPS in a fixture can be unknown (a double gameweek without
-- per-fixture data): NULL, distinct from a known 0 or negative score.

-- +goose Up
ALTER TABLE player_fixture_stats ALTER COLUMN bps DROP NOT NULL, ALTER COLUMN bps DROP DEFAULT;

-- +goose Down
UPDATE player_fixture_stats SET bps = 0 WHERE bps IS NULL;
ALTER TABLE player_fixture_stats ALTER COLUMN bps SET DEFAULT 0, ALTER COLUMN bps SET NOT NULL;
