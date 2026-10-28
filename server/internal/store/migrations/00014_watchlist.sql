-- Each member's watchlist in a league (the Players tab's star): players
-- they keep an eye on, private to them. Player ids are checked by the API,
-- not by a foreign key, as the players table follows upstream.

-- +goose Up
CREATE TABLE watchlist_entries (
    user_id   text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    player_id text NOT NULL CHECK (player_id ~ '^plr_[0-9a-hjkmnp-tv-z]{26}$'),
    added_at  timestamptz NOT NULL,
    PRIMARY KEY (user_id, league_id, player_id)
);
CREATE INDEX watchlist_entries_league_idx ON watchlist_entries (league_id);

-- +goose Down
DROP TABLE watchlist_entries;
