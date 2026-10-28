-- Leagues choose their waiver order rule, settings.waiverOrder (ADR 0008
-- rule 12, amended 2026-10-15): reverse_standings (FPL Draft's) or
-- rolling. New leagues default to reverse_standings. Existing imported FPL
-- leagues switch to it, as FPL plays them; existing native leagues keep
-- the rolling order they were made with. Each gets a new settings version.

-- +goose Up
INSERT INTO league_settings (league_id, version, settings)
SELECT s.league_id, s.version + 1,
       jsonb_set(s.settings, '{waiverOrder}',
                 to_jsonb(CASE WHEN l.source = 'fpl_import' THEN 'reverse_standings' ELSE 'rolling' END))
FROM (
    SELECT DISTINCT ON (league_id) league_id, version, settings
    FROM league_settings
    ORDER BY league_id, version DESC
) s
JOIN leagues l ON l.id = s.league_id
WHERE NOT s.settings ? 'waiverOrder';

-- +goose Down
-- The earlier settings versions are kept, and a league without the
-- setting is rolling; nothing to undo.
SELECT 1;
