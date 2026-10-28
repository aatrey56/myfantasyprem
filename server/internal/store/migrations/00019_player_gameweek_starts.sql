-- Games started per gameweek line (FPL's live `starts`; 2 in a double where
-- the player started both). NULL where the provider did not send it,
-- including lines stored before this column until their gameweek is read
-- again.

-- +goose Up
ALTER TABLE player_gameweek_stats ADD COLUMN starts int NULL CHECK (starts >= 0);

-- +goose Down
ALTER TABLE player_gameweek_stats DROP COLUMN starts;
