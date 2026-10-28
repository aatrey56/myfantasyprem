import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { useDraft } from '@/api/draft-hooks';
import { useFreshness } from '@/api/freshness';
import { useLeagueContext, useTransactions } from '@/api/hooks';
import { ActivityEntry } from '@/components/activity/ActivityRows';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { Chip } from '@/components/Chip';
import { EmptyState } from '@/components/EmptyState';
import { Activity, Clock } from '@/components/icons';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { SkeletonRows } from '@/components/Skeleton';
import {
  ACTIVITY_FILTERS,
  flattenFeed,
  groupFeed,
  kindFor,
  type ActivityFilter,
  type FeedEntry,
  type FeedGroup,
} from '@/lib/activity';
import { originBack } from '@/lib/navigation';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

type FeedRow =
  | { kind: 'head'; key: string; group: FeedGroup }
  | { kind: 'entry'; key: string; entry: FeedEntry; first: boolean };

const EMPTY: Record<ActivityFilter, string> = {
  all: 'Adds, drops, waiver results and trades show up here.',
  waiver: 'Waiver results show here after each waiver run.',
  free_agent: 'Free-agent adds show here as they happen.',
  trade: 'Processed and vetoed trades show here.',
};

/** Activity (opened from League): the league's moves, newest first, grouped by gameweek, loaded as you scroll. */
export default function ActivityScreen() {
  const { leagueId, from } = useLocalSearchParams<{ leagueId: string; from?: string }>();
  const router = useRouter();
  const { spacing } = useTheme();
  const [filter, setFilter] = useState<ActivityFilter>('all');
  const home = useLeagueContext(leagueId);
  // The feed starts with the draft: missed-clock autopicks show while drafting.
  const started = home.data !== undefined && home.data.data.league.status !== 'pre_draft';
  const feed = useTransactions(leagueId, kindFor(filter), 20);
  const draft = useDraft(leagueId);
  const first = feed.data?.pages[0];
  const freshness = useFreshness({ ...feed, data: first });
  const items = flattenFeed(feed.data?.pages, filter);
  const draftDone =
    filter === 'all' && !feed.hasNextPage && draft.data?.data.status === 'complete'
      ? (draft.data.data.completedAt ?? null)
      : null;
  const groups = groupFeed(items, draftDone);
  // One row per gameweek heading and per move, for the list.
  const rows: FeedRow[] = groups.flatMap((g) => [
    { kind: 'head' as const, key: `gw-${g.gameweek ?? 'draft'}`, group: g },
    ...g.entries.map((e, i) => ({ kind: 'entry' as const, key: e.id, entry: e, first: i === 0 })),
  ]);
  const showFeed = !(home.data && !started) && first !== undefined;
  const loadMore = () => {
    if (feed.hasNextPage && !feed.isFetchingNextPage) void feed.fetchNextPage();
  };

  return (
    <Screen
      title="Activity"
      back={originBack(leagueId, from, 'league')}
      freshness={freshness}
      onEndReached={loadMore}
      // Long: drawn as you scroll on native (FlashList).
      list={{
        data: showFeed ? rows : [],
        keyExtractor: (r) => r.key,
        getItemType: (r) => r.kind,
        testID: 'activity-feed',
        // The gaps of the grouped layout: lg between gameweeks, xs under a
        // heading, sm between moves.
        renderItem: (r, i) =>
          r.kind === 'head' ? (
            <View style={{ marginTop: i > 0 ? spacing.lg : 0 }}>
              <SectionHeader
                title={r.group.gameweek === null ? 'Draft' : `Gameweek ${r.group.gameweek}`}
                detail={String(r.group.entries.length)}
              />
            </View>
          ) : (
            <View style={{ marginTop: r.first ? spacing.xs : spacing.sm }}>
              <ActivityEntry entry={r.entry} />
            </View>
          ),
        footer: feed.hasNextPage ? (
          <Button
            label="Load older moves"
            variant="secondary"
            onPress={loadMore}
            loading={feed.isFetchingNextPage}
            testID="activity-more"
          />
        ) : null,
      }}
      actions={
        <Button
          label="Deadlines"
          icon={Clock}
          size="sm"
          variant="secondary"
          onPress={() => router.push(`/leagues/${leagueId}/activity/deadlines`)}
          testID="open-deadlines"
        />
      }
    >
      <View
        style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}
        accessibilityLabel="Filter activity"
      >
        {ACTIVITY_FILTERS.map((f) => (
          <Chip
            key={f.value}
            label={f.label}
            selected={filter === f.value}
            onPress={() => setFilter(f.value)}
            testID={`filter-${f.value}`}
          />
        ))}
      </View>
      {home.data && !started ? (
        <Card>
          <EmptyState
            icon={Activity}
            title="No moves yet"
            body="Adds, drops, waivers and trades start once the draft is done."
            testID="activity-preseason"
          />
        </Card>
      ) : (
        <QueryStatus
          query={{ ...feed, data: first }}
          loading={
            <Card flush>
              <SkeletonRows count={6} />
            </Card>
          }
        >
          {() =>
            groups.length === 0 ? (
              <Card>
                <EmptyState
                  icon={Activity}
                  title="No moves yet"
                  body={EMPTY[filter]}
                  testID="activity-empty"
                />
              </Card>
            ) : null
          }
        </QueryStatus>
      )}
    </Screen>
  );
}
