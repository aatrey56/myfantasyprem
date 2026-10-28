-- Each row keeps the points breakdown it was scored with, so the API
-- shows lines from the same table as the points (upstream's published
-- table can differ from the standard one).

-- +goose Up
ALTER TABLE player_fixture_stats ADD COLUMN breakdown jsonb NOT NULL DEFAULT '[]';

-- +goose Down
ALTER TABLE player_fixture_stats DROP COLUMN breakdown;
