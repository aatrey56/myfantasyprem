import { View } from 'react-native';

import { AppText } from '../AppText';
import { IconButton } from '../Button';
import { Bot, X } from '../icons';
import type { MissedClockNotice } from '@/lib/draft';
import { useTheme } from '@/theme';

/**
 * Says who was autopicked when a pick clock ran out, and with which
 * player. Announced politely to screen readers; dismissible.
 */
export function MissedClockBanner({
  notice,
  onDismiss,
}: {
  notice: MissedClockNotice;
  onDismiss: () => void;
}) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View
      testID="missed-clock-banner"
      accessibilityRole="alert"
      aria-live="polite"
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.sm,
        paddingLeft: spacing.md,
        borderRadius: radii.md,
        borderWidth: 1,
        borderColor: notice.mine ? colors.warning : colors.border,
        backgroundColor: colors.surface,
      }}
    >
      <Bot size={18} color={notice.mine ? colors.warning : colors.textMuted} aria-hidden />
      <AppText variant="meta" weight={notice.mine ? 'semibold' : undefined} style={{ flex: 1 }}>
        {notice.text}
      </AppText>
      <IconButton icon={X} label="Dismiss" onPress={onDismiss} size={16} />
    </View>
  );
}
