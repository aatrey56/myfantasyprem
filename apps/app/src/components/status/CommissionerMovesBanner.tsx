import { useRouter } from 'expo-router';
import { Pressable, View } from 'react-native';

import {
  useCommissionerMovesSummary,
  useMarkCommissionerMovesSeen,
} from '@/api/commissioner-hooks';
import { AppText } from '../AppText';
import { ChevronRight, Shield } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { movesBanner, movesHref } from '@/lib/commissioner-moves';
import { tint, useTheme } from '@/theme';

/**
 * "Commissioner moves · 2 new": shown while the commissioner has done
 * something the signed-in member has not seen (docs/adr/0017). It stays
 * across sessions and devices (the seen-up-to marker is the server's) until
 * they tap it: that marks the moves seen and opens League settings at the
 * Commissioner Moves section.
 */
export function CommissionerMovesBanner({ leagueId }: { leagueId: string }) {
  const { colors, spacing, radii } = useTheme();
  const router = useRouter();
  const summary = useCommissionerMovesSummary(leagueId);
  const mark = useMarkCommissionerMovesSeen(leagueId);
  const banner = movesBanner(summary.data?.data);
  if (!banner) return null;
  return (
    <Pressable
      accessibilityRole="link"
      accessibilityLabel={`${banner.title}${banner.detail ? `. ${banner.detail}` : ''}`}
      testID="commissioner-moves-banner"
      onPress={() => {
        mark.mutate(summary.data?.data.latest?.id);
        router.push(movesHref(leagueId) as never);
      }}
      style={(state: PressState) => [
        {
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.md,
          minHeight: 56,
          paddingHorizontal: spacing.lg,
          paddingVertical: spacing.sm,
          borderRadius: radii.md,
          borderWidth: 1,
          borderColor: colors.accent,
          backgroundColor: tint(colors.accent, colors.background, 0.12),
        },
        ...pressFeedback(colors, state, { hoverFill: false }),
      ]}
    >
      <Shield size={18} color={colors.accent} strokeWidth={2.4} aria-hidden />
      <View style={{ flex: 1, minWidth: 0 }}>
        <AppText weight="semibold" testID="commissioner-moves-title">
          {banner.title}
        </AppText>
        {banner.detail ? (
          <AppText variant="meta" muted numberOfLines={2} testID="commissioner-moves-latest">
            {banner.detail}
          </AppText>
        ) : null}
      </View>
      <ChevronRight size={18} color={colors.textMuted} aria-hidden />
    </Pressable>
  );
}
