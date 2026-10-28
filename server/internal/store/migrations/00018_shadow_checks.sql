-- Shadow checks (internal/mirror, docs/adr/0016): read-only comparisons
-- of an imported league with FPL Draft, run on a schedule from the FPL
-- calendar until the switch-over. Each run stores its report here; the
-- league itself is never written. A league keeps its newest 200.

-- +goose Up
CREATE TABLE mirror_shadow_checks (
    id         bigserial PRIMARY KEY,
    league_id  text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    checked_at timestamptz NOT NULL,
    reason     text NOT NULL,
    gameweek   integer NOT NULL,
    status     text NOT NULL,                -- clean | mismatch | incomplete
    items      jsonb NOT NULL
);
CREATE INDEX mirror_shadow_checks_league_idx ON mirror_shadow_checks (league_id, checked_at DESC);

-- +goose Down
DROP TABLE mirror_shadow_checks;
