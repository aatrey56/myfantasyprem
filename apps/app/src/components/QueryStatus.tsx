import type { Envelope } from '@myfantasyprem/api-client';
import type { ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from './AppText';
import { Button } from './Button';
import { Card } from './Card';
import { RefreshCw } from './icons';
import { SkeletonRows } from './Skeleton';
import { describeError } from '@/lib/errors';
import { formatAsOf } from '@/lib/time';
import { focusRing, useTheme } from '@/theme';

/** The parts of a TanStack Query result this component reads. */
export interface QueryLike<T> {
  data: T | undefined;
  error: Error | null;
  isPending: boolean;
  isError: boolean;
  isLoadingError: boolean;
  isRefetchError: boolean;
  dataUpdatedAt: number;
  errorUpdatedAt: number;
  refetch: () => unknown;
}

interface Props<T> {
  query: QueryLike<T>;
  children: (data: T) => ReactNode;
  /** Skeleton shown while the first load runs; defaults to list-row skeletons in a card. */
  loading?: ReactNode;
}

/**
 * Loading, error-with-retry, or the data. Stale-while-revalidate: the full
 * error card appears only when there is nothing to show. When a refetch
 * fails the last good data stays on screen with a small "couldn't refresh"
 * strip above it.
 */
export function QueryStatus<T extends Envelope>({ query, children, loading }: Props<T>) {
  if (query.data === undefined) {
    if (query.isLoadingError || query.isError) {
      return (
        <Card testID="query-error">
          <AppText weight="semibold">Couldn&apos;t load this.</AppText>
          <AppText muted variant="caption">
            {describeError(query.error)}
          </AppText>
          <Button
            label="Try again"
            variant="secondary"
            icon={RefreshCw}
            onPress={() => void query.refetch()}
            testID="retry"
            style={{ alignSelf: 'flex-start' }}
          />
        </Card>
      );
    }
    return (
      loading ?? (
        <Card flush>
          <SkeletonRows count={4} />
        </Card>
      )
    );
  }
  return (
    <>
      {query.isRefetchError ? (
        <RefreshFailed lastUpdatedAt={query.dataUpdatedAt} onRetry={() => void query.refetch()} />
      ) : null}
      {children(query.data)}
    </>
  );
}

function RefreshFailed({ lastUpdatedAt, onRetry }: { lastUpdatedAt: number; onRetry: () => void }) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View
      testID="refresh-error"
      style={{
        flexDirection: 'row',
        flexWrap: 'wrap',
        alignItems: 'center',
        gap: spacing.xs,
        paddingVertical: spacing.xs,
        paddingHorizontal: spacing.md,
        borderRadius: radii.sm,
        backgroundColor: colors.surfaceRaised,
      }}
    >
      <AppText variant="caption" muted>
        {`Couldn't refresh · last updated ${formatAsOf(lastUpdatedAt)} ·`}
      </AppText>
      <Pressable
        accessibilityRole="button"
        onPress={onRetry}
        testID="refresh-retry"
        style={({ focused }: { pressed: boolean; focused?: boolean }) =>
          focused ? focusRing(colors) : null
        }
      >
        <AppText variant="caption" accent style={{ fontWeight: '700' }}>
          Retry
        </AppText>
      </Pressable>
    </View>
  );
}
