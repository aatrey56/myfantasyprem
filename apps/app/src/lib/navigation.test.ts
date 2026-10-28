import { linkNextPath, originBack, parseOrigin, safeNextPath, withOrigin } from './navigation';

const L = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';

describe('origin-aware back links', () => {
  it('goes back to League when opened from the League page', () => {
    expect(originBack(L, 'league', 'players')).toEqual({
      label: 'League',
      href: `/leagues/${L}/league`,
    });
    expect(originBack(L, 'league', 'activity')).toEqual({
      label: 'League',
      href: `/leagues/${L}/league`,
    });
  });

  it('goes back to Status, the league landing page, when opened from there', () => {
    expect(originBack(L, 'status', 'players')).toEqual({ label: 'Status', href: `/leagues/${L}` });
  });

  it('falls back to the tab the page lives under', () => {
    expect(originBack(L, undefined, 'players')).toEqual({
      label: 'Players',
      href: `/leagues/${L}/players`,
    });
    expect(originBack(L, undefined, 'activity')).toEqual({
      label: 'Activity',
      href: `/leagues/${L}/activity`,
    });
  });

  it('ignores unknown or crafted origins', () => {
    expect(parseOrigin('https://evil.example')).toBeUndefined();
    expect(parseOrigin('toString')).toBeUndefined();
    expect(parseOrigin(['league', 'players'])).toBe('league');
    expect(originBack(L, 'constructor', 'players').label).toBe('Players');
  });

  it('carries the origin on to the next page', () => {
    expect(withOrigin('/a', 'league')).toBe('/a?from=league');
    expect(withOrigin('/a?tab=outgoing', 'league')).toBe('/a?tab=outgoing&from=league');
    expect(withOrigin('/a', undefined)).toBe('/a');
  });
});

describe('safeNextPath', () => {
  it('keeps in-app paths only', () => {
    expect(safeNextPath('/leagues')).toBe('/leagues');
    expect(safeNextPath('//evil.example')).toBeNull();
  });
});

describe('linkNextPath', () => {
  it('passes app paths the server puts in a sign-in link', () => {
    expect(linkNextPath('/join/K7QM2XRP4D')).toBe('/join/K7QM2XRP4D');
    expect(linkNextPath(`/leagues/${L}/team`)).toBe(`/leagues/${L}/team`);
  });

  it('drops anything the server would refuse', () => {
    for (const bad of [
      null,
      '',
      '/',
      '//evil.example',
      '/\\evil',
      '/a?from=status',
      '/a/../b',
      'https://x.example',
      `/${'a'.repeat(200)}`,
    ]) {
      expect(linkNextPath(bad)).toBeNull();
    }
  });
});
