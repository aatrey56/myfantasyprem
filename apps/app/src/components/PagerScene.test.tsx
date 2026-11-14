import { render, screen } from '@testing-library/react-native';
import { Text } from 'react-native';

import { PagerScene } from './PagerScene';

describe('PagerScene', () => {
  it('shows the visible page to screen readers and taps', async () => {
    await render(
      <PagerScene shown>
        <Text>Team</Text>
      </PagerScene>,
    );
    const scene = screen.getByTestId('pager-scene');
    expect(scene.props['aria-hidden']).toBe(false);
    expect(scene.props.importantForAccessibility).toBe('auto');
    expect(scene.props.accessibilityElementsHidden).toBe(false);
    expect(scene).toHaveStyle({ pointerEvents: 'auto' });
    expect(screen.getByText('Team')).toBeOnTheScreen();
  });

  it('hides an off-screen neighbour page from screen readers and taps', async () => {
    await render(
      <PagerScene shown={false}>
        <Text>Players</Text>
      </PagerScene>,
    );
    const scene = screen.getByTestId('pager-scene', { includeHiddenElements: true });
    expect(scene.props['aria-hidden']).toBe(true);
    expect(scene.props.importantForAccessibility).toBe('no-hide-descendants');
    expect(scene.props.accessibilityElementsHidden).toBe(true);
    expect(scene).toHaveStyle({ pointerEvents: 'none' });
    // Still rendered (a swipe shows it at once), just not reachable.
    expect(screen.queryByText('Players')).toBeNull();
    expect(screen.getByText('Players', { includeHiddenElements: true })).toBeTruthy();
  });
});
