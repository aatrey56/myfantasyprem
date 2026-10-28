import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { useHasTopBar } from './tab-bar-context';
import type { Freshness } from '@/api/freshness';
import { formatAsOf } from '@/lib/time';
import { useTheme } from '@/theme';

const TEST_IDS = {
  offline: 'offline-banner',
  serverUnreachable: 'unreachable-banner',
  upstreamStale: 'stale-banner',
} as const;

export function freshnessText(f: Exclude<Freshness, { kind: 'fresh' }>): string {
  switch (f.kind) {
    case 'offline':
      return "You're offline";
    case 'serverUnreachable':
      return `Can't reach the server · showing data from ${formatAsOf(f.lastUpdatedAt)}`;
    case 'upstreamStale':
      return `Player data delayed since ${formatAsOf(f.since)}`;
  }
}

/**
 * The one freshness banner of a screen, at the top under the safe-area inset.
 * The screen keeps showing the last good data underneath. Announced politely
 * by screen readers: it is a status, not an alert.
 */
export function StaleBanner({ freshness }: { freshness: Freshness }) {
  const { colors, spacing } = useTheme();
  const insets = useSafeAreaInsets();
  const hasTopBar = useHasTopBar();
  if (freshness.kind === 'fresh') return null;
  return (
    <View
      accessibilityLiveRegion="polite"
      aria-live="polite"
      testID={TEST_IDS[freshness.kind]}
      style={{
        backgroundColor: colors.warning,
        paddingTop: spacing.sm + (hasTopBar ? 0 : insets.top),
        paddingBottom: spacing.sm,
        paddingLeft: spacing.lg + insets.left,
        paddingRight: spacing.lg + insets.right,
      }}
    >
      <AppText variant="caption" style={{ color: colors.onWarning, fontWeight: '700' }}>
        {freshnessText(freshness)}
      </AppText>
    </View>
  );
}
