import { useEffect, useState } from 'react';
import { Animated, Easing, View } from 'react-native';

import { useReducedMotion } from '../Skeleton';
import { useTheme } from '@/theme';

interface Props {
  /** 0–100; null draws an empty track. */
  percent: number | null;
  /** The outer edge the fill grows from, so it reaches toward the centre. */
  from: 'left' | 'right';
  color: string;
  /** Bar thickness. */
  height?: number;
}

/**
 * A thick split bar for one side of a matchup hero: a muted track with a
 * fill that grows from the outer edge toward the centre. Width changes
 * animate over 400ms unless the user asks for reduced motion. Decorative:
 * the share is always written next to it.
 *
 * React Native's Animated, like the skeletons: Reanimated is installed but
 * its worklets runtime does not load under Jest without extra setup.
 */
export function ShareBar({ percent, from, color, height = 6 }: Props) {
  const { colors } = useTheme();
  const reduced = useReducedMotion();
  const target = Math.min(100, Math.max(0, percent ?? 0));
  const [width] = useState(() => new Animated.Value(0));
  const [fill] = useState(() =>
    width.interpolate({ inputRange: [0, 100], outputRange: ['0%', '100%'] }),
  );
  useEffect(() => {
    if (reduced) {
      width.setValue(target);
      return;
    }
    const anim = Animated.timing(width, {
      toValue: target,
      duration: 400,
      easing: Easing.out(Easing.cubic),
      // Width is a layout property: no native driver.
      useNativeDriver: false,
    });
    anim.start();
    return () => anim.stop();
  }, [target, reduced, width]);
  return (
    <View
      aria-hidden
      importantForAccessibility="no-hide-descendants"
      accessibilityElementsHidden
      style={{
        height,
        borderRadius: height / 2,
        backgroundColor: colors.surfaceSelected,
        overflow: 'hidden',
        flexDirection: from === 'left' ? 'row' : 'row-reverse',
      }}
    >
      <Animated.View
        style={{
          height,
          borderRadius: height / 2,
          backgroundColor: color,
          width: fill,
        }}
      />
    </View>
  );
}
