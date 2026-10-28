-- Sign-in links remember who issued them (docs/adr/0013 addendum): a link
-- holds only while its issuer still commissions the league and the league
-- still lists the manager. Links issued before this have no issuer and go.

-- +goose Up
DELETE FROM signin_links;
ALTER TABLE signin_links ADD COLUMN issuer_id text NOT NULL REFERENCES users (id) ON DELETE CASCADE;
CREATE INDEX signin_links_league_idx ON signin_links (league_id);

-- +goose Down
DROP INDEX IF EXISTS signin_links_league_idx;
ALTER TABLE signin_links DROP COLUMN IF EXISTS issuer_id;
