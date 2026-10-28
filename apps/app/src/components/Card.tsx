import { Pressable, View, type ViewProps, type ViewStyle } from 'react-native';

import { pressFeedback, type PressState } from './pressable';
import { useTheme } from '@/theme';

interface Props extends ViewProps {
  /** Removes the inner padding, for cards made of rows. */
  flush?: boolean;
  /** Makes the whole card one button. */
  onPress?: () => void;
  accessibilityLabel?: string;
}

/** The one card style: surface, 1px border, 16px radius. */
export function Card({ style, flush, onPress, accessibilityLabel, ...rest }: Props) {
  const { colors, radii, spacing } = useTheme();
  const base: ViewStyle = {
    backgroundColor: colors.surface,
    borderColor: colors.border,
    borderWidth: 1,
    borderRadius: radii.lg,
    padding: flush ? 0 : spacing.lg,
    gap: flush ? 0 : spacing.sm,
    overflow: 'hidden',
  };
  if (onPress) {
    return (
      <Pressable
        {...rest}
        accessibilityRole="button"
        accessibilityLabel={accessibilityLabel}
        onPress={onPress}
        style={(state: PressState) => [base, ...pressFeedback(colors, state), style as ViewStyle]}
      />
    );
  }
  return <View {...rest} style={[base, style]} />;
}
