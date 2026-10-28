import { useIsFocused, useLocalSearchParams } from 'expo-router';
import { useEffect } from 'react';

import { useFreshness, useNow } from '@/api/freshness';
import { useCurrentGameweek } from '@/api/hooks';
import { DeadlinesView } from '@/components/DeadlinesView';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { originBack } from '@/lib/navigation';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Ask again at most this often while the server has nothing past the last deadline. */
const RECHECK_MS = 60_000;

/**
 * Deadlines: lineup lock, waiver run and trade deadline, with a live
 * countdown. The clock runs on server time (meta.serverTime plus the time
 * since the answer arrived) and only while the screen is in view; when the
 * last deadline it knows of passes, it asks the server for the next one.
 */
export default function DeadlinesScreen() {
  const { leagueId, from } = useLocalSearchParams<{ leagueId: string; from?: string }>();
  const focused = useIsFocused();
  const query = useCurrentGameweek();
  const freshness = useFreshness(query);
  const now = useNow(1000, focused);
  const serverTime = query.data ? Date.parse(query.data.meta.serverTime) : NaN;
  const offset = Number.isFinite(serverTime) ? serverTime - query.dataUpdatedAt : 0;
  const serverNow = now + offset;

  const status = query.data?.data;
  const lastKnown = status
    ? Math.max(
        Date.parse(status.current.deadlineAt),
        status.next ? Date.parse(status.next.deadlineAt) : -Infinity,
      )
    : NaN;
  // Ask once as soon as the boundary passes, then (or after a failure) at
  // most once a minute until the server reveals the next gameweek.
  const { refetch, isFetching, dataUpdatedAt, errorUpdatedAt } = query;
  const askedBeforeBoundary = dataUpdatedAt + offset < lastKnown && errorUpdatedAt <= dataUpdatedAt;
  const due =
    focused &&
    !isFetching &&
    serverNow >= lastKnown &&
    (askedBeforeBoundary || now - Math.max(dataUpdatedAt, errorUpdatedAt) >= RECHECK_MS);
  useEffect(() => {
    if (due) void refetch();
  }, [due, refetch]);

  return (
    <Screen title="Deadlines" back={originBack(leagueId, from, 'activity')} freshness={freshness}>
      <QueryStatus query={query}>
        {(env) => <DeadlinesView status={env.data} now={serverNow} />}
      </QueryStatus>
    </Screen>
  );
}
