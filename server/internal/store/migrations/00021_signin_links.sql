-- Commissioner-issued sign-in links (2026-10-27, docs/adr/0013 addendum):
-- one link signs an account back in once, within 7 days. Only a SHA-256 of
-- the code is kept. Issuing a new link for an account deletes its older
-- unused ones; a used link is deleted.

-- +goose Up
CREATE TABLE signin_links (
    code_sha256 text PRIMARY KEY,
    user_id     text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id   text NOT NULL,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL
);
CREATE INDEX signin_links_user_idx ON signin_links (user_id);

-- +goose Down
DROP TABLE IF EXISTS signin_links;
