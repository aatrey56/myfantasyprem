-- Imported FPL Draft leagues are playable at once (docs/adr/0012, amended
-- 2026-10-14): no automatic sync, no read-only state and no switch-over.
-- The commissioner re-syncs on demand; mirror_sync records the last
-- re-sync or check and the one queued or running.

-- +goose Up
ALTER TABLE leagues
    DROP COLUMN sync_state,
    DROP COLUMN switch_gameweek,
    DROP COLUMN switch_confirmed_at,
    DROP COLUMN switch_confirmed_by,
    DROP COLUMN switched_at;

ALTER TABLE mirror_sync
    DROP COLUMN next_sync_at,
    DROP COLUMN consecutive_failures,
    ADD COLUMN pending      text NULL CHECK (pending IN ('resync', 'check')),
    ADD COLUMN requested_at timestamptz NULL;
COMMENT ON COLUMN mirror_sync.note IS 'the last re-sync''s notice for members';

-- The periodic sync jobs are gone; drop any still queued.
-- +goose StatementBegin
DO $$
BEGIN
    IF to_regclass('river_job') IS NOT NULL THEN
        DELETE FROM river_job WHERE kind IN ('fpl_sync', 'fpl_sync_due');
    END IF;
END
$$;
-- +goose StatementEnd

-- +goose Down
ALTER TABLE mirror_sync
    DROP COLUMN pending,
    DROP COLUMN requested_at,
    ADD COLUMN next_sync_at         timestamptz NOT NULL DEFAULT now(),
    ADD COLUMN consecutive_failures int NOT NULL DEFAULT 0;
CREATE INDEX mirror_sync_due_idx ON mirror_sync (next_sync_at);

ALTER TABLE leagues
    ADD COLUMN sync_state          text NULL,
    ADD COLUMN switch_gameweek     int NULL CHECK (switch_gameweek BETWEEN 1 AND 38),
    ADD COLUMN switch_confirmed_at timestamptz NULL,
    ADD COLUMN switch_confirmed_by text NULL REFERENCES users (id) ON DELETE SET NULL,
    ADD COLUMN switched_at         timestamptz NULL;
UPDATE leagues SET sync_state = 'native' WHERE source = 'fpl_import';
