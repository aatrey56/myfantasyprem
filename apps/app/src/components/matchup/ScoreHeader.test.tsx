import type { Schemas } from '@myfantasyprem/api-client';
import { render, screen, within } from '@testing-library/react-native';

import { MatchupList } from './MatchupList';
import { PointsSplit, ScoreHeader } from './ScoreHeader';
import { orient, pointsShare, recordLine, sidePoints, teamRecords, yetToPlay } from '@/lib/matchup';
import * as data from '@/mocks/data';

jest.mock('expo-router', () => ({
  ...jest.requireActual('expo-router'),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), navigate: jest.fn() }),
}));

const detailWhere = (pick: (m: Schemas['MatchupSummary']) => boolean) => {
  const summary = data.matchupSummaries(data.CURRENT_GW).find(pick)!;
  return data.matchupDetail(data.CURRENT_GW, summary.id)!;
};

const realSwitch = process.env.EXPO_PUBLIC_SHOW_PROJECTIONS;
afterEach(() => {
  if (realSwitch === undefined) delete process.env.EXPO_PUBLIC_SHOW_PROJECTIONS;
  else process.env.EXPO_PUBLIC_SHOW_PROJECTIONS = realSwitch;
});
const withSplit = () => (process.env.EXPO_PUBLIC_SHOW_PROJECTIONS = 'true');

/** Any text with a percent sign, hidden ones too. */
const percents = () => screen.queryAllByText(/%/, { includeHiddenElements: true });

describe('with projections off (the default)', () => {
  it('the score header shows only the points-share bar; matchup cards show no percentages', async () => {
    delete process.env.EXPO_PUBLIC_SHOW_PROJECTIONS;
    const detail = detailWhere((m) => m.home.kind === 'team' && m.away.kind === 'team');
    const { left, right } = orient(detail, undefined);
    expect(pointsShare(sidePoints(left), sidePoints(right))).not.toBeNull();
    const { unmount } = await render(
      <ScoreHeader
        leagueId={data.LEAGUE_ID}
        left={left}
        right={right}
        state={detail.state}
        gameweek={detail.gameweek}
      />,
    );
    expect(screen.queryByTestId('share-left')).toBeNull();
    // The thin points-share bar is the only percentage pair (not a win chance).
    expect(percents()).toHaveLength(2);
    expect(screen.getByTestId('points-split')).toBeTruthy();
    await unmount();
    await render(
      <MatchupList leagueId={data.LEAGUE_ID} matchups={data.matchupSummaries(data.CURRENT_GW)} />,
    );
    expect(percents()).toHaveLength(0);
  });
});

describe('ScoreHeader', () => {
  beforeEach(withSplit);
  it('splits the live points into honest shares and counts starters yet to play', async () => {
    const detail = detailWhere((m) => m.home.kind === 'team' && m.away.kind === 'team');
    const { left, right } = orient(detail, undefined);
    const records = teamRecords(data.standings());
    await render(
      <ScoreHeader
        leagueId={data.LEAGUE_ID}
        left={left}
        right={right}
        state={detail.state}
        gameweek={detail.gameweek}
        records={records}
      />,
    );
    const share = pointsShare(sidePoints(left), sidePoints(right));
    if (share) {
      expect(screen.getByTestId('share-left')).toHaveTextContent(`${share.left}%`, {
        exact: false,
      });
      expect(screen.getByTestId('share-right')).toHaveTextContent(`${share.right}%`, {
        exact: false,
      });
    }
    expect(screen.queryByText(/win/i)).toBeNull();
    const lineup = left as Schemas['TeamLineup'];
    expect(screen.getByTestId('yet-to-play-left')).toHaveTextContent(
      `yet to play (${yetToPlay(lineup.starters).count})`,
    );
    expect(screen.getByTestId('score-left-points')).toHaveTextContent(String(sidePoints(left)));
    expect(screen.getByTestId('score-state')).toHaveTextContent('Live');
    // Manager, record, streak and rank: `Sam P. • 3-0-3 ▼1L (#6)`.
    expect(screen.getByTestId('subline-left')).toHaveTextContent(
      `${lineup.team.managerName} • ${recordLine(records.get(lineup.team.id)!)}`,
    );
    expect(recordLine(records.get(lineup.team.id)!)).toMatch(/^\d+-\d+-\d+ [▲▼=]\d[WDL] \(#\d+\)$/);
    // A dot per starter under "yet to play", filled once he has played.
    const played = lineup.starters.length - yetToPlay(lineup.starters).count;
    expect(
      within(screen.getByTestId('played-dots-left')).getAllByTestId('dot-played', {
        includeHiddenElements: true,
      }),
    ).toHaveLength(played);
    expect(
      within(screen.getByTestId('played-dots-left')).queryAllByTestId('dot-to-play', {
        includeHiddenElements: true,
      }),
    ).toHaveLength(lineup.starters.length - played);
  });

  it('keeps the league-average side without a lineup count', async () => {
    const detail = detailWhere((m) => m.away.kind === 'average' || m.home.kind === 'average');
    const { left, right } = orient(detail, undefined);
    await render(
      <ScoreHeader
        leagueId={data.LEAGUE_ID}
        left={left}
        right={right}
        state={detail.state}
        gameweek={detail.gameweek}
      />,
    );
    expect(screen.getByTestId('score-right-name')).toHaveTextContent('League average');
    expect(screen.getByText('Average of all teams')).toBeOnTheScreen();
    expect(screen.queryByTestId('yet-to-play-right')).toBeNull();
  });

  it('shows a dash, not a share, at 0-0', async () => {
    const team = (id: string, name: string): Schemas['MatchupSide'] => ({
      kind: 'team',
      team: { id, name, managerName: 'M' },
      points: 0,
    });
    await render(
      <ScoreHeader
        leagueId={data.LEAGUE_ID}
        left={team('tm_a', 'A')}
        right={team('tm_b', 'B')}
        state="live"
        gameweek={7}
      />,
    );
    expect(screen.getByTestId('share-left')).toHaveTextContent('–');
    expect(screen.getByTestId('share-right')).toHaveTextContent('–');
  });
});

describe('MatchupList', () => {
  beforeEach(withSplit);
  it('renders one card per matchup with records and ranks', async () => {
    const matchups = data.matchupSummaries(data.CURRENT_GW);
    const records = teamRecords(data.standings());
    await render(<MatchupList leagueId={data.LEAGUE_ID} matchups={matchups} records={records} />);
    for (const m of matchups) expect(screen.getByTestId(`matchup-row-${m.id}`)).toBeOnTheScreen();
    const row = data.standings()[0]!;
    expect(screen.getAllByText(new RegExp(`\\(#${row.rank}\\)`)).length).toBeGreaterThan(0);
    expect(screen.getAllByText('VS')).toHaveLength(matchups.length);
  });

  it('shows streaks and a medal on the top three avatars', async () => {
    const matchups = data.matchupSummaries(data.CURRENT_GW);
    const standings = data.standings();
    const records = teamRecords(standings);
    await render(<MatchupList leagueId={data.LEAGUE_ID} matchups={matchups} records={records} />);
    const hidden = { includeHiddenElements: true };
    const medals = [
      ...screen.queryAllByTestId('avatar-left-medal', hidden),
      ...screen.queryAllByTestId('avatar-right-medal', hidden),
    ];
    // Every team plays this gameweek, so each of the top three shows once.
    expect(medals.map((m) => within(m).getByText(/^[123]$/, hidden).props.children).sort()).toEqual(
      ['1', '2', '3'],
    );
    const leader = standings.find((r) => r.rank === 1)!;
    expect(
      screen.getAllByText(
        new RegExp(recordLine(records.get(leader.team.id)!).replace(/[()]/g, '\\$&')),
      ).length,
    ).toBe(1);
  });
});

describe('PointsSplit', () => {
  it('splits the points scored, the side ahead green and the side behind red', async () => {
    await render(<PointsSplit left={54} right={46} />);
    expect(screen.getByTestId('points-split-left')).toHaveTextContent('54%');
    expect(screen.getByTestId('points-split-right')).toHaveTextContent('46%');
    const l = screen.getByTestId('points-split-bar-left', { includeHiddenElements: true });
    const r = screen.getByTestId('points-split-bar-right', { includeHiddenElements: true });
    expect(l).toHaveStyle({ flex: 54 });
    expect(r).toHaveStyle({ flex: 46 });
    const lc = (l.props.style as { backgroundColor: string }).backgroundColor;
    const rc = (r.props.style as { backgroundColor: string }).backgroundColor;
    expect(lc).not.toBe(rc);
  });

  it('is grey with dashes before any points, and level shares match colours', async () => {
    const { unmount } = await render(<PointsSplit left={null} right={null} />);
    expect(screen.getByTestId('points-split-left')).toHaveTextContent('–');
    expect(
      screen.queryByTestId('points-split-bar-left', { includeHiddenElements: true }),
    ).toBeNull();
    await unmount();
    await render(<PointsSplit left={30} right={30} />);
    const lc = screen.getByTestId('points-split-bar-left', { includeHiddenElements: true }).props
      .style.backgroundColor;
    const rc = screen.getByTestId('points-split-bar-right', { includeHiddenElements: true }).props
      .style.backgroundColor;
    expect(lc).toBe(rc);
  });

  it('can be hidden (a future gameweek preview)', async () => {
    const detail = detailWhere((m) => m.home.kind === 'team' && m.away.kind === 'team');
    const { left, right } = orient(detail, undefined);
    await render(
      <ScoreHeader
        leagueId={data.LEAGUE_ID}
        left={left}
        right={right}
        state={detail.state}
        gameweek={detail.gameweek}
        shareBar={false}
      />,
    );
    expect(screen.queryByTestId('points-split')).toBeNull();
  });
});

describe('team names', () => {
  it('both read left-aligned and wrap rather than cut short', async () => {
    const detail = detailWhere((m) => m.home.kind === 'team' && m.away.kind === 'team');
    const { left, right } = orient(detail, undefined);
    const long = 'The Extraordinarily Long Named Football Club Of Champions';
    const rename = <S,>(s: S): S => ({
      ...s,
      team: { ...(s as { team: Schemas['TeamRef'] }).team, name: long },
    });
    await render(
      <ScoreHeader
        leagueId={data.LEAGUE_ID}
        left={rename(left)}
        right={rename(right)}
        state={detail.state}
        gameweek={detail.gameweek}
      />,
    );
    for (const id of ['score-left-name', 'score-right-name']) {
      const name = screen.getByTestId(id);
      expect(name).toHaveTextContent(long);
      expect(name).toHaveStyle({ textAlign: 'left' });
      expect(name.props.numberOfLines).toBeUndefined();
    }
  });
});
