/**
 * Where tapping a notification goes. The server stores a path with each
 * notification; older ones used `/league/<id>/...`, which is not a route in
 * this app (the tap fell through to "Page not found"), so both that and the
 * current `/leagues/<id>/...` form map onto real screens here:
 *
 * - `players/<plr_id>`: the player's card in that league (news, events)
 * - `trades[/<id>]`: the league's trades (the trade is in its Active list)
 * - `waivers`: the league's waiver claims
 * - `matchup`: the Match tab
 * - `draft`, `team`, `players`, `settings`: those screens (the FPL check's
 *   mismatch alert opens settings)
 *
 * Anything else inside a league opens the league; anything that is not an
 * in-app path opens the Feed. A player's card gets `?from=feed`, so its
 * back link returns to the Feed.
 */
export function notificationHref(url: string | null | undefined): string {
  if (!url || !url.startsWith('/') || url.startsWith('//')) return '/feed';
  const [path] = url.split(/[?#]/);
  const parts = path!.split('/').filter(Boolean);
  if (parts[0] !== 'league' && parts[0] !== 'leagues') {
    return SAFE_TOP.has(parts[0] ?? '') ? path! : '/feed';
  }
  const leagueId = parts[1];
  if (!leagueId) return '/leagues';
  const base = `/leagues/${encodeURIComponent(leagueId)}`;
  const [section, id] = parts.slice(2);
  switch (section) {
    case 'players':
      if (id === 'trades' || id === 'waivers') return `${base}/players/${id}`;
      return id ? `${base}/players/${encodeURIComponent(id)}?from=feed` : `${base}/players`;
    case 'trades':
      return `${base}/players/trades`;
    case 'waivers':
      return `${base}/players/waivers`;
    case 'matchup':
      return `${base}/matchup`;
    case 'draft':
      return `${base}/draft`;
    case 'team':
      return `${base}/team`;
    case 'activity':
      return `${base}/activity`;
    case 'settings':
      return `${base}/settings`;
    default:
      return base;
  }
}

/** Top-level screens a notification may link to as they are. */
const SAFE_TOP = new Set(['feed', 'scores', 'account', 'leagues']);
