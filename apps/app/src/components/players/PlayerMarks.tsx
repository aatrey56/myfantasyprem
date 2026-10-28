import { View } from 'react-native';

import { AppText } from '../AppText';
import type { TagMeta } from '@/lib/tags';
import { tint, useTheme } from '@/theme';

/**
 * A row's personal marks after the name: my tag's emoji (private to me)
 * and a small BLOCK chip when the player is on his team's trade block.
 */
export function PlayerMarks({ tag, onBlock }: { tag?: TagMeta | null; onBlock?: boolean }) {
  const { colors, radii } = useTheme();
  if (!tag && !onBlock) return null;
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
      {tag ? (
        <AppText
          testID="row-tag"
          accessibilityLabel={`Tagged ${tag.label}`}
          style={{ fontSize: 13, lineHeight: 16 }}
        >
          {tag.emoji}
        </AppText>
      ) : null}
      {onBlock ? (
        <View
          testID="row-block"
          accessibilityLabel="On the trade block"
          style={{
            paddingHorizontal: 4,
            borderRadius: radii.sm,
            backgroundColor: tint(colors.accent, colors.surface),
          }}
        >
          <AppText
            weight="bold"
            color={colors.accentBright}
            style={{ fontSize: 10, lineHeight: 14, letterSpacing: 0.6 }}
          >
            BLOCK
          </AppText>
        </View>
      ) : null}
    </View>
  );
}

/** Spoken marks for a row's label. */
export function marksA11y(tag?: TagMeta | null, onBlock?: boolean): string {
  return [tag ? `tagged ${tag.label}` : '', onBlock ? 'on the trade block' : '']
    .filter(Boolean)
    .join(', ');
}
