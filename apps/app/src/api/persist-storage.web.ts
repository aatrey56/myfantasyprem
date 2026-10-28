import { flushablePersister, type FlushablePersister } from './persist-throttle';

/**
 * Web: localStorage. Missing storage (private mode, a blocked origin)
 * means no persistence: the persister is a no-op without it.
 */
export function cacheStorage(key: string): FlushablePersister {
  let storage: Storage | undefined;
  try {
    storage = globalThis.localStorage;
  } catch {
    storage = undefined;
  }
  return flushablePersister(storage, key, 1_000);
}
