import { createContext, useContext, useMemo, useState, type ReactNode } from 'react';
import { Animated, PanResponder, View } from 'react-native';

import { useReducedMotion } from '../Skeleton';

/** How far a swipe must travel, and how much more sideways than down, to turn a page. */
export const SWIPE_DISTANCE = 56;
const SIDEWAYS = 1.5;

/** A horizontal drag, clearly more sideways than vertical (not a vertical scroll). */
export const isSideways = (dx: number, dy: number) =>
  Math.abs(dx) > 12 && Math.abs(dx) > Math.abs(dy) * SIDEWAYS;

/**
 * The page a finished swipe lands on: a left swipe goes to the next one, a
 * right swipe to the previous; short swipes and the ends stay put.
 */
export function swipeTarget<T>(pages: readonly T[], current: T, dx: number, dy: number): T {
  const at = pages.indexOf(current);
  if (at < 0 || Math.abs(dx) < SWIPE_DISTANCE || !isSideways(dx, dy)) return current;
  const next = at + (dx < 0 ? 1 : -1);
  return pages[Math.max(0, Math.min(pages.length - 1, next))]!;
}

/**
 * Lets a horizontal scroller inside a page keep its own gesture: it calls
 * `claim` when a touch starts in it, and the pager leaves that touch alone
 * (the usual nested-scroll rule: the inner scroller wins).
 */
const NestedScroll = createContext<() => void>(() => {});

/** Spread on a horizontal scroller inside a SwipePager page. */
export function useNestedScroll() {
  const claim = useContext(NestedScroll);
  return { onTouchStart: claim };
}

/**
 * Page content that turns with a sideways swipe, like the league's top
 * tabs: the page follows the finger, then changes on release. Vertical
 * drags (scrolling the card, pulling the sheet down) pass through.
 */
export function SwipePager<T extends string>({
  pages,
  value,
  onChange,
  children,
  testID,
}: {
  pages: readonly T[];
  value: T;
  onChange: (page: T) => void;
  children: ReactNode;
  testID?: string;
}) {
  const reduced = useReducedMotion();
  const [dx] = useState(() => new Animated.Value(0));
  // Plain mutable state for the current touch (read only in gesture handlers).
  const [touch] = useState(() => {
    let claimed = false;
    return { claimed: () => claimed, claim: (on: boolean) => void (claimed = on) };
  });
  const responder = useMemo(
    () =>
      PanResponder.create({
        onMoveShouldSetPanResponder: (_e, g) => !touch.claimed() && isSideways(g.dx, g.dy),
        onPanResponderMove: (_e, g) => {
          // Resist at the ends: no page past the first or last.
          const at = pages.indexOf(value);
          const edge = (g.dx > 0 && at === 0) || (g.dx < 0 && at === pages.length - 1);
          dx.setValue(edge ? g.dx / 4 : g.dx);
        },
        onPanResponderRelease: (_e, g) => {
          const to = swipeTarget(pages, value, g.dx, g.dy);
          if (to !== value) onChange(to);
          dx.setValue(0);
        },
        onPanResponderTerminate: () => dx.setValue(0),
        onPanResponderTerminationRequest: () => true,
      }),
    [pages, value, onChange, touch, dx],
  );
  return (
    <NestedScroll.Provider
      value={() => {
        touch.claim(true);
      }}
    >
      <View
        testID={testID}
        {...responder.panHandlers}
        // Every new touch starts unclaimed; a nested scroller claims it in its own onTouchStart (which runs first).
        onTouchEndCapture={() => {
          touch.claim(false);
        }}
        onTouchCancel={() => {
          touch.claim(false);
        }}
      >
        <Animated.View style={reduced ? null : { transform: [{ translateX: dx }] }}>
          {children}
        </Animated.View>
      </View>
    </NestedScroll.Provider>
  );
}
