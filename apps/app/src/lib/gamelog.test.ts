import type { Schemas } from '@myfantasyprem/api-client';

import {
  GROUP_COLUMNS,
  GROUP_DETAILS,
  MISSING,
  cardsText,
  detailSections,
  eventChips,
  formatStat,
  opponentLabel,
  groupTotals,
  ordinal,
  rowCells,
  type StatCell,
} from './gamelog';
import { NO_STATS } from '@/mocks/data';

type Stats = Schemas['PlayerGameStats'];

const stats = (over: Partial<Stats> = {}): Stats => ({
  ...(Object.fromEntries(Object.keys(NO_STATS).map((k) => [k, 0])) as Stats),
  ...over,
});
const gw = (minutes: number, s: Stats) => ({ minutes, stats: s });
const texts = (cells: StatCell[]) => cells.map((c) => c.text);

describe('game log columns', () => {
  it('orders the scoring columns with cards as one column and DC before B', () => {
    expect(GROUP_COLUMNS.scoring.map((c) => c.label)).toEqual([
      'MP',
      'G',
      'A',
      'CS',
      'GC',
      'Cards',
      'DC',
      'B',
      'BPS',
    ]);
    const labels = GROUP_COLUMNS.scoring.map((c) => c.label);
    expect(labels.indexOf('DC')).toBe(labels.indexOf('B') - 1);
    expect(labels).not.toContain('RC');
    expect(labels).not.toContain('YC');
  });

  it('shows the underlying and defensive columns from the design', () => {
    expect(GROUP_COLUMNS.underlying.map((c) => c.label)).toEqual([
      'I',
      'C',
      'T',
      'ICT',
      'xG',
      'xA',
      'xGI',
      'xGC',
    ]);
    expect(GROUP_COLUMNS.defensive.map((c) => c.label)).toEqual(['CBIT', 'R', 'T', 'DC']);
  });

  it('lists every stat in a phone row, defensive last', () => {
    const sections = detailSections(gw(90, stats({ cbit: 11, recoveries: 3, tackles: 2 })));
    expect(sections.map((s) => s.title)).toEqual(['Scoring', 'Underlying', 'Defensive']);
    expect(sections[0]!.stats).toHaveLength(GROUP_DETAILS.scoring.length);
    expect(sections[2]!.stats.map((s) => [s.label, s.text])).toEqual([
      ['CBIT', '11'],
      ['Recoveries', '3'],
      ['Tackles', '2'],
    ]);
  });
});

describe('formatting', () => {
  it('formats integers, decimals and DC points, and dashes missing values', () => {
    expect(formatStat(3)).toBe('3');
    expect(formatStat(-2)).toBe('-2');
    expect(formatStat(0.5, 'dec2')).toBe('0.50');
    expect(formatStat(43, 'dec1')).toBe('43.0');
    expect(formatStat(2, 'plus')).toBe('+2');
    expect(formatStat(0, 'plus')).toBe('0');
    expect(formatStat(null)).toBe(MISSING);
    expect(formatStat(undefined, 'dec2')).toBe(MISSING);
    expect(formatStat(Number.NaN)).toBe(MISSING);
  });

  it('renders a scoring row with a cards cell instead of yellow/red columns', () => {
    const cells = rowCells(
      gw(76, stats({ assists: 1, goalsConceded: 1, yellowCards: 1, bps: 17 })),
      'scoring',
    );
    expect(texts(cells)).toEqual(['76', '0', '1', '0', '1', '1 yellow', '0', '0', '17']);
    const cards = cells[5]!;
    expect(cards.kind).toBe('cards');
    expect(cards.kind === 'cards' && cards.cards).toEqual({ yellow: 1, red: 0 });
    expect(cells[1]).toMatchObject({ zero: true });
    expect(cells[2]).toMatchObject({ zero: false });
  });

  it('describes cards', () => {
    expect(cardsText({ yellow: 0, red: 0 })).toBe('None');
    expect(cardsText({ yellow: 1, red: 1 })).toBe('1 yellow, 1 red');
    expect(cardsText({ yellow: null, red: null })).toBe(MISSING);
  });

  it('dashes every stat of a blank gameweek, minutes included', () => {
    const cells = rowCells(gw(0, NO_STATS), 'scoring');
    expect(texts(cells).every((t) => t === MISSING)).toBe(true);
    expect(texts(rowCells(gw(0, NO_STATS), 'underlying'))).toEqual(Array(8).fill(MISSING));
    // Benched with stats: 0 minutes is a real zero.
    expect(rowCells(gw(0, stats()), 'scoring')[0]).toMatchObject({ text: '0', zero: true });
  });

  it('dashes only the missing stats when a source has some', () => {
    const partial = { ...NO_STATS, goalsScored: 1, assists: 0 };
    expect(texts(rowCells(gw(90, partial), 'scoring'))).toEqual([
      '90',
      '1',
      '0',
      MISSING,
      MISSING,
      MISSING,
      MISSING,
      MISSING,
      MISSING,
    ]);
  });

  it('formats underlying stats with fixed decimals', () => {
    const s = stats({
      influence: 40.6,
      creativity: 45.3,
      threat: 43,
      ictIndex: 12.9,
      expectedGoals: 1.47,
      expectedAssists: 0.21,
      expectedGoalInvolvements: 1.68,
      expectedGoalsConceded: 0.29,
    });
    expect(texts(rowCells(gw(77, s), 'underlying'))).toEqual([
      '40.6',
      '45.3',
      '43.0',
      '12.9',
      '1.47',
      '0.21',
      '1.68',
      '0.29',
    ]);
  });
});

describe('totals', () => {
  it('sums each column, skipping missing values, and dashes all-missing columns', () => {
    const weeks = [
      gw(90, stats({ goalsScored: 1, yellowCards: 1, bonus: 2, expectedGoals: 0.25 })),
      gw(0, NO_STATS),
      gw(67, stats({ redCards: 1, defensiveContributionPoints: 2, expectedGoals: 0.1 })),
    ];
    expect(texts(groupTotals(weeks, 'scoring'))).toEqual([
      '157',
      '1',
      '0',
      '0',
      '0',
      '1 yellow, 1 red',
      '2',
      '2',
      '0',
    ]);
    expect(groupTotals(weeks, 'underlying')[4]!.text).toBe('0.35');
    expect(texts(groupTotals([gw(0, NO_STATS)], 'defensive'))).toEqual(Array(4).fill(MISSING));
  });
});

describe('event chips', () => {
  it('lists what happened, with cards in colour', () => {
    expect(
      eventChips(
        'DEF',
        stats({
          goalsScored: 2,
          assists: 1,
          cleanSheets: 1,
          defensiveContributionPoints: 2,
          bonus: 3,
          yellowCards: 1,
          redCards: 1,
        }),
      ),
    ).toEqual([
      { text: '2 goals', tone: 'neutral' },
      { text: 'Assist', tone: 'neutral' },
      { text: 'Clean sheet', tone: 'neutral' },
      { text: 'DC +2', tone: 'neutral' },
      { text: 'Bonus +3', tone: 'neutral' },
      { text: 'Yellow', tone: 'yellow' },
      { text: 'Red', tone: 'red' },
    ]);
    expect(eventChips('DEF', stats({ goalsConceded: 4 }))).toEqual([
      { text: '4 conceded', tone: 'neutral' },
    ]);
    expect(eventChips('DEF', stats({ goalsConceded: 1 }))).toEqual([]);
    expect(eventChips('DEF', NO_STATS)).toEqual([]);
  });

  // Only what scores for the position (FPL 2026-27): a clean sheet scores
  // for GK, DEF and MID; goals conceded only for GK and DEF; saves (one point
  // per three) for whoever keeps goal; DC and bonus when they earned points.
  const everything = stats({
    goalsScored: 1,
    assists: 1,
    cleanSheets: 1,
    goalsConceded: 2,
    saves: 3,
    penaltiesSaved: 1,
    penaltiesMissed: 1,
    ownGoals: 1,
    defensiveContributionPoints: 2,
    bonus: 1,
    yellowCards: 1,
  });
  const chipTexts = (position: string, s: Stats) => eventChips(position, s).map((c) => c.text);

  it('shows a forward no clean sheet and no goals conceded', () => {
    expect(chipTexts('FWD', everything)).toEqual([
      'Goal',
      'Assist',
      '3 saves',
      'Pen saved',
      'DC +2',
      'Bonus +1',
      'Own goal',
      'Pen missed',
      'Yellow',
    ]);
    expect(chipTexts('FWD', stats({ cleanSheets: 1, goalsConceded: 4 }))).toEqual([]);
  });

  it('shows a midfielder a clean sheet but no goals conceded', () => {
    expect(chipTexts('MID', everything)).toEqual([
      'Goal',
      'Assist',
      'Clean sheet',
      '3 saves',
      'Pen saved',
      'DC +2',
      'Bonus +1',
      'Own goal',
      'Pen missed',
      'Yellow',
    ]);
    expect(chipTexts('MID', stats({ goalsConceded: 3 }))).toEqual([]);
  });

  it('shows a defender a clean sheet and goals conceded', () => {
    expect(chipTexts('DEF', everything)).toEqual([
      'Goal',
      'Assist',
      'Clean sheet',
      '2 conceded',
      '3 saves',
      'Pen saved',
      'DC +2',
      'Bonus +1',
      'Own goal',
      'Pen missed',
      'Yellow',
    ]);
  });

  it('shows a keeper saves only once they score, and goals conceded', () => {
    expect(chipTexts('GKP', stats({ saves: 2, goalsConceded: 1, cleanSheets: 0 }))).toEqual([]);
    expect(chipTexts('GKP', stats({ saves: 7, goalsConceded: 3, penaltiesSaved: 2 }))).toEqual([
      '3 conceded',
      '7 saves',
      '2 pens saved',
    ]);
    expect(chipTexts('GKP', stats({ cleanSheets: 1, bonus: 2 }))).toEqual([
      'Clean sheet',
      'Bonus +2',
    ]);
  });

  it('shows every scoring event for a position it does not know', () => {
    expect(chipTexts('WING', stats({ cleanSheets: 1, goalsConceded: 2 }))).toEqual([
      'Clean sheet',
      '2 conceded',
    ]);
  });

  it('writes ranks as ordinals', () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 102, 111].map(ordinal)).toEqual([
      '1st',
      '2nd',
      '3rd',
      '4th',
      '11th',
      '12th',
      '13th',
      '21st',
      '102nd',
      '111th',
    ]);
    expect(ordinal(null)).toBe(MISSING);
  });
});

describe('opponents', () => {
  const fx = (code: string, isHome: boolean, homeScore: number | null, awayScore: number | null) =>
    ({ opponent: { code, name: code }, isHome, homeScore, awayScore }) as Schemas['PlayerFixture'];
  it('labels each game with venue and score, home first', () => {
    expect(opponentLabel([fx('avl', true, 4, 0)])).toBe('AVL (H) 4-0');
    expect(opponentLabel([fx('SUN', false, 0, 2), fx('CHE', true, null, null)])).toBe(
      'SUN (A) 0-2, CHE (H)',
    );
    expect(opponentLabel([])).toBe('No fixture');
  });
});
