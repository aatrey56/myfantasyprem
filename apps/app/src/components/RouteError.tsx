import type { ErrorBoundaryProps } from 'expo-router';
import { View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { Button } from './Button';
import { Card } from './Card';
import { useHasBottomTabBar, useHasTopBar } from './tab-bar-context';
import { useTheme } from '@/theme';

/**
 * Per-route error boundary. Re-export it from a route file as
 * `export { RouteError as ErrorBoundary } from '@/components/RouteError'`.
 * Pads for the safe-area insets the league shell's bars do not take, as
 * Screen does.
 */
export function RouteError({ error, retry }: ErrorBoundaryProps) {
  const { colors, spacing } = useTheme();
  const insets = useSafeAreaInsets();
  const hasTopBar = useHasTopBar();
  const hasTabBar = useHasBottomTabBar();
  return (
    <View
      style={{
        flex: 1,
        backgroundColor: colors.background,
        padding: spacing.lg,
        paddingTop: spacing.lg + (hasTopBar ? 0 : insets.top),
        paddingBottom: spacing.lg + (hasTabBar ? 0 : insets.bottom),
        paddingLeft: spacing.lg + insets.left,
        paddingRight: spacing.lg + insets.right,
      }}
      testID="route-error"
    >
      <Card>
        <AppText variant="heading">Something went wrong on this screen</AppText>
        <AppText muted>{error.message}</AppText>
        <Button label="Try again" onPress={() => void retry()} />
      </Card>
    </View>
  );
}
