import type { Href } from 'expo-router';
import type { ReactNode } from 'react';
import { Pressable, View, type ViewStyle } from 'react-native';

import { AppText } from './AppText';
import { ChevronRight } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { PressableLink } from './PressableLink';
import { tint, useTheme } from '@/theme';

interface Props {
  title: ReactNode;
  subtitle?: ReactNode;
  left?: ReactNode;
  right?: ReactNode;
  onPress?: () => void;
  href?: Href;
  chevron?: boolean;
  selected?: boolean;
  disabled?: boolean;
  /** Draws a divider above the row (rows after the first in a card). */
  divider?: boolean;
  testID?: string;
  accessibilityLabel?: string;
  accessibilityHint?: string;
  style?: ViewStyle;
}

/**
 * A 56px list row: optional leading element, a title with a subtitle, and a
 * trailing element. Pressable when it has `onPress` or `href`.
 */
export function ListRow({
  title,
  subtitle,
  left,
  right,
  onPress,
  href,
  chevron,
  selected,
  disabled,
  divider,
  testID,
  accessibilityLabel,
  accessibilityHint,
  style,
}: Props) {
  const { colors, spacing, layout } = useTheme();
  const base: ViewStyle = {
    minHeight: layout.rowHeight,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderTopWidth: divider ? 1 : 0,
    borderTopColor: colors.border,
    backgroundColor: selected ? tint(colors.accent, colors.surface) : 'transparent',
  };
  const content = (
    <>
      {left}
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        {typeof title === 'string' ? (
          <AppText weight="semibold" numberOfLines={1}>
            {title}
          </AppText>
        ) : (
          title
        )}
        {typeof subtitle === 'string' ? (
          <AppText variant="meta" muted numberOfLines={1}>
            {subtitle}
          </AppText>
        ) : (
          subtitle
        )}
      </View>
      {right}
      {chevron ? <ChevronRight size={18} color={colors.textMuted} aria-hidden /> : null}
    </>
  );
  if (!onPress && !href) {
    return (
      <View testID={testID} style={[base, style]}>
        {content}
      </View>
    );
  }
  const shared = {
    testID,
    accessibilityLabel,
    accessibilityHint,
    accessibilityState: { disabled: Boolean(disabled), selected },
    disabled,
    style: (state: PressState) => [
      base,
      ...pressFeedback(colors, state, { disabled }),
      style ?? {},
    ],
  };
  return href ? (
    <PressableLink href={href} {...shared}>
      {content}
    </PressableLink>
  ) : (
    <Pressable accessibilityRole="button" onPress={onPress} {...shared}>
      {content}
    </Pressable>
  );
}
