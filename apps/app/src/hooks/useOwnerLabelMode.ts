import AsyncStorage from '@react-native-async-storage/async-storage';
import { useCallback, useEffect, useSyncExternalStore } from 'react';

import {
  DEFAULT_OWNER_LABEL,
  OWNER_LABEL_KEY,
  parseOwnerLabel,
  type OwnerLabelMode,
} from '@/lib/owner-label';

/**
 * "Show owners as" (Account → Settings), kept on the device. One value for
 * every owner chip on screen: a change in settings shows everywhere at
 * once. Starts on the default until the stored choice is read; a pick made
 * before then wins.
 */
let mode: OwnerLabelMode = DEFAULT_OWNER_LABEL;
let read: Promise<void> | null = null;
let edited = false;
const listeners = new Set<() => void>();

function emit() {
  listeners.forEach((l) => l());
}

function load() {
  read ??= AsyncStorage.getItem(OWNER_LABEL_KEY).then(
    (raw) => {
      if (edited) return;
      mode = parseOwnerLabel(raw);
      emit();
    },
    () => {},
  );
}

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}

const snapshot = () => mode;

export function setOwnerLabelMode(next: OwnerLabelMode) {
  edited = true;
  mode = next;
  emit();
  AsyncStorage.setItem(OWNER_LABEL_KEY, JSON.stringify(next)).catch(() => {});
}

export function useOwnerLabelMode(): [OwnerLabelMode, (next: OwnerLabelMode) => void] {
  useEffect(load, []);
  const value = useSyncExternalStore(subscribe, snapshot, snapshot);
  const set = useCallback((next: OwnerLabelMode) => setOwnerLabelMode(next), []);
  return [value, set];
}

/** Tests only: forget the held value so the next hook reads storage again. */
export function resetOwnerLabelModeForTests() {
  mode = DEFAULT_OWNER_LABEL;
  read = null;
  edited = false;
}
