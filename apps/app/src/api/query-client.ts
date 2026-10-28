import { ApiError, MalformedResponseError, type Meta } from '@myfantasyprem/api-client';
import { QueryClient, type Query, type QueryKey } from '@tanstack/react-query';

import { PERSIST_MAX_AGE_MS } from './persist';

/** Retries after the first failure, so at most four attempts in total. */
const MAX_RETRIES = 3;

/**
 * Retry network failures, 5xx, 408 and 429. Never retry other 4xx or a
 * malformed body: the same request will fail the same way.
 */
export function shouldRetry(failureCount: number, error: unknown): boolean {
  if (failureCount >= MAX_RETRIES) return false;
  if (error instanceof MalformedResponseError) return false;
  if (error instanceof ApiError) {
    const s = error.status;
    return s === 408 || s === 429 || s >= 500;
  }
  return true;
}

/** Live scores move every few seconds upstream; the server polls about every 30-60 s. */
export const LIVE_REFETCH_MS = 30_000;
/**
 * Matches are over but bonus is not added yet (`provisional`). Bonus lands
 * within an hour or so; the server checks every 10 minutes, so 5 minutes
 * picks it up promptly without polling a quiet screen hard.
 */
export const PROVISIONAL_REFETCH_MS = 5 * 60_000;

/** `refetchInterval` for an envelope query: poll only while scores can move. */
export function refetchIntervalFor(meta: Meta | undefined): number | false {
  if (meta?.scoreState === 'live') return LIVE_REFETCH_MS;
  if (meta?.scoreState === 'provisional') return PROVISIONAL_REFETCH_MS;
  return false;
}

const SECOND = 1_000;
const MINUTE = 60 * SECOND;

/** Anything not listed below: live-ish league data (matchups, rosters, the league home). */
export const DEFAULT_STALE_MS = 15 * SECOND;

/**
 * How long an answer counts as fresh, by query family. Static data
 * (clubs, the schedule, the gameweek calendar) keeps for minutes; anything
 * that moves with live scores stays short. Live screens do not rely on
 * this: they poll while scores move (refetchIntervalFor) and the realtime
 * channel invalidates what an event changes. A hook's own staleTime wins.
 */
export function staleTimeFor(queryKey: QueryKey): number {
  const [family, a, b] = queryKey;
  switch (family) {
    case 'clubs':
    case 'config':
      return 60 * MINUTE;
    case 'me':
      // The profile; ['me', 'leagues'] carries live matchup scores.
      return a === undefined ? 5 * MINUTE : DEFAULT_STALE_MS;
    case 'gameweek':
      // ['gameweek', 'current'] moves once a week; fixtures carry live scores.
      return a === 'current' ? MINUTE : 30 * SECOND;
    case 'league':
      switch (b) {
        case 'schedule':
          return 10 * MINUTE;
        case 'standings':
        case 'members':
          return 5 * MINUTE;
        case 'players':
        case 'player':
          return MINUTE;
        case 'transactions':
        case 'trades':
        case 'waivers':
          return 30 * SECOND;
        default:
          return DEFAULT_STALE_MS;
      }
    default:
      return DEFAULT_STALE_MS;
  }
}

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: {
        staleTime: (query: Query) => staleTimeFor(query.queryKey),
        // Kept as long as the persisted cache, so a restored entry is not
        // collected (and dropped from storage) before anything asks for it.
        gcTime: PERSIST_MAX_AGE_MS,
        retry: shouldRetry,
        refetchOnWindowFocus: true,
        refetchOnReconnect: true,
      },
    },
  });
}
