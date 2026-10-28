import { StyleSheet, Text, type TextProps, type TextStyle } from 'react-native';

import { useTheme, type Theme } from '@/theme';

/** BRIEF §5 type scale. `caption` is the old name for `meta`. */
export type TextVariant = 'display' | 'title' | 'heading' | 'body' | 'meta' | 'caption' | 'eyebrow';

interface Props extends TextProps {
  variant?: TextVariant;
  /** text-2. */
  muted?: boolean;
  /** text-3: background and surface only. */
  subtle?: boolean;
  accent?: boolean;
  /** Tabular figures, for scores, ranks and clocks. */
  numeric?: boolean;
  weight?: keyof Theme['fontWeights'];
  color?: string;
}

function variantStyle(theme: Theme, variant: TextVariant): TextStyle {
  const { fontSizes, fontWeights, lineHeights } = theme;
  switch (variant) {
    case 'display':
      return {
        fontSize: fontSizes.display,
        lineHeight: lineHeights.display,
        fontWeight: fontWeights.bold,
      };
    case 'title':
      return {
        fontSize: fontSizes.xl,
        lineHeight: lineHeights.title,
        fontWeight: fontWeights.bold,
      };
    case 'heading':
      return {
        fontSize: fontSizes.heading,
        lineHeight: lineHeights.heading,
        fontWeight: fontWeights.semibold,
      };
    case 'meta':
    case 'caption':
      return { fontSize: fontSizes.sm, lineHeight: lineHeights.meta };
    case 'eyebrow':
      return {
        fontSize: fontSizes.eyebrow,
        lineHeight: lineHeights.eyebrow,
        fontWeight: fontWeights.semibold,
        letterSpacing: 0.6,
        textTransform: 'uppercase',
      };
    case 'body':
      return { fontSize: fontSizes.md, lineHeight: lineHeights.body };
  }
}

/**
 * Dynamic Type: text follows the user's iOS text size, capped so dense
 * rows, scores and fixed-height chrome keep their layout. Big text grows
 * least (it is already large); body copy grows most.
 */
export const MAX_FONT_SCALE: Record<TextVariant, number> = {
  display: 1.15,
  title: 1.25,
  heading: 1.35,
  body: 1.4,
  meta: 1.35,
  caption: 1.35,
  eyebrow: 1.2,
};

/** For text inputs: the same cap as body copy. */
export const INPUT_MAX_FONT_SCALE = MAX_FONT_SCALE.body;

/**
 * Geist ships one file per weight, so the weight picks the family (a custom
 * family plus `fontWeight` renders wrongly on Android). `numeric` text uses
 * Geist Mono, which has medium and semibold only.
 */
function familyFor(theme: Theme, fontWeight: unknown, numeric: boolean): string {
  const { fontFamilies } = theme;
  const w = fontWeight === 'bold' ? 700 : Number(fontWeight ?? 400) || 400;
  if (numeric) return w >= 600 ? fontFamilies.monoSemibold : fontFamilies.monoMedium;
  if (w >= 700) return fontFamilies.bold;
  if (w >= 600) return fontFamilies.semibold;
  if (w >= 500) return fontFamilies.medium;
  return fontFamilies.regular;
}

export function AppText({
  variant = 'body',
  muted,
  subtle,
  accent,
  numeric,
  weight,
  color,
  style,
  ...rest
}: Props) {
  const theme = useTheme();
  const c = theme.colors;
  const resolved =
    color ?? (accent ? c.accent : subtle ? c.textSubtle : muted ? c.textMuted : c.text);
  const { fontWeight, ...flat } = StyleSheet.flatten([
    variantStyle(theme, variant),
    { color: resolved },
    numeric ? { fontVariant: ['tabular-nums'] } : null,
    weight ? { fontWeight: theme.fontWeights[weight] } : null,
    style,
  ]) as TextStyle;
  // A caller-supplied fontFamily wins; otherwise Geist (or Geist Mono).
  return (
    <Text
      maxFontSizeMultiplier={MAX_FONT_SCALE[variant]}
      {...rest}
      style={[{ fontFamily: familyFor(theme, fontWeight, !!numeric) }, flat]}
    />
  );
}
