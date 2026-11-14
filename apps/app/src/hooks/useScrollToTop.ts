import { useEffect, useRef, type RefObject } from 'react';
import type { ScrollView } from 'react-native';

/**
 * Scrolls `scrollRef` to the top when asked, and again once the content
 * asked for has rendered: Safari drops a smooth scroll when the content
 * resizes under it (a placeholder swapping for the chosen matchup
 * mid-scroll), leaving the screen scrolled down. `ready` is false while a
 * placeholder still shows; `content` changes when the asked-for content
 * renders.
 *
 * The returned function takes whether the content asked for already shows:
 * then the one scroll is enough, and a later change to `content` (a live
 * score update) never scrolls the reader back up unasked.
 */
export function useScrollToTop(
  scrollRef: RefObject<ScrollView | null>,
  ready: boolean,
  content: unknown,
): (shown: boolean) => void {
  const pending = useRef(false);
  useEffect(() => {
    if (!pending.current || !ready) return;
    pending.current = false;
    scrollRef.current?.scrollTo({ y: 0, animated: true });
  }, [scrollRef, ready, content]);
  return (shown) => {
    pending.current = !shown;
    scrollRef.current?.scrollTo({ y: 0, animated: true });
  };
}
