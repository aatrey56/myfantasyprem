import type { Schemas } from '@myfantasyprem/api-client';
import { fireEvent, render, screen, within } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import {
  CareerTable,
  CardsGlyphs,
  currentGameweekOf,
  Fixtures,
  GameLog,
  GameweekStrip,
  kickoffText,
  NextFixture,
  SeasonStats,
  StatStrip,
  StatusBanner,
} from './PlayerCard';
import { cardsText, type Cards } from '@/lib/gamelog';
import { NO_STATS } from '@/mocks/data';
import { darkColors, tint } from '@/theme/tokens';

const glyphs = async (cards: Cards) => {
  await render(
    <CardsGlyphs cell={{ kind: 'cards', key: 'cards', cards, text: cardsText(cards) }} />,
  );
};
const fill = (id: string) =>
  screen.getAllByTestId(id).map((n) => StyleSheet.flatten(n.props.style).backgroundColor);

describe('CardsGlyphs', () => {
  it('draws one coloured card per yellow and red, labelled for screen readers', async () => {
    await glyphs({ yellow: 1, red: 1 });
    expect(screen.getByLabelText('1 yellow, 1 red')).toBeTruthy();
    expect(fill('card-yellow')).toHaveLength(1);
    expect(fill('card-yellow')[0]).toBe(darkColors.warning);
    expect(fill('card-red')[0]).toBe(darkColors.loss);
    expect(screen.queryByText(/R|Y/)).toBeNull();
  });

  it('draws two yellows as two cards', async () => {
    await glyphs({ yellow: 2, red: 0 });
    expect(fill('card-yellow')).toHaveLength(2);
    expect(screen.queryByTestId('card-red')).toBeNull();
  });

  it('shows 0 for no cards', async () => {
    await glyphs({ yellow: 0, red: 0 });
    expect(screen.getByText('0')).toBeTruthy();
    expect(screen.queryByTestId('card-yellow')).toBeNull();
  });

  it('shows a dash when cards are unknown', async () => {
    await glyphs({ yellow: null, red: null });
    expect(screen.getByText('–')).toBeTruthy();
  });
});

describe('SeasonStats', () => {
  const line = (stat: string, value: number, points: number) =>
    ({ stat, value, points }) as Schemas['PointsLine'];
  const fixture = (...breakdown: Schemas['PointsLine'][]) =>
    ({ breakdown }) as Schemas['PlayerFixture'];
  const detail = {
    gameweeks: [
      {
        gameweek: 2,
        fixtures: [
          fixture(line('minutes', 90, 2), line('goals_scored', 1, 5), line('bonus', 3, 3)),
        ],
      },
      { gameweek: 1, fixtures: [fixture(line('minutes', 30, 1)), fixture(line('minutes', 0, 0))] },
    ],
  } as Schemas['PlayerDetail'];

  it('reads minutes as appearances, with the minutes as secondary text', async () => {
    await render(<SeasonStats d={detail} />);
    const row = screen.getByTestId('season-row-minutes');
    expect(within(row).getByText('Appearances')).toBeTruthy();
    expect(within(row).getByText('2')).toBeTruthy();
    expect(within(row).getByText('120 min')).toBeTruthy();
    expect(within(row).getByText('+3')).toBeTruthy();
    expect(row.props.accessibilityLabel).toBe('Appearances: 2, 120 minutes, 3 points');
    expect(screen.queryByText(/Minutes played/)).toBeNull();
  });

  it('adds the season starts beside the minutes when the source sends them', async () => {
    await render(<SeasonStats d={{ ...detail, starts: 1 }} />);
    const row = screen.getByTestId('season-row-minutes');
    expect(within(row).getByText('120 min · 1 start')).toBeTruthy();
    expect(row.props.accessibilityLabel).toBe('Appearances: 2, 120 minutes, 1 start, 3 points');
  });

  it('heads the right-hand column as points', async () => {
    await render(<SeasonStats d={detail} />);
    // Visual only: each row's label already says "points" to screen readers.
    const head = screen.getByTestId('card-season-head', { includeHiddenElements: true });
    expect(within(head).getByText('Pts', { includeHiddenElements: true })).toBeTruthy();
    const goals = screen.getByTestId('season-row-goals_scored');
    expect(within(goals).getByText('Goals')).toBeTruthy();
    expect(goals.props.accessibilityLabel).toBe('Goals: 1, 5 points');
  });

  it('shows bonus once, as points, with the count column empty', async () => {
    await render(<SeasonStats d={detail} />);
    const bonus = screen.getByTestId('season-row-bonus');
    expect(within(bonus).getByText('Bonus')).toBeTruthy();
    expect(within(bonus).getByText('+3')).toBeTruthy();
    expect(within(bonus).queryByText('3')).toBeNull();
    expect(bonus.props.accessibilityLabel).toBe('Bonus: 3 points');
  });
});

describe('GameLog', () => {
  const stats = (over: Partial<Schemas['PlayerGameStats']>) => ({
    ...(Object.fromEntries(Object.keys(NO_STATS).map((k) => [k, 0])) as Schemas['PlayerGameStats']),
    ...over,
  });
  const week = (gameweek: number, over: Partial<Schemas['PlayerGameStats']> = {}) =>
    ({
      gameweek,
      points: 6,
      minutes: 90,
      fixtures: [],
      stats: stats(over),
    }) as Schemas['PlayerGameweek'];
  const blankWeek = (gameweek: number) =>
    ({
      gameweek,
      points: 0,
      minutes: 0,
      fixtures: [],
      stats: Object.fromEntries(
        Object.keys(NO_STATS).map((k) => [k, null]),
      ) as Schemas['PlayerGameStats'],
    }) as Schemas['PlayerGameweek'];
  const log = (
    position: string,
    gameweeks: Schemas['PlayerGameweek'][],
    upcoming: Schemas['UpcomingFixture'][] = [],
  ) =>
    render(
      <GameLog
        d={{ player: { position } as Schemas['PlayerDetail']['player'], gameweeks, upcoming }}
      />,
    );
  const expanded = (gw: number) =>
    screen.getByTestId(`log-row-${gw}`).props.accessibilityState?.expanded;
  const tileFill = (node: ReturnType<typeof screen.getByTestId>) =>
    StyleSheet.flatten(node.props.style).backgroundColor;

  it('pins GW and OPP opaquely over the left padding, so nothing scrolls out beside them', async () => {
    await log('GKP', [week(1)]);
    const head = StyleSheet.flatten(
      screen.getByTestId('log-pinned-head', { includeHiddenElements: true }).props.style,
    );
    expect(head.backgroundColor).toBe(darkColors.surface);
    expect(head.marginLeft).toBeLessThan(0);
    expect(head.paddingLeft).toBe(-head.marginLeft);
    expect(head.zIndex).toBeGreaterThan(0);
    expect(head.borderRightWidth).toBe(1);
  });

  it('opens with every row collapsed; a tap expands and collapses a row', async () => {
    await log('MID', [week(3), week(2), week(1)]);
    for (const gw of [3, 2, 1]) {
      expect(expanded(gw)).toBe(false);
      expect(screen.queryByTestId(`log-detail-${gw}`)).toBeNull();
    }
    await fireEvent.press(screen.getByTestId('log-row-3'));
    expect(expanded(3)).toBe(true);
    expect(screen.getByTestId('log-detail-3')).toBeTruthy();
    await fireEvent.press(screen.getByTestId('log-row-1'));
    expect(expanded(3)).toBe(false);
    expect(expanded(1)).toBe(true);
    await fireEvent.press(screen.getByTestId('log-row-1'));
    expect(expanded(1)).toBe(false);
    expect(screen.queryByTestId('log-detail-1')).toBeNull();
  });

  it("shows only chips that score for the player's position", async () => {
    await log('FWD', [week(1, { cleanSheets: 1, goalsConceded: 2, goalsScored: 1 })]);
    await fireEvent.press(screen.getByTestId('log-row-1'));
    // The chips above the full stat list.
    const chips = within(screen.getByTestId('log-detail-1')).getByTestId('log-chips-1');
    expect(within(chips).getByText('Goal')).toBeTruthy();
    expect(within(chips).queryByText('Clean sheet')).toBeNull();
    expect(within(chips).queryByText('2 conceded')).toBeNull();
  });

  it('tints each tile by quality', async () => {
    await log('MID', [week(1, { goalsScored: 1, bonus: 0 })]);
    const row = screen.getByTestId('log-row-1');
    expect(tileFill(within(row).getByTestId('log-goalsScored'))).toBe(
      tint(darkColors.win, darkColors.surface),
    );
    expect(tileFill(within(row).getByTestId('log-bonus'))).toBe(darkColors.surfaceRaised);
    expect(tileFill(within(row).getByTestId('log-points'))).toBe(
      tint(darkColors.accent, darkColors.surface),
    );
  });

  it('greys a blank gameweek (given or missing) with dashes, and it does not expand', async () => {
    await log('DEF', [week(4), blankWeek(2), week(1)]);
    for (const gw of [2, 3]) {
      const row = screen.getByTestId(`log-blank-${gw}`);
      expect(within(row).getByText('Blank')).toBeTruthy();
      expect(within(row).getAllByText('–').length).toBeGreaterThan(5);
      expect(row.props.accessibilityLabel).toBe(`Gameweek ${gw}, no fixture`);
      expect(row.props.accessibilityState?.expanded).toBeUndefined();
    }
    expect(screen.queryByTestId('log-row-2')).toBeNull();
    expect(screen.getByTestId('log-row-4')).toBeTruthy();
  });

  it('lists the next gameweeks with dashes after the played ones', async () => {
    const next = {
      fixtureId: 'fx_1',
      gameweek: 3,
      opponent: { code: 'che', name: 'Chelsea' },
      isHome: false,
      kickoffAt: '2026-10-24T14:00:00Z',
    } as Schemas['UpcomingFixture'];
    await log('MID', [week(2), week(1)], [next]);
    const row = screen.getByTestId('log-next-3');
    expect(within(row).getByText('@ CHE')).toBeTruthy();
    expect(within(row).getAllByText('–').length).toBeGreaterThan(5);
  });
});

describe('StatusBanner', () => {
  it('shows doubtful in amber with the chance from the news', async () => {
    await render(
      <StatusBanner player={{ availability: 'doubtful', news: 'Knock - 75% chance of playing' }} />,
    );
    const banner = screen.getByTestId('card-status');
    expect(within(banner).getByText('DOUBTFUL · 75%')).toBeTruthy();
    expect(StyleSheet.flatten(banner.props.style).backgroundColor).toBe(darkColors.accent);
  });

  it('shows out in red, and nothing for an available player', async () => {
    await render(<StatusBanner player={{ availability: 'injured', news: null }} />);
    const banner = screen.getByTestId('card-status');
    expect(StyleSheet.flatten(banner.props.style).backgroundColor).toBe(darkColors.danger);
    await render(<StatusBanner player={{ availability: 'available', news: null }} />);
    expect(screen.queryByTestId('card-status')).toBeNull();
  });
});

describe('NextFixture', () => {
  it("names the opponent by code, like the player's club, so the rank chip and kickoff fit", async () => {
    const d = {
      player: { position: 'DEF', club: { code: 'iro', name: 'Ironbridge' } },
      upcoming: [
        {
          fixtureId: 'fx_1',
          gameweek: 8,
          opponent: { code: 'dal', name: 'Dalton Athletic' },
          isHome: true,
          kickoffAt: '2026-10-23T16:08:00Z',
        },
      ],
    } as unknown as Parameters<typeof NextFixture>[0]['d'];
    await render(<NextFixture d={d} />);
    const next = screen.getByTestId('card-next');
    expect(within(next).getByText('IRO')).toBeTruthy();
    expect(within(next).getByText('DAL')).toBeTruthy();
    expect(within(next).queryByText('Dalton Athletic')).toBeNull();
  });
});

describe('StatStrip', () => {
  it('lays rank, points per gameweek and returns out in three equal, centred thirds', async () => {
    await render(
      <StatStrip
        d={{
          player: { position: 'GKP' } as Schemas['PlayerDetail']['player'],
          gameweeks: [],
          totalPoints: 0,
          positionRank: 3,
          draftRank: 40,
          form: null,
        }}
      />,
    );
    for (const label of ['Rank', 'Pts / GW', 'Returns']) {
      const style = StyleSheet.flatten(screen.getByTestId(`card-strip-${label}`).props.style);
      expect(style).toMatchObject({ flex: 1, flexBasis: 0, alignItems: 'center' });
    }
  });
});

describe('GameweekStrip', () => {
  const week = (gameweek: number, status: Schemas['FixtureStatus'] = 'finished') =>
    ({
      gameweek,
      points: 2,
      minutes: 90,
      fixtures: [
        {
          fixtureId: `fx_${gameweek}`,
          status,
          isHome: true,
          opponent: { code: 'ars', name: 'Arsenal' },
        },
      ],
      stats: Object.fromEntries(Object.keys(NO_STATS).map((k) => [k, 0])),
    }) as unknown as Schemas['PlayerGameweek'];

  it('shows every gameweek from the first to the current one, never a future one', async () => {
    const gameweeks = [1, 2, 3, 4, 5, 6, 7].map((n) => week(n)).concat(week(8, 'scheduled'));
    await render(<GameweekStrip d={{ gameweeks }} current={7} />);
    for (let n = 1; n <= 7; n++) expect(screen.getByTestId(`gw-card-${n}`)).toBeTruthy();
    expect(screen.queryByTestId('gw-card-8')).toBeNull();
  });

  it('without a current gameweek, stops at the last one played', async () => {
    await render(<GameweekStrip d={{ gameweeks: [week(1), week(2, 'scheduled')] }} />);
    expect(screen.getByTestId('gw-card-1')).toBeTruthy();
    expect(screen.queryByTestId('gw-card-2')).toBeNull();
  });

  it('takes the current gameweek as the latest whose deadline has passed', () => {
    const now = Date.parse('2026-10-17T12:00:00Z');
    expect(currentGameweekOf({ number: 8, deadlineAt: '2026-10-24T10:00:00Z' }, now)).toBe(7);
    expect(currentGameweekOf({ number: 7, deadlineAt: '2026-10-10T10:00:00Z' }, now)).toBe(7);
    expect(currentGameweekOf(undefined, now)).toBeUndefined();
  });
});

describe('Fixtures', () => {
  const up = (
    fixtureId: string,
    gameweek: number | null,
    over: Partial<Schemas['UpcomingFixture']> = {},
  ) =>
    ({
      fixtureId,
      gameweek,
      isHome: true,
      opponent: { code: 'ars', name: 'Arsenal' },
      kickoffAt: '2026-12-01T15:00:00Z',
      ...over,
    }) as Schemas['UpcomingFixture'];
  const player = { position: 'MID' } as Schemas['PlayerDetail']['player'];

  it('lists every remaining gameweek to the last, with doubles, blanks, moved and TBC', async () => {
    const upcoming = [8, 9, 9, 11, 38].map((gw, i) => up(`fx_${i}`, gw));
    upcoming.push(up('fx_tbc', 12, { kickoffAt: null }));
    upcoming.push(up('fx_moved', null, { kickoffAt: null, status: 'postponed' }));
    await render(<Fixtures d={{ player, upcoming }} />);
    for (let gw = 8; gw <= 38; gw++) expect(screen.getByTestId(`card-gw-${gw}`)).toBeTruthy();
    expect(within(screen.getByTestId('card-gw-9')).getByText('Double')).toBeTruthy();
    expect(within(screen.getByTestId('card-gw-10')).getByText('— Blank')).toBeTruthy();
    expect(within(screen.getByTestId('card-fixture-fx_tbc')).getByText(/· TBC$/)).toBeTruthy();
    expect(within(screen.getByTestId('card-gw-tbc')).getByText(/· Moved$/)).toBeTruthy();
  });

  it('reads the kickoff, Moved when postponed, TBC without a time', () => {
    expect(kickoffText({ kickoffAt: null })).toBe('TBC');
    expect(kickoffText({ kickoffAt: '2026-12-01T15:00:00Z', status: 'postponed' })).toBe('Moved');
    expect(kickoffText({ kickoffAt: '2026-12-01T15:00:00Z' })).not.toBe('TBC');
  });
});

describe('CareerTable', () => {
  const blank = {
    points: null,
    minutes: null,
    goalsScored: null,
    assists: null,
    cleanSheets: null,
    bonus: null,
    starts: null,
    expectedGoals: null,
    expectedAssists: null,
    expectedGoalInvolvements: null,
    ictIndex: null,
    defensiveContribution: null,
  };
  // Henderson's real 2015/16 (no ICT, starts, xG yet) and 2022/23.
  const seasons: Schemas['PlayerSeasonTotals'][] = [
    {
      ...blank,
      season: '2015/16',
      points: 57,
      minutes: 1290,
      goalsScored: 2,
      assists: 3,
      cleanSheets: 5,
      bonus: 3,
    },
    {
      ...blank,
      season: '2022/23',
      points: 69,
      minutes: 2060,
      goalsScored: 0,
      assists: 3,
      cleanSheets: 8,
      bonus: 0,
      starts: 23,
      expectedGoals: 1.35,
      expectedAssists: 3.59,
      expectedGoalInvolvements: 4.93,
      ictIndex: 103,
    },
  ];

  it('lists seasons newest first with a career row, "—" where untracked', async () => {
    await render(<CareerTable seasons={seasons} />);
    const ids = screen.getAllByTestId(/^career-row-[^-]+$/).map((n) => n.props.testID);
    expect(ids).toEqual(['career-row-2022/23', 'career-row-2015/16', 'career-row-total']);
    expect(screen.getByTestId('career-row-2015/16-ictIndex')).toHaveTextContent('—');
    expect(screen.getByTestId('career-row-2015/16-starts')).toHaveTextContent('—');
    expect(screen.getByTestId('career-row-2022/23-goalsScored')).toHaveTextContent('0');
    expect(screen.getByTestId('career-row-2022/23-expectedGoals')).toHaveTextContent('1.35');
    // Career: 57 + 69 = 126 points; ICT only from 2022/23 (103.0); DC never tracked.
    expect(screen.getByTestId('career-row-total-points')).toHaveTextContent('126');
    expect(screen.getByTestId('career-row-total-ictIndex')).toHaveTextContent('103.0');
    expect(screen.getByTestId('career-row-total-defensiveContribution')).toHaveTextContent('—');
    expect(
      screen.getByTestId('career-head-assists', { includeHiddenElements: true }).props[
        'aria-label'
      ],
    ).toBe('FPL assists');
    expect(screen.getByTestId('career-note')).toHaveTextContent(/FPL assists/);
  });

  it('says so when there are no previous seasons', async () => {
    await render(<CareerTable seasons={[]} />);
    expect(screen.getByTestId('career-empty')).toHaveTextContent(
      'No previous Premier League seasons.',
    );
  });
});
