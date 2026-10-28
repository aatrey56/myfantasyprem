import type { ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from './AppText';
import type { LucideIcon } from './icons';
import { useTheme } from '@/theme';

interface Props {
  icon?: LucideIcon;
  title: string;
  body?: string;
  /** Buttons. */
  children?: ReactNode;
  testID?: string;
}

/** Centred icon, title, explanation and the next action. */
export function EmptyState({ icon: Icon, title, body, children, testID }: Props) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View
      testID={testID}
      style={{
        alignItems: 'center',
        gap: spacing.sm,
        paddingVertical: spacing.xxl,
        paddingHorizontal: spacing.lg,
      }}
    >
      {Icon ? (
        <View
          aria-hidden
          style={{
            width: 56,
            height: 56,
            borderRadius: radii.pill,
            alignItems: 'center',
            justifyContent: 'center',
            backgroundColor: colors.surfaceRaised,
            marginBottom: spacing.xs,
          }}
        >
          <Icon size={26} color={colors.textMuted} />
        </View>
      ) : null}
      <AppText variant="heading" style={{ textAlign: 'center' }} accessibilityRole="header">
        {title}
      </AppText>
      {body ? (
        <AppText muted style={{ textAlign: 'center', maxWidth: 420 }}>
          {body}
        </AppText>
      ) : null}
      {children ? (
        <View
          style={{
            flexDirection: 'row',
            flexWrap: 'wrap',
            justifyContent: 'center',
            gap: spacing.sm,
            marginTop: spacing.sm,
          }}
        >
          {children}
        </View>
      ) : null}
    </View>
  );
}
