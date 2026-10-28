-- Security hardening before launch (docs/ARCHITECTURE.md "Security").

-- +goose Up

-- A realtime ticket belongs to the session that asked for it: revoking
-- the session (logout, refresh-token reuse) drops its unused tickets and
-- closes its sockets.
ALTER TABLE realtime_tickets
    ADD COLUMN session_id text NULL REFERENCES sessions (id) ON DELETE CASCADE;
CREATE INDEX realtime_tickets_session_idx ON realtime_tickets (session_id);
CREATE INDEX realtime_tickets_user_idx ON realtime_tickets (user_id);

-- Invites now expire after a week by default and new codes have 10
-- characters. Existing 8-character codes keep working until they expire;
-- the ones that never would get a week from now.
UPDATE invites SET expires_at = now() + interval '7 days'
    WHERE expires_at IS NULL AND revoked_at IS NULL;

-- +goose Down
DROP INDEX IF EXISTS realtime_tickets_session_idx, realtime_tickets_user_idx;
ALTER TABLE realtime_tickets DROP COLUMN session_id;
