import { fireEvent, render, screen, within } from '@testing-library/react-native';
import { Dimensions, StyleSheet } from 'react-native';

import { FormationPitch, type PitchLine, type PitchPlayer } from './FormationPitch';
import { lineTops, PITCH_HEIGHT, tokenHeight } from './pitchGeometry';

const player = (id: string, extra: Partial<PitchPlayer> = {}): PitchPlayer => ({
  id,
  name: `Player ${id}`,
  kit: '#C81E3A',
  detail: `v ABC`,
  ...extra,
});

/** A 4-4-2, keeper's line first. */
const xi = (extra: (id: string) => Partial<PitchPlayer> = () => ({})): PitchLine[] =>
  [
    ['GKP', ['g1']],
    ['DEF', ['d1', 'd2', 'd3', 'd4']],
    ['MID', ['m1', 'm2', 'm3', 'm4']],
    ['FWD', ['f1', 'f2']],
  ].map(([key, ids]) => ({
    key: key as string,
    players: (ids as string[]).map((id) => player(id, extra(id))),
  }));

const top = (testID: string) =>
  StyleSheet.flatten(screen.getByTestId(testID).props.style).top as number;

describe('FormationPitch', () => {
  it('draws the keeper lowest and the attack highest, one line per row', async () => {
    await render(<FormationPitch lines={xi()} />);
    const lines = within(screen.getByTestId('pitch-xi')).getAllByTestId(/^pitch-line-/);
    // Top to bottom in reading order: the attack first, the keeper last.
    expect(lines.map((l) => l.props.testID)).toEqual([
      'pitch-line-FWD',
      'pitch-line-MID',
      'pitch-line-DEF',
      'pitch-line-GKP',
    ]);
    // At the window's font scale (the jest mock's is 2, capped to the pitch's maximum).
    const scale = Dimensions.get('window').fontScale;
    const ys = lineTops(PITCH_HEIGHT, 4, tokenHeight('lineup', false, scale));
    expect(['GKP', 'DEF', 'MID', 'FWD'].map((k) => top(`pitch-line-${k}`))).toEqual(ys);
    expect(
      within(screen.getByTestId('pitch-line-DEF')).getAllByTestId(/^pitch-player-/),
    ).toHaveLength(4);
    expect(screen.getByTestId('pitch-background', { includeHiddenElements: true })).toBeTruthy();
  });

  it('a short pitch turns compact: name only, still a 44px tap target', async () => {
    await render(<FormationPitch lines={xi()} height={200} onPress={() => {}} />);
    expect(screen.queryAllByTestId('pitch-detail')).toHaveLength(0);
    expect(screen.getAllByTestId('pitch-name')).toHaveLength(11);
    expect(tokenHeight('lineup', true)).toBeGreaterThanOrEqual(44);
  });

  it('lineup variant: name and fixture in the chip, no points box', async () => {
    await render(<FormationPitch lines={xi((id) => ({ points: id.length }))} />);
    const keeper = screen.getByTestId('pitch-player-g1');
    expect(within(keeper).getByTestId('pitch-name')).toHaveTextContent('Player g1');
    expect(within(keeper).getByTestId('pitch-detail')).toHaveTextContent('v ABC');
    expect(screen.queryAllByTestId('pitch-points')).toHaveLength(0);
  });

  it('points variant: name chip over a bordered points box, no fixture', async () => {
    await render(
      <FormationPitch variant="points" lines={xi((id) => ({ points: id === 'f1' ? 14 : 2 }))} />,
    );
    const striker = screen.getByTestId('pitch-player-f1');
    expect(within(striker).getByTestId('pitch-name')).toHaveTextContent('Player f1');
    expect(within(striker).queryByTestId('pitch-detail')).toBeNull();
    const box = within(striker).getByTestId('pitch-points');
    expect(box).toHaveTextContent('14');
    const style = StyleSheet.flatten(box.props.style);
    expect(style.borderColor).toBe('#F2F4F7');
    expect(style.borderWidth).toBeGreaterThan(0);
    expect(style.borderStyle).toBe('solid');
    // Every box is solid: provisional bonus is not marked (owner decision 2026-10-25).
    for (const b of screen.getAllByTestId('pitch-points')) {
      expect(StyleSheet.flatten(b.props.style).borderStyle).toBe('solid');
    }
    expect(screen.getAllByTestId('pitch-points')).toHaveLength(11);
    // Read-only: described for screen readers with the points, no buttons.
    expect(striker.props.accessibilityLabel).toBe('Player f1, 14 points');
    expect(screen.queryAllByRole('button')).toHaveLength(0);
  });

  it('separates the swap tap from the ⓘ player card button', async () => {
    const onPress = jest.fn();
    const onInfo = jest.fn();
    await render(<FormationPitch lines={xi()} onPress={onPress} onInfo={onInfo} selectedId="d2" />);
    await fireEvent.press(screen.getByRole('button', { name: 'Swap Player m3' }));
    expect(onPress).toHaveBeenCalledWith('m3');
    expect(onInfo).not.toHaveBeenCalled();
    await fireEvent.press(screen.getByRole('button', { name: 'Player card for Player m3' }));
    expect(onInfo).toHaveBeenCalledWith('m3');
    expect(onPress).toHaveBeenCalledTimes(1);
    // 11 swap buttons and 11 info buttons.
    expect(screen.getAllByRole('button')).toHaveLength(22);
    expect(screen.getByRole('button', { name: 'Swap Player d2' })).toBeSelected();
    expect(screen.getByRole('button', { name: 'Swap Player d1' })).not.toBeSelected();
  });

  it("pressAction 'open': each player is a plain button to their card, not a toggle", async () => {
    const onPress = jest.fn();
    await render(
      <FormationPitch
        variant="points"
        lines={xi(() => ({ points: 6 }))}
        onPress={onPress}
        pressAction="open"
      />,
    );
    const button = screen.getByRole('button', { name: 'Player card for Player m3, 6 points' });
    expect(button.props.accessibilityState?.selected).toBeUndefined();
    await fireEvent.press(button);
    expect(onPress).toHaveBeenCalledWith('m3');
    expect(screen.getAllByRole('button')).toHaveLength(11);
    expect(screen.queryByRole('button', { name: /^Swap / })).toBeNull();
  });

  it('shows no ⓘ without onInfo and no buttons without onPress', async () => {
    await render(<FormationPitch lines={xi()} onInfo={jest.fn()} />);
    expect(screen.getAllByRole('button')).toHaveLength(11);
    expect(screen.queryByRole('button', { name: /^Swap / })).toBeNull();
  });
});
