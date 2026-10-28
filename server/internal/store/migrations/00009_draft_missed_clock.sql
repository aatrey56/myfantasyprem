-- A missed pick clock is announced (ADR 0010 amendment, 2026-10-13): the
-- pick records that the clock ran out, and each team can turn "autopick when
-- my clock runs out" off (rows here; on by default).

-- +goose Up
ALTER TABLE draft_picks ADD COLUMN missed_clock boolean NOT NULL DEFAULT false;

CREATE TABLE draft_stay_manual (
    draft_id text NOT NULL REFERENCES drafts (id) ON DELETE CASCADE,
    team_id  text NOT NULL REFERENCES teams (id) ON DELETE CASCADE,
    PRIMARY KEY (draft_id, team_id)
);

-- +goose Down
DROP TABLE IF EXISTS draft_stay_manual;
ALTER TABLE draft_picks DROP COLUMN missed_clock;
