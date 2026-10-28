import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { AccessibilityInfo } from 'react-native';
import * as Reanimated from 'react-native-reanimated';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { Sheet } from './Sheet';
import { PANEL_LAYOUT_TIMEOUT } from './sheet-snap';

const PHONE = {
  frame: { x: 0, y: 0, width: 375, height: 812 },
  insets: { top: 47, left: 0, right: 0, bottom: 34 },
};

/** The panel lays out `height` tall (Jest has no layout pass). */
const layout = (height: number) =>
  act(async () => {
    fireEvent(screen.getByTestId('sheet'), 'layout', {
      nativeEvent: { layout: { x: 0, y: 0, width: 375, height } },
    });
  });

const wait = (ms: number) =>
  act(async () => {
    jest.advanceTimersByTime(ms);
  });

/**
 * A sheet that the owner closes on `onClose`, as every caller does: what is
 * left on screen once the safety has had its say.
 */
function Owned({
  visible,
  onClose,
  children,
  empty,
}: {
  visible: boolean;
  onClose: () => void;
  children?: React.ReactNode;
  /** No content at all. */
  empty?: boolean;
}) {
  return (
    <SafeAreaProvider initialMetrics={PHONE}>
      <Sheet visible={visible} onClose={onClose} title="Swap" testID="sheet">
        {empty ? null : (children ?? <AppText>Body</AppText>)}
      </Sheet>
    </SafeAreaProvider>
  );
}

describe('Sheet never leaves an invisible Modal over the app', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    PANEL_LAYOUT_TIMEOUT.ms = 500;
    jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false);
  });
  afterEach(() => {
    PANEL_LAYOUT_TIMEOUT.ms = Infinity;
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it('closes itself when the panel never lays out within 500 ms', async () => {
    const onClose = jest.fn();
    await render(<Owned visible onClose={onClose} />);
    await wait(499);
    expect(onClose).not.toHaveBeenCalled();
    await wait(1);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes itself when the panel measures 0 tall (zero-height content)', async () => {
    const onClose = jest.fn();
    await render(<Owned visible onClose={onClose} empty />);
    await layout(0);
    await wait(500);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('stays open once the panel is on screen', async () => {
    const onClose = jest.fn();
    await render(<Owned visible onClose={onClose} />);
    await layout(400);
    await wait(5_000);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeOnTheScreen();
  });

  it('open then open again (a second tap) still places the panel, with one timer', async () => {
    const onClose = jest.fn();
    const view = await render(<Owned visible onClose={onClose} />);
    await view.rerender(
      <Owned visible onClose={onClose}>
        <AppText>Other player</AppText>
      </Owned>,
    );
    await layout(400);
    await wait(1_000);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('opens again during the slide down of the last close, and lands on screen', async () => {
    const timing = jest.spyOn(Reanimated, 'withTiming');
    const onClose = jest.fn();
    const view = await render(<Owned visible onClose={onClose} />);
    await layout(400);
    // ✕: the slide down starts; the owner hides it and a new tap shows it again.
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    await view.rerender(<Owned visible={false} onClose={onClose} />);
    expect(screen.queryByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeNull();
    await view.rerender(<Owned visible onClose={onClose} />);
    timing.mockClear();
    await layout(400);
    // Slides up to rest fully open, not ignored as part of the last close.
    expect(timing).toHaveBeenCalledWith(0, expect.anything());
    const calls = onClose.mock.calls.length;
    await wait(1_000);
    expect(onClose).toHaveBeenCalledTimes(calls);
  });

  it('with the owner closing on onClose, no backdrop remains after a lost layout', async () => {
    function Host() {
      const [open, setOpen] = React.useState(true);
      return <Owned visible={open} onClose={() => setOpen(false)} />;
    }
    await render(<Host />);
    expect(screen.getByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeOnTheScreen();
    await wait(500);
    expect(screen.queryByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeNull();
  });
});
