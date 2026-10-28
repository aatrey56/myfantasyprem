import type { Query, QueryClient, QueryKey } from '@tanstack/react-query';
import type { PersistQueryClientOptions } from '@tanstack/react-query-persist-client';
import Constants from 'expo-constants';

import { cacheStorage } from './persist-storage';

/**
 * The query cache is saved to device storage (web: localStorage, native:
 * AsyncStorage, see persist-storage.ts) so a cold start paints the last
 * screen's data at once and revalidates behind it: a restored entry keeps
 * its age, so anything past its staleTime refetches on mount.
 */

/** A restored cache older than this is thrown away. */
export const PERSIST_MAX_AGE_MS = 24 * 60 * 60_000;

/**
 * Bump when a cached response shape changes in a way old code or new code
 * cannot read (a renamed field, a new key layout). Every install then
 * starts from an empty cache.
 */
export const CACHE_SCHEMA = 1;

/** A new app version or cache schema discards what an older one saved. */
export const CACHE_BUSTER = `${Constants.expoConfig?.version ?? '0'}:${CACHE_SCHEMA}`;

/**
 * Mock builds save under their own key: `dev:web` and `dev:web:mocks`
 * share an origin, and fake data must never paint in a real build.
 * Inline env check, folded at build time.
 */
export const CACHE_STORAGE_KEY =
  process.env.EXPO_PUBLIC_USE_MOCKS === 'true' ? 'mfp.queryCache.mocks' : 'mfp.queryCache';

/**
 * Query families never written to storage: the signed-in user (session
 * territory), the server's client config (the branded-assets switch must
 * come from the server, not a day-old copy), invite and import lookups
 * (one-off, tied to a code someone typed) and free-text player searches
 * (a new entry per keystroke).
 */
const NEVER_PERSIST = new Set(['config', 'invite', 'import']);

export function shouldPersistKey(queryKey: QueryKey): boolean {
  const [family] = queryKey;
  if (typeof family !== 'string' || NEVER_PERSIST.has(family)) return false;
  // ['me'] is the profile; ['me', 'leagues'] (the home screen) is kept.
  if (family === 'me' && queryKey.length === 1) return false;
  if (family === 'league' && queryKey[2] === 'players') {
    const filter = queryKey[3] as { q?: string } | undefined;
    if (filter?.q) return false;
  }
  return true;
}

/** Only settled, successful answers: never an error or a fetch in flight. */
export function shouldPersistQuery(query: Query): boolean {
  return query.state.status === 'success' && shouldPersistKey(query.queryKey);
}

const persister = cacheStorage(CACHE_STORAGE_KEY);

export const persistOptions: Omit<PersistQueryClientOptions, 'queryClient'> = {
  persister,
  maxAge: PERSIST_MAX_AGE_MS,
  buster: CACHE_BUSTER,
  dehydrateOptions: { shouldDehydrateQuery: shouldPersistQuery },
};

/**
 * Drops every cached query, in memory and in storage: sign-out, a
 * different user, a deleted account. The in-memory clear also rewrites
 * storage (the persister follows the cache), but removing the entry
 * directly does not wait on its throttle.
 */
export function dropQueryCache(queryClient: QueryClient): Promise<void> {
  queryClient.clear();
  return Promise.resolve(persistOptions.persister.removeClient()).catch(() => undefined);
}

/**
 * At a launch with nobody signed in: drops what a restore brought back
 * and the saved copy, but leaves queries that are never saved (the
 * server config the sign-in screen is already reading).
 */
export function dropSavedQueries(queryClient: QueryClient): Promise<void> {
  queryClient.removeQueries({ predicate: (q) => shouldPersistKey(q.queryKey) });
  return Promise.resolve(persistOptions.persister.removeClient()).catch(() => undefined);
}

/**
 * Writes the cache change still waiting on the save throttle (at most a
 * second) now. Called when the app leaves the foreground (lifecycle.native.ts):
 * iOS may kill a backgrounded app without another chance to save.
 */
export function flushQueryCache(): Promise<void> {
  return persister.flush();
}

/**
 * Saves the cache as soon as a mutation settles, so a change the user just
 * made (a lineup save, a claim) survives the app being killed right after.
 * The persister's own cache listener takes the snapshot on the same event;
 * the flush runs on the next tick, once it has. Returns the unsubscribe.
 */
export function setupPersistFlush(queryClient: QueryClient): () => void {
  const timers = new Set<ReturnType<typeof setTimeout>>();
  const unsubscribe = queryClient.getMutationCache().subscribe((event) => {
    if (event.type !== 'updated') return;
    const settled = event.action.type === 'success' || event.action.type === 'error';
    if (!settled) return;
    const t = setTimeout(() => {
      timers.delete(t);
      void flushQueryCache();
    }, 0);
    timers.add(t);
  });
  return () => {
    unsubscribe();
    timers.forEach(clearTimeout);
    timers.clear();
  };
}
