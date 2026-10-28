import AsyncStorage from '@react-native-async-storage/async-storage';
import { onlineManager, QueryClient, QueryObserver } from '@tanstack/react-query';
import {
  persistQueryClientSave,
  persistQueryClientSubscribe,
} from '@tanstack/react-query-persist-client';
import { AppState, type AppStateStatus } from 'react-native';

import { setupQueryLifecycle } from './lifecycle.native';
import { CACHE_STORAGE_KEY, persistOptions } from './persist';
import { createQueryClient, DEFAULT_STALE_MS } from './query-client';

type NetworkListener = (s: { isConnected?: boolean; isInternetReachable?: boolean }) => void;
let mockNetworkListener: NetworkListener | undefined;
jest.mock('expo-network', () => ({
  getNetworkStateAsync: () => Promise.resolve({ isConnected: true, isInternetReachable: true }),
  addNetworkStateListener: (l: NetworkListener) => {
    mockNetworkListener = l;
    return { remove: () => {} };
  },
}));

const LEAGUE = ['league', 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy'] as const;
const CLUBS = ['clubs'] as const;
const env = (n: number) => ({ data: { n }, meta: {} });

let appStateListener: ((s: AppStateStatus) => void) | undefined;
let client: QueryClient;
let teardown: () => void;
const unsubscribes: (() => void)[] = [];
const fetched: string[] = [];

/** A screen observing `key`, the way useQuery would. */
function observe(key: readonly string[], name: string) {
  const observer = new QueryObserver(client, {
    queryKey: key,
    queryFn: async () => {
      fetched.push(name);
      return env(2);
    },
  });
  unsubscribes.push(observer.subscribe(() => {}));
}

const flush = () => new Promise((r) => setTimeout(r, 0));

beforeEach(() => {
  fetched.length = 0;
  jest.spyOn(AppState, 'addEventListener').mockImplementation((_type, listener) => {
    appStateListener = listener as (s: AppStateStatus) => void;
    return { remove: () => {} } as ReturnType<typeof AppState.addEventListener>;
  });
  teardown = setupQueryLifecycle();
  client = createQueryClient();
  client.mount();
});
afterEach(() => {
  unsubscribes.splice(0).forEach((u) => u());
  client.unmount();
  client.clear();
  teardown();
  jest.restoreAllMocks();
});

describe('app returns to the foreground', () => {
  it('refetches the stale active queries, not the fresh ones', async () => {
    client.setQueryData(LEAGUE, env(1));
    client.setQueryData(CLUBS, env(1));
    observe(LEAGUE, 'league');
    observe(CLUBS, 'clubs');
    await flush();
    expect(fetched).toEqual([]);
    // Time passes in the background: the league home goes stale, clubs keep.
    appStateListener?.('background');
    client.setQueryData(LEAGUE, env(1), { updatedAt: Date.now() - DEFAULT_STALE_MS - 1 });
    await flush();
    expect(fetched).toEqual([]);

    appStateListener?.('active');
    await flush();

    expect(fetched).toEqual(['league']);
    expect(client.getQueryData(LEAGUE)).toEqual(env(2));
  });
});

describe('app leaves the foreground', () => {
  const savedLeague = async () => {
    const raw = await AsyncStorage.getItem(CACHE_STORAGE_KEY);
    const queries = raw
      ? (
          JSON.parse(raw) as {
            clientState: { queries: { queryKey: unknown[]; state: { data: unknown } }[] };
          }
        ).clientState.queries
      : [];
    return queries.find((q) => q.queryKey[0] === 'league')?.state.data;
  };

  it.each(['background', 'inactive'] as const)(
    'saves the change waiting on the save throttle at once (%s)',
    async (status) => {
      await AsyncStorage.clear();
      client.setQueryData(LEAGUE, env(1));
      // A write has just landed: the next one would wait out the 1 s throttle.
      await persistQueryClientSave({ queryClient: client, ...persistOptions });
      const unsubscribe = persistQueryClientSubscribe({ queryClient: client, ...persistOptions });
      try {
        client.setQueryData(LEAGUE, env(2));
        appStateListener?.(status);
        // iOS may kill a backgrounded app at any moment: well inside the
        // throttle, the change must already be in storage.
        await new Promise((r) => setTimeout(r, 100));
        expect(await savedLeague()).toEqual(env(2));
      } finally {
        unsubscribe();
      }
    },
  );
});

describe('connectivity', () => {
  it('follows expo-network', () => {
    mockNetworkListener?.({ isConnected: false });
    expect(onlineManager.isOnline()).toBe(false);
    mockNetworkListener?.({ isConnected: true, isInternetReachable: true });
    expect(onlineManager.isOnline()).toBe(true);
  });
});
