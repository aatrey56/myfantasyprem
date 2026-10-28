import type { ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from './AppText';
import { pressFeedback, type PressState } from './pressable';
import { useTheme } from '@/theme';

interface Props {
  title: string;
  /** A count or note after the title. */
  detail?: string;
  action?: { label: string; onPress: () => void; testID?: string };
  right?: ReactNode;
  testID?: string;
}

/** Eyebrow-style section title with an optional action on the right. */
export function SectionHeader({ title, detail, action, right, testID }: Props) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View
      testID={testID}
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        justifyContent: 'space-between',
        gap: spacing.sm,
        minHeight: 32,
      }}
    >
      <View
        style={{ flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm, flexShrink: 1 }}
      >
        <AppText variant="eyebrow" muted accessibilityRole="header" numberOfLines={1}>
          {title}
        </AppText>
        {detail ? (
          <AppText variant="meta" subtle numeric>
            {detail}
          </AppText>
        ) : null}
      </View>
      {right}
      {action ? (
        <Pressable
          accessibilityRole="button"
          onPress={action.onPress}
          testID={action.testID}
          hitSlop={8}
          style={(state: PressState) => [
            { paddingHorizontal: spacing.sm, paddingVertical: spacing.xs, borderRadius: radii.sm },
            ...pressFeedback(colors, state),
          ]}
        >
          <AppText variant="meta" accent weight="semibold">
            {action.label}
          </AppText>
        </Pressable>
      ) : null}
    </View>
  );
}
