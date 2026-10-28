import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { createRef } from 'react';
import {
  AccessibilityInfo,
  Dimensions,
  Keyboard,
  StyleSheet,
  TextInput,
  type KeyboardEvent,
} from 'react-native';
import { fireGestureHandler, getByGestureTestId } from 'react-native-gesture-handler/jest-utils';
import * as Reanimated from 'react-native-reanimated';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AppText } from './AppText';

import { Sheet, type SheetHandle } from './Sheet';

/** The keyboard listeners the sheet registers, so the test can play the OS. */
const listeners = new Map<string, ((e: KeyboardEvent) => void)[]>();
beforeEach(() => {
  listeners.clear();
  jest.spyOn(Keyboard, 'addListener').mockImplementation(((
    event: string,
    cb: (e: KeyboardEvent) => void,
  ) => {
    listeners.set(event, [...(listeners.get(event) ?? []), cb]);
    return { remove: () => undefined } as ReturnType<typeof Keyboard.addListener>;
  }) as typeof Keyboard.addListener);
});
afterEach(() => jest.restoreAllMocks());

const emit = (event: string, height: number) =>
  act(async () => {
    (listeners.get(event) ?? []).forEach((cb) =>
      cb({ endCoordinates: { height, screenX: 0, screenY: 812 - height, width: 375 } } as never),
    );
  });

describe('Sheet on a phone (iOS)', () => {
  it('rises above the keyboard so a focused input stays visible', async () => {
    await render(
      <SafeAreaProvider
        initialMetrics={{
          frame: { x: 0, y: 0, width: 375, height: 812 },
          insets: { top: 47, left: 0, right: 0, bottom: 34 },
        }}
      >
        <Sheet visible onClose={() => undefined} title="Join a league" testID="sheet">
          <TextInput testID="code" />
        </Sheet>
      </SafeAreaProvider>,
    );
    const panel = () => StyleSheet.flatten(screen.getByTestId('sheet').props.style);
    expect(panel().bottom).toBe(0);
    await emit('keyboardWillShow', 336);
    expect(panel().bottom).toBe(336);
    // It still fits between the status bar and the keyboard.
    const window = Dimensions.get('window').height;
    expect(panel().maxHeight).toBeLessThanOrEqual(window - 336 - 47);
    await emit('keyboardWillHide', 0);
    expect(panel().bottom).toBe(0);
  });
});

describe('Sheet as a centred dialog (iPad, iOS)', () => {
  const ipad = { width: 820, height: 1180, scale: 2, fontScale: 1 };
  beforeEach(() => {
    jest.spyOn(Dimensions, 'get').mockReturnValue(ipad);
  });

  it('shrinks and lifts into the area above the keyboard', async () => {
    await render(
      <SafeAreaProvider
        initialMetrics={{
          frame: { x: 0, y: 0, width: ipad.width, height: ipad.height },
          insets: { top: 24, left: 0, right: 0, bottom: 20 },
        }}
      >
        <Sheet visible onClose={() => undefined} title="Join a league" testID="sheet">
          <TextInput testID="code" autoFocus />
        </Sheet>
      </SafeAreaProvider>,
    );
    const panel = () => StyleSheet.flatten(screen.getByTestId('sheet').props.style);
    // Centred dialog, not the bottom sheet.
    expect(panel().position).toBeUndefined();
    const before = panel().maxHeight as number;
    expect(panel().marginBottom ?? 0).toBe(0);

    await emit('keyboardWillShow', 400);
    // Lifted by the keyboard height (centred in what is left) and short enough
    // to fit between the status bar and the keyboard, scrolling inside.
    expect(panel().marginBottom).toBe(400);
    expect(panel().maxHeight).toBeLessThan(before);
    expect(panel().maxHeight).toBeLessThanOrEqual(ipad.height - 400 - 24);

    await emit('keyboardWillHide', 0);
    expect(panel().marginBottom ?? 0).toBe(0);
    expect(panel().maxHeight).toBe(before);
  });
});

const PHONE = {
  frame: { x: 0, y: 0, width: 375, height: 812 },
  insets: { top: 47, left: 0, right: 0, bottom: 34 },
};

/** Plays a vertical drag on the grabber and header. */
const drag = (translationY: number, velocityY = 0) =>
  act(async () => {
    fireGestureHandler(getByGestureTestId('sheet-drag'), [
      { translationY: 0, velocityY: 0 },
      { translationY: translationY / 2, velocityY },
      { translationY, velocityY },
    ]);
  });

/** The sheet lays out `height` tall (Jest has no layout pass). */
const layout = (height: number) =>
  act(async () => {
    fireEvent(screen.getByTestId('sheet'), 'layout', {
      nativeEvent: { layout: { x: 0, y: 0, width: 375, height } },
    });
  });

describe('Sheet open, drag and close (phone)', () => {
  const phone = (onClose: () => void, extra: Partial<Parameters<typeof Sheet>[0]> = {}) =>
    render(
      <SafeAreaProvider initialMetrics={PHONE}>
        <Sheet visible onClose={onClose} title="Breakdown" testID="sheet" {...extra}>
          <AppText>Body</AppText>
        </Sheet>
      </SafeAreaProvider>,
    );

  it('springs back after a short drag', async () => {
    const onClose = jest.fn();
    await phone(onClose);
    await layout(400);
    await drag(60);
    await act(async () => undefined);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('closes on a long pull or a fast fling down', async () => {
    const onClose = jest.fn();
    await phone(onClose);
    await layout(400);
    await drag(300);
    await act(async () => undefined);
    expect(onClose).toHaveBeenCalledTimes(1);

    const onFling = jest.fn();
    await phone(onFling);
    await layout(400);
    await drag(30, 2000);
    await act(async () => undefined);
    expect(onFling).toHaveBeenCalledTimes(1);
  });

  it('closes by its ✕ and the backdrop, sliding down first', async () => {
    const onClose = jest.fn();
    await phone(onClose);
    await layout(400);
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    expect(onClose).toHaveBeenCalledTimes(1);
    await act(async () =>
      fireEvent.press(screen.getByTestId('sheet-backdrop', { includeHiddenElements: true })),
    );
    // A second close while the first slides is ignored.
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('is as tall as its highest snap point, within the room under the status bar', async () => {
    await phone(jest.fn(), { snapPoints: ['50%', '100%'] });
    const style = StyleSheet.flatten(screen.getByTestId('sheet').props.style);
    // The window, less the status bar (47) and the gap above the sheet (8).
    expect(style.height).toBe(Dimensions.get('window').height - 47 - 8);
  });

  it('rests at a lower snap after a short drag down from the top one', async () => {
    const onClose = jest.fn();
    await phone(onClose, { snapPoints: ['50%', '100%'], initialSnap: 1 });
    const full = Dimensions.get('window').height - 47 - 8;
    await layout(full);
    // From full height to a little past half: snaps to half, does not close.
    await drag(full / 2 + 40);
    await act(async () => undefined);
    expect(onClose).not.toHaveBeenCalled();
    // From half, a long pull closes.
    await drag(300);
    await act(async () => undefined);
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('at a lower snap, leaves room so the end of the content can be scrolled to', async () => {
    await phone(jest.fn(), { snapPoints: ['50%', '100%'] });
    const full = Dimensions.get('window').height - 47 - 8;
    await layout(full);
    const body = StyleSheet.flatten(screen.getByTestId('sheet-body').props.style);
    expect(body.paddingBottom).toBe(full - Math.round(full / 2));
  });

  it('has a single ✕: in its header, or none when the content draws its own', async () => {
    await phone(jest.fn());
    expect(screen.getAllByTestId('sheet-close')).toHaveLength(1);
    await phone(jest.fn(), { hideHeader: true });
    expect(screen.queryAllByTestId('sheet-close')).toHaveLength(0);
  });
});

describe('Sheet closeAnimated (a choice closes it)', () => {
  const open = (onClose: () => void, ref = createRef<SheetHandle>()) =>
    render(
      <SafeAreaProvider initialMetrics={PHONE}>
        <Sheet ref={ref} visible onClose={onClose} title="Sort by" testID="sheet">
          <AppText>Body</AppText>
        </Sheet>
      </SafeAreaProvider>,
    ).then(() => ref);

  it('slides down as the ✕ does, then closes once', async () => {
    const timing = jest.spyOn(Reanimated, 'withTiming');
    const onClose = jest.fn();
    const ref = await open(onClose);
    await layout(400);
    timing.mockClear();
    await act(async () => ref.current!.closeAnimated());
    // The slide to the panel's height (off screen), then onClose from its end.
    expect(timing).toHaveBeenCalledWith(400, expect.anything(), expect.any(Function));
    expect(onClose).toHaveBeenCalledTimes(1);
    await act(async () => ref.current!.closeAnimated());
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closes at once with reduced motion', async () => {
    jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(true);
    const timing = jest.spyOn(Reanimated, 'withTiming');
    const onClose = jest.fn();
    const ref = await open(onClose);
    await layout(400);
    timing.mockClear();
    await act(async () => ref.current!.closeAnimated());
    expect(timing).not.toHaveBeenCalled();
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe('Sheet close racing a touch (rapid open/close)', () => {
  it('a drag during the slide down neither cancels the close nor leaves the ✕ dead', async () => {
    // Animations stay in flight until the test ends them; cancelling one
    // ends it unfinished, as on a device.
    let pending: ((done: boolean) => void)[] = [];
    jest.spyOn(Reanimated, 'withTiming').mockImplementation(((
      to: number,
      _config: unknown,
      done?: (finished: boolean) => void,
    ) => {
      if (!done) return to;
      // A slide down that has only just started: the panel is still up.
      pending.push(done);
      return 0;
    }) as never);
    jest.spyOn(Reanimated, 'cancelAnimation').mockImplementation(() => {
      const cancelled = pending;
      pending = [];
      cancelled.forEach((cb) => cb(false));
    });
    const finish = () =>
      act(async () => {
        const ended = pending;
        pending = [];
        ended.forEach((cb) => cb(true));
      });
    const onClose = jest.fn();
    await render(
      <SafeAreaProvider initialMetrics={PHONE}>
        <Sheet visible onClose={onClose} title="Player" testID="sheet">
          <AppText>Body</AppText>
        </Sheet>
      </SafeAreaProvider>,
    );
    await layout(400);
    await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
    // A finger lands on the sheet while it slides away.
    await drag(20);
    await finish();
    // The slide still lands and closes the sheet.
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('opens again after a close, placed on screen by its first layout', async () => {
    jest.spyOn(AccessibilityInfo, 'isReduceMotionEnabled').mockResolvedValue(false);
    const timing = jest.spyOn(Reanimated, 'withTiming');
    const onClose = jest.fn();
    const sheet = (visible: boolean) => (
      <SafeAreaProvider initialMetrics={PHONE}>
        <Sheet visible={visible} onClose={onClose} title="Player" testID="sheet">
          <AppText>Body</AppText>
        </Sheet>
      </SafeAreaProvider>
    );
    const view = await render(sheet(true));
    for (let i = 0; i < 10; i += 1) {
      await layout(400);
      await act(async () => fireEvent.press(screen.getByTestId('sheet-close')));
      expect(onClose).toHaveBeenCalledTimes(i + 1);
      await view.rerender(sheet(false));
      await view.rerender(sheet(true));
      timing.mockClear();
      await layout(400);
      // Slides up to rest fully open (offset 0).
      expect(timing).toHaveBeenCalledWith(0, expect.anything());
    }
  });
});
