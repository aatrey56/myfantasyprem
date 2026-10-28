import type { Schemas } from '@myfantasyprem/api-client';

import {
  chanceFromNews,
  fixtureResult,
  heatCell,
  heatColors,
  heatGroups,
  heatQuality,
  isBlank,
  logRows,
  splitName,
} from './heat';
import { NO_STATS } from '@/mocks/data';
import { darkColors, tint } from '@/theme/tokens';

const nulls = () =>
  Object.fromEntries(Object.keys(NO_STATS).map((k) => [k, null])) as Schemas['PlayerGameStats'];
const zeros = () =>
  Object.fromEntries(Object.keys(NO_STATS).map((k) => [k, 0])) as Schemas['PlayerGameStats'];
const fx = (over: Partial<Schemas['PlayerFixture']> = {}) =>
  ({ fixtureId: 'fx', status: 'finished', isHome: true, ...over }) as Schemas['PlayerFixture'];
const gw = (gameweek: number, over: Partial<Schemas['PlayerGameweek']> = {}) =>
  ({
    gameweek,
    points: 2,
    minutes: 90,
    fixtures: [fx()],
    stats: zeros(),
    ...over,
  }) as Schemas['PlayerGameweek'];
const blank = (gameweek: number) =>
  gw(gameweek, { points: 0, minutes: 0, fixtures: [], stats: nulls() });
const upcoming = (gameweek: number) =>
  ({ fixtureId: `fx_${gameweek}`, gameweek, isHome: true }) as Schemas['UpcomingFixture'];

describe('heatQuality', () => {
  it('is none for a missing value, whatever the stat', () => {
    expect(heatQuality('points', null, 'MID')).toBe('none');
    expect(heatQuality('goalsScored', undefined, 'MID')).toBe('none');
    expect(heatQuality('expectedGoals', Number.NaN, 'MID')).toBe('none');
  });

  it('grades points at 7 and 3, with zero and negatives poor', () => {
    expect(heatQuality('points', 7, 'MID')).toBe('good');
    expect(heatQuality('points', 6, 'MID')).toBe('mid');
    expect(heatQuality('points', 3, 'MID')).toBe('mid');
    expect(heatQuality('points', 2, 'MID')).toBe('poor');
    expect(heatQuality('points', 0, 'MID')).toBe('poor');
    expect(heatQuality('points', -2, 'MID')).toBe('poor');
  });

  it('grades minutes: 60+ good, a cameo middling, none poor', () => {
    expect(heatQuality('minutes', 60, 'FWD')).toBe('good');
    expect(heatQuality('minutes', 59, 'FWD')).toBe('mid');
    expect(heatQuality('minutes', 1, 'FWD')).toBe('mid');
    expect(heatQuality('minutes', 0, 'FWD')).toBe('poor');
  });

  it('keeps zeros of counting stats neutral', () => {
    for (const key of ['goalsScored', 'assists', 'bonus', 'saves', 'yellowCards'] as const) {
      expect(heatQuality(key, 0, 'MID')).toBe('neutral');
    }
    expect(heatQuality('goalsScored', 1, 'MID')).toBe('good');
  });

  it('reads BPS: negative poor, 15 middling, 30 good', () => {
    expect(heatQuality('bps', -3, 'DEF')).toBe('poor');
    expect(heatQuality('bps', 14, 'DEF')).toBe('neutral');
    expect(heatQuality('bps', 15, 'DEF')).toBe('mid');
    expect(heatQuality('bps', 30, 'DEF')).toBe('good');
  });

  it('counts a clean sheet only where it scores', () => {
    expect(heatQuality('cleanSheets', 1, 'DEF')).toBe('good');
    expect(heatQuality('cleanSheets', 1, 'MID')).toBe('good');
    expect(heatQuality('cleanSheets', 1, 'FWD')).toBe('neutral');
    // An unknown position scores like the rest.
    expect(heatQuality('cleanSheets', 1, 'XYZ')).toBe('good');
  });

  it('marks goals conceded down for goalkeepers and defenders only', () => {
    expect(heatQuality('goalsConceded', 2, 'GKP')).toBe('poor');
    expect(heatQuality('goalsConceded', 1, 'DEF')).toBe('mid');
    expect(heatQuality('goalsConceded', 0, 'DEF')).toBe('neutral');
    expect(heatQuality('goalsConceded', 4, 'MID')).toBe('neutral');
    expect(heatQuality('expectedGoalsConceded', 2.1, 'DEF')).toBe('poor');
    expect(heatQuality('expectedGoalsConceded', 2.1, 'FWD')).toBe('neutral');
  });

  it('grades expected stats and cards', () => {
    expect(heatQuality('expectedGoals', 0.5, 'FWD')).toBe('good');
    expect(heatQuality('expectedGoals', 0.2, 'FWD')).toBe('mid');
    expect(heatQuality('expectedGoals', 0.19, 'FWD')).toBe('neutral');
    expect(heatQuality('yellowCards', 1, 'MID')).toBe('mid');
    expect(heatQuality('redCards', 1, 'MID')).toBe('poor');
  });
});

describe('heatColors', () => {
  it('uses tint pairs for good, middling and poor; raised and sunken for the rest', () => {
    const s = darkColors.surface;
    expect(heatColors('good', darkColors)).toEqual({
      bg: tint(darkColors.win, s),
      fg: darkColors.win,
    });
    expect(heatColors('mid', darkColors)).toEqual({
      bg: tint(darkColors.accent, s),
      fg: darkColors.accent,
    });
    expect(heatColors('poor', darkColors)).toEqual({
      bg: tint(darkColors.loss, s),
      fg: darkColors.loss,
    });
    expect(heatColors('neutral', darkColors)).toEqual({
      bg: darkColors.surfaceRaised,
      fg: darkColors.text,
    });
    expect(heatColors('none', darkColors)).toEqual({
      bg: darkColors.surfaceSunken,
      fg: darkColors.textMuted,
    });
  });
});

describe('heatGroups and heatCell', () => {
  it('adds the keeping group for goalkeepers only', () => {
    expect(heatGroups('GKP').map((g) => g.id)).toContain('keeping');
    expect(heatGroups('DEF').map((g) => g.id)).not.toContain('keeping');
    expect(heatGroups('MID')[0]!.columns[0]!.key).toBe('points');
  });

  it('formats and grades a cell; a blank has no minutes', () => {
    const [xg] = heatGroups('FWD')[1]!.columns.filter((c) => c.key === 'expectedGoals');
    expect(heatCell(gw(1, { stats: { ...zeros(), expectedGoals: 0.613 } }), xg!, 'FWD')).toEqual({
      text: '0.61',
      quality: 'good',
    });
    const minutes = heatGroups('FWD')[0]!.columns[1]!;
    expect(heatCell(blank(1), minutes, 'FWD')).toEqual({ text: '–', quality: 'none' });
  });
});

describe('the ICT group', () => {
  const ict = heatGroups('GKP').find((g) => g.id === 'ict')!.columns;

  it('grades every column on one rule scaled to it, with one decimal', () => {
    const g = gw(1, {
      stats: { ...zeros(), influence: 31.2, creativity: 15, threat: 4, ictIndex: 5 },
    });
    expect(ict.map((c) => heatCell(g, c, 'MID'))).toEqual([
      { text: '31.2', quality: 'good' },
      { text: '15.0', quality: 'mid' },
      { text: '4.0', quality: 'neutral' },
      { text: '5.0', quality: 'mid' },
    ]);
    expect(heatQuality('creativity', 30, 'MID')).toBe(heatQuality('influence', 30, 'MID'));
    expect(heatQuality('ictIndex', 6, 'MID')).toBe('good');
  });

  it('is a dash while a played game has no ICT yet, and 0.0 once final', () => {
    const live = gw(7, { minutes: 90, fixtures: [fx({ status: 'live' })], stats: zeros() });
    expect(ict.map((c) => heatCell(live, c, 'GKP').text)).toEqual(['–', '–', '–', '–']);
    const done = gw(7, { minutes: 90, fixtures: [fx({ status: 'finished' })], stats: zeros() });
    expect(ict.map((c) => heatCell(done, c, 'GKP').text)).toEqual(['0.0', '0.0', '0.0', '0.0']);
  });
});

describe('logRows', () => {
  it('is empty with no games and no fixtures', () => {
    expect(logRows([], [])).toEqual([]);
  });

  it('orders oldest first and fills missing and blank gameweeks', () => {
    const rows = logRows([gw(4), blank(2), gw(1)], []);
    expect(rows.map((r) => [r.gameweek, r.kind])).toEqual([
      [1, 'played'],
      [2, 'blank'],
      [3, 'blank'],
      [4, 'played'],
    ]);
  });

  it('reads a gameweek whose games have not kicked off as still to come', () => {
    const rows = logRows([gw(2, { fixtures: [fx({ status: 'scheduled' })] }), gw(1)], []);
    expect(rows.map((r) => r.kind)).toEqual(['played', 'future']);
  });

  it('adds up to five upcoming gameweeks, with gaps as blanks', () => {
    const rows = logRows(
      [gw(1)],
      [upcoming(2), upcoming(4), upcoming(5), upcoming(6), upcoming(7), upcoming(9)],
    );
    expect(rows.map((r) => [r.gameweek, r.kind])).toEqual([
      [1, 'played'],
      [2, 'future'],
      [3, 'blank'],
      [4, 'future'],
      [5, 'future'],
      [6, 'future'],
    ]);
  });

  it('shows a blank straight after the log', () => {
    expect(logRows([gw(1)], [upcoming(3)]).map((r) => [r.gameweek, r.kind])).toEqual([
      [1, 'played'],
      [2, 'blank'],
      [3, 'future'],
    ]);
  });

  it('skips fixtures of gameweeks already in the log', () => {
    expect(logRows([gw(2), gw(1)], [upcoming(2)]).map((r) => r.gameweek)).toEqual([1, 2]);
  });
});

describe('helpers', () => {
  it('isBlank needs no fixture, no minutes and no stats', () => {
    expect(isBlank(blank(1))).toBe(true);
    expect(isBlank(gw(1, { fixtures: [] }))).toBe(false);
  });

  it('fixtureResult reads the score from the player side', () => {
    expect(fixtureResult({ isHome: true, homeScore: 2, awayScore: 1 })).toBe('win');
    expect(fixtureResult({ isHome: false, homeScore: 2, awayScore: 1 })).toBe('loss');
    expect(fixtureResult({ isHome: false, homeScore: 1, awayScore: 1 })).toBe('draw');
    expect(fixtureResult({ isHome: true, homeScore: null, awayScore: null })).toBeNull();
  });

  it('splitName keeps a single name as the last name', () => {
    expect(splitName('Bukayo Saka')).toEqual({ first: 'Bukayo', last: 'Saka' });
    expect(splitName('Kevin De Bruyne')).toEqual({ first: 'Kevin De', last: 'Bruyne' });
    expect(splitName(' Rodri ')).toEqual({ first: '', last: 'Rodri' });
  });

  it('chanceFromNews finds a percentage, or null', () => {
    expect(chanceFromNews('Knee injury - 75% chance of playing')).toBe(75);
    expect(chanceFromNews('Suspended until GW9')).toBeNull();
    expect(chanceFromNews(null)).toBeNull();
    expect(chanceFromNews('250% fit')).toBeNull();
  });
});
