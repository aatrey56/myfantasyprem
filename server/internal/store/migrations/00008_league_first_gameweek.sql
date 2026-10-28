-- A drafted league's startGameweek is its first scoring gameweek (ADR 0010):
-- the draft now records it on completion. Leagues drafted mid-season before
-- that still say 1; give them a new settings version with their first
-- matchup's gameweek, so no view offers gameweeks before they existed.

-- +goose Up
INSERT INTO league_settings (league_id, version, settings)
SELECT s.league_id, s.version + 1, jsonb_set(s.settings, '{startGameweek}', to_jsonb(m.first))
FROM (
    SELECT DISTINCT ON (league_id) league_id, version, settings
    FROM league_settings
    ORDER BY league_id, version DESC
) s
JOIN (SELECT league_id, min(gameweek) AS first FROM matchups GROUP BY league_id) m USING (league_id)
WHERE coalesce((s.settings ->> 'startGameweek')::int, 1) < m.first;

-- +goose Down
-- The earlier settings versions are kept; nothing to undo.
SELECT 1;
