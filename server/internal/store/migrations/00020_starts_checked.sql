-- Gameweeks the poller re-read for `starts` and still got none (the source
-- does not send it, as the demo's recordings once did not). A gameweek here
-- is not re-read for starts again on restart; lines stored before 00019
-- are re-read once and then filled or listed here.

-- +goose Up
CREATE TABLE starts_checked (
    gameweek   int PRIMARY KEY CHECK (gameweek BETWEEN 1 AND 38),
    checked_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE starts_checked;
