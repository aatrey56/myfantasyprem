import { send } from '@myfantasyprem/api-client';
import { useQueries } from '@tanstack/react-query';
import { View } from 'react-native';

import { useMyLeagues } from '@/api/account-hooks';
import { api } from '@/api/client';
import { useFreshness } from '@/api/freshness';
import { queryKeys } from '@/api/hooks';
import { useScreenFocus } from '@/api/screen-focus';
import { ActivityEntry } from '@/components/activity/ActivityRows';
import { AppText } from '@/components/AppText';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import { Newspaper } from '@/components/icons';
import { Inbox } from '@/components/notifications/Inbox';
import { PressableLink } from '@/components/PressableLink';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SkeletonRows } from '@/components/Skeleton';
import { pressFeedback, type PressState } from '@/components/pressable';
import { mergeLeagueFeeds } from '@/lib/feed';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** How many recent moves each league contributes. */
const PER_LEAGUE = 15;

/** Under the league's key, so anything that refreshes the league refreshes this. */
const feedKey = (leagueId: string) => [...queryKeys.league(leagueId), 'feed', PER_LEAGUE];

/** FEED: the latest adds, drops, waiver results and trades across all my leagues. */
export default function FeedScreen() {
  const { spacing, colors, radii } = useTheme();
  const leagues = useMyLeagues();
  const freshness = useFreshness(leagues);
  // A league's moves start with its draft.
  const started = (leagues.data?.data ?? []).filter((r) => r.league.status !== 'pre_draft');
  const { subscribed } = useScreenFocus(...started.map((r) => feedKey(r.league.id)));
  const feeds = useQueries({
    queries: started.map((r) => ({
      queryKey: feedKey(r.league.id),
      queryFn: () =>
        send(
          api.GET('/v1/leagues/{leagueId}/transactions', {
            params: { path: { leagueId: r.league.id }, query: { limit: PER_LEAGUE } },
          }),
        ),
      subscribed,
    })),
  });
  const items = mergeLeagueFeeds(
    started.map((r, i) => ({
      league: { id: r.league.id, name: r.league.name },
      txns: feeds[i]?.data?.data ?? [],
    })),
  );
  const loadingFeeds = feeds.some((f) => f.isPending && f.fetchStatus !== 'idle');

  return (
    <Screen title="Feed" subtitle="Moves across your leagues" freshness={freshness} maxWidth={720}>
      <Inbox />
      <QueryStatus
        query={leagues}
        loading={
          <Card flush>
            <SkeletonRows count={6} testID="feed-loading" />
          </Card>
        }
      >
        {() =>
          items.length === 0 && loadingFeeds ? (
            <Card flush>
              <SkeletonRows count={6} testID="feed-loading" />
            </Card>
          ) : items.length === 0 ? (
            <Card>
              <EmptyState
                icon={Newspaper}
                title="Nothing yet"
                body="Adds, drops, waiver results and trades in your leagues show up here."
                testID="feed-empty"
              />
            </Card>
          ) : (
            <View style={{ gap: spacing.md }} testID="feed-list">
              {items.map(({ league, txn }) => (
                <View key={`${league.id}/${txn.id}`} style={{ gap: spacing.xs }}>
                  <PressableLink
                    href={`/leagues/${league.id}/activity`}
                    accessibilityLabel={`${league.name} activity`}
                    testID="feed-league"
                    style={(state: PressState) => [
                      { alignSelf: 'flex-start', borderRadius: radii.sm, paddingRight: spacing.xs },
                      ...pressFeedback(colors, state),
                    ]}
                  >
                    <AppText variant="eyebrow" accent numberOfLines={1}>
                      {league.name}
                    </AppText>
                  </PressableLink>
                  <ActivityEntry
                    entry={{
                      kind: 'transaction',
                      id: txn.id,
                      gameweek: txn.gameweek ?? null,
                      at: txn.occurredAt,
                      txn,
                    }}
                  />
                </View>
              ))}
            </View>
          )
        }
      </QueryStatus>
    </Screen>
  );
}
