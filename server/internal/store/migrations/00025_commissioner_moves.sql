-- The commissioner's moves (ADR 0017): an entry for every use of a power a
-- manager does not have, written in the transaction of the action, and each
-- member's seen-up-to marker, so the Status banner follows them across
-- devices.

-- +goose Up
CREATE TABLE commissioner_moves (
    seq        bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    id         text NOT NULL UNIQUE CHECK (id ~ '^cmv_[0-9a-hjkmnp-tv-z]{26}$'),
    league_id  text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    -- NULL once the commissioner's account is deleted.
    actor_id   text NULL REFERENCES users (id) ON DELETE SET NULL,
    action     text NOT NULL,
    summary    text NOT NULL,
    team_ids   text[] NOT NULL DEFAULT '{}',
    details    jsonb NULL,
    created_at timestamptz NOT NULL
);
CREATE INDEX commissioner_moves_league_idx ON commissioner_moves (league_id, seq DESC);

CREATE TABLE commissioner_moves_seen (
    league_id  text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    seen_seq   bigint NOT NULL,
    updated_at timestamptz NOT NULL,
    PRIMARY KEY (league_id, user_id)
);

-- +goose Down
DROP TABLE commissioner_moves_seen;
DROP TABLE commissioner_moves;
