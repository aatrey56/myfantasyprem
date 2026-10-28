import { useEffect, useState } from 'react';
import { AccessibilityInfo, Animated, View, type DimensionValue } from 'react-native';

import { useTheme } from '@/theme';

/** True when the user asked the OS for less motion. */
export function useReducedMotion(): boolean {
  const [reduced, setReduced] = useState(false);
  useEffect(() => {
    let alive = true;
    void AccessibilityInfo.isReduceMotionEnabled?.()
      .then((v) => alive && setReduced(v))
      .catch(() => undefined);
    const sub = AccessibilityInfo.addEventListener?.('reduceMotionChanged', setReduced);
    return () => {
      alive = false;
      sub?.remove();
    };
  }, []);
  return reduced;
}

interface Props {
  width?: DimensionValue;
  height?: number;
  radius?: number;
}

/** A pulsing placeholder block. Static when reduced motion is on. */
export function Skeleton({ width = '100%', height = 14, radius }: Props) {
  const { colors, radii } = useTheme();
  const reduced = useReducedMotion();
  const [opacity] = useState(() => new Animated.Value(0.6));
  useEffect(() => {
    if (reduced) return;
    const loop = Animated.loop(
      Animated.sequence([
        Animated.timing(opacity, { toValue: 1, duration: 700, useNativeDriver: false }),
        Animated.timing(opacity, { toValue: 0.6, duration: 700, useNativeDriver: false }),
      ]),
    );
    loop.start();
    return () => loop.stop();
  }, [opacity, reduced]);
  return (
    <Animated.View
      aria-hidden
      style={{
        width,
        height,
        borderRadius: radius ?? radii.sm,
        backgroundColor: colors.surfaceRaised,
        opacity,
      }}
    />
  );
}

/** Rows shaped like list rows, for loading states. Announced once as "Loading". */
export function SkeletonRows({
  count = 5,
  testID = 'loading',
}: {
  count?: number;
  testID?: string;
}) {
  const { spacing, layout } = useTheme();
  return (
    <View testID={testID} accessibilityLabel="Loading" accessibilityRole="progressbar">
      {Array.from({ length: count }, (_, i) => (
        <View
          key={i}
          style={{
            minHeight: layout.rowHeight,
            flexDirection: 'row',
            alignItems: 'center',
            gap: spacing.md,
            paddingHorizontal: spacing.lg,
          }}
        >
          <Skeleton width={36} height={22} />
          <View style={{ flex: 1, gap: spacing.xs }}>
            <Skeleton width="55%" height={14} />
            <Skeleton width="35%" height={11} />
          </View>
          <Skeleton width={28} height={18} />
        </View>
      ))}
    </View>
  );
}
