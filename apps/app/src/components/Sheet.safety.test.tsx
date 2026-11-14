import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { AccessibilityInfo, Platform, StyleSheet, View } from 'react-native';
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

/**
 * Web: react-native-web's onLayout can land after the safety net's 500 ms
 * on a busy main thread, or not at all. The panel element itself decides.
 */
describe('Sheet on web, when the panel layout report is late or lost', () => {
  const document = (globalThis as { document?: unknown }).document;
  beforeEach(() => {
    jest.useFakeTimers();
    PANEL_LAYOUT_TIMEOUT.ms = 500;
    jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false);
    jest.replaceProperty(Platform, 'OS', 'web');
  });
  afterEach(() => {
    PANEL_LAYOUT_TIMEOUT.ms = Infinity;
    jest.useRealTimers();
    jest.restoreAllMocks();
  });
  // The dialog's focus handling reads the document (no DOM in this suite);
  // kept until the last sheet unmounts.
  beforeAll(() => {
    (globalThis as { document?: unknown }).document = {
      activeElement: null,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
    };
  });
  afterAll(() => {
    (globalThis as { document?: unknown }).document = document;
  });

  /**
   * The panel as the page has it: on the page or not, and how tall. Jest's
   * View is a component whose instance is what a ref holds: every View gets
   * these for the test, and only the sheet's panel and focus handling read
   * them (focus is already inside the dialog: nothing to move).
   */
  const proto = View.prototype as unknown as Record<string, unknown>;
  const panelElement = (el: { isConnected?: unknown; offsetHeight?: unknown }) => {
    Object.assign(proto, { ...el, contains: () => true });
  };
  afterEach(() => {
    delete proto.isConnected;
    delete proto.offsetHeight;
    delete proto.contains;
  });
  /** How far the panel is pushed down from fully open (px). */
  const translateY = () => {
    const style = StyleSheet.flatten(screen.getByTestId('sheet').props.style);
    return (style.transform as { translateY: number }[] | undefined)?.[0]?.translateY;
  };

  it('a panel on the page with a height and no onLayout yet is placed and stays open', async () => {
    const timing = jest.spyOn(Reanimated, 'withTiming');
    const onClose = jest.fn();
    await render(<Owned visible onClose={onClose} />);
    panelElement({ isConnected: true, offsetHeight: 400 });
    await wait(500);
    // Slides up to rest fully open.
    expect(timing).toHaveBeenCalledWith(0, expect.anything());
    await wait(5_000);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeOnTheScreen();
  });

  it('a bottom sheet whose layout report is lost is not left below the screen', async () => {
    const timing = jest.spyOn(Reanimated, 'withTiming');
    const onClose = jest.fn();
    await render(<Owned visible onClose={onClose} />);
    panelElement({ isConnected: true, offsetHeight: 400 });
    // Below the screen until it is measured.
    expect(translateY()).toBeGreaterThan(0);
    await wait(500);
    expect(timing).toHaveBeenCalledWith(0, expect.anything());
    // The report, had it only been late, finds it placed: no second slide.
    timing.mockClear();
    await layout(400);
    expect(timing).not.toHaveBeenCalled();
    // The off-screen watchdog, now that it knows the panel, finds it on screen.
    await wait(1_000);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('a measured panel that still never reaches the screen is closed', async () => {
    // The slide never moves it: it stays where it started, below the screen.
    jest.spyOn(Reanimated, 'withTiming').mockImplementation(() => 10_000 as never);
    const onClose = jest.fn();
    await render(<Owned visible onClose={onClose} />);
    panelElement({ isConnected: true, offsetHeight: 400 });
    await wait(500);
    expect(onClose).not.toHaveBeenCalled();
    await wait(500);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});
