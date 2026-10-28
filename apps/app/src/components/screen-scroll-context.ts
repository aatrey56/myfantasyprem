import { createContext, useContext } from 'react';

/** The screen's scroll view, for content that sizes itself to the viewport. */
export interface ScreenScroll {
  /** How far the content is scrolled now. */
  offset: () => number;
  /** Bumped when the content or the viewport changes size: re-measure then. */
  version: number;
}

export const ScreenScrollContext = createContext<ScreenScroll | null>(null);

/** The enclosing Screen's scroll view; null outside one (or when it doesn't scroll). */
export const useScreenScroll = () => useContext(ScreenScrollContext);
