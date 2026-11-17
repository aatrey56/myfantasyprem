-- Trades are decided by a league veto vote by default (ADR 0017): an
-- accepted trade waits 24 hours, and a third of the managers not in it
-- (rounded up) can veto it. Leagues still on the old default, no review,
-- get the new one; leagues that chose commissioner review or a league vote
-- (and imported FPL leagues, which mirror FPL's commissioner review) are
-- left as they are. Each switched league gets a new settings version.
--
-- Trades already open keep working: an offer accepted from now on goes
-- into review under the league's new setting, and a trade already in
-- review carries its own review mode and window.

-- +goose Up
INSERT INTO league_settings (league_id, version, settings)
SELECT s.league_id, s.version + 1,
       s.settings || jsonb_build_object(
           'tradeReview', 'league_vote',
           'reviewHours', 24,
           'vetoThreshold', 0.3333333333333333)
FROM (
    SELECT DISTINCT ON (league_id) league_id, version, settings
    FROM league_settings
    ORDER BY league_id, version DESC
) s
WHERE coalesce(s.settings ->> 'tradeReview', 'none') = 'none';

-- +goose Down
-- The earlier settings versions are kept; nothing to undo.
SELECT 1;
