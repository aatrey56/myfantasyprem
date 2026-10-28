import type { Schemas } from '@myfantasyprem/api-client';

import {
  fixtureA11yLabel,
  fixturePhase,
  fixtureStatusLabel,
  fixtureTimeLabel,
  groupFixturesByDay,
  liveClock,
  liveCount,
  scoreText,
  winnerOf,
} from './scores';

const club = (code: string, name: string) => ({ code, name }) as Schemas['ClubRef'];
const fx = (
  id: string,
  status: string,
  home: number | null,
  away: number | null,
  extra: Partial<Schemas['GameweekFixture']> = {},
) =>
  ({
    fixtureId: id,
    gameweek: 7,
    kickoffAt: '2026-10-17T14:00:00Z',
    status,
    home: club('ARS', 'Arsenal'),
    away: club('CHE', 'Chelsea'),
    homeScore: home,
    awayScore: away,
    minute: null,
    period: null,
    ...extra,
  }) as unknown as Schemas['GameweekFixture'];
const NOW = Date.parse('2026-10-15T12:00:00Z');
const LONDON = 'Europe/London';

describe('scores', () => {
  it('maps statuses to phases, unknown as upcoming', () => {
    expect(fixturePhase('live')).toBe('live');
    expect(fixturePhase('finished')).toBe('done');
    expect(fixturePhase('finished_provisional')).toBe('done');
    expect(fixturePhase('postponed')).toBe('postponed');
    expect(fixturePhase('scheduled')).toBe('upcoming');
    expect(fixturePhase('abandoned')).toBe('upcoming');
  });

  it('shows the live clock: the minute, HT, or LIVE before a minute is known', () => {
    expect(liveClock({ minute: 67, period: 'second_half' })).toBe("67'");
    expect(liveClock({ minute: 12, period: 'first_half' })).toBe("12'");
    expect(liveClock({ minute: 45, period: 'half_time' })).toBe('HT');
    expect(liveClock({ minute: null, period: null })).toBe('LIVE');
    // A period this build does not know (open enum): no guessing.
    expect(liveClock({ minute: 95, period: 'extra_time' as never })).toBe('LIVE');
  });

  it('labels the status column', () => {
    expect(fixtureStatusLabel(fx('a', 'live', 1, 0), NOW)).toBe('LIVE');
    expect(
      fixtureStatusLabel(fx('a', 'live', 1, 0, { minute: 71, period: 'second_half' }), NOW),
    ).toBe("71'");
    expect(fixtureStatusLabel(fx('a', 'finished', 1, 0), NOW)).toBe('FT');
    expect(fixtureStatusLabel(fx('a', 'finished_provisional', 1, 0), NOW)).toBe('FT');
    expect(fixtureStatusLabel(fx('a', 'postponed', null, null), NOW)).toBe('PP');
    expect(fixtureStatusLabel(fx('a', 'scheduled', null, null), NOW, 'UTC')).toBe('Sat 14:00');
    expect(fixtureStatusLabel({ ...fx('a', 'scheduled', null, null), kickoffAt: null }, NOW)).toBe(
      'TBC',
    );
  });

  it('labels a row under its day heading with the time of day', () => {
    expect(fixtureTimeLabel(fx('a', 'scheduled', null, null), LONDON)).toBe('15:00');
    expect(fixtureTimeLabel(fx('a', 'live', 0, 0, { minute: 45, period: 'half_time' }))).toBe('HT');
    expect(fixtureTimeLabel(fx('a', 'finished', 2, 2))).toBe('FT');
    expect(fixtureTimeLabel({ ...fx('a', 'scheduled', null, null), kickoffAt: null })).toBe('TBC');
  });

  it('shows a dash before a score exists', () => {
    expect(scoreText(null)).toBe('–');
    expect(scoreText(0)).toBe('0');
  });

  it('names the winner of a finished match only', () => {
    expect(winnerOf(fx('a', 'finished', 2, 1))).toBe('home');
    expect(winnerOf(fx('a', 'finished_provisional', 0, 3))).toBe('away');
    expect(winnerOf(fx('a', 'finished', 1, 1))).toBeNull();
    expect(winnerOf(fx('a', 'live', 2, 0))).toBeNull();
  });

  it('groups by day in the device zone, then undated, then postponed', () => {
    const days = groupFixturesByDay(
      [
        fx('sun2', 'scheduled', null, null, { kickoffAt: '2026-10-18T15:30:00Z' }),
        fx('pp', 'postponed', null, null, { kickoffAt: null }),
        fx('sat2', 'live', 1, 0, { kickoffAt: '2026-10-17T14:00:00Z' }),
        fx('tbc', 'scheduled', null, null, { kickoffAt: null }),
        fx('sat1', 'finished', 2, 2, { kickoffAt: '2026-10-17T11:30:00Z' }),
        fx('sat3', 'scheduled', null, null, { kickoffAt: '2026-10-17T14:00:00Z' }),
        // 23:30 UTC Saturday is 00:30 Sunday in London.
        fx('sun1', 'scheduled', null, null, { kickoffAt: '2026-10-17T23:30:00Z' }),
        // A postponed fixture keeps its old date but is not played on it.
        fx('pp2', 'postponed', null, null, { kickoffAt: '2026-10-17T11:30:00Z' }),
      ],
      NOW,
      LONDON,
    );
    expect(days.map((d) => [d.title, d.fixtures.map((f) => f.fixtureId)])).toEqual([
      ['Sat 17 Oct', ['sat1', 'sat2', 'sat3']],
      ['Sun 18 Oct', ['sun1', 'sun2']],
      ['Date to be confirmed', ['tbc']],
      ['Postponed', ['pp', 'pp2']],
    ]);
    expect(groupFixturesByDay([], NOW)).toEqual([]);
  });

  it('counts live fixtures', () => {
    expect(
      liveCount([fx('a', 'live', 0, 0), fx('b', 'finished', 1, 0), fx('c', 'live', 1, 1)]),
    ).toBe(2);
  });

  it('reads a fixture out', () => {
    expect(fixtureA11yLabel(fx('a', 'live', 2, 1), NOW)).toBe('Arsenal 2, Chelsea 1, live');
    expect(
      fixtureA11yLabel(fx('a', 'live', 2, 1, { minute: 67, period: 'second_half' }), NOW),
    ).toBe('Arsenal 2, Chelsea 1, live, 67 minutes');
    expect(fixtureA11yLabel(fx('a', 'live', 0, 0, { minute: 45, period: 'half_time' }), NOW)).toBe(
      'Arsenal 0, Chelsea 0, live, half time',
    );
    expect(fixtureA11yLabel(fx('a', 'finished', 0, 0), NOW)).toBe(
      'Arsenal 0, Chelsea 0, full time',
    );
    expect(fixtureA11yLabel(fx('a', 'postponed', null, null), NOW)).toBe(
      'Arsenal v Chelsea, postponed',
    );
  });
});
