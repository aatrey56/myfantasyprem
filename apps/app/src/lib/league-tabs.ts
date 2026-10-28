/**
 * The league's top tabs, in pager order: STATUS · MATCH · TEAM · PLAYERS ·
 * LEAGUE. `name` is the route under app/(app)/leagues/[leagueId]/(tabs);
 * Status is the index route, so a league opens on it.
 */
export const LEAGUE_TABS = [
  { name: 'index', label: 'Status', path: '' },
  { name: 'matchup', label: 'Match', path: '/matchup' },
  { name: 'team', label: 'Team', path: '/team' },
  { name: 'players', label: 'Players', path: '/players' },
  { name: 'league', label: 'League', path: '/league' },
] as const;

export type LeagueTab = (typeof LEAGUE_TABS)[number]['name'];

/** Where a tab lives: `/leagues/{id}` plus the tab's path. */
export function leagueTabHref(leagueId: string, tab: LeagueTab): string {
  const path = LEAGUE_TABS.find((t) => t.name === tab)?.path ?? '';
  return `/leagues/${leagueId}${path}`;
}

/** The tab's position in the pager, or -1 for a name that is not a tab. */
export function leagueTabIndex(tab: string): number {
  return LEAGUE_TABS.findIndex((t) => t.name === tab);
}

/**
 * The tab a pathname belongs to: `/leagues/{id}` is Status, and every
 * screen nested in a tab (`/leagues/{id}/players/waivers`) is that tab.
 * Null outside a league's tabs (the hub, settings, the draft room, Activity).
 */
export function leagueTabForPath(pathname: string): LeagueTab | null {
  const m = /^\/leagues\/([^/?#]+)(?:\/([^/?#]*))?/.exec(pathname);
  if (!m || m[1] === 'new') return null;
  const section = m[2] ?? '';
  if (section === '') return 'index';
  const tab = LEAGUE_TABS.find((t) => t.path === `/${section}`);
  return tab ? tab.name : null;
}

/**
 * How many pages apart two tabs are in the pager (0: the same tab). The
 * pager keeps the focused page's neighbours rendered so a swipe shows them
 * at once, and those neighbours keep their queries live.
 */
export function leagueTabDistance(a: string, b: string): number {
  const i = leagueTabIndex(a);
  const j = leagueTabIndex(b);
  if (i < 0 || j < 0) return Infinity;
  return Math.abs(i - j);
}

/** The tabs either side of `tab`, nearest first, for prefetching. */
export function leagueTabNeighbours(tab: string): LeagueTab[] {
  const i = leagueTabIndex(tab);
  if (i < 0) return [];
  return [LEAGUE_TABS[i + 1], LEAGUE_TABS[i - 1]].filter(Boolean).map((t) => t!.name);
}
