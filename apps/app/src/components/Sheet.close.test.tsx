import React from 'react';
import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { AccessibilityInfo } from 'react-native';
import * as Reanimated from 'react-native-reanimated';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { CLOSE_MS, OFFSCREEN_MS, Sheet, type SnapPoint } from './Sheet';

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

const backdrop = () => screen.queryByTestId('sheet-backdrop', { includeHiddenElements: true });

type Timing = typeof Reanimated.withTiming;
/** withTiming that lands on `to` but never reports its end (a lost callback). */
const silent = ((to: number) => to) as unknown as Timing;
/** withTiming whose slide is cut short: it stays where it was, reports false. */
const cancelled = ((_to: number, _c: unknown, cb?: (done: boolean) => void) => {
  cb?.(false);
  return 0;
}) as unknown as Timing;

/** The owner hides the sheet on onClose, as every caller does. */
function Host({
  onClose,
  label = 'Body',
  snapPoints,
}: {
  onClose: () => void;
  label?: string;
  snapPoints?: readonly SnapPoint[];
}) {
  const [open, setOpen] = React.useState(true);
  return (
    <SafeAreaProvider initialMetrics={PHONE}>
      <Sheet
        visible={open}
        // A new function each render, as inline handlers are.
        onClose={() => {
          onClose();
          setOpen(false);
        }}
        title="Player"
        testID="sheet"
        snapPoints={snapPoints}
      >
        <AppText>{label}</AppText>
      </Sheet>
    </SafeAreaProvider>
  );
}

describe('Sheet closing is decided on the JS side', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false);
  });
  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  it('closes on its timer when the slide never reports its end', async () => {
    const onClose = jest.fn();
    await render(<Host onClose={onClose} />);
    await layout(400);
    // A slide that stays on screen and never calls back.
    jest.spyOn(Reanimated, 'withTiming').mockImplementation((() => 0) as unknown as Timing);
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    // Closing: nothing in it takes a touch.
    expect(
      screen.getByTestId('sheet-root', { includeHiddenElements: true }).props.pointerEvents,
    ).toBe('none');
    await wait(CLOSE_MS - 1);
    expect(onClose).not.toHaveBeenCalled();
    await wait(1);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(backdrop()).toBeNull();
    await wait(5_000);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes when the slide is cut short (finished=false)', async () => {
    const onClose = jest.fn();
    await render(<Host onClose={onClose} />);
    await layout(400);
    jest.spyOn(Reanimated, 'withTiming').mockImplementation(cancelled);
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    // Landed by the cut-short slide's report, well before the timer.
    await wait(50);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(backdrop()).toBeNull();
    await wait(5_000);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('a re-render mid-slide (new content, snap points, layout) neither reopens nor cancels it', async () => {
    const onClose = jest.fn();
    const view = await render(<Host onClose={onClose} />);
    await layout(400);
    const timing = jest.spyOn(Reanimated, 'withTiming').mockImplementation(silent);
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    timing.mockClear();
    // The mutation's re-render: other content, other snaps, a new layout.
    await view.rerender(<Host onClose={onClose} label="Tag removed" snapPoints={['50%']} />);
    await layout(380);
    await wait(100);
    // Nothing slid it back up.
    expect(timing).not.toHaveBeenCalled();
    await wait(CLOSE_MS);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(backdrop()).toBeNull();
  });

  it('a query update (state change) while closing still closes, once', async () => {
    const onClose = jest.fn();
    // A query cache the card reads from, updated from outside React.
    let n = 0;
    const subs = new Set<() => void>();
    const bump = () => {
      n += 1;
      subs.forEach((f) => f());
    };
    const subscribe = (f: () => void) => {
      subs.add(f);
      return () => subs.delete(f);
    };
    function Updating() {
      const tags = React.useSyncExternalStore(subscribe, () => n);
      return <Host onClose={onClose} label={`Tags ${tags}`} />;
    }
    await render(<Updating />);
    await layout(400);
    jest.spyOn(Reanimated, 'withTiming').mockImplementation(silent);
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    for (let i = 0; i < 5; i++) {
      await act(async () => bump());
      await wait(50);
    }
    await wait(CLOSE_MS);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(backdrop()).toBeNull();
  });

  it('closes a visible sheet whose panel sits off screen (the watchdog)', async () => {
    const onClose = jest.fn();
    // Every slide stays where the panel starts: below the screen.
    jest
      .spyOn(Reanimated, 'withTiming')
      .mockImplementation(((to: number) => (to === 0 ? 812 : to)) as unknown as Timing);
    await render(<Host onClose={onClose} />);
    await layout(400);
    await wait(OFFSCREEN_MS - 100);
    expect(onClose).not.toHaveBeenCalled();
    await wait(300);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(backdrop()).toBeNull();
  });

  it('leaves an open sheet on screen alone', async () => {
    const onClose = jest.fn();
    await render(<Host onClose={onClose} />);
    await layout(400);
    await wait(5_000);
    expect(onClose).not.toHaveBeenCalled();
    expect(backdrop()).toBeOnTheScreen();
  });
});
