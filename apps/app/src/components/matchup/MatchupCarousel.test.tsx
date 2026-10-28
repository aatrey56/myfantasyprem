import { fireEvent, render, screen } from '@testing-library/react-native';

import { MatchupCarousel } from './MatchupCarousel';
import { teamOf, teamRecords } from '@/lib/matchup';
import { carouselOrder, seasonLines } from '@/lib/matchup-carousel';
import * as data from '@/mocks/data';

jest.mock('expo-router', () => ({
  ...jest.requireActual('expo-router'),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), navigate: jest.fn() }),
}));

const summaries = data.matchupSummaries(data.CURRENT_GW);
const mine = summaries.find((m) => teamOf(m.home) && teamOf(m.away))!;
const myTeamId = teamOf(mine.away)!.id;
const order = carouselOrder(summaries, myTeamId);

const draw = (page: number, onPageChange = jest.fn()) =>
  render(
    <MatchupCarousel
      leagueId={data.LEAGUE_ID}
      order={order}
      page={page}
      onPageChange={onPageChange}
      myTeamId={myTeamId}
      records={teamRecords(data.standings())}
      seasonLines={seasonLines(data.standings(), [
        { team: teamOf(mine.home)!, seasonAccuracy: 0.874 },
        { team: teamOf(mine.away)!, seasonAccuracy: null },
      ] as never)}
    />,
  );

describe('MatchupCarousel', () => {
  it('draws a page and a dot a matchup, mine first and marked', async () => {
    await draw(0);
    expect(order[0].id).toBe(mine.id);
    expect(screen.getAllByTestId(/^carousel-page-\d+$/)).toHaveLength(summaries.length);
    expect(screen.getAllByTestId(/^carousel-dot-\d+$/)).toHaveLength(summaries.length);
    expect(screen.getByTestId('carousel-dot-0')).toHaveProp(
      'accessibilityLabel',
      expect.stringMatching(/your matchup$/),
    );
    expect(screen.getByTestId('carousel-dot-1')).toHaveProp(
      'accessibilityLabel',
      expect.not.stringMatching(/your matchup/),
    );
    // Only the page on screen carries the plain ids.
    expect(screen.getAllByTestId('score-header')).toHaveLength(1);
  });

  it('shows each side’s start/sit accuracy and average points, mine on the left', async () => {
    await draw(0);
    const [strip] = screen.getAllByTestId('season-start-sit');
    expect(strip).toHaveProp('accessibilityLabel', expect.stringContaining('–'));
    expect(strip).toHaveProp('accessibilityLabel', expect.stringContaining('87%'));
    const label = strip.props.accessibilityLabel as string;
    // Mine (no accuracy yet) is the left side.
    expect(label.indexOf('–')).toBeLessThan(label.indexOf('87%'));
  });

  it('adds each side’s average points against a gameweek', async () => {
    await draw(0);
    const avg = (id: string) => {
      const r = data.standings().find((s) => s.team.id === id)!;
      return r.played > 0 ? (Math.round((r.pointsAgainst / r.played) * 10) / 10).toFixed(1) : '–';
    };
    const [row] = screen.getAllByTestId('season-avg-against');
    expect(row).toHaveProp('accessibilityLabel', expect.stringMatching(/^Avg pts against: /));
    const label = row.props.accessibilityLabel as string;
    expect(label).toContain(avg(myTeamId));
    expect(label).toContain(avg(teamOf(mine.home)!.id));
    expect(screen.getAllByTestId('season-start-sit').length).toBeGreaterThan(0);
    expect(screen.getAllByTestId('season-avg').length).toBeGreaterThan(0);
  });

  it('a dot moves to its page', async () => {
    const onPageChange = jest.fn();
    await draw(0, onPageChange);
    await fireEvent.press(screen.getByTestId('carousel-dot-2'));
    expect(onPageChange).toHaveBeenCalledWith(2);
    onPageChange.mockClear();
    await fireEvent.press(screen.getByTestId('carousel-dot-0'));
    expect(onPageChange).not.toHaveBeenCalled();
  });
});
