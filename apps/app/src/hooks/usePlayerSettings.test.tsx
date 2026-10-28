/**
 * The Players View/Available and sort are kept per league: switching league
 * while the screen stays mounted shows the new league's stored settings,
 * never the last league's.
 */
import AsyncStorage from '@react-native-async-storage/async-storage';
import { renderHook, waitFor } from '@testing-library/react-native';

import { usePlayerSort } from './usePlayerSort';
import { usePlayerView } from './usePlayerView';
import { sortStorageKey } from '@/lib/playerSort';
import { viewStorageKey } from '@/lib/playerView';

beforeEach(async () => {
  await AsyncStorage.clear();
});

describe('usePlayerView', () => {
  it("league B shows its own stored View and Available, not league A's", async () => {
    await AsyncStorage.setItem(
      viewStorageKey('lg_A'),
      JSON.stringify({ view: { kind: 'position', position: 'DEF' }, available: false }),
    );
    await AsyncStorage.setItem(
      viewStorageKey('lg_B'),
      JSON.stringify({ view: { kind: 'position', position: 'FWD' }, available: true }),
    );
    const { result, rerender } = await renderHook(({ id }: { id: string }) => usePlayerView(id), {
      initialProps: { id: 'lg_A' },
    });
    await waitFor(() => expect(result.current[2]).toBe(true));
    expect(result.current[0]).toEqual({
      view: { kind: 'position', position: 'DEF' },
      available: false,
    });
    await rerender({ id: 'lg_B' });
    await waitFor(() => expect(result.current[2]).toBe(true));
    expect(result.current[0]).toEqual({
      view: { kind: 'position', position: 'FWD' },
      available: true,
    });
  });

  it('a league with nothing stored starts on the default after another league', async () => {
    await AsyncStorage.setItem(
      viewStorageKey('lg_A'),
      JSON.stringify({ view: { kind: 'position', position: 'DEF' }, available: false }),
    );
    const { result, rerender } = await renderHook(({ id }: { id: string }) => usePlayerView(id), {
      initialProps: { id: 'lg_A' },
    });
    await waitFor(() => expect(result.current[0].available).toBe(false));
    await rerender({ id: 'lg_B' });
    await waitFor(() => expect(result.current[2]).toBe(true));
    expect(result.current[0]).toEqual({ view: { kind: 'all' }, available: true });
  });
});

describe('usePlayerSort', () => {
  it("league B shows its own stored sort, not league A's", async () => {
    await AsyncStorage.setItem(sortStorageKey('lg_A'), JSON.stringify({ sort: 'form' }));
    const { result, rerender } = await renderHook(({ id }: { id: string }) => usePlayerSort(id), {
      initialProps: { id: 'lg_A' },
    });
    await waitFor(() => expect(result.current[0].sort).toBe('form'));
    await rerender({ id: 'lg_B' });
    await waitFor(() => expect(result.current[2]).toBe(true));
    expect(result.current[0]).toEqual({ sort: 'total_points' });
  });
});
