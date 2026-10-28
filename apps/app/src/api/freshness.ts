import type { Meta } from '@myfantasyprem/api-client';
import { onlineManager } from '@tanstack/react-query';
import { useEffect, useState, useSyncExternalStore } from 'react';

import { STALE_AFTER_MS } from '@/constants/app';

/** The parts of a query result freshness needs. Infinite queries pass their first page. */
export interface FreshnessQuery {
  data: { meta: Meta } | undefined;
  /** Device time the data arrived. */
  dataUpdatedAt: number;
  /** Device time of the latest failed fetch, 0 if none. */
  errorUpdatedAt: number;
  isRefetchError: boolean;
}

/** What one screen's banner should say. Times are on the device clock. */
export type Freshness =
  | { kind: 'fresh' }
  | { kind: 'offline' }
  | { kind: 'serverUnreachable'; lastUpdatedAt: number }
  | { kind: 'upstreamStale'; since: number };

/**
 * Pure rule, in priority order: offline, then our server unreachable (the
 * latest fetch failed, so the data on screen is older than it looks), then
 * upstream (FPL) data stale: flagged by the server, or past the server's
 * `staleAt` (its poller's next poll plus a grace) without a newer fetch.
 *
 * Age is measured without trusting the device clock against the server's:
 * `serverTime - dataAsOf` is the age when the response was built (one clock),
 * and `now - dataUpdatedAt` is the time since it arrived (the other clock).
 */
export function computeFreshness(query: FreshnessQuery, now: number, online: boolean): Freshness {
  if (!online) return { kind: 'offline' };
  const meta = query.data?.meta;
  if (!meta) return { kind: 'fresh' };
  if (query.isRefetchError || query.errorUpdatedAt > query.dataUpdatedAt) {
    return { kind: 'serverUnreachable', lastUpdatedAt: query.dataUpdatedAt };
  }
  const serverTime = Date.parse(meta.serverTime);
  const dataAsOf = Date.parse(meta.dataAsOf);
  const valid = Number.isFinite(serverTime) && Number.isFinite(dataAsOf);
  const ageAtServer = valid ? serverTime - dataAsOf : 0;
  const age = ageAtServer + (now - query.dataUpdatedAt);
  // With `staleAt` the server says when the data would be late: its
  // poller's next scheduled poll plus a grace (hours away between match
  // days). Without it (league state) only the server's flag counts: the
  // data on screen being a few minutes old says nothing about upstream.
  let late = false;
  if (meta.staleAt !== undefined) {
    const staleAt = Date.parse(meta.staleAt);
    // Time left when it arrived (server clock), spent on the device clock.
    late =
      Number.isFinite(staleAt) && Number.isFinite(serverTime)
        ? now - query.dataUpdatedAt > staleAt - serverTime
        : age > STALE_AFTER_MS;
  }
  if (meta.stale || late) {
    // dataAsOf converted to the device clock, for display.
    const since = valid ? query.dataUpdatedAt - ageAtServer : query.dataUpdatedAt;
    return { kind: 'upstreamStale', since };
  }
  return { kind: 'fresh' };
}

/**
 * A clock that ticks every `intervalMs`, so freshness re-evaluates without a
 * refetch. Pass `enabled: false` (e.g. a hidden tab) to stop it; it catches
 * up at once when enabled again.
 */
export function useNow(intervalMs: number, enabled = true): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!enabled) return;
    const tick = () => setNow(Date.now());
    const catchUp = setTimeout(tick, 0);
    const id = setInterval(tick, intervalMs);
    return () => {
      clearTimeout(catchUp);
      clearInterval(id);
    };
  }, [intervalMs, enabled]);
  return now;
}

/** TanStack Query's view of connectivity (browser events on web, expo-network on native). */
export function useOnline(): boolean {
  return useSyncExternalStore(
    (onChange) => onlineManager.subscribe(onChange),
    () => onlineManager.isOnline(),
    () => true,
  );
}

export function useFreshness(query: FreshnessQuery): Freshness {
  const now = useNow(15_000);
  const online = useOnline();
  return computeFreshness(query, now, online);
}
