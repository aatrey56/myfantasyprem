import AsyncStorage from '@react-native-async-storage/async-storage';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { resetOwnerLabelModeForTests, useOwnerLabelMode } from './useOwnerLabelMode';
import { OWNER_LABEL_KEY } from '@/lib/owner-label';

beforeEach(async () => {
  await AsyncStorage.clear();
  resetOwnerLabelModeForTests();
});

describe('useOwnerLabelMode', () => {
  it('starts on first names', async () => {
    const { result } = await renderHook(() => useOwnerLabelMode());
    expect(result.current[0]).toBe('first_name');
  });

  it('keeps a change on the device and reads it back after a restart', async () => {
    const { result } = await renderHook(() => useOwnerLabelMode());
    await act(async () => result.current[1]('team_name'));
    expect(result.current[0]).toBe('team_name');
    expect(await AsyncStorage.getItem(OWNER_LABEL_KEY)).toBe('"team_name"');

    resetOwnerLabelModeForTests();
    const again = await renderHook(() => useOwnerLabelMode());
    await waitFor(() => expect(again.result.current[0]).toBe('team_name'));
  });

  it('shows a change in every place at once', async () => {
    const a = await renderHook(() => useOwnerLabelMode());
    const b = await renderHook(() => useOwnerLabelMode());
    await act(async () => a.result.current[1]('team_name'));
    expect(b.result.current[0]).toBe('team_name');
  });
});
