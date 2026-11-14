/**
 * A pick in the matchup list scrolls the screen to the top, and once more
 * when the picked matchup renders (Safari drops the first scroll). Picking
 * the matchup already open must not leave a scroll pending for the next
 * live-score update.
 */
import { renderHook } from '@testing-library/react-native';
import type { ScrollView } from 'react-native';

import { useScrollToTop } from './useScrollToTop';

function setup() {
  const scrollTo = jest.fn();
  const ref = { current: { scrollTo } as unknown as ScrollView };
  return { scrollTo, ref };
}

describe('useScrollToTop', () => {
  it('re-picking the open matchup does not scroll on a later data update', async () => {
    const { scrollTo, ref } = setup();
    const { result, rerender } = await renderHook(
      ({ ready, content }: { ready: boolean; content: object }) =>
        useScrollToTop(ref, ready, content),
      { initialProps: { ready: true, content: { id: 'a', score: 1 } } },
    );
    result.current(true);
    expect(scrollTo).toHaveBeenCalledTimes(1);
    await rerender({ ready: true, content: { id: 'a', score: 2 } });
    expect(scrollTo).toHaveBeenCalledTimes(1);
  });

  it('picking another matchup scrolls again once it renders, then not on updates', async () => {
    const { scrollTo, ref } = setup();
    const { result, rerender } = await renderHook(
      ({ ready, content }: { ready: boolean; content: object }) =>
        useScrollToTop(ref, ready, content),
      { initialProps: { ready: true, content: { id: 'a' } } },
    );
    result.current(false);
    expect(scrollTo).toHaveBeenCalledTimes(1);
    // The last matchup still shows as placeholder: wait.
    await rerender({ ready: false, content: { id: 'a' } });
    expect(scrollTo).toHaveBeenCalledTimes(1);
    await rerender({ ready: true, content: { id: 'b' } });
    expect(scrollTo).toHaveBeenCalledTimes(2);
    await rerender({ ready: true, content: { id: 'b', score: 3 } });
    expect(scrollTo).toHaveBeenCalledTimes(2);
  });
});
