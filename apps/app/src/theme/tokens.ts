/**
 * Design tokens (docs/design/BRIEF.md §5; values from the 2026 design system
 * board). Dark only, whatever the device setting: neutral graphite with one amber accent. Green and red mean winning and losing only. Position
 * colours tint badges at 16% with full-colour text. No Premier League,
 * club or Sleeper colours.
 */

export interface ColorTokens {
  /** App background (`bg`). */
  background: string;
  /** Cards and panels (`surface`). */
  surface: string;
  /** Sheets, cards on cards, hovered rows, selected nav item (`surface-2`). */
  surfaceRaised: string;
  border: string;
  text: string;
  /** Secondary text (`text-2`). */
  textMuted: string;
  /** Tertiary text (`text-3`): the floor for any text. Only on background and surface. */
  textSubtle: string;
  /** The single accent: selected, live, links, focus. */
  accent: string;
  /** Text on top of the accent. */
  onAccent: string;
  /** Winning only. */
  win: string;
  /** Losing only (and destructive actions). */
  loss: string;
  /** Stale-data banner background. */
  warning: string;
  onWarning: string;
  /** Modal backdrop. */
  scrim: string;
  /** Inset tracks (segmented control track), sunken under a surface (`ground-2`). */
  surfaceSunken: string;
  /** Secondary buttons and neutral controls. */
  surfaceControl: string;
  /** Selected segment / selected nav item. */
  surfaceSelected: string;
  /** Brighter amber for text on dark: live labels, GW chips (`amber-2`). */
  accentBright: string;
  /** Solid alert red: Out status, destructive confirm. */
  danger: string;
  /** Text and icons on `danger`. */
  onDanger: string;
  /** Red text on the dark ground: outlined destructive buttons (Veto). */
  dangerText: string;
  /** Pitch only. Base grass. */
  turf: string;
  /** Pitch only. Mown stripe. */
  turfStripe: string;
  posGKP: string;
  posDEF: string;
  posMID: string;
  posFWD: string;
  /** Text on a solid position pill (dark on the pastel position colours). */
  onPosition: string;
}

export const darkColors: ColorTokens = {
  background: '#0B0D10',
  surface: '#14171C',
  surfaceRaised: '#1B1F26',
  border: '#2A303A',
  text: '#F2F4F7',
  textMuted: '#A6ADBA',
  textSubtle: '#7C8594',
  accent: '#FFB020',
  onAccent: '#1A1204',
  win: '#34D399',
  loss: '#F87171',
  warning: '#FFC21A',
  onWarning: '#1A1204',
  scrim: 'rgba(4, 5, 7, 0.8)',
  surfaceSunken: '#0F1216',
  surfaceControl: '#1F242C',
  surfaceSelected: '#2A303A',
  accentBright: '#FFC857',
  danger: '#DA291C',
  onDanger: '#FFFFFF',
  dangerText: '#FCA5A5',
  turf: '#1E7A46',
  turfStripe: '#22854D',
  posGKP: '#FCD34D',
  posDEF: '#93C5FD',
  posMID: '#6EE7B7',
  posFWD: '#F9A8D4',
  onPosition: '#0B0D10',
};

/** Share of the colour in a tinted fill (position badges, selected rows). */
export const TINT_ALPHA = 0.16;

/** `fg` laid over `bg` at `alpha`, as an opaque #RRGGBB. Both must be #RRGGBB. */
export function tint(fg: string, bg: string, alpha = TINT_ALPHA): string {
  const ch = (hex: string, i: number) => parseInt(hex.slice(i, i + 2), 16);
  return `#${[1, 3, 5]
    .map((i) =>
      Math.round(ch(fg, i) * alpha + ch(bg, i) * (1 - alpha))
        .toString(16)
        .padStart(2, '0'),
    )
    .join('')}`;
}

type ColorKey = keyof ColorTokens;

/**
 * Text colour on background colour pairs that must meet WCAG AA (4.5:1).
 * theme/contrast.test.ts checks every pair.
 */
export const TEXT_PAIRS: readonly (readonly [ColorKey, ColorKey])[] = [
  ['text', 'background'],
  ['text', 'surface'],
  ['text', 'surfaceRaised'],
  ['textMuted', 'background'],
  ['textMuted', 'surface'],
  ['textMuted', 'surfaceRaised'],
  ['textSubtle', 'background'],
  ['textSubtle', 'surface'],
  ['accent', 'background'],
  ['accent', 'surface'],
  ['accent', 'surfaceRaised'],
  ['onAccent', 'accent'],
  ['win', 'background'],
  ['win', 'surface'],
  ['win', 'surfaceRaised'],
  ['loss', 'background'],
  ['loss', 'surface'],
  ['loss', 'surfaceRaised'],
  ['onWarning', 'warning'],
  ['onDanger', 'danger'],
  ['text', 'surfaceSunken'],
  ['text', 'surfaceControl'],
  ['text', 'surfaceSelected'],
  ['textMuted', 'surfaceSunken'],
  ['textMuted', 'surfaceControl'],
  ['accentBright', 'background'],
  ['accentBright', 'surface'],
  ['dangerText', 'background'],
  ['dangerText', 'surface'],
  ['onAccent', 'accentBright'],
  ['posGKP', 'surface'],
  ['posDEF', 'surface'],
  ['posMID', 'surface'],
  ['posFWD', 'surface'],
  ['onPosition', 'posGKP'],
  ['onPosition', 'posDEF'],
  ['onPosition', 'posMID'],
  ['onPosition', 'posFWD'],
];

/**
 * Text on a 16% tint of a colour over a surface: [text, tint colour, surface].
 * Position badges, the selected-row fill and the accent chip use these.
 */
export const TINT_PAIRS: readonly (readonly [ColorKey, ColorKey, ColorKey])[] = [
  ['posGKP', 'posGKP', 'surface'],
  ['posDEF', 'posDEF', 'surface'],
  ['posMID', 'posMID', 'surface'],
  ['posFWD', 'posFWD', 'surface'],
  ['posGKP', 'posGKP', 'background'],
  ['posDEF', 'posDEF', 'background'],
  ['posMID', 'posMID', 'background'],
  ['posFWD', 'posFWD', 'background'],
  ['text', 'accent', 'surface'],
  ['accent', 'accent', 'surface'],
  ['text', 'accent', 'background'],
  ['win', 'win', 'surface'],
  ['loss', 'loss', 'surface'],
  ['accentBright', 'accent', 'background'],
  ['accentBright', 'accent', 'surface'],
];

/** Visible keyboard-focus outline for pressables on web. */
export function focusRing(colors: ColorTokens) {
  return {
    outlineColor: colors.accent,
    outlineStyle: 'solid',
    outlineWidth: 2,
    outlineOffset: 2,
  } as const;
}

/** 4-pt scale. Gutters: 16 on phone, 24 on desktop. */
export const spacing = { xxs: 2, xs: 4, sm: 8, md: 12, lg: 16, xl: 24, xxl: 32 } as const;

/** Chip 999, badge 8, card 16, panel 24, sheet 20. `md` is for inputs and buttons. */
export const radii = { sm: 8, md: 12, lg: 16, sheet: 20, panel: 24, pill: 999 } as const;

/** Display 40, title 26, section 18, body 15, caption 13, label 12. */
export const fontSizes = {
  eyebrow: 12,
  xs: 13,
  sm: 13,
  md: 15,
  heading: 18,
  lg: 18,
  xl: 26,
  display: 40,
  /** Big score lines (mono). */
  score: 56,
} as const;

export const lineHeights = {
  eyebrow: 14,
  meta: 18,
  body: 20,
  heading: 24,
  title: 32,
  display: 44,
} as const;

/**
 * Font families, loaded in the root layout (`theme/fonts.ts`). One family per
 * weight because React Native cannot synthesise weights from a single file.
 */
export const fontFamilies = {
  regular: 'Geist_400Regular',
  medium: 'Geist_500Medium',
  semibold: 'Geist_600SemiBold',
  bold: 'Geist_700Bold',
  monoMedium: 'GeistMono_500Medium',
  monoSemibold: 'GeistMono_600SemiBold',
} as const;

export const fontWeights = {
  regular: '400',
  medium: '500',
  semibold: '600',
  bold: '700',
} as const;

/** Layout constants (BRIEF §2). */
export const layout = {
  /** Phone below `tablet` (bottom tabs), top nav bar from `tablet`, side panels from `wide`. */
  breakpoints: { tablet: 768, wide: 1024 },
  maxContentWidth: 960,
  /** The horizontal league nav bar from `tablet` up. */
  topNavHeight: 56,
  detailPanelWidth: 380,
  bottomTabsHeight: 60,
  /** Minimum touch target. */
  touch: 44,
  /** List row height (BRIEF: 52-56). */
  rowHeight: 56,
} as const;

export const motion = { fast: 150, normal: 200, sheet: 300 } as const;

export interface Theme {
  colors: ColorTokens;
  spacing: typeof spacing;
  radii: typeof radii;
  fontSizes: typeof fontSizes;
  lineHeights: typeof lineHeights;
  fontWeights: typeof fontWeights;
  fontFamilies: typeof fontFamilies;
  layout: typeof layout;
  motion: typeof motion;
}

export const darkTheme: Theme = {
  colors: darkColors,
  spacing,
  radii,
  fontSizes,
  lineHeights,
  fontWeights,
  fontFamilies,
  layout,
  motion,
};

/** The colour key for a position. Unknown positions (open enum) fall back to muted text. */
export function positionColorKey(position: string): ColorKey {
  switch (position) {
    case 'GKP':
      return 'posGKP';
    case 'DEF':
      return 'posDEF';
    case 'MID':
      return 'posMID';
    case 'FWD':
      return 'posFWD';
    default:
      return 'textMuted';
  }
}
