import {
  removeOldestQuery,
  type PersistedClient,
  type Persister,
} from '@tanstack/react-query-persist-client';

/** AsyncStorage or localStorage: either may answer synchronously. */
export interface CacheStore {
  getItem(key: string): string | null | Promise<string | null>;
  setItem(key: string, value: string): unknown;
  removeItem(key: string): unknown;
}

/** A persister whose throttled write can be forced out now. */
export interface FlushablePersister extends Persister {
  /**
   * Writes the latest snapshot still waiting on the throttle at once and
   * resolves when every write so far has landed. Nothing waiting: resolves
   * after the write in flight, if any. Never writes an empty cache of its
   * own accord (a flush before the restore finished is a no-op).
   */
  flush(): Promise<void>;
}

/**
 * Saves the query cache at most once per `throttleMs`, always the latest
 * snapshot, one write at a time and in order. Unlike TanStack's own
 * throttled persisters, the waiting write can be flushed: the app does so
 * when it leaves the foreground and when a mutation settles, because iOS
 * may kill a backgrounded app (and a test harness quits it) before the
 * throttle fires, losing the user's last change.
 */
export function flushablePersister(
  store: CacheStore | undefined,
  key: string,
  throttleMs: number,
): FlushablePersister {
  if (!store) {
    return {
      persistClient: () => {},
      restoreClient: () => Promise.resolve(undefined),
      removeClient: () => {},
      flush: () => Promise.resolve(),
    };
  }

  let pending: PersistedClient | null = null;
  /** Callers whose snapshot (or a newer one) is waiting to be written. */
  let waiters: (() => void)[] = [];
  let timer: ReturnType<typeof setTimeout> | null = null;
  let lastWriteAt = -Infinity;
  /** Every write and removal, chained so they land in the order made. */
  let chain: Promise<void> = Promise.resolve();

  const save = async (client: PersistedClient) => {
    let next: PersistedClient | undefined = client;
    let errorCount = 0;
    while (next) {
      try {
        await store.setItem(key, JSON.stringify(next));
        return;
      } catch (error) {
        errorCount += 1;
        // Out of space: drop the oldest query and try again.
        next = removeOldestQuery({ persistedClient: next, error: error as Error, errorCount });
      }
    }
  };

  const clearTimer = () => {
    if (timer !== null) clearTimeout(timer);
    timer = null;
  };

  const flush = (): Promise<void> => {
    clearTimer();
    const client = pending;
    const done = waiters;
    pending = null;
    waiters = [];
    if (client) {
      lastWriteAt = Date.now();
      chain = chain.then(() => save(client)).catch(() => undefined);
    }
    void chain.then(() => done.forEach((resolve) => resolve()));
    return chain;
  };

  return {
    /** Resolves once this snapshot, or a newer one, has been written. */
    persistClient: (client) =>
      new Promise<void>((resolve) => {
        pending = client;
        waiters.push(resolve);
        if (timer !== null) return;
        const wait = Math.max(0, lastWriteAt + throttleMs - Date.now());
        timer = setTimeout(() => void flush(), wait);
      }),
    restoreClient: async () => {
      const raw = await store.getItem(key);
      return raw ? (JSON.parse(raw) as PersistedClient) : undefined;
    },
    removeClient: () => {
      // A snapshot still waiting must not bring the removed cache back.
      clearTimer();
      pending = null;
      const done = waiters;
      waiters = [];
      done.forEach((resolve) => resolve());
      chain = chain.then(() => store.removeItem(key) as Promise<void>).catch(() => undefined);
      return chain;
    },
    flush,
  };
}
