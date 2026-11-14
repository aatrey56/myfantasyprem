/**
 * A list's column keeps the last list's label and value until the rows for
 * the new choice are in (Leaders Season → GW, Trend risers → fallers).
 */
import { renderHook } from '@testing-library/react-native';

import { useShownWith } from './useShownWith';

const same = (a: string, b: string) => a === b;

describe('useShownWith', () => {
  it('keeps the shown value while the new rows load, then follows', async () => {
    const { result, rerender } = await renderHook(
      ({ value, loaded }: { value: string; loaded: boolean }) => useShownWith(value, loaded, same),
      { initialProps: { value: 'season', loaded: true } },
    );
    expect(result.current).toBe('season');
    // GW picked: the season rows are still on screen (placeholder data).
    await rerender({ value: 'gw', loaded: false });
    expect(result.current).toBe('season');
    // The GW rows are in.
    await rerender({ value: 'gw', loaded: true });
    expect(result.current).toBe('gw');
  });

  it('starts with the first value, loaded or not', async () => {
    const { result } = await renderHook(() => useShownWith('trend', false, same));
    expect(result.current).toBe('trend');
  });
});
