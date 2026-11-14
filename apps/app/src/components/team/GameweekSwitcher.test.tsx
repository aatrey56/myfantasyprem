/**
 * The empty slot holds the stepper's place while the gameweek loads: same
 * label text style (so it scales with the user's text size), same arrow
 * widths, invisible, and out of the accessibility tree.
 */
import { render, screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { GameweekSwitcher, GameweekSwitcherSlot } from './GameweekSwitcher';

describe('GameweekSwitcherSlot', () => {
  it('is hidden from screen readers and taps, with no buttons', async () => {
    await render(<GameweekSwitcherSlot gameweek={38} />);
    const slot = screen.getByTestId('gw-switcher-slot', { includeHiddenElements: true });
    expect(slot.props.accessibilityElementsHidden).toBe(true);
    expect(slot.props.importantForAccessibility).toBe('no-hide-descendants');
    expect(slot.props.pointerEvents).toBe('none');
    expect(StyleSheet.flatten(slot.props.style).opacity).toBe(0);
    expect(screen.queryAllByRole('button', { includeHiddenElements: true })).toHaveLength(0);
  });

  it('lays its label out exactly like the stepper', async () => {
    await render(<GameweekSwitcher gameweek={38} min={1} max={38} onChange={() => {}} />);
    const real = StyleSheet.flatten(screen.getByTestId('gw-label').props.style);
    await render(<GameweekSwitcherSlot gameweek={38} />);
    const label = screen.getByText('GW 38', { includeHiddenElements: true });
    const slot = StyleSheet.flatten(label.props.style);
    for (const key of ['fontFamily', 'fontSize', 'lineHeight', 'minWidth', 'textAlign'] as const)
      expect(slot[key]).toEqual(real[key]);
  });
});
