import { fireEvent, render, screen, within } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { positionLabel } from '../Badges';
import { InjuriesCard, PlayersOfTheWeek, TeamOfTheWeek } from './StatusSections';
import { LEAGUE_ID, leagues } from '@/mocks/data';
import type { InSeasonLeague } from '@/mocks/league';
import { gameweekAwards, leagueStatus, playersOfTheWeek } from '@/mocks/status';

const awards = gameweekAwards(6)!;

describe('TeamOfTheWeek', () => {
  it('fields the XI on the formation pitch, each player over a points box', async () => {
    await render(
      <TeamOfTheWeek
        awards={awards}
        gameweek={6}
        maxGameweek={8}
        onChange={jest.fn()}
        onOpen={jest.fn()}
      />,
    );
    const pitch = screen.getByTestId('totw-pitch');
    expect(screen.getByTestId('pitch-background', { includeHiddenElements: true })).toBeTruthy();
    // Keeper at the bottom, attack at the top, in reading order.
    const lines = within(pitch)
      .getAllByTestId(/^pitch-line-/)
      .map((l) => l.props.testID as string);
    expect(lines[0]).toBe('pitch-line-FWD');
    expect(lines.at(-1)).toBe('pitch-line-GKP');
    const players = within(pitch).getAllByTestId(/^totw-player-/);
    expect(players).toHaveLength(11);
    for (const { player, stats } of awards.teamOfTheWeek) {
      const token = screen.getByTestId(`totw-player-${player.id}`);
      expect(within(token).getByTestId('pitch-name')).toHaveTextContent(/\S/);
      const box = within(token).getByTestId('pitch-points');
      expect(box).toHaveTextContent(String(stats.points));
      expect(StyleSheet.flatten(box.props.style).borderWidth).toBeGreaterThan(0);
    }
    // No formation text and no ⓘ: the whole player is the button.
    expect(screen.queryByText(/\b[345]-[2-5]-[1-3]\b/)).toBeNull();
    expect(screen.queryAllByTestId(/^player-info-/)).toHaveLength(0);
  });

  it('opens the player card for the tapped player', async () => {
    const onOpen = jest.fn();
    await render(
      <TeamOfTheWeek
        awards={awards}
        gameweek={6}
        maxGameweek={8}
        onChange={jest.fn()}
        onOpen={onOpen}
      />,
    );
    const { player, stats } = awards.teamOfTheWeek[0]!;
    const button = screen.getByTestId(`totw-player-${player.id}`);
    expect(button.props.accessibilityLabel).toMatch(
      new RegExp(`^Player card for .+, ${stats.points} points, `),
    );
    expect(button.props.accessibilityState?.selected).toBeUndefined();
    await fireEvent.press(button);
    expect(onOpen).toHaveBeenCalledWith(player.id);
  });
});

describe('PlayersOfTheWeek (native)', () => {
  const weeks = playersOfTheWeek();
  const latest = weeks.at(-1)!.gameweek;
  const layout = async (width: number) => {
    const wrapper = screen.getByTestId('potw-frame');
    await fireEvent(wrapper, 'layout', {
      nativeEvent: { layout: { width, height: 200, x: 0, y: 0 } },
    });
  };
  const disabled = (id: string) => screen.getByTestId(id).props.accessibilityState?.disabled;

  it('snaps one compact card at a time, five to a 390px phone row', async () => {
    await render(<PlayersOfTheWeek weeks={weeks} onOpen={jest.fn()} />);
    // 390 - 2 * 16 screen gutter - 2 * 16 card padding.
    await layout(326);
    const row = screen.getByTestId('potw-row');
    const card = StyleSheet.flatten(screen.getByTestId(`potw-card-${latest}`).props.style);
    // The row bleeds 8px into the card's padding; five cards and four 4px gaps fill it.
    expect(card.width).toBeCloseTo((326 + 16 - 16) / 5);
    expect(row.props.snapToInterval).toBeCloseTo(card.width + 4);
    expect(row.props.disableIntervalMomentum).toBe(true);
    expect(
      within(screen.getByTestId(`potw-card-${latest}`)).getByTestId('potw-name'),
    ).toHaveTextContent(/^[^.]+$/);
    expect(disabled('potw-next')).toBe(true);
    expect(disabled('potw-prev')).toBe(false);

    // A swipe that settles one card back frees the next arrow.
    const step = row.props.snapToInterval as number;
    await fireEvent.scroll(row, {
      nativeEvent: { contentOffset: { x: (weeks.length - 6) * step, y: 0 } },
    });
    expect(disabled('potw-next')).toBe(false);
    await fireEvent.scroll(row, { nativeEvent: { contentOffset: { x: 0, y: 0 } } });
    expect(disabled('potw-prev')).toBe(true);
  });

  it('ends a short season on the right, with both arrows off', async () => {
    await render(<PlayersOfTheWeek weeks={weeks.slice(0, 3)} onOpen={jest.fn()} />);
    await layout(326);
    expect(screen.getAllByTestId(/^potw-card-/)).toHaveLength(3);
    expect(
      StyleSheet.flatten(screen.getByTestId('potw-row').props.contentContainerStyle).justifyContent,
    ).toBe('flex-end');
    expect(disabled('potw-prev')).toBe(true);
    expect(disabled('potw-next')).toBe(true);
  });
});

describe('InjuriesCard', () => {
  const { injuries } = leagueStatus(leagues.get(LEAGUE_ID) as InSeasonLeague);

  const slotHas = (id: string) =>
    within(screen.getByTestId(`injury-owner-slot-${id}`)).queryByTestId(`injury-owner-${id}`) !==
    null;

  it('lists the players as given, an owner chip only for owned ones', async () => {
    await render(<InjuriesCard players={injuries} onOpen={jest.fn()} />);
    expect(screen.getByRole('header', { name: 'Latest Injuries and Suspensions' })).toBeTruthy();
    const rows = screen.getAllByTestId(/^injury-plr_/);
    expect(rows.map((r) => r.props.testID)).toEqual(injuries.map((i) => `injury-${i.player.id}`));
    for (const i of injuries) expect(slotHas(i.player.id)).toBe(i.owner !== null);
  });

  it('stacks a narrow card: position and club under the name, "Free agent" for no owner', async () => {
    await render(<InjuriesCard players={injuries} onOpen={jest.fn()} />);
    await fireEvent(screen.getByTestId('status-injuries'), 'layout', {
      nativeEvent: { layout: { width: 360, height: 400 } },
    });
    expect(screen.queryByTestId('injuries-header', { includeHiddenElements: true })).toBeNull();
    const first = injuries[0]!.player;
    expect(screen.getByTestId(`injury-detail-${first.id}`)).toHaveTextContent(
      `${positionLabel(first.position)} · ${first.club.code}`,
    );
    const free = injuries.filter((i) => i.owner === null);
    expect(free.length).toBeGreaterThan(0);
    expect(screen.getAllByText('Free agent')).toHaveLength(free.length);
  });

  it('puts a wide card in columns under a header, a dash for no owner', async () => {
    await render(<InjuriesCard players={injuries} onOpen={jest.fn()} />);
    await fireEvent(screen.getByTestId('status-injuries'), 'layout', {
      nativeEvent: { layout: { width: 700, height: 400 } },
    });
    const header = screen.getByTestId('injuries-header', { includeHiddenElements: true });
    for (const label of ['Player', 'Pos', 'Club', 'Mgr']) {
      expect(within(header).getByText(label, { includeHiddenElements: true })).toBeTruthy();
    }
    expect(screen.queryByTestId(/^injury-detail-/)).toBeNull();
    expect(screen.queryByText('Free agent')).toBeNull();
    expect(screen.getAllByText('–')).toHaveLength(injuries.filter((i) => i.owner === null).length);
  });

  it('opens the player card from the badge', async () => {
    const onOpen = jest.fn();
    await render(<InjuriesCard players={injuries} onOpen={onOpen} />);
    await fireEvent.press(screen.getByTestId(`injury-badge-${injuries[0]!.player.id}`));
    expect(onOpen).toHaveBeenCalledWith(injuries[0]!.player.id);
  });

  it('says so, briefly, when nothing qualifies', async () => {
    await render(<InjuriesCard players={[]} onOpen={jest.fn()} />);
    expect(screen.getByText('Nothing new')).toBeTruthy();
    expect(screen.getByText('No injuries or suspensions since the last gameweek.')).toBeTruthy();
    expect(screen.queryByTestId('injuries-header', { includeHiddenElements: true })).toBeNull();
  });
});
