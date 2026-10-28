/**
 * The bottom sheet's drag maths, apart from the gesture so it can be tested
 * (and run as worklets on the UI thread).
 *
 * A sheet's position is its `offset`: how far the panel is pushed down from
 * fully open, in px. A snap point is a height the sheet rests at; its offset
 * is `panelHeight - snapHeight`. Closed is `offset === panelHeight`.
 */

/** A resting height: px, or a share of the room the sheet may fill (`'50%'`). */
export type SnapPoint = number | `${number}%`;

/** How far past the lowest snap (px), or how fast down (px/s), closes a sheet. */
export const DISMISS_DISTANCE = 120;
export const DISMISS_VELOCITY = 1200;

/** How far ahead a fling's speed carries the sheet when picking a snap (s). */
const PROJECTION = 0.15;

/** Resolves snap points to px heights within `room`, lowest first, without repeats. */
export function snapHeights(points: readonly SnapPoint[], room: number): number[] {
  const px = points.map((p) => {
    const h = typeof p === 'number' ? p : (parseFloat(p) / 100) * room;
    return Math.round(Math.min(Math.max(h, 0), room));
  });
  return [...new Set(px)].filter((h) => h > 0).sort((a, b) => a - b);
}

/** The offset at which the sheet shows `height` of a `panelHeight` panel. */
export function snapOffset(height: number, panelHeight: number): number {
  'worklet';
  return Math.max(0, panelHeight - height);
}

/**
 * Where a drag leaves the sheet: `offset` is the start offset plus the drag.
 * Pulled above the top it resists (a third of the finger's travel); below
 * the panel it stops.
 */
export function dragOffset(offset: number, panelHeight: number): number {
  'worklet';
  if (offset < 0) return offset / 3;
  return Math.min(offset, panelHeight);
}

/**
 * Where the sheet goes when the finger lifts at `offset` moving at
 * `velocity` (px/s, down positive): the snap nearest to where the fling
 * would carry it, or closed (-1) when it is pulled or flung well below its
 * lowest snap.
 */
export function settle(
  offset: number,
  velocity: number,
  heights: readonly number[],
  panelHeight: number,
): number {
  'worklet';
  if (heights.length === 0) return -1;
  const lowest = snapOffset(heights[0]!, panelHeight);
  // Short sheets close after a shorter pull: half their height at most.
  const distance = Math.min(DISMISS_DISTANCE, heights[0]! / 2);
  if (offset - lowest > distance) return -1;
  if (velocity > DISMISS_VELOCITY && offset >= lowest - 1) return -1;
  const projected = offset + velocity * PROJECTION;
  let best = 0;
  let gap = Infinity;
  for (let i = 0; i < heights.length; i += 1) {
    const d = Math.abs(snapOffset(heights[i]!, panelHeight) - projected);
    if (d < gap) {
      gap = d;
      best = i;
    }
  }
  return best;
}

/**
 * Whether a pull on a gorhom sheet closes it when the finger lifts: the sheet
 * moved `below` px under its lowest snap (which shows `lowestHeight` px),
 * moving down at `velocity` px/s. The hand-written sheet's rule (settle):
 * a pull past DISMISS_DISTANCE (half a short sheet at most) or a fling down,
 * not gorhom's own "past halfway", which springs a tall card back.
 */
export function pullCloses(below: number, velocity: number, lowestHeight: number): boolean {
  'worklet';
  if (below <= 0) return false;
  return below > Math.min(DISMISS_DISTANCE, lowestHeight / 2) || velocity > DISMISS_VELOCITY;
}

/**
 * The backdrop's opacity at `offset`: full while the sheet rests at or above
 * its lowest snap, fading to nothing as it is pulled closed.
 */
export function backdropOpacity(offset: number, lowestOffset: number, panelHeight: number): number {
  'worklet';
  const span = panelHeight - lowestOffset;
  if (span <= 0) return 1;
  return Math.min(1, Math.max(0, 1 - (offset - lowestOffset) / span));
}

/**
 * How long an open sheet waits for its panel to lay out before it closes
 * itself (ms). A Modal whose panel never appears is an invisible layer over
 * the whole app that takes every touch. Jest has no layout pass, so its
 * setup turns this off (`Infinity`) except in the tests that check it.
 */
export const PANEL_LAYOUT_TIMEOUT = { ms: 500 };

/**
 * Phones on iOS and Android draw sheets with @gorhom/bottom-sheet
 * (NativeSheet). Jest turns it off: its suites drive the hand-written sheet
 * that web and wide layouts still use (NativeSheet.test.tsx turns it on).
 */
export const NATIVE_SHEET = { enabled: true };
