/**
 * The player card's one close per breakpoint: on phones the round button
 * over the bottom-right corner, wide a ✕ in the card (above it while the
 * card has no hero, as here while it loads).
 */
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { Dimensions } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { PlayerSheet } from './PlayerCard';

jest.mock('@/api/hooks', () => ({
  ...jest.requireActual('@/api/hooks'),
  // Still loading: no hero yet.
  useLeaguePlayer: () => ({ data: undefined, isError: false, isLoadingError: false }),
  useLeagueContext: () => ({ data: undefined }),
}));

const open = (width: number, height: number, onClose = jest.fn()) => {
  jest.spyOn(Dimensions, 'get').mockReturnValue({ width, height, scale: 2, fontScale: 1 });
  return render(
    <SafeAreaProvider
      initialMetrics={{
        frame: { x: 0, y: 0, width, height },
        insets: { top: 47, left: 0, right: 0, bottom: 34 },
      }}
    >
      <PlayerSheet leagueId="lg_1" playerId="plr_1" onClose={onClose} />
    </SafeAreaProvider>,
  );
};
afterEach(() => jest.restoreAllMocks());

/** The closes inside the card (the backdrop is outside it). */
const closes = () =>
  screen.getAllByLabelText('Close').filter((n) => n.props.testID === 'sheet-close');

describe('PlayerSheet close', () => {
  it('has one close on a phone: the round floating button', async () => {
    const onClose = jest.fn();
    await open(390, 844, onClose);
    expect(closes()).toHaveLength(1);
    await act(async () => fireEvent.press(closes()[0]!));
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('has one close from 768px: in the card, no floating button', async () => {
    const onClose = jest.fn();
    await open(1280, 900, onClose);
    expect(closes()).toHaveLength(1);
    await act(async () => fireEvent.press(closes()[0]!));
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
