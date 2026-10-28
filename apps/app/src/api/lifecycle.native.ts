import { focusManager, onlineManager } from '@tanstack/react-query';
import { addNetworkStateListener, getNetworkStateAsync } from 'expo-network';
import { AppState, type AppStateStatus } from 'react-native';

import { flushQueryCache } from './persist';

/**
 * Native: tell TanStack Query when the app comes to the foreground (refetch
 * stale queries, like a browser tab regaining focus) and when the device
 * goes offline or back online (pause and resume fetching). Leaving the
 * foreground also saves the query cache at once: iOS may kill a background
 * app before the persister's throttle fires.
 */
export function setupQueryLifecycle(): () => void {
  focusManager.setEventListener((setFocused) => {
    const sub = AppState.addEventListener('change', (status: AppStateStatus) => {
      if (status !== 'active') void flushQueryCache();
      setFocused(status === 'active');
    });
    return () => sub.remove();
  });
  onlineManager.setEventListener((setOnline) => {
    const toOnline = (state: { isConnected?: boolean; isInternetReachable?: boolean }) =>
      setOnline(state.isConnected !== false && state.isInternetReachable !== false);
    void Promise.resolve(getNetworkStateAsync()).then(
      (s) => s && toOnline(s),
      () => {},
    );
    const sub = addNetworkStateListener(toOnline);
    // Optional chaining: the module is auto-mocked (returns undefined) under jest-expo.
    return () => sub?.remove?.();
  });
  return () => {
    focusManager.setEventListener(() => () => {});
    onlineManager.setEventListener(() => () => {});
  };
}
