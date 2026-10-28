import {
  BottomSheetBackdrop,
  BottomSheetFooter,
  BottomSheetModal,
  BottomSheetModalProvider,
  BottomSheetScrollView,
  BottomSheetTextInput,
  BottomSheetView,
  ANIMATION_SOURCE,
  GESTURE_SOURCE,
  useBottomSheetInternal,
  useGestureEventsHandlersDefault,
  type BottomSheetBackdropProps,
  type BottomSheetFooterProps,
} from '@gorhom/bottom-sheet';
import {
  useCallback,
  useContext,
  useEffect,
  useImperativeHandle,
  useLayoutEffect,
  useRef,
  useState,
  type ReactNode,
  type Ref,
} from 'react';
import {
  BackHandler,
  Keyboard,
  StyleSheet,
  View,
  type StyleProp,
  type TextInputProps,
  type ViewStyle,
} from 'react-native';
import { ReduceMotion, runOnJS } from 'react-native-reanimated';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { IconButton } from './Button';
import { X } from './icons';
import { SheetCloseContext } from './sheet-context';
import { SheetTextInputContext } from './sheet-input';
import { PANEL_LAYOUT_TIMEOUT, pullCloses, snapHeights, type SnapPoint } from './sheet-snap';
import { useReducedMotion } from './Skeleton';
import { OpenCardContext } from './players/PlayerCardContext';
import { DifficultyContext } from './projections/Difficulty';
import { PagerSceneContext } from '@/api/screen-focus';
import { LeagueIdContext } from '@/hooks/useLeagueId';
import { useLayout } from '@/hooks/useLayout';
import { useTheme } from '@/theme';

// A plain function for the worklet below: capturing `Keyboard` itself makes
// Worklets throw "Cannot copy value of type KeyboardImpl" (the app crashes).
const dismissKeyboard = Keyboard.dismiss;

/**
 * gorhom's gesture handlers with the app's close rule: a pull of about
 * 120px below the lowest snap, or a fling down, closes the sheet on the
 * first try (pullCloses). gorhom alone closes only past halfway, so a
 * moderate pull on the full-height card sprang back. Anything else (and a
 * pull the content's own scroll took) goes to gorhom's handler.
 */
function useCloseOnPull() {
  const handlers = useGestureEventsHandlersDefault();
  const { animatedPosition, animatedDetentsState, animatedScrollableState, animateToPosition } =
    useBottomSheetInternal();
  const { handleOnEnd } = handlers;
  const onEnd: typeof handleOnEnd = useCallback(
    (source, event) => {
      'worklet';
      const { detents, closedDetentPosition } = animatedDetentsState.get();
      const scrolled =
        source === GESTURE_SOURCE.CONTENT && animatedScrollableState.get().contentOffsetY > 0;
      if (detents && detents.length > 0 && closedDetentPosition !== undefined && !scrolled) {
        const lowest = Math.max(...detents);
        const below = animatedPosition.value - lowest;
        if (pullCloses(below, event.velocityY, closedDetentPosition - lowest)) {
          runOnJS(dismissKeyboard)();
          animateToPosition(closedDetentPosition, ANIMATION_SOURCE.GESTURE, event.velocityY / 2);
          return;
        }
      }
      handleOnEnd(source, event);
    },
    [
      animatedDetentsState,
      animatedScrollableState,
      animatedPosition,
      animateToPosition,
      handleOnEnd,
    ],
  );
  return { ...handlers, handleOnEnd: onEnd };
}

/** A sheet's text input: it lifts the sheet above the keyboard. */
const renderSheetInput = (props: TextInputProps) => <BottomSheetTextInput {...props} />;

/** The gap between a full-height sheet and the status bar. */
const TOP_GAP = 8;

/**
 * A sheet draws in the provider's portal at the app root, outside the screen
 * that opened it: it carries over the screen's contexts its content reads
 * (an RN Modal kept them).
 */
function useScreenContexts() {
  return {
    leagueId: useContext(LeagueIdContext),
    difficulty: useContext(DifficultyContext),
    openCard: useContext(OpenCardContext),
    scene: useContext(PagerSceneContext),
  };
}

function ContextBridge({
  value,
  children,
}: {
  value: ReturnType<typeof useScreenContexts>;
  children: ReactNode;
}) {
  return (
    <LeagueIdContext.Provider value={value.leagueId}>
      <DifficultyContext.Provider value={value.difficulty}>
        <OpenCardContext.Provider value={value.openCard}>
          <PagerSceneContext.Provider value={value.scene}>{children}</PagerSceneContext.Provider>
        </OpenCardContext.Provider>
      </DifficultyContext.Provider>
    </LeagueIdContext.Provider>
  );
}

/** Hosts the native sheets: once, at the app's root (inside the gesture root). */
export function SheetProvider({ children }: { children: ReactNode }) {
  return <BottomSheetModalProvider>{children}</BottomSheetModalProvider>;
}

export interface NativeSheetHandle {
  closeAnimated: () => void;
}

export interface NativeSheetProps {
  ref?: Ref<NativeSheetHandle>;
  visible: boolean;
  onClose: () => void;
  title: string;
  children: ReactNode;
  footer?: ReactNode;
  testID?: string;
  scroll?: boolean;
  contentStyle?: StyleProp<ViewStyle>;
  overlay?: ReactNode;
  snapPoints?: readonly SnapPoint[];
  initialSnap?: number;
  hideHeader?: boolean;
}

/**
 * A phone bottom sheet on iOS and Android, drawn by @gorhom/bottom-sheet: it
 * slides up over a fading backdrop, rests at its snap points (or fits its
 * content), and a drag on the grabber or header, or a pull down anywhere on
 * its content while that is scrolled to the top, moves it; a pull far or
 * fast enough closes it. Its inputs keep it above the keyboard. Reduced
 * motion jumps instead of sliding. Sheet picks it; the API is Sheet's.
 */
export function NativeSheet({
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
  hideHeader,
}: NativeSheetProps) {
  const { colors, spacing, radii } = useTheme();
  const { height } = useLayout();
  const insets = useSafeAreaInsets();
  const reduced = useReducedMotion();
  const modal = useRef<BottomSheetModal>(null);
  const screen = useScreenContexts();
  const room = height - insets.top - TOP_GAP;
  const fixed = snapPoints ? snapHeights(snapPoints, room) : null;
  const index = fixed ? Math.min(Math.max(0, initialSnap), fixed.length - 1) : 0;

  const latestClose = useRef(onClose);
  useEffect(() => {
    latestClose.current = onClose;
  });
  // The parent's `visible`, as of the last render: a dismissal it did not
  // ask for (a pull, the backdrop, the ✕) tells it with onClose.
  const wanted = useRef(visible);
  useLayoutEffect(() => {
    wanted.current = visible;
  });
  // Unmounted while open (a width change or multi-window swaps the sheet):
  // gorhom's own dismiss then fires onDismiss, which must not close the
  // owner's replacement sheet.
  useEffect(
    () => () => {
      wanted.current = false;
    },
    [],
  );
  // Whether the sheet has reached a snap since it was last presented.
  const shown = useRef(false);
  const presented = useRef(false);

  useEffect(() => {
    if (visible) {
      shown.current = false;
      presented.current = true;
      modal.current?.present();
    } else if (presented.current) {
      // Only after a present: gorhom's dismiss on a sheet never shown
      // leaves it "dismissing", and the next present never shows it.
      presented.current = false;
      modal.current?.dismiss();
    }
  }, [visible]);

  const close = useCallback(() => {
    if (!modal.current) {
      latestClose.current();
      return;
    }
    modal.current.dismiss();
  }, []);
  useImperativeHandle(ref, () => ({ closeAnimated: close }), [close]);

  const onDismiss = useCallback(() => {
    // Gone: the owner's close that follows must not dismiss it again.
    presented.current = false;
    if (wanted.current) latestClose.current();
  }, []);
  const onChange = useCallback((i: number) => {
    if (i >= 0) shown.current = true;
  }, []);

  // The cheap safety net: a sheet asked to show that never reaches a snap
  // tells its owner it closed, so app state never thinks one is up.
  useEffect(() => {
    const ms = PANEL_LAYOUT_TIMEOUT.ms;
    if (!visible || !Number.isFinite(ms)) return;
    const id = setTimeout(() => {
      if (!shown.current && wanted.current) {
        modal.current?.dismiss();
        latestClose.current();
      }
    }, ms * 3);
    return () => clearTimeout(id);
  }, [visible]);

  // Android's back button.
  useEffect(() => {
    if (!visible) return;
    const sub = BackHandler.addEventListener('hardwareBackPress', () => {
      close();
      return true;
    });
    return () => sub.remove();
  }, [visible, close]);

  // The content stays drawn while the sheet slides away after its owner
  // closed it (the owner may already have cleared what it shows).
  const [kept, setKept] = useState(children);
  if (visible && kept !== children) setKept(children);

  const [footerHeight, setFooterHeight] = useState(0);
  const bottomPad = footer ? footerHeight : insets.bottom;

  const renderBackdrop = useCallback(
    (props: BottomSheetBackdropProps) => (
      <BottomSheetBackdrop
        {...props}
        appearsOnIndex={0}
        disappearsOnIndex={-1}
        opacity={1}
        pressBehavior="close"
        style={[props.style, { backgroundColor: colors.scrim }]}
      />
    ),
    [colors.scrim],
  );

  const renderHandle = useCallback(
    () => (
      <View testID="sheet-handle" collapsable={false}>
        <View
          testID="sheet-grabber"
          aria-hidden
          style={{ alignSelf: 'stretch', alignItems: 'center', paddingTop: spacing.sm }}
        >
          <View style={{ width: 36, height: 4, borderRadius: 2, backgroundColor: colors.border }} />
        </View>
        {hideHeader ? null : (
          <View
            testID="sheet-header"
            style={{
              flexDirection: 'row',
              alignItems: 'center',
              gap: spacing.sm,
              paddingLeft: spacing.lg,
              paddingRight: spacing.xs,
              paddingTop: spacing.xs,
              paddingBottom: spacing.xs,
              borderBottomWidth: 1,
              borderBottomColor: colors.border,
            }}
          >
            <AppText
              variant="heading"
              style={{ flex: 1 }}
              accessibilityRole="header"
              numberOfLines={1}
            >
              {title}
            </AppText>
            <IconButton icon={X} label="Close" onPress={close} testID="sheet-close" />
          </View>
        )}
      </View>
    ),
    [hideHeader, title, close, spacing, colors.border],
  );

  const renderFooter = useCallback(
    (props: BottomSheetFooterProps) => (
      <BottomSheetFooter {...props}>
        <View
          onLayout={(e) => setFooterHeight(Math.round(e.nativeEvent.layout.height))}
          style={{
            padding: spacing.lg,
            paddingBottom: spacing.lg + insets.bottom,
            borderTopWidth: 1,
            borderTopColor: colors.border,
            backgroundColor: colors.surfaceRaised,
            gap: spacing.sm,
          }}
        >
          {footer}
        </View>
      </BottomSheetFooter>
    ),
    [footer, spacing, insets.bottom, colors.border, colors.surfaceRaised],
  );

  const content = visible ? children : kept;
  const body = scroll ? (
    <BottomSheetScrollView
      testID={testID ? `${testID}-scroll` : undefined}
      contentContainerStyle={[{ padding: spacing.lg, gap: spacing.md }, contentStyle]}
      keyboardShouldPersistTaps="handled"
    >
      {content}
      <View style={{ height: bottomPad }} />
    </BottomSheetScrollView>
  ) : (
    <BottomSheetView style={fixed ? { flex: 1 } : undefined}>
      {content}
      <View style={{ height: bottomPad }} />
    </BottomSheetView>
  );

  return (
    <BottomSheetModal
      ref={modal}
      // A sheet opened from a sheet sits on top of it, as stacked Modals did.
      stackBehavior="push"
      index={index}
      {...(fixed
        ? { snapPoints: fixed, enableDynamicSizing: false }
        : { enableDynamicSizing: true, maxDynamicContentSize: Math.min(height * 0.9, room) })}
      enablePanDownToClose
      gestureEventsHandlersHook={useCloseOnPull}
      // A vertical drag moves the sheet; a sideways one belongs to the
      // content (the card's pager, the game log table).
      activeOffsetY={[-6, 6]}
      failOffsetX={[-24, 24]}
      onDismiss={onDismiss}
      onChange={onChange}
      backdropComponent={renderBackdrop}
      handleComponent={renderHandle}
      footerComponent={footer ? renderFooter : undefined}
      backgroundStyle={{
        backgroundColor: colors.surfaceRaised,
        borderTopLeftRadius: radii.sheet,
        borderTopRightRadius: radii.sheet,
        borderWidth: 1,
        borderBottomWidth: 0,
        borderColor: colors.border,
      }}
      style={{ maxWidth: 640, width: '100%', alignSelf: 'center' }}
      keyboardBehavior="interactive"
      keyboardBlurBehavior="restore"
      android_keyboardInputMode="adjustResize"
      overrideReduceMotion={reduced ? ReduceMotion.Always : ReduceMotion.System}
      accessible={false}
    >
      <ContextBridge value={screen}>
        <SheetCloseContext.Provider value={close}>
          <SheetTextInputContext.Provider value={renderSheetInput}>
            <View
              testID={testID}
              // Not aria-modal: on iOS that hides the grabber and header ✕
              // (gorhom draws them beside the content) from VoiceOver.
              role="dialog"
              aria-label={title}
              collapsable={false}
              style={fixed ? { flex: 1 } : undefined}
            >
              {body}
              <View
                pointerEvents="box-none"
                style={[StyleSheet.absoluteFill, { bottom: footer ? footerHeight : insets.bottom }]}
              >
                {overlay}
              </View>
            </View>
          </SheetTextInputContext.Provider>
        </SheetCloseContext.Provider>
      </ContextBridge>
    </BottomSheetModal>
  );
}
