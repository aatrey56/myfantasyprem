import { ActivityIndicator, Pressable, View, type ViewStyle } from 'react-native';

import { AppText } from './AppText';
import type { LucideIcon } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { useTheme } from '@/theme';

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'destructive';

interface Props {
  label: string;
  onPress: () => void;
  disabled?: boolean;
  /** Shows a spinner and blocks presses. */
  loading?: boolean;
  testID?: string;
  variant?: ButtonVariant;
  /** `sm` is 36px tall with a hit slop up to the 44px touch target. */
  size?: 'md' | 'sm';
  icon?: LucideIcon;
  fullWidth?: boolean;
  accessibilityLabel?: string;
  accessibilityHint?: string;
  style?: ViewStyle;
}

/** Primary (amber), secondary (outlined), ghost (text) and destructive buttons. */
export function Button({
  label,
  onPress,
  disabled,
  loading,
  testID,
  variant = 'primary',
  size = 'md',
  icon: Icon,
  fullWidth,
  accessibilityLabel,
  accessibilityHint,
  style,
}: Props) {
  const { colors, radii, spacing, layout } = useTheme();
  const inactive = Boolean(disabled || loading);
  const fg =
    variant === 'primary'
      ? colors.onAccent
      : variant === 'destructive'
        ? colors.loss
        : variant === 'ghost'
          ? colors.accent
          : colors.text;
  const height = size === 'sm' ? 36 : layout.touch;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityHint={accessibilityHint}
      accessibilityState={{ disabled: inactive, busy: Boolean(loading) }}
      disabled={inactive}
      onPress={onPress}
      testID={testID}
      hitSlop={size === 'sm' ? 4 : undefined}
      style={(state: PressState) => [
        {
          minHeight: height,
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'center',
          gap: spacing.sm,
          paddingHorizontal: size === 'sm' ? spacing.md : spacing.lg,
          borderRadius: radii.md,
          borderWidth: 1,
          borderColor:
            variant === 'primary'
              ? colors.accent
              : variant === 'ghost'
                ? 'transparent'
                : variant === 'destructive'
                  ? colors.loss
                  : colors.border,
          backgroundColor: variant === 'primary' ? colors.accent : 'transparent',
          alignSelf: fullWidth ? 'stretch' : undefined,
        },
        ...pressFeedback(colors, state, { disabled: inactive, hoverFill: variant !== 'primary' }),
        style ?? {},
      ]}
    >
      {loading ? (
        <ActivityIndicator size="small" color={fg} />
      ) : Icon ? (
        <View aria-hidden>
          <Icon size={size === 'sm' ? 16 : 18} color={fg} strokeWidth={2.25} />
        </View>
      ) : null}
      <AppText variant={size === 'sm' ? 'meta' : 'body'} weight="semibold" color={fg}>
        {label}
      </AppText>
    </Pressable>
  );
}

interface IconButtonProps {
  icon: LucideIcon;
  /** Read by screen readers; also the web tooltip. */
  label: string;
  onPress: () => void;
  disabled?: boolean;
  testID?: string;
  /** `filled` sits on a raised circle. */
  variant?: 'ghost' | 'filled' | 'accent';
  size?: number;
  color?: string;
  selected?: boolean;
}

/** A 44px square icon button with an accessible label. */
export function IconButton({
  icon: Icon,
  label,
  onPress,
  disabled,
  testID,
  variant = 'ghost',
  size = 20,
  color,
  selected,
}: IconButtonProps) {
  const { colors, radii, layout } = useTheme();
  const fg = color ?? (variant === 'accent' ? colors.onAccent : colors.text);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ disabled: Boolean(disabled), selected }}
      // Web tooltip.
      {...({ title: label } as object)}
      disabled={disabled}
      onPress={onPress}
      testID={testID}
      style={(state: PressState) => [
        {
          width: layout.touch,
          height: layout.touch,
          borderRadius: radii.pill,
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor:
            variant === 'accent'
              ? colors.accent
              : variant === 'filled' || selected
                ? colors.surfaceRaised
                : 'transparent',
        },
        ...pressFeedback(colors, state, { disabled, hoverFill: variant !== 'accent' }),
      ]}
    >
      <Icon size={size} color={fg} strokeWidth={2} />
    </Pressable>
  );
}
