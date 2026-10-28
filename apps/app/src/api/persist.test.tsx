import AsyncStorage from '@react-native-async-storage/async-storage';
import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import {
  PersistQueryClientProvider,
  persistQueryClientRestore,
  persistQueryClientSave,
  persistQueryClientSubscribe,
} from '@tanstack/react-query-persist-client';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useDeleteAccount } from './account-hooks';
import { queryKeys } from './hooks';
import {
  CACHE_BUSTER,
  CACHE_SCHEMA,
  CACHE_STORAGE_KEY,
  PERSIST_MAX_AGE_MS,
  persistOptions,
  setupPersistFlush,
  shouldPersistKey,
} from './persist';
import { cacheStorage } from './persist-storage';
import { createQueryClient as newClient, DEFAULT_STALE_MS, staleTimeFor } from './query-client';
import { AuthProvider, useAuth } from '@/auth/AuthProvider';
import { memoryPendingRevocationStore } from '@/auth/revocation';
import { memorySessionStore, tokens, type StoredSession } from '@/auth/session';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({ useIsFocused: () => true }));
jest.mock('@/auth/providers', () => ({}));
jest.mock('expo-constants', () => ({ expoConfig: { version: '1.2.3' } }));

/** Every client made here, cleared after each test: a day-long gcTime timer would keep Jest alive. */
const clients: QueryClient[] = [];
const createQueryClient = () => {
  const c = newClient();
  clients.push(c);
  return c;
};
afterEach(() => clients.splice(0).forEach((c) => c.clear()));

const LG = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';
const env = (n: number) => ({ data: { n }, meta: {} });

const session: StoredSession = {
  accessToken: 'a1',
  refreshToken: 'r1',
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: { id: 'usr_a', displayName: 'Sam' } as StoredSession['user'],
};

const saved = async () => {
  const raw = await AsyncStorage.getItem(CACHE_STORAGE_KEY);
  return raw === null
    ? null
    : (JSON.parse(raw) as { buster: string; clientState: { queries: { queryKey: unknown[] }[] } });
};
const savedKeys = async () => (await saved())?.clientState.queries.map((q) => q.queryKey) ?? [];

/** What a previous run left in storage: a league home and the profile. */
async function seedStorage() {
  const before = createQueryClient();
  before.setQueryData(queryKeys.league(LG), env(1));
  await persistQueryClientSave({ queryClient: before, ...persistOptions });
  expect(await savedKeys()).toEqual([queryKeys.league(LG)]);
}

beforeEach(async () => {
  await AsyncStorage.clear();
  tokens.set(null);
});

describe('what is saved', () => {
  it('skips the session, server config, lookups and searches; keeps league data', () => {
    expect(shouldPersistKey(queryKeys.me)).toBe(false);
    expect(shouldPersistKey(['config'])).toBe(false);
    expect(shouldPersistKey(queryKeys.invite('ABC123'))).toBe(false);
    expect(shouldPersistKey(queryKeys.importPreview({ fplLeagueId: 1 }))).toBe(false);
    expect(shouldPersistKey(queryKeys.import('imp_1'))).toBe(false);
    expect(shouldPersistKey(queryKeys.players(LG, { q: 'saka' }))).toBe(false);

    expect(shouldPersistKey(queryKeys.myLeagues)).toBe(true);
    expect(shouldPersistKey(queryKeys.league(LG))).toBe(true);
    expect(shouldPersistKey(queryKeys.matchups(LG, 7))).toBe(true);
    expect(shouldPersistKey(queryKeys.players(LG, { status: 'available' }))).toBe(true);
    expect(shouldPersistKey(queryKeys.players(LG, { q: '' }))).toBe(true);
    expect(shouldPersistKey(queryKeys.clubs)).toBe(true);
  });

  it('writes only successful answers of the kept families', async () => {
    const client = createQueryClient();
    client.setQueryData(queryKeys.league(LG), env(1));
    client.setQueryData(queryKeys.me, env(2));
    client.setQueryData(['config'], env(3));
    client.setQueryData(queryKeys.players(LG, { q: 'sak' }), env(4));
    // A failed query: never saved.
    await client
      .fetchQuery({
        queryKey: queryKeys.standings(LG),
        queryFn: () => Promise.reject(new Error('x')),
        retry: false,
      })
      .catch(() => undefined);

    await persistQueryClientSave({ queryClient: client, ...persistOptions });
    expect(await savedKeys()).toEqual([queryKeys.league(LG)]);
  });

  it('stores under a key of its own, so mock data never paints in a real build', () => {
    // Jest runs without EXPO_PUBLIC_USE_MOCKS.
    expect(CACHE_STORAGE_KEY).toBe('mfp.queryCache');
  });
});

describe('restoring', () => {
  it('brings back a saved answer, marked with its age so it revalidates', async () => {
    await seedStorage();
    const after = createQueryClient();
    await persistQueryClientRestore({ queryClient: after, ...persistOptions });
    expect(after.getQueryData(queryKeys.league(LG))).toEqual(env(1));
  });

  it('throws the cache away for a new app version or cache schema', async () => {
    expect(CACHE_BUSTER).toBe(`1.2.3:${CACHE_SCHEMA}`);
    await seedStorage();
    const after = createQueryClient();
    await persistQueryClientRestore({
      queryClient: after,
      ...persistOptions,
      buster: `1.2.4:${CACHE_SCHEMA}`,
    });
    expect(after.getQueryData(queryKeys.league(LG))).toBeUndefined();
    expect(await saved()).toBeNull();
  });

  it('throws away a cache older than a day', async () => {
    expect(PERSIST_MAX_AGE_MS).toBe(24 * 60 * 60_000);
    await seedStorage();
    const now = Date.now();
    jest.spyOn(Date, 'now').mockReturnValue(now + PERSIST_MAX_AGE_MS + 1);
    try {
      const after = createQueryClient();
      await persistQueryClientRestore({ queryClient: after, ...persistOptions });
      expect(after.getQueryData(queryKeys.league(LG))).toBeUndefined();
    } finally {
      jest.restoreAllMocks();
    }
  });
});

describe('a cold start', () => {
  /** Saves a league home `age` ms old, then mounts a screen reading it under the persisting provider. */
  async function launch(age: number) {
    // A storage key per launch: an earlier test's throttled save cannot land in this one.
    const options = { ...persistOptions, persister: cacheStorage(`${CACHE_STORAGE_KEY}.${age}`) };
    const before = createQueryClient();
    before.setQueryData(queryKeys.league(LG), env(1), { updatedAt: Date.now() - age });
    await persistQueryClientSave({ queryClient: before, ...options });

    const queryClient = createQueryClient();
    const painted: unknown[] = [];
    let fetches = 0;
    const wrapper = ({ children }: { children: ReactNode }) => (
      <PersistQueryClientProvider client={queryClient} persistOptions={options}>
        {children}
      </PersistQueryClientProvider>
    );
    const view = await renderHook(
      () => {
        const q = useQuery({
          queryKey: queryKeys.league(LG),
          queryFn: async () => {
            fetches += 1;
            return env(2);
          },
        });
        if (q.data !== undefined) painted.push({ data: q.data, fetchesSoFar: fetches });
        return q;
      },
      { wrapper },
    );
    return { view, painted, fetches: () => fetches };
  }

  it('paints the restored answer first, then revalidates a stale one once', async () => {
    const { view, painted, fetches } = await launch(DEFAULT_STALE_MS + 1_000);
    await waitFor(() => expect(view.result.current.data).toEqual(env(2)));
    expect(painted[0]).toEqual({ data: env(1), fetchesSoFar: 0 });
    expect(fetches()).toBe(1);
  });

  it('does not refetch a restored answer that is still fresh', async () => {
    const { view, fetches } = await launch(1_000);
    await waitFor(() => expect(view.result.current.data).toEqual(env(1)));
    await act(async () => {});
    expect(fetches()).toBe(0);
  });
});

describe('a change the user just made', () => {
  const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
  const savedLeague = async () =>
    (await saved())?.clientState.queries.find((q) => q.queryKey[2] === undefined) as
      { state: { data: unknown } } | undefined;

  it('is saved as soon as its mutation settles, not a throttle later', async () => {
    const client = createQueryClient();
    client.setQueryData(queryKeys.league(LG), env(1));
    // A write has just landed, so the next one would wait out the 1 s throttle.
    await persistQueryClientSave({ queryClient: client, ...persistOptions });
    expect((await savedLeague())?.state.data).toEqual(env(1));
    const unsubscribe = persistQueryClientSubscribe({ queryClient: client, ...persistOptions });
    const stopFlush = setupPersistFlush(client);
    try {
      await client
        .getMutationCache()
        .build(client, {
          mutationFn: async () => env(2),
          onSuccess: (envelope) => client.setQueryData(queryKeys.league(LG), envelope),
        })
        .execute(undefined);
      // The app can be killed now (iOS, a test harness quitting it): well
      // inside the 1 s throttle, the change must already be in storage.
      await sleep(100);
      expect((await savedLeague())?.state.data).toEqual(env(2));
    } finally {
      stopFlush();
      unsubscribe();
    }
  });
});

describe('dropping the saved cache', () => {
  // The fake server: logout and account deletion both succeed.
  const realFetch = globalThis.fetch;
  beforeEach(() => {
    globalThis.fetch = (async () => new Response(null, { status: 204 })) as typeof fetch;
  });
  afterEach(() => {
    globalThis.fetch = realFetch;
  });

  function mount(store = memorySessionStore(session)) {
    const queryClient = new QueryClient({
      defaultOptions: { mutations: { retry: false, gcTime: 0 } },
    });
    clients.push(queryClient);
    queryClient.setQueryData(queryKeys.league(LG), env(1));
    const wrapper = ({ children }: { children: ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        <AuthProvider store={store} revocations={memoryPendingRevocationStore()}>
          {children}
        </AuthProvider>
      </QueryClientProvider>
    );
    const view = renderHook(() => ({ auth: useAuth(), del: useDeleteAccount() }), { wrapper });
    return { queryClient, view };
  }

  it('on sign-out', async () => {
    await seedStorage();
    const { queryClient, view } = mount();
    const { result } = await view;
    await waitFor(() => expect(result.current.auth.status).toBe('signedIn'));
    expect(await saved()).not.toBeNull();

    await act(() => result.current.auth.signOut());
    expect(await saved()).toBeNull();
    expect(queryClient.getQueryData(queryKeys.league(LG))).toBeUndefined();
  });

  it('on account deletion', async () => {
    await seedStorage();
    const { view } = mount();
    const { result } = await view;
    await waitFor(() => expect(result.current.auth.status).toBe('signedIn'));

    await act(async () => {
      await result.current.del.mutateAsync();
    });
    await waitFor(() => expect(result.current.auth.status).toBe('signedOut'));
    await waitFor(async () => expect(await saved()).toBeNull());
  });

  it('at a launch with nobody signed in, keeping the never-saved config', async () => {
    await seedStorage();
    const { queryClient, view } = mount(memorySessionStore(null));
    queryClient.setQueryData(['config'], env(9));
    const { result } = await view;
    await waitFor(() => expect(result.current.auth.status).toBe('signedOut'));
    await waitFor(async () => expect(await saved()).toBeNull());
    expect(queryClient.getQueryData(queryKeys.league(LG))).toBeUndefined();
    expect(queryClient.getQueryData(['config'])).toEqual(env(9));
  });

  it('keeps it for a launch with the same user signed in', async () => {
    await seedStorage();
    const { view } = mount();
    const { result } = await view;
    await waitFor(() => expect(result.current.auth.status).toBe('signedIn'));
    expect(await savedKeys()).toEqual([queryKeys.league(LG)]);
  });
});

describe('staleTime by query family', () => {
  it('keeps static data for long and live data short', () => {
    expect(staleTimeFor(queryKeys.clubs)).toBe(60 * 60_000);
    expect(staleTimeFor(queryKeys.schedule(LG))).toBe(10 * 60_000);
    expect(staleTimeFor(queryKeys.standings(LG))).toBe(5 * 60_000);
    expect(staleTimeFor(queryKeys.me)).toBe(5 * 60_000);
    expect(staleTimeFor(queryKeys.gameweek)).toBe(60_000);
    expect(staleTimeFor(queryKeys.players(LG, {}))).toBe(60_000);
    expect(staleTimeFor(queryKeys.player(LG, 'plr_1'))).toBe(60_000);
    expect(staleTimeFor(queryKeys.fixtures(7))).toBe(30_000);
    expect(staleTimeFor(queryKeys.transactions(LG))).toBe(30_000);
    // Live scores and the league home: short.
    expect(DEFAULT_STALE_MS).toBe(15_000);
    expect(staleTimeFor(queryKeys.myLeagues)).toBe(DEFAULT_STALE_MS);
    expect(staleTimeFor(queryKeys.league(LG))).toBe(DEFAULT_STALE_MS);
    expect(staleTimeFor(queryKeys.matchup(LG, 7, 'mu_1'))).toBe(DEFAULT_STALE_MS);
    expect(staleTimeFor(queryKeys.draft(LG))).toBe(DEFAULT_STALE_MS);
    expect(staleTimeFor(['something-new'])).toBe(DEFAULT_STALE_MS);
  });

  it('is what the client uses: 20 s on, the league home refetches, clubs do not', async () => {
    const client = createQueryClient();
    client.setQueryData(queryKeys.clubs, env(1));
    client.setQueryData(queryKeys.league(LG), env(1));
    const now = Date.now();
    jest.spyOn(Date, 'now').mockReturnValue(now + 20_000);
    try {
      const fetched: string[] = [];
      const read = (key: readonly unknown[], name: string) =>
        client.fetchQuery({
          queryKey: key,
          queryFn: async () => {
            fetched.push(name);
            return env(2);
          },
        });
      await read(queryKeys.clubs, 'clubs');
      await read(queryKeys.league(LG), 'league');
      expect(fetched).toEqual(['league']);
    } finally {
      jest.restoreAllMocks();
    }
  });
});
