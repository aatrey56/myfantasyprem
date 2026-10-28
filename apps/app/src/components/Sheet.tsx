import {
  useCallback,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from 'react';
import {
  Keyboard,
  Modal,
  Platform,
  Pressable,
  StyleSheet,
  View,
  type LayoutChangeEvent,
  type StyleProp,
  type ViewStyle,
} from 'react-native';
import { Gesture, GestureDetector, GestureHandlerRootView } from 'react-native-gesture-handler';
import Animated, {
  cancelAnimation,
  useAnimatedScrollHandler,
  useAnimatedStyle,
  useSharedValue,
  withTiming,
  type SharedValue,
} from 'react-native-reanimated';
import { useSafeAreaInsets } from 'react-native-safe-area-context';
import { scheduleOnRN } from 'react-native-worklets';

import { AppText } from './AppText';
import { Button, IconButton } from './Button';
import { X } from './icons';
import {
  backdropOpacity,
  dragOffset,
  NATIVE_SHEET,
  PANEL_LAYOUT_TIMEOUT,
  settle,
  snapHeights,
  snapOffset,
  type SnapPoint,
} from './sheet-snap';
import { SheetCloseContext, useSheetClose } from './sheet-context';
import { NativeSheet } from './NativeSheet';
import { useReducedMotion } from './Skeleton';
import { useLayout } from '@/hooks/useLayout';
import { useTheme } from '@/theme';

export type { SnapPoint } from './sheet-snap';
export { useSheetClose } from './sheet-context';

/** What a `ref` on a Sheet can do. */
export interface SheetHandle {
  /**
   * Closes as the ✕ does: a phone sheet slides down first (reduced motion:
   * at once), then `onClose` runs. For a sheet that closes after a choice.
   */
  closeAnimated: () => void;
}

interface SheetProps {
  ref?: Ref<SheetHandle>;
  visible: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  /** Pinned under the content (action buttons). */
  footer?: ReactNode;
  testID?: string;
  /** Wrap the content in a ScrollView (default). */
  scroll?: boolean;
  /** Padding and gap of the scrolling content (default: lg padding, md gap). */
  contentStyle?: StyleProp<ViewStyle>;
  /** Drawn over the content, outside the scroll (a floating button). */
  overlay?: ReactNode;
  /**
   * Phones: the heights the sheet rests at, lowest first after sorting (px
   * or a share of the room between the status bar and the keyboard, e.g.
   * `['50%', '100%']`). Without them the sheet is as tall as its content.
   */
  snapPoints?: readonly SnapPoint[];
  /** Which snap point it opens at (default the lowest). */
  initialSnap?: number;
  /** The centred dialog's width from 768px up (default 520). */
  wideWidth?: number;
  /** `center`: the centred dialog on phones too (small pop-ups); default bottom sheet below 768px. */
  presentation?: 'sheet' | 'center';
  /**
   * Leave out the title bar: the content draws its own heading and a close
   * button (SheetCloseButton, testID `sheet-close`). `title` still names the dialog.
   */
  hideHeader?: boolean;
}

/**
 * How much of the window's bottom the software keyboard covers (0 when it is
 * down). A Modal does not resize for the iOS keyboard, so the sheet must make
 * room itself; Android resizes the window. On web (`web: true`) the covered
 * part is read from `visualViewport`, which shrinks for the keyboard in iOS
 * Safari while the layout viewport does not.
 */
function useKeyboardHeight(active: boolean, web: boolean): number {
  const [height, setHeight] = useState(0);
  useEffect(() => {
    if (!active) return;
    if (Platform.OS === 'ios') {
      const show = Keyboard.addListener('keyboardWillShow', (e) =>
        setHeight(e.endCoordinates.height),
      );
      const hide = Keyboard.addListener('keyboardWillHide', () => setHeight(0));
      return () => {
        show.remove();
        hide.remove();
        setHeight(0);
      };
    }
    if (Platform.OS !== 'web' || !web || typeof window === 'undefined') return;
    const vv = window.visualViewport;
    if (!vv) return;
    const update = () =>
      // Pinch zoom also shrinks the visual viewport; only count the keyboard.
      setHeight(
        vv.scale > 1.01
          ? 0
          : Math.max(0, Math.round(window.innerHeight - vv.height - vv.offsetTop)),
      );
    update();
    vv.addEventListener('resize', update);
    vv.addEventListener('scroll', update);
    return () => {
      vv.removeEventListener('resize', update);
      vv.removeEventListener('scroll', update);
      setHeight(0);
    };
  }, [active, web]);
  return height;
}

/** The slide in and out (ms). */
const SLIDE_MS = 240;
/**
 * A close lands this long after it starts whatever its slide does: the
 * slide's own end usually lands it first, but a slide cut short, restarted
 * or never reported must not leave the Modal up (it takes every touch).
 */
export const CLOSE_MS = SLIDE_MS + 120;
/** A visible sheet whose panel sits off screen this long is closed (ms). */
export const OFFSCREEN_MS = 300;
/** The gap between a full-height sheet and the status bar. */
const TOP_GAP = 8;

/** The ✕ for a sheet that draws its own heading (`hideHeader`). */
export function SheetCloseButton() {
  const close = useSheetClose();
  return <IconButton icon={X} label="Close" onPress={close} testID="sheet-close" />;
}

interface Motion {
  /** How far the panel is pushed down from fully open (px). */
  offset: SharedValue<number>;
  /** The panel's measured height (0 until it lays out). */
  panel: SharedValue<number>;
  /** The snap heights (px), lowest first. */
  heights: SharedValue<number[]>;
  /** The snap the sheet rests at. */
  snap: SharedValue<number>;
  /** The content's scroll offset, for the pull to close from the top. */
  scrollY: SharedValue<number>;
}

/**
 * A phone sheet's motion: it slides up once it has laid out, rests at its
 * snap points, follows a drag on the grabber and header (touch or mouse)
 * and, on native, a pull down from the top of its scrolling content; it
 * slides down before `onClose`. Reduced motion jumps instead of sliding.
 */
function useSheetMotion({
  visible,
  active,
  onClose,
  snapPoints,
  initialSnap,
  room,
  windowHeight,
}: {
  visible: boolean;
  active: boolean;
  onClose: () => void;
  snapPoints: readonly SnapPoint[] | undefined;
  initialSnap: number;
  room: number;
  windowHeight: number;
}) {
  const reduced = useReducedMotion();
  const motion: Motion = {
    offset: useSharedValue(windowHeight),
    panel: useSharedValue(0),
    heights: useSharedValue<number[]>([]),
    snap: useSharedValue(0),
    scrollY: useSharedValue(0),
  };
  const { offset, panel, heights, snap, scrollY } = motion;
  const closing = useSharedValue(false);
  // The JS side's own record of a ✕ close in flight: a shared value set from
  // JS may not read back at once there, and a stale `closing` would make
  // the next opening ignore its layout (the panel would stay off screen).
  const closingJs = useRef(false);
  // Where a drag started, and whether a content drag pulls the sheet.
  const start = useSharedValue(0);
  const pulling = useSharedValue(false);
  const latestClose = useRef(onClose);
  useEffect(() => {
    latestClose.current = onClose;
  });
  // Which opening this is: a close that lands after the sheet opened again
  // (its slide outlived the opening) must not close the new one.
  const opening = useRef(0);
  // The panel height this opening measured, on the JS side (a shared value
  // set from JS may read back stale, and a stale match would skip placing
  // the panel, leaving it off screen).
  const measured = useRef(0);
  // The close in flight: its opening and its timer. Closing is decided on
  // the JS side: the timer lands it at CLOSE_MS, the slide's end may land
  // it sooner, and whichever comes first calls onClose, once.
  const pending = useRef<{ from: number; timer: ReturnType<typeof setTimeout> } | null>(null);
  // Drives pointerEvents and the gestures while the sheet slides away.
  const [isClosing, setIsClosing] = useState(false);
  // Each opening (and each close by the parent) starts not closing.
  const [lastVisible, setLastVisible] = useState(visible);
  if (lastVisible !== visible) {
    setLastVisible(visible);
    setIsClosing(false);
  }
  // The opening onClose last ran for: once per opening.
  const closed = useRef(-1);
  const [finishClose] = useState(() => () => {
    const p = pending.current;
    if (!p) return;
    clearTimeout(p.timer);
    pending.current = null;
    if (p.from !== opening.current || closed.current === p.from) return;
    closed.current = p.from;
    latestClose.current();
  });
  const [beginClose] = useState(() => () => {
    if (pending.current || closed.current === opening.current) return;
    closingJs.current = true;
    pending.current = { from: opening.current, timer: setTimeout(finishClose, CLOSE_MS) };
    setIsClosing(true);
  });
  useEffect(() => () => clearTimeout(pending.current?.timer), []);
  // How much of a fixed-height panel rests below the screen (at a lower
  // snap): the content leaves that much room so its end can be scrolled to.
  const [hidden, setHidden] = useState(0);

  const fixed = snapPoints ? snapHeights(snapPoints, room) : null;
  // The panel is as tall as its highest snap, or as its content.
  const fixedHeight = fixed ? fixed[fixed.length - 1] : undefined;
  const fixedKey = fixed?.join(',');

  const move = useCallback(
    (to: number) => {
      offset.set(reduced ? to : withTiming(to, { duration: SLIDE_MS }));
    },
    [offset, reduced],
  );

  // A new opening starts below the screen, at the first snap. A layout
  // effect: on native the panel's first onLayout can arrive before a
  // passive effect runs, and must not find the last close's `closing`
  // still set (it would be ignored, leaving the panel off screen under a
  // Modal that takes every touch).
  useLayoutEffect(() => {
    closingJs.current = false;
    opening.current += 1;
    measured.current = 0;
    if (pending.current) {
      clearTimeout(pending.current.timer);
      pending.current = null;
    }
    if (!visible) {
      closing.set(false);
      return;
    }
    cancelAnimation(offset);
    closing.set(false);
    scrollY.set(0);
    panel.set(0);
    snap.set(Math.max(0, initialSnap));
    offset.set(windowHeight);
    // Only on opening: later size changes are handled on layout.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible]);

  /** Rests the sheet on its current snap for a panel `h` tall. */
  const place = useCallback(
    (h: number) => {
      const list = fixed ?? [h];
      heights.set(list);
      const i = Math.min(snap.get(), list.length - 1);
      snap.set(i);
      const to = snapOffset(list[i]!, h);
      setHidden(to);
      move(to);
    },
    // fixedKey stands for fixed.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [fixedKey, heights, snap, move],
  );

  const onLayout = (e: LayoutChangeEvent) => {
    const h = Math.round(e.nativeEvent.layout.height);
    if (!active || closingJs.current || h === 0 || h === measured.current) return;
    measured.current = h;
    panel.set(h);
    place(h);
  };

  const close = useCallback(() => {
    if (!active || reduced || measured.current === 0) {
      onClose();
      return;
    }
    if (pending.current || closed.current === opening.current) return;
    beginClose();
    closing.set(true);
    // Finished or cut short, the slide's end lands the close (the timer
    // lands it anyway if this never reports).
    offset.set(
      withTiming(measured.current, { duration: SLIDE_MS }, () => {
        scheduleOnRN(finishClose);
      }),
    );
  }, [active, reduced, onClose, closing, offset, beginClose, finishClose]);

  // The watchdog: a visible sheet whose laid-out panel sits fully off screen
  // (not sliding in, since placing it starts the slide at once) is an
  // invisible layer over the app. Close it.
  useEffect(() => {
    if (!visible || !active) return;
    let since = 0;
    const id = setInterval(() => {
      const h = measured.current;
      if (h === 0 || offset.get() < h - 1) {
        since = 0;
        return;
      }
      const now = Date.now();
      if (since === 0) since = now;
      else if (now - since >= OFFSCREEN_MS) {
        since = 0;
        if (pending.current) finishClose();
        else if (closed.current !== opening.current) {
          closed.current = opening.current;
          latestClose.current();
        }
      }
    }, 100);
    return () => clearInterval(id);
  }, [visible, active, offset, finishClose]);

  const gestures = useMemo(() => {
    const end = (velocity: number) => {
      'worklet';
      const i = settle(offset.get(), velocity, heights.get(), panel.get());
      if (i < 0) {
        closing.set(true);
        scheduleOnRN(beginClose);
        offset.set(
          reduced
            ? panel.get()
            : withTiming(panel.get(), { duration: SLIDE_MS / 1.5 }, () => {
                scheduleOnRN(finishClose);
              }),
        );
        if (reduced) scheduleOnRN(finishClose);
        return;
      }
      snap.set(i);
      const to = snapOffset(heights.get()[i]!, panel.get());
      scheduleOnRN(setHidden, to);
      offset.set(reduced ? to : withTiming(to, { duration: SLIDE_MS / 1.5 }));
    };
    // The grabber and the header: any vertical drag moves the sheet.
    const handle = Gesture.Pan()
      .withTestId('sheet-drag')
      .enabled(active && !isClosing)
      .activeOffsetY([-6, 6])
      .failOffsetX([-24, 24])
      .onStart(() => {
        // A touch during the slide down must not cancel it: the close
        // would never land and the sheet would stay up with a dead ✕.
        if (closing.get()) return;
        cancelAnimation(offset);
        start.set(offset.get());
      })
      .onUpdate((e) => {
        if (closing.get()) return;
        offset.set(dragOffset(start.get() + e.translationY, panel.get()));
      })
      .onEnd((e) => {
        if (!closing.get()) end(e.velocityY);
      });
    // The content's scroll view, natively: scrolling as usual, and a pull
    // down that starts at its top moves the sheet instead.
    const scroller = Gesture.Native();
    const content = Gesture.Pan()
      .withTestId('sheet-content-drag')
      .enabled(active && !isClosing && Platform.OS !== 'web')
      .simultaneousWithExternalGesture(scroller)
      .activeOffsetY([-6, 6])
      .failOffsetX([-24, 24])
      .onStart(() => {
        pulling.set(!closing.get() && scrollY.get() <= 0);
        if (!pulling.get()) return;
        cancelAnimation(offset);
        start.set(offset.get());
      })
      .onUpdate((e) => {
        if (!pulling.get() || closing.get()) return;
        offset.set(dragOffset(start.get() + Math.max(0, e.translationY), panel.get()));
      })
      .onEnd((e) => {
        if (pulling.get() && !closing.get() && e.translationY > 0) end(e.velocityY);
        pulling.set(false);
      });
    return { handle, content, scroller };
  }, [
    active,
    isClosing,
    reduced,
    offset,
    panel,
    heights,
    snap,
    scrollY,
    closing,
    start,
    pulling,
    beginClose,
    finishClose,
  ]);

  const onScroll = useAnimatedScrollHandler((e) => {
    scrollY.set(e.contentOffset.y);
  });

  const panelStyle = useAnimatedStyle(() => ({
    transform: [{ translateY: active ? offset.get() : 0 }],
  }));
  const backdropStyle = useAnimatedStyle(() => {
    if (!active) return { opacity: 1 };
    if (panel.get() === 0) return { opacity: 0 };
    const list = heights.get();
    const lowest = list.length ? snapOffset(list[0]!, panel.get()) : 0;
    return { opacity: backdropOpacity(offset.get(), lowest, panel.get()) };
  });

  return {
    close,
    onLayout,
    gestures,
    onScroll,
    panelStyle,
    backdropStyle,
    fixedHeight,
    hidden: active && fixed ? hidden : 0,
    isClosing,
  };
}

/**
 * Web: moves focus into the open dialog (unless something inside already has
 * it) and closes on Escape, and hands focus back to the opener on close.
 */
function useDialogFocus(visible: boolean, onClose: () => void) {
  const ref = useRef<View>(null);
  const close = useRef(onClose);
  useEffect(() => {
    close.current = onClose;
  });
  useEffect(() => {
    if (Platform.OS !== 'web' || !visible) return;
    const opener = document.activeElement as HTMLElement | null;
    const id = setTimeout(() => {
      const el = ref.current as unknown as HTMLElement | null;
      if (!el || el.contains(document.activeElement)) return;
      (el.querySelector<HTMLElement>('[data-testid="sheet-close"]') ?? el).focus();
    }, 0);
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        close.current();
        return;
      }
      if (e.key !== 'Tab') return;
      const el = ref.current as unknown as HTMLElement | null;
      if (!el) return;
      const items = [
        ...el.querySelectorAll<HTMLElement>(
          'a[href],button:not([disabled]),input:not([disabled]),select,textarea,[tabindex]:not([tabindex="-1"])',
        ),
      ].filter((n) => n.offsetParent !== null);
      if (items.length === 0) return;
      const first = items[0]!;
      const last = items[items.length - 1]!;
      const active = document.activeElement;
      if (!el.contains(active)) {
        e.preventDefault();
        first.focus();
      } else if (e.shiftKey && active === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('keydown', onKey);
      clearTimeout(id);
      if (opener && opener.isConnected) opener.focus();
    };
  }, [visible]);
  return ref;
}

/**
 * A bottom sheet on phones (<768px); a centred modal dialog (520px wide by
 * default, 85% tall, scrolling inside) from 768px up. Both stay above the
 * software keyboard. Escape (web), the hardware back button, the ✕ and a
 * tap on the backdrop close it.
 *
 * On phones it slides up as the backdrop fades in, rests at its snap points
 * and follows a drag on the grabber or header (finger or mouse); on native a
 * pull down from the top of its content does the same. A short drag springs
 * back to the nearest snap; a long pull or a fling down closes it.
 */
export function Sheet(props: SheetProps) {
  const { isWide } = useLayout();
  // Phones on iOS and Android: gorhom's sheet (NativeSheet). Web keeps this
  // one: its mouse drag, focus trap and Escape are tested here, and gorhom's
  // web build is a best-effort port (no content pull on a mouse). Centred
  // pop-ups (`presentation="center"`) stay on the Modal dialog everywhere.
  if (Platform.OS !== 'web' && !isWide && props.presentation !== 'center' && NATIVE_SHEET.enabled) {
    const { wideWidth: _wide, presentation: _presentation, ...rest } = props;
    return <NativeSheet {...rest} />;
  }
  return <ModalSheet {...props} />;
}

function ModalSheet({
  ref,
  visible,
  onClose,
  title,
  children,
  footer,
  testID,
  scroll = true,
  contentStyle,
  overlay,
  snapPoints,
  initialSnap = 0,
  wideWidth = 520,
  hideHeader,
  presentation = 'sheet',
}: SheetProps) {
  const { colors, spacing, radii } = useTheme();
  const layout = useLayout();
  const { height } = layout;
  const isWide = layout.isWide || presentation === 'center';
  const insets = useSafeAreaInsets();
  // Phones keep the native-only lift; the centred dialog also tracks the web
  // keyboard (iPad Safari) so its input and footer stay above it.
  const keyboard = useKeyboardHeight(visible, isWide);
  // The room between the status bar and the keyboard.
  const room = height - keyboard - insets.top - TOP_GAP;
  const motion = useSheetMotion({
    visible,
    active: !isWide,
    onClose,
    snapPoints,
    initialSnap,
    room,
    windowHeight: height,
  });
  const close = motion.close;
  // The safety net: an open Modal whose panel never laid out (measured 0
  // tall, or its layout lost) is an invisible layer that takes every touch.
  // Close it rather than leave the app looking frozen.
  const laidOut = useRef(false);
  const latestOnClose = useRef(onClose);
  useEffect(() => {
    latestOnClose.current = onClose;
  });
  useLayoutEffect(() => {
    laidOut.current = false;
  }, [visible]);
  useEffect(() => {
    const ms = PANEL_LAYOUT_TIMEOUT.ms;
    if (!visible || !Number.isFinite(ms)) return;
    const id = setTimeout(() => {
      if (!laidOut.current) latestOnClose.current();
    }, ms);
    return () => clearTimeout(id);
  }, [visible]);
  const onPanelLayout = (e: LayoutChangeEvent) => {
    if (e.nativeEvent.layout.height > 0) laidOut.current = true;
    motion.onLayout(e);
  };
  useImperativeHandle(ref, () => ({ closeAnimated: close }), [close]);
  const dialogRef = useDialogFocus(visible, close);
  const contentPadding = [{ padding: spacing.lg, gap: spacing.md }, contentStyle];
  const body = scroll ? (
    <GestureDetector gesture={motion.gestures.scroller}>
      <Animated.ScrollView
        testID={testID ? `${testID}-scroll` : undefined}
        style={{ flexGrow: 0, flexShrink: 1 }}
        contentContainerStyle={contentPadding}
        keyboardShouldPersistTaps="handled"
        onScroll={motion.onScroll}
        scrollEventThrottle={16}
        // At the top a pull down moves the sheet, not the content.
        bounces={isWide}
      >
        {children}
      </Animated.ScrollView>
    </GestureDetector>
  ) : (
    <View style={{ flexShrink: 1, flexGrow: 0 }}>{children}</View>
  );
  const header = hideHeader ? null : (
    <View
      testID="sheet-header"
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.sm,
        paddingLeft: spacing.lg,
        paddingRight: spacing.xs,
        paddingTop: isWide ? spacing.md : spacing.xs,
        paddingBottom: spacing.xs,
        borderBottomWidth: 1,
        borderBottomColor: colors.border,
      }}
    >
      <AppText variant="heading" style={{ flex: 1 }} accessibilityRole="header" numberOfLines={1}>
        {title}
      </AppText>
      <IconButton icon={X} label="Close" onPress={close} testID="sheet-close" />
    </View>
  );
  const foot = footer ? (
    <View
      style={{
        padding: spacing.lg,
        paddingBottom: spacing.lg + (keyboard ? 0 : insets.bottom),
        borderTopWidth: 1,
        borderTopColor: colors.border,
        gap: spacing.sm,
      }}
    >
      {footer}
    </View>
  ) : (
    <View style={{ height: keyboard ? 0 : insets.bottom }} />
  );
  const content = (
    <View
      testID={testID ? `${testID}-content` : undefined}
      collapsable={false}
      style={{ flexGrow: motion.fixedHeight && !isWide ? 1 : 0, flexShrink: 1, minHeight: 0 }}
    >
      {body}
      {overlay}
    </View>
  );

  return (
    <Modal
      visible={visible}
      transparent
      // Phones slide the panel themselves (and follow a drag).
      animationType={isWide ? 'fade' : 'none'}
      onRequestClose={close}
      statusBarTranslucent
    >
      <SheetCloseContext.Provider value={close}>
        {/* A Modal is its own native root: gestures need a root inside it. */}
        <GestureHandlerRootView style={{ flex: 1 }}>
          <View
            testID={testID ? `${testID}-root` : undefined}
            // Sliding away: nothing in it takes a touch.
            pointerEvents={motion.isClosing ? 'none' : 'auto'}
            style={{
              flex: 1,
              alignItems: 'center',
              justifyContent: 'center',
              padding: isWide ? spacing.lg : 0,
            }}
          >
            <Animated.View
              style={[
                StyleSheet.absoluteFill,
                { backgroundColor: colors.scrim },
                motion.backdropStyle,
              ]}
            >
              <Pressable
                accessibilityLabel="Close"
                accessibilityRole="button"
                onPress={close}
                testID="sheet-backdrop"
                style={StyleSheet.absoluteFill}
                // The panel's close button is the keyboard path; skip the backdrop.
                {...({ tabIndex: -1 } as object)}
              />
            </Animated.View>
            <Animated.View
              testID={testID}
              role="dialog"
              aria-modal
              aria-label={title}
              onLayout={onPanelLayout}
              style={[
                isWide
                  ? {
                      width: wideWidth,
                      maxWidth: '100%',
                      // Centred in the part of the window the keyboard leaves
                      // visible: the bottom margin lifts it by the keyboard height
                      // and the cap keeps it inside, scrolling within.
                      marginBottom: keyboard,
                      maxHeight: keyboard
                        ? Math.min(height * 0.85, height - keyboard - insets.top - spacing.lg * 2)
                        : height * 0.85,
                      backgroundColor: colors.surfaceRaised,
                      borderRadius: radii.sheet,
                      borderWidth: 1,
                      borderColor: colors.border,
                      overflow: 'hidden',
                    }
                  : {
                      position: 'absolute',
                      left: 0,
                      right: 0,
                      // Above the iOS keyboard, so a focused input stays in view.
                      bottom: keyboard,
                      ...(motion.fixedHeight
                        ? { height: motion.fixedHeight, maxHeight: room + TOP_GAP }
                        : { maxHeight: Math.min(height * 0.9, room + TOP_GAP) }),
                      alignSelf: 'center',
                      width: '100%',
                      maxWidth: 640,
                      marginHorizontal: 'auto',
                      backgroundColor: colors.surfaceRaised,
                      borderTopLeftRadius: radii.sheet,
                      borderTopRightRadius: radii.sheet,
                      borderWidth: 1,
                      borderBottomWidth: 0,
                      borderColor: colors.border,
                      overflow: 'hidden',
                    },
                motion.panelStyle,
              ]}
            >
              <View
                ref={dialogRef}
                testID={testID ? `${testID}-body` : undefined}
                style={{
                  flexGrow: motion.fixedHeight && !isWide ? 1 : 0,
                  flexShrink: 1,
                  minHeight: 0,
                  paddingBottom: motion.hidden,
                }}
              >
                {isWide ? (
                  header
                ) : (
                  <GestureDetector gesture={motion.gestures.handle}>
                    {/* The grabber and header: a drag here moves the sheet (finger or mouse). */}
                    <View
                      testID="sheet-handle"
                      collapsable={false}
                      style={{
                        ...(Platform.OS === 'web'
                          ? ({ userSelect: 'none', cursor: 'grab' } as object)
                          : {}),
                      }}
                    >
                      <View
                        testID="sheet-grabber"
                        aria-hidden
                        style={{
                          alignSelf: 'stretch',
                          alignItems: 'center',
                          paddingTop: spacing.sm,
                        }}
                      >
                        <View
                          style={{
                            width: 36,
                            height: 4,
                            borderRadius: 2,
                            backgroundColor: colors.border,
                          }}
                        />
                      </View>
                      {header}
                    </View>
                  </GestureDetector>
                )}
                {isWide ? (
                  content
                ) : (
                  <GestureDetector gesture={motion.gestures.content}>{content}</GestureDetector>
                )}
                {foot}
              </View>
            </Animated.View>
          </View>
        </GestureHandlerRootView>
      </SheetCloseContext.Provider>
    </Modal>
  );
}

interface ConfirmProps {
  visible: boolean;
  title: string;
  message: string;
  confirmLabel: string;
  cancelLabel?: string;
  destructive?: boolean;
  loading?: boolean;
  onConfirm: () => void;
  onCancel: () => void;
  testID?: string;
  /** Extra content between the message and the buttons. */
  children?: ReactNode;
}

/** A centred alert dialog for irreversible actions. */
export function ConfirmDialog({
  visible,
  title,
  message,
  confirmLabel,
  cancelLabel = 'Cancel',
  destructive,
  loading,
  onConfirm,
  onCancel,
  testID,
  children,
}: ConfirmProps) {
  const { colors, spacing, radii } = useTheme();
  return (
    <Modal visible={visible} transparent animationType="fade" onRequestClose={onCancel}>
      <View
        style={{
          flex: 1,
          alignItems: 'center',
          justifyContent: 'center',
          padding: spacing.lg,
          backgroundColor: colors.scrim,
        }}
      >
        <View
          testID={testID}
          role="alertdialog"
          aria-modal
          aria-label={title}
          style={{
            width: '100%',
            maxWidth: 400,
            gap: spacing.md,
            padding: spacing.xl,
            borderRadius: radii.sheet,
            backgroundColor: colors.surfaceRaised,
            borderWidth: 1,
            borderColor: colors.border,
          }}
        >
          <AppText variant="heading" accessibilityRole="header">
            {title}
          </AppText>
          <AppText muted>{message}</AppText>
          {children}
          <View
            style={{
              flexDirection: 'row',
              justifyContent: 'flex-end',
              gap: spacing.sm,
              flexWrap: 'wrap',
            }}
          >
            <Button
              label={cancelLabel}
              variant="secondary"
              onPress={onCancel}
              testID="confirm-cancel"
            />
            <Button
              label={confirmLabel}
              variant={destructive ? 'destructive' : 'primary'}
              onPress={onConfirm}
              loading={loading}
              testID="confirm-ok"
            />
          </View>
        </View>
      </View>
    </Modal>
  );
}
