import { Pressable, View } from 'react-native';

import { AppText } from './AppText';
import type { LucideIcon } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { tint, useTheme } from '@/theme';

interface ChipProps {
  label: string;
  selected?: boolean;
  onPress: () => void;
  icon?: LucideIcon;
  testID?: string;
  accessibilityLabel?: string;
}

/** A selectable filter chip (pill). 36px tall with hit slop to 44. */
export function Chip({
  label,
  selected,
  onPress,
  icon: Icon,
  testID,
  accessibilityLabel,
}: ChipProps) {
  const { colors, spacing, radii } = useTheme();
  const fg = selected ? colors.onAccent : colors.text;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityState={{ selected: Boolean(selected) }}
      aria-pressed={Boolean(selected)}
      onPress={onPress}
      testID={testID}
      hitSlop={4}
      style={(state: PressState) => [
        {
          // 36 tall plus the 4pt hit slop each way: a 44pt target. A minimum,
          // not a fixed height, so larger Dynamic Type sizes still fit.
          minHeight: 36,
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.xs,
          paddingHorizontal: spacing.md,
          borderRadius: radii.pill,
          borderWidth: 1,
          borderColor: selected ? colors.accent : colors.border,
          backgroundColor: selected ? colors.accent : colors.surface,
        },
        ...pressFeedback(colors, state, { hoverFill: !selected }),
      ]}
    >
      {Icon ? <Icon size={14} color={fg} aria-hidden /> : null}
      <AppText variant="meta" weight="semibold" color={fg}>
        {label}
      </AppText>
    </Pressable>
  );
}

export type PillTone = 'neutral' | 'accent' | 'win' | 'loss' | 'warning' | 'live';

interface PillProps {
  label: string;
  tone?: PillTone;
  /** A leading dot (pulsing is not needed; colour + text carry the meaning). */
  dot?: boolean;
  icon?: LucideIcon;
  testID?: string;
}

/** A small static status label: LIVE, Pre-draft, Locked, Vetoed. */
export function Pill({ label, tone = 'neutral', dot, icon: Icon, testID }: PillProps) {
  const { colors, spacing, radii } = useTheme();
  const fg =
    tone === 'accent' || tone === 'live' || tone === 'warning'
      ? colors.accent
      : tone === 'win'
        ? colors.win
        : tone === 'loss'
          ? colors.loss
          : colors.textMuted;
  const bg = tone === 'neutral' ? colors.surfaceRaised : tint(fg, colors.surface);
  return (
    <View
      testID={testID}
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        alignSelf: 'flex-start',
        gap: spacing.xs,
        paddingHorizontal: spacing.sm,
        paddingVertical: 2,
        borderRadius: radii.pill,
        backgroundColor: bg,
      }}
    >
      {dot ? (
        <View aria-hidden style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: fg }} />
      ) : null}
      {Icon ? <Icon size={12} color={fg} aria-hidden /> : null}
      <AppText variant="eyebrow" color={fg}>
        {label}
      </AppText>
    </View>
  );
}
