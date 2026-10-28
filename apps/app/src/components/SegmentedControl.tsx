import { Pressable, View } from 'react-native';

import { AppText } from './AppText';
import type { LucideIcon } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { useTheme } from '@/theme';

export interface Segment<T extends string> {
  value: T;
  label: string;
  icon?: LucideIcon;
  testID?: string;
  /** A count shown after the label, as a small badge. */
  badge?: number;
}

interface Props<T extends string> {
  segments: readonly Segment<T>[];
  value: T;
  onChange: (value: T) => void;
  /** `tabs` switches panes (tablist); `radio` picks a setting (radiogroup). */
  kind?: 'tabs' | 'radio';
  accessibilityLabel: string;
  testID?: string;
  /**
   * `underline`: uppercase tracked labels with a short accent bar under the
   * selected one (the default for tabs, docs/design/SLEEPER-OBSERVED.md).
   * `pill`: equal segments on a raised track, the selected one amber (the
   * default for radio settings).
   */
  variant?: 'underline' | 'pill';
}

/** Tabs or a radio group. See `variant` for the two looks. */
export function SegmentedControl<T extends string>(props: Props<T>) {
  const variant = props.variant ?? ((props.kind ?? 'tabs') === 'tabs' ? 'underline' : 'pill');
  return variant === 'underline' ? <UnderlineTabs {...props} /> : <PillSegments {...props} />;
}

function UnderlineTabs<T extends string>({
  segments,
  value,
  onChange,
  kind = 'tabs',
  accessibilityLabel,
  testID,
}: Props<T>) {
  const { colors, spacing, radii, fontFamilies, fontSizes, lineHeights } = useTheme();
  return (
    <View
      testID={testID}
      accessibilityRole={kind === 'tabs' ? 'tablist' : 'radiogroup'}
      accessibilityLabel={accessibilityLabel}
      style={{
        flexDirection: 'row',
        borderBottomWidth: 1,
        borderBottomColor: colors.border,
      }}
    >
      {segments.map((s) => {
        const selected = s.value === value;
        const fg = selected ? colors.accentBright : colors.textMuted;
        const Icon = s.icon;
        return (
          <Pressable
            key={s.value}
            testID={s.testID}
            accessibilityRole={kind === 'tabs' ? 'tab' : 'radio'}
            accessibilityState={kind === 'tabs' ? { selected } : { checked: selected }}
            onPress={() => onChange(s.value)}
            style={(state: PressState) => [
              {
                flex: 1,
                minHeight: 44,
                alignItems: 'center',
                justifyContent: 'center',
                paddingHorizontal: spacing.xs,
                borderRadius: radii.sm,
              },
              ...pressFeedback(colors, state, { hoverFill: !selected }),
            ]}
          >
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
              {Icon ? <Icon size={15} color={fg} aria-hidden /> : null}
              <AppText
                color={fg}
                numberOfLines={1}
                style={{
                  flexShrink: 1,
                  fontFamily: fontFamilies.bold,
                  fontSize: fontSizes.sm,
                  lineHeight: lineHeights.meta,
                  letterSpacing: 0.8,
                  textTransform: 'uppercase',
                }}
              >
                {s.label}
              </AppText>
              {s.badge !== undefined && s.badge > 0 ? (
                <View
                  style={{
                    minWidth: 18,
                    paddingHorizontal: spacing.xs,
                    borderRadius: radii.pill,
                    alignItems: 'center',
                    backgroundColor: colors.danger,
                  }}
                >
                  <AppText
                    numeric
                    weight="semibold"
                    color={colors.onDanger}
                    numberOfLines={1}
                    style={{ fontSize: fontSizes.eyebrow, lineHeight: lineHeights.eyebrow + 2 }}
                  >
                    {String(s.badge)}
                  </AppText>
                </View>
              ) : null}
            </View>
            <View
              aria-hidden
              style={{
                position: 'absolute',
                bottom: -1,
                height: 3,
                width: '56%',
                borderRadius: 2,
                backgroundColor: selected ? colors.accent : 'transparent',
              }}
            />
          </Pressable>
        );
      })}
    </View>
  );
}

function PillSegments<T extends string>({
  segments,
  value,
  onChange,
  kind = 'tabs',
  accessibilityLabel,
  testID,
}: Props<T>) {
  const { colors, spacing, radii, fontSizes, lineHeights } = useTheme();
  return (
    <View
      testID={testID}
      accessibilityRole={kind === 'tabs' ? 'tablist' : 'radiogroup'}
      accessibilityLabel={accessibilityLabel}
      style={{
        flexDirection: 'row',
        padding: 3,
        gap: 3,
        borderRadius: radii.md,
        backgroundColor: colors.surfaceRaised,
      }}
    >
      {segments.map((s) => {
        const selected = s.value === value;
        const fg = selected ? colors.onAccent : colors.textMuted;
        const Icon = s.icon;
        return (
          <Pressable
            key={s.value}
            testID={s.testID}
            accessibilityRole={kind === 'tabs' ? 'tab' : 'radio'}
            accessibilityState={kind === 'tabs' ? { selected } : { checked: selected }}
            onPress={() => onChange(s.value)}
            style={(state: PressState) => [
              {
                flex: 1,
                minHeight: 38,
                flexDirection: 'row',
                alignItems: 'center',
                justifyContent: 'center',
                gap: spacing.xs,
                // Tight sides so four tabs with a count fit a 390px phone.
                paddingHorizontal: spacing.xs,
                borderRadius: radii.md - 3,
                backgroundColor: selected ? colors.accent : 'transparent',
              },
              ...pressFeedback(colors, state, { hoverFill: !selected }),
            ]}
          >
            {Icon ? <Icon size={16} color={fg} aria-hidden /> : null}
            <AppText
              variant="meta"
              weight="semibold"
              color={fg}
              numberOfLines={1}
              style={{ flexShrink: 1 }}
            >
              {s.label}
            </AppText>
            {s.badge !== undefined && s.badge > 0 ? (
              <View
                style={{
                  flexShrink: 0,
                  minWidth: 18,
                  paddingHorizontal: spacing.xs,
                  paddingVertical: spacing.xxs,
                  borderRadius: radii.pill,
                  alignItems: 'center',
                  backgroundColor: selected ? colors.onAccent : colors.surfaceSelected,
                }}
              >
                <AppText
                  numeric
                  weight="semibold"
                  color={selected ? colors.accent : colors.text}
                  numberOfLines={1}
                  style={{ fontSize: fontSizes.eyebrow, lineHeight: lineHeights.eyebrow }}
                >
                  {String(s.badge)}
                </AppText>
              </View>
            ) : null}
          </Pressable>
        );
      })}
    </View>
  );
}
