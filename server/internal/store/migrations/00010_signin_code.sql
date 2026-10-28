-- Email sign-in codes (2026-10-14): each magic link also carries a 6-digit
-- code. Only a salted SHA-256 of it is kept; wrong guesses count per link
-- (5 kill it) and per address (sign_in_code_failures, 10 per 24 hours).

-- +goose Up
ALTER TABLE magic_links
    ADD COLUMN seq bigint GENERATED ALWAYS AS IDENTITY,
    ADD COLUMN code_sha256 text NULL,
    ADD COLUMN code_failures integer NOT NULL DEFAULT 0;
CREATE INDEX magic_links_email_idx ON magic_links (email, seq);

CREATE TABLE sign_in_code_failures (
    email     text NOT NULL,
    failed_at timestamptz NOT NULL
);
CREATE INDEX sign_in_code_failures_email_idx ON sign_in_code_failures (email, failed_at);

-- +goose Down
DROP TABLE IF EXISTS sign_in_code_failures;
DROP INDEX IF EXISTS magic_links_email_idx;
ALTER TABLE magic_links DROP COLUMN code_failures, DROP COLUMN code_sha256, DROP COLUMN seq;
