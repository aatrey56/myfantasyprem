-- Wave C: the trade block, player news and personal tags.
--
-- trade_block: players a team put on the block, with an optional note and
-- the positions it wants back. Public to the league. A player is on one
-- team at a time, so the key is (league, player); the league's State
-- drops a row when the team no longer holds the player (drop, trade,
-- waiver). Player ids are checked by the API, as in watchlist_entries.
--
-- player_tags: each member's private tag on a player in a league, one of
-- a fixed set (the API checks the value; the column is an open enum).
--
-- players: when upstream added the current news (FPL news_added) and its
-- chance of playing next round (FPL chance_of_playing_next_round), both
-- NULL when upstream has none.

-- +goose Up
CREATE TABLE trade_block (
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    team_id   text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    player_id text NOT NULL CHECK (player_id ~ '^plr_[0-9a-hjkmnp-tv-z]{26}$'),
    note      text NOT NULL DEFAULT '' CHECK (char_length(note) <= 140),
    wanted    text[] NOT NULL DEFAULT '{}',
    added_at  timestamptz NOT NULL,
    PRIMARY KEY (league_id, player_id)
);
CREATE INDEX trade_block_team_idx ON trade_block (team_id);

CREATE TABLE player_tags (
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id  text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    player_id  text NOT NULL CHECK (player_id ~ '^plr_[0-9a-hjkmnp-tv-z]{26}$'),
    tag        text NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (user_id, league_id, player_id)
);
CREATE INDEX player_tags_league_idx ON player_tags (league_id);

ALTER TABLE players
    ADD COLUMN news_at     timestamptz NULL,
    ADD COLUMN chance_next smallint NULL CHECK (chance_next BETWEEN 0 AND 100);

-- +goose Down
ALTER TABLE players DROP COLUMN chance_next, DROP COLUMN news_at;
DROP TABLE player_tags;
DROP TABLE trade_block;
