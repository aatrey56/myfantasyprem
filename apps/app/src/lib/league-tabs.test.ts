import {
  LEAGUE_TABS,
  leagueTabDistance,
  leagueTabForPath,
  leagueTabHref,
  leagueTabIndex,
  leagueTabNeighbours,
} from './league-tabs';

const L = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';

describe('league tabs', () => {
  it('runs STATUS · MATCH · TEAM · PLAYERS · LEAGUE, Status first', () => {
    expect(LEAGUE_TABS.map((t) => t.label)).toEqual([
      'Status',
      'Match',
      'Team',
      'Players',
      'League',
    ]);
    expect(LEAGUE_TABS[0].name).toBe('index');
  });

  it('maps every tab to its URL and back', () => {
    for (const tab of LEAGUE_TABS) {
      const href = leagueTabHref(L, tab.name);
      expect(leagueTabForPath(href)).toBe(tab.name);
    }
    expect(leagueTabHref(L, 'index')).toBe(`/leagues/${L}`);
    expect(leagueTabHref(L, 'matchup')).toBe(`/leagues/${L}/matchup`);
    expect(leagueTabHref(L, 'league')).toBe(`/leagues/${L}/league`);
  });

  it('puts a screen nested in a tab under that tab (deep links)', () => {
    expect(leagueTabForPath(`/leagues/${L}/players/waivers`)).toBe('players');
    expect(leagueTabForPath(`/leagues/${L}/players/trades/new`)).toBe('players');
    expect(leagueTabForPath(`/leagues/${L}/team/tm_01`)).toBe('team');
    expect(leagueTabForPath(`/leagues/${L}/matchup/8/mu_01`)).toBe('matchup');
    expect(leagueTabForPath(`/leagues/${L}/matchup?gw=7`)).toBe('matchup');
    expect(leagueTabForPath(`/leagues/${L}/`)).toBe('index');
  });

  it('is null outside the tabs', () => {
    for (const path of [
      '/leagues',
      '/leagues/new',
      `/leagues/${L}/settings`,
      `/leagues/${L}/draft`,
      `/leagues/${L}/activity`,
      `/leagues/${L}/activity/deadlines`,
      '/account',
      '/scores',
    ]) {
      expect(leagueTabForPath(path)).toBeNull();
    }
  });

  it('measures pager distance and finds neighbours', () => {
    expect(leagueTabIndex('players')).toBe(3);
    expect(leagueTabIndex('activity')).toBe(-1);
    expect(leagueTabDistance('index', 'index')).toBe(0);
    expect(leagueTabDistance('index', 'matchup')).toBe(1);
    expect(leagueTabDistance('league', 'index')).toBe(4);
    expect(leagueTabDistance('index', 'nope')).toBe(Infinity);
    expect(leagueTabNeighbours('index')).toEqual(['matchup']);
    expect(leagueTabNeighbours('team')).toEqual(['players', 'matchup']);
    expect(leagueTabNeighbours('league')).toEqual(['players']);
    expect(leagueTabNeighbours('nope')).toEqual([]);
  });
});
