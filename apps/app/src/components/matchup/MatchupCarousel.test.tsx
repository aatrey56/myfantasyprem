import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { Platform } from 'react-native';

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

  it('a page set from outside is not sent back by its own scroll animation (web)', async () => {
    const os = Platform.OS;
    Platform.OS = 'web' as typeof Platform.OS;
    jest.useFakeTimers();
    try {
      const onPageChange = jest.fn();
      const props = {
        leagueId: data.LEAGUE_ID,
        order,
        onPageChange,
        myTeamId,
        seasonLines: new Map(),
      };
      const view = await render(<MatchupCarousel {...props} page={0} />);
      await fireEvent(screen.getByLabelText(/^Matchups, /), 'layout', {
        nativeEvent: { layout: { width: 400, height: 200, x: 0, y: 0 } },
      });
      const scroll = async (x: number) => {
        await fireEvent.scroll(screen.getByTestId('matchup-carousel-scroller'), {
          nativeEvent: { contentOffset: { x, y: 0 } },
        });
        await act(() => jest.advanceTimersByTime(200));
      };
      // A list card picks page 2: the scroller animates 0 -> 800 and the main
      // thread stalls at 350, nearer page 1. That is not a swipe to page 1.
      await view.rerender(<MatchupCarousel {...props} page={2} />);
      await scroll(350);
      await scroll(800);
      expect(onPageChange).not.toHaveBeenCalled();
      // Once there, a swipe lands as before.
      await scroll(400);
      expect(onPageChange).toHaveBeenCalledWith(1);
    } finally {
      jest.useRealTimers();
      Platform.OS = os;
    }
  });

  describe('a page set from outside', () => {
    const os = Platform.OS;
    afterEach(() => {
      jest.useRealTimers();
      Platform.OS = os;
    });
    const setup = async (platform: typeof Platform.OS = 'web') => {
      Platform.OS = platform;
      jest.useFakeTimers();
      const onPageChange = jest.fn();
      const props = {
        leagueId: data.LEAGUE_ID,
        order,
        onPageChange,
        myTeamId,
        seasonLines: new Map(),
      };
      const view = await render(<MatchupCarousel {...props} page={0} />);
      const layout = (width: number) =>
        fireEvent(screen.getByLabelText(/^Matchups, /), 'layout', {
          nativeEvent: { layout: { width, height: 200, x: 0, y: 0 } },
        });
      await layout(400);
      const scroller = () => screen.getByTestId('matchup-carousel-scroller');
      const offset = (x: number) => ({ nativeEvent: { contentOffset: { x, y: 0 } } });
      const scroll = async (x: number) => {
        await fireEvent.scroll(scroller(), offset(x));
        await act(() => jest.advanceTimersByTime(200));
      };
      const goTo = (page: number) => view.rerender(<MatchupCarousel {...props} page={page} />);
      return { onPageChange, layout, scroller, offset, scroll, goTo };
    };

    it.each([
      ['a mouse down (drag, scrollbar)', 'mouseDown'],
      ['a key on the scroller', 'keyDown'],
      ['a touch', 'touchStart'],
      ['a wheel', 'wheel'],
    ])('%s mid-animation hands the scroll back to the user (web)', async (_, event) => {
      const { onPageChange, scroller, scroll, goTo } = await setup();
      await goTo(2);
      await scroll(350);
      await fireEvent(scroller(), event, { key: 'PageUp', preventDefault: () => {} });
      await scroll(400);
      expect(onPageChange).toHaveBeenCalledWith(1);
    });

    it('an unreachable target (clamped short) still settles', async () => {
      const { onPageChange, scroll, goTo } = await setup();
      await goTo(2);
      await scroll(798.5);
      await scroll(400);
      expect(onPageChange).toHaveBeenCalledWith(1);
    });

    it('a resize mid-animation does not latch', async () => {
      const { onPageChange, layout, scroll, goTo } = await setup();
      await goTo(2);
      await scroll(350);
      await layout(500);
      await scroll(500);
      expect(onPageChange).toHaveBeenCalledWith(1);
    });

    it('a target the scroller already shows does not latch', async () => {
      const { onPageChange, scroll, goTo } = await setup();
      await goTo(2);
      await goTo(0);
      await scroll(400);
      expect(onPageChange).toHaveBeenCalledWith(1);
    });

    it('lets go a second after the animation starts, landing where it stopped', async () => {
      const { onPageChange, scroll, goTo } = await setup();
      await goTo(2);
      await scroll(350);
      expect(onPageChange).not.toHaveBeenCalled();
      await act(() => jest.advanceTimersByTime(1000));
      expect(onPageChange).toHaveBeenCalledWith(1);
    });

    it('a drag mid-animation hands the scroll back to the user (native)', async () => {
      const { onPageChange, scroller, offset, goTo } = await setup('ios');
      await goTo(2);
      await fireEvent(scroller(), 'scrollBeginDrag', offset(350));
      await fireEvent(scroller(), 'momentumScrollEnd', offset(400));
      expect(onPageChange).toHaveBeenCalledWith(1);
    });
  });
});
