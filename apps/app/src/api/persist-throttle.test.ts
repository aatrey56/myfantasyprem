import type { PersistedClient } from '@tanstack/react-query-persist-client';

import { flushablePersister } from './persist-throttle';

const snapshot = (n: number): PersistedClient => ({
  buster: 'b',
  timestamp: n,
  clientState: { mutations: [], queries: [] },
});

function memoryStore() {
  const items = new Map<string, string>();
  const writes: number[] = [];
  return {
    writes,
    getItem: async (k: string) => items.get(k) ?? null,
    setItem: async (k: string, v: string) => {
      writes.push((JSON.parse(v) as PersistedClient).timestamp);
      items.set(k, v);
    },
    removeItem: async (k: string) => {
      items.delete(k);
    },
  };
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

describe('flushablePersister', () => {
  it('writes at most once per throttle, always the latest snapshot', async () => {
    const store = memoryStore();
    const p = flushablePersister(store, 'k', 200);
    await p.persistClient(snapshot(1));
    void p.persistClient(snapshot(2));
    void p.persistClient(snapshot(3));
    await sleep(50);
    // Inside the 200 ms throttle: only the first write so far.
    expect(store.writes).toEqual([1]);
    await sleep(250);
    // 2 was replaced by 3 before its turn came.
    expect(store.writes).toEqual([1, 3]);
  });

  it('flush writes the waiting snapshot at once', async () => {
    const store = memoryStore();
    const p = flushablePersister(store, 'k', 10_000);
    await p.persistClient(snapshot(1));
    void p.persistClient(snapshot(2));
    await p.flush();
    expect(store.writes).toEqual([1, 2]);
    expect((await p.restoreClient())?.timestamp).toBe(2);
  });

  it('a flush with nothing waiting writes nothing', async () => {
    const store = memoryStore();
    const p = flushablePersister(store, 'k', 10_000);
    await p.flush();
    expect(store.writes).toEqual([]);
  });

  it('a removal drops the waiting snapshot, so it cannot bring the cache back', async () => {
    const store = memoryStore();
    const p = flushablePersister(store, 'k', 100);
    await p.persistClient(snapshot(1));
    void p.persistClient(snapshot(2));
    await p.removeClient();
    await sleep(150);
    expect(store.writes).toEqual([1]);
    expect(await p.restoreClient()).toBeUndefined();
  });
});
