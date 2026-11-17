import { createContext, useContext } from 'react';
import type { ScrollView, View } from 'react-native';

/** Closes the sheet around it: slides a bottom sheet down first. */
export const SheetCloseContext = createContext<(() => void) | null>(null);

/** The close of the sheet around the caller (its animated close), else `fallback`. */
export function useSheetClose(fallback?: () => void): () => void {
  const close = useContext(SheetCloseContext);
  return close ?? fallback ?? (() => undefined);
}

/** Scrolls the sheet's own scroll view so `node` sits near its top (a row to point at). */
export type ScrollIntoView = (node: View | null) => void;

export const SheetScrollContext = createContext<ScrollIntoView | null>(null);

/** The enclosing sheet's scroll-into-view; null outside a sheet. */
export const useSheetScrollIntoView = () => useContext(SheetScrollContext);

/**
 * Scrolls `scroller` so `node` (a descendant) sits a little below its top.
 * Does nothing when either is missing or the node cannot be measured.
 */
export function scrollNodeIntoView(scroller: ScrollView | null, node: View | null, inset = 160) {
  // measureLayout needs the inner view's host ref: on iOS and Android
  // getInnerViewNode() is a numeric tag, which measureLayout ignores.
  // getInnerViewRef exists on both platforms but is missing from the types.
  const withRef = scroller as (ScrollView & { getInnerViewRef?: () => View | null }) | null;
  const inner = withRef?.getInnerViewRef?.() ?? null;
  if (!scroller || !node || !inner) return;
  node.measureLayout(
    inner,
    (_x, y) => scroller.scrollTo({ y: Math.max(0, y - inset), animated: true }),
    () => undefined,
  );
}
