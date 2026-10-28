import { View } from 'react-native';
import Svg, { Path } from 'react-native-svg';

import { AppText } from './AppText';
import { useBrandedAssets } from './branded/BrandedAssets';
import { RemoteImage } from './branded/RemoteImage';
import { clubCrestUrl, clubShirtUrl, playerPhotoUrl } from '@/api/assets';
import { clubColors, initialsOf, userColor } from '@/lib/identity';
import { positionColorKey, useTheme } from '@/theme';

/** Display labels. The contract says GKP; the UI says GK (BRIEF). Unknown values pass through. */
export function positionLabel(position: string): string {
  return position === 'GKP' ? 'GK' : position;
}

const PILL_SIZES = {
  sm: { minWidth: 30, height: 18, fontSize: 11 },
  md: { minWidth: 36, height: 22, fontSize: 12 },
  /** Roster rows: the pill is the tap target that opens the swap sheet. */
  lg: { minWidth: 44, height: 28, fontSize: 13 },
  /** Head-to-head rows: a square in the middle of the row. */
  square: { minWidth: 30, height: 30, fontSize: 12 },
} as const;

/**
 * GK / DEF / MID / FWD as a solid pastel pill with dark text
 * (docs/design/SLEEPER-OBSERVED.md). Unknown positions get a muted pill.
 */
export function PositionBadge({
  position,
  size = 'md',
  muted = false,
}: {
  position: string;
  size?: keyof typeof PILL_SIZES;
  /** Bench rows: a quiet grey pill instead of the position colour. */
  muted?: boolean;
}) {
  const { colors, radii, fontFamilies } = useTheme();
  const key = positionColorKey(position);
  const known = key !== 'textMuted' && !muted;
  const dims = PILL_SIZES[size];
  return (
    <View
      accessibilityLabel={`Position ${positionLabel(position)}`}
      style={{
        minWidth: dims.minWidth,
        paddingHorizontal: 4,
        height: dims.height,
        borderRadius: size === 'sm' ? 6 : radii.sm,
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: known ? colors[key] : colors.surfaceSelected,
      }}
    >
      <AppText
        color={known ? colors.onPosition : colors.textMuted}
        style={{
          fontFamily: fontFamilies.bold,
          fontSize: dims.fontSize,
          lineHeight: dims.height - 4,
          letterSpacing: 0.3,
        }}
      >
        {positionLabel(position)}
      </AppText>
    </View>
  );
}

/**
 * Our own shirt outline, filled with a colour derived from the club code.
 * Not a kit: no stripes, crests or sponsor marks. Decorative, so hidden from
 * screen readers (the club code next to it carries the meaning).
 */
export function ShirtGlyph({ code, size = 16 }: { code: string; size?: number }) {
  const { colors } = useTheme();
  const { body, trim } = clubColors(code);
  return (
    <View
      testID="generic-shirt"
      aria-hidden
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
    >
      <Svg width={size} height={size} viewBox="0 0 24 24">
        <Path
          d="M8 2.5 3 5l-1.5 5 3.5 1.2V21.5h14V11.2L22.5 10 21 5l-5-2.5c-.6 1.6-2.2 2.6-4 2.6s-3.4-1-4-2.6Z"
          fill={body}
          stroke={colors.border}
          strokeWidth={1}
          strokeLinejoin="round"
        />
        <Path
          d="M8 2.5c.6 1.6 2.2 2.6 4 2.6s3.4-1 4-2.6"
          fill="none"
          stroke={trim}
          strokeWidth={1.6}
        />
      </Svg>
    </View>
  );
}

/**
 * The club's real shirt (the goalkeeper's for keepers) while branded
 * images are on (useBrandedAssets); otherwise, or if it fails to load, our
 * generic ShirtGlyph. Same box either way.
 */
export function ClubShirt({
  code,
  goalkeeper = false,
  size = 16,
}: {
  code: string;
  goalkeeper?: boolean;
  size?: number;
}) {
  const branded = useBrandedAssets();
  const generic = <ShirtGlyph code={code} size={size} />;
  if (!branded) return generic;
  return (
    <RemoteImage
      kind="shirt"
      uri={clubShirtUrl(code, goalkeeper)}
      width={size}
      height={size}
      fallback={generic}
    />
  );
}

/**
 * The club's crest while branded images are on. Off, nothing (callers that
 * always showed a shirt use ClubTag); a crest that fails shows ShirtGlyph.
 */
export function ClubCrest({ code, size = 14 }: { code: string; size?: number }) {
  const branded = useBrandedAssets();
  if (!branded) return null;
  return (
    <RemoteImage
      kind="crest"
      uri={clubCrestUrl(code)}
      width={size}
      height={size}
      fallback={<ShirtGlyph code={code} size={size} />}
    />
  );
}

/**
 * A player's photo while branded images are on, else (or on failure) the
 * club shirt glyph. A square box: photos are portrait and sit inside it.
 */
export function PlayerPhoto({
  playerId,
  code,
  size,
  fallbackSize = size,
  placeholder,
}: {
  playerId: string;
  /** The player's club, for the fallback shirt. */
  code: string;
  size: number;
  /** The fallback shirt's size, when it is smaller than the photo's box. */
  fallbackSize?: number;
  placeholder?: string;
}) {
  const branded = useBrandedAssets();
  const generic = <ShirtGlyph code={code} size={fallbackSize} />;
  if (!branded) return generic;
  return (
    <RemoteImage
      kind="photo"
      uri={playerPhotoUrl(playerId)}
      width={size}
      height={size}
      placeholder={placeholder}
      fallback={generic}
    />
  );
}

/** The club's crest (or our shirt glyph) and the 3-letter club code. */
export function ClubTag({
  code,
  name,
  muted = true,
}: {
  code: string;
  name?: string;
  muted?: boolean;
}) {
  const { spacing } = useTheme();
  const branded = useBrandedAssets();
  return (
    <View
      accessibilityLabel={name ?? code}
      style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}
    >
      {branded ? <ClubCrest code={code} size={14} /> : <ShirtGlyph code={code} size={14} />}
      <AppText variant="meta" muted={muted} weight="medium">
        {code.toUpperCase()}
      </AppText>
    </View>
  );
}

interface AvatarProps {
  name: string;
  /** #RRGGBB from the profile; otherwise derived from the name. */
  color?: string | null;
  size?: number;
  testID?: string;
  /** Rounded square, for leagues and teams; circles are people. */
  square?: boolean;
}

/** Initials on a colour. No photos. Text is always dark or light to stay readable. */
export function Avatar({ name, color, size = 36, testID, square }: AvatarProps) {
  const { bg, fg } = userColor(name, color);
  return (
    <View
      testID={testID}
      aria-hidden
      style={{
        width: size,
        height: size,
        borderRadius: square ? size * 0.28 : size / 2,
        backgroundColor: bg,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <AppText
        weight="bold"
        color={fg}
        style={{ fontSize: Math.round(size * 0.38), lineHeight: Math.round(size * 0.5) }}
      >
        {initialsOf(name)}
      </AppText>
    </View>
  );
}
