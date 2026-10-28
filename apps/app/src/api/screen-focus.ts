import { useQueryClient, type Query, type QueryClient, type QueryKey } from '@tanstack/react-query';
import { useIsFocused } from 'expo-router';
import { createContext, useContext, useEffect, useRef } from 'react';

import { leagueTabDistance } from '@/lib/league-tabs';

/**
 * Set by the league pager around each page: the page's tab and the tab
 * the pager is showing. Null outside the league tabs.
 */
export const PagerSceneContext = createContext<{ scene: string; active: string } | null>(null);

/** True on a pager page next to the one showing: rendered, and half a swipe away. */
export function usePagerNeighbour(): boolean {
  const pager = useContext(PagerSceneContext);
  return pager !== null && leagueTabDistance(pager.scene, pager.active) === 1;
}

/**
 * Screens use `subscribed` for their queries: a screen nobody can see (a
 * tab two pages away, a screen under a stack) stops observing its queries.
 * The pages either side of the visible one stay subscribed, so a swipe
 * reveals live data, not a stale snapshot. Only the visible screen polls
 * (`focused` gates `refetchInterval`).
 *
 * Pass the screen's query keys: when the screen comes back into view (a
 * pager tab swiped or tapped to, a stack screen popped back to), those of
 * its queries past their staleTime refetch once. Pager pages stay mounted,
 * so `refetchOnMount` alone never fires for them.
 */
export function useScreenFocus(...keys: QueryKey[]): { subscribed: boolean; focused: boolean } {
  const focused = useIsFocused();
  const neighbour = usePagerNeighbour();
  useRefetchStaleOnFocus(focused, keys);
  return { subscribed: focused || neighbour, focused };
}

function useRefetchStaleOnFocus(focused: boolean, keys: QueryKey[]): void {
  const client = useQueryClient();
  const latestKeys = useRef(keys);
  const wasFocused = useRef(focused);
  // Declared first, so the focus effect below reads this render's keys.
  useEffect(() => {
    latestKeys.current = keys;
  });
  useEffect(() => {
    const was = wasFocused.current;
    wasFocused.current = focused;
    // The first render is a mount: refetchOnMount covers it.
    if (focused && !was) refetchStale(client, latestKeys.current);
  }, [focused, client]);
}

/**
 * Refetches the active queries under exactly these keys that are stale
 * now. Two observers of one query on a screen share the request
 * (`cancelRefetch: false` joins a fetch in flight instead of restarting it).
 */
export function refetchStale(client: QueryClient, keys: readonly QueryKey[]): void {
  for (const queryKey of keys) {
    void client.refetchQueries(
      { queryKey, exact: true, type: 'active', predicate: isStaleNow },
      { cancelRefetch: false },
    );
  }
}

/**
 * Stale by the clock at this moment. `Query.isStale()` reads each
 * observer's last result, which can lag behind a staleTime that ran out
 * while the screen was hidden.
 */
function isStaleNow(query: Query): boolean {
  if (query.state.data === undefined || query.state.isInvalidated) return true;
  return query.observers.some((o) => {
    const { enabled, staleTime } = o.options;
    if ((typeof enabled === 'function' ? enabled(query) : enabled) === false) return false;
    const time = typeof staleTime === 'function' ? staleTime(query) : (staleTime ?? 0);
    return time !== 'static' && query.isStaleByTime(time);
  });
}
