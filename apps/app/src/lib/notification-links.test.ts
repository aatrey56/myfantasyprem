import { existsSync } from 'fs';
import { join } from 'path';

import { notificationHref } from './notification-links';

const APP = join(__dirname, '..', 'app');
const LG = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';
const PLR = 'plr_01k7m2xq8z00000000000001';

/** Whether an app path (no query) is a screen: a route file under src/app, groups and [params] included. */
function routeExists(href: string): boolean {
  const parts = href.split('?')[0]!.split('/').filter(Boolean);
  const walk = (dir: string, rest: string[]): boolean => {
    if (rest.length === 0) {
      return existsSync(join(dir, 'index.tsx')) || groups(dir).some((g) => walk(join(dir, g), []));
    }
    const [head, ...tail] = rest as [string, ...string[]];
    if (tail.length === 0 && existsSync(join(dir, `${head}.tsx`))) return true;
    if (existsSync(join(dir, head)) && walk(join(dir, head), tail)) return true;
    for (const p of params(dir)) {
      if (p.endsWith('.tsx') && tail.length === 0) return true;
      if (!p.endsWith('.tsx') && walk(join(dir, p), tail)) return true;
    }
    return groups(dir).some((g) => walk(join(dir, g), rest));
  };
  return walk(APP, parts);
}
// eslint-disable-next-line @typescript-eslint/no-require-imports
const ls = (dir: string): string[] => (existsSync(dir) ? require('fs').readdirSync(dir) : []);
const groups = (dir: string) => ls(dir).filter((f) => /^\(.+\)$/.test(f));
const params = (dir: string) => ls(dir).filter((f) => /^\[.+\](\.tsx)?$/.test(f));

describe('notificationHref', () => {
  // Every path the server's producers emit (server/internal/notifications,
  // links_test.go), in both the current and the older `/league/` form.
  const cases: [type: string, url: string, want: string][] = [
    ['player_news', `/leagues/${LG}/players/${PLR}`, `/leagues/${LG}/players/${PLR}?from=feed`],
    ['opponent_news', `/league/${LG}/players/${PLR}`, `/leagues/${LG}/players/${PLR}?from=feed`],
    ['player_goal / opponent_*', `/league/${LG}/matchup`, `/leagues/${LG}/matchup`],
    ['player_goal / opponent_*', `/leagues/${LG}/matchup`, `/leagues/${LG}/matchup`],
    ['trade_*', `/league/${LG}/trades/trd_1`, `/leagues/${LG}/players/trades`],
    ['trade_*', `/leagues/${LG}/players/trades`, `/leagues/${LG}/players/trades`],
    ['waiver_result / waivers_due', `/league/${LG}/waivers`, `/leagues/${LG}/players/waivers`],
    [
      'waiver_result / waivers_due',
      `/leagues/${LG}/players/waivers`,
      `/leagues/${LG}/players/waivers`,
    ],
    ['draft_*', `/league/${LG}/draft`, `/leagues/${LG}/draft`],
    ['lineup_invalid', `/league/${LG}/team`, `/leagues/${LG}/team`],
    ['free_agency_closing', `/league/${LG}/players`, `/leagues/${LG}/players`],
    ['activity (mocks)', `/leagues/${LG}/activity`, `/leagues/${LG}/activity`],
    ['fpl_shadow_mismatch', `/league/${LG}/settings`, `/leagues/${LG}/settings`],
  ];

  it.each(cases)('%s: %s opens %s, a route that exists', (_type, url, want) => {
    expect(notificationHref(url)).toBe(want);
    expect(routeExists(want)).toBe(true);
  });

  it('the old /league/ form was not a route (the "Page not found" tap)', () => {
    expect(routeExists(`/league/${LG}/players/${PLR}`)).toBe(false);
  });

  it('anything unknown or off-site opens a real screen, never Page not found', () => {
    for (const url of [
      null,
      '',
      'https://evil.example',
      '//evil.example/x',
      '/nope',
      `/league/${LG}/whatever`,
      '/league',
    ]) {
      const href = notificationHref(url);
      expect(routeExists(href)).toBe(true);
    }
  });
});
