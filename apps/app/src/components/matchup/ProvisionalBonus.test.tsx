import type { Schemas } from '@myfantasyprem/api-client';
import { render, screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { BreakdownPoints } from './H2HLineups';
import { PointsBreakdownSheet } from './PointsBreakdownSheet';
import * as data from '@/mocks/data';

jest.mock('expo-router', () => ({
  ...jest.requireActual('expo-router'),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), navigate: jest.fn() }),
}));

/** A slot from the live gameweek whose bonus is still projected from BPS. */
function provisionalSlot(): Schemas['LineupSlot'] {
  for (const s of data.matchupSummaries(data.CURRENT_GW)) {
    const detail = data.matchupDetail(data.CURRENT_GW, s.id)!;
    for (const side of [detail.home, detail.away]) {
      if (!('starters' in side)) continue;
      const slot = side.starters.find((x) =>
        x.fixtures.some((f) => f.bonusProvisional && f.breakdown.some((l) => l.stat === 'bonus')),
      );
      if (slot) return slot;
    }
  }
  throw new Error('no provisional bonus in the mocks');
}

// Owner decision 2026-10-25: provisional bonus is shown like any other line.
describe('provisional bonus is not surfaced', () => {
  it('the breakdown shows bonus as a plain line, with no footnote', async () => {
    const slot = provisionalSlot();
    await render(
      <SafeAreaProvider
        initialMetrics={{
          frame: { x: 0, y: 0, width: 390, height: 844 },
          insets: { top: 0, left: 0, right: 0, bottom: 0 },
        }}
      >
        <PointsBreakdownSheet slot={slot} onClose={() => {}} />
      </SafeAreaProvider>,
    );
    expect(await screen.findByTestId('breakdown-total')).toHaveTextContent(String(slot.points));
    expect(screen.getByText('Bonus')).toBeOnTheScreen();
    expect(screen.queryAllByText(/provisional/i, { includeHiddenElements: true })).toHaveLength(0);
    expect(screen.queryAllByLabelText(/provisional/i)).toHaveLength(0);
  });

  it("the player's points box has no dashed outline or provisional label", async () => {
    const slot = provisionalSlot();
    await render(<BreakdownPoints slot={slot} onPress={() => {}} />);
    const button = screen.getByTestId(`h2h-breakdown-${slot.player.id}`);
    expect(button.props.accessibilityLabel).toBe(
      `Points breakdown for ${slot.player.displayName}: ${slot.points} points`,
    );
    const style = StyleSheet.flatten(button.props.style);
    expect(style.borderStyle).not.toBe('dashed');
    expect(style.borderWidth ?? 0).toBe(0);
  });
});
