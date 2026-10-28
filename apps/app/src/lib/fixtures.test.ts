import type { Schemas } from '@myfantasyprem/api-client';

import { expandRowFixtures, toRowFixture } from './fixtures';

const full: Schemas['UpcomingFixture'] = {
  fixtureId: 'fx_01k7m2xq8w0000000000000046',
  gameweek: 9,
  opponent: { code: 'ARS', name: 'Arsenal' },
  isHome: false,
  kickoffAt: '2026-10-31T15:00:00Z',
  status: 'scheduled',
};

describe('row fixtures', () => {
  it('compacts to the opponent code, without a scheduled status', () => {
    expect(toRowFixture(full)).toEqual({
      fixtureId: full.fixtureId,
      gameweek: 9,
      opponent: 'ARS',
      isHome: false,
      kickoffAt: full.kickoffAt,
    });
    expect(toRowFixture({ ...full, status: 'postponed' }).status).toBe('postponed');
  });

  it('expands with the club list, a missing club standing for its own name', () => {
    const clubs = [{ code: 'ARS', name: 'Arsenal' }];
    const row = toRowFixture(full);
    expect(expandRowFixtures([row], clubs)[0]!.opponent).toEqual({ code: 'ARS', name: 'Arsenal' });
    expect(expandRowFixtures([{ ...row, opponent: 'XYZ' }], clubs)[0]!.opponent).toEqual({
      code: 'XYZ',
      name: 'XYZ',
    });
    expect(expandRowFixtures([row], undefined)[0]!.opponent.name).toBe('ARS');
  });
});
