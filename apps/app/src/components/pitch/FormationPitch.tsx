import { useState } from 'react';
import { Pressable, StyleSheet, useWindowDimensions, View, type ViewStyle } from 'react-native';
import Svg, { Circle, Path, Rect } from 'react-native-svg';

import { AppText } from '../AppText';
import { ArrowDown, ArrowUp, TriangleAlert } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import {
  compactPointsBadge,
  COMPACT_POINTS,
  fitsFull,
  PITCH,
  PITCH_FONT_SCALE_MAX,
  PITCH_HEIGHT,
  pitchMarkings,
  placeLines,
  SLOT,
  tokenHeight,
} from './pitchGeometry';
import { useBrandedAssets } from '../branded/BrandedAssets';
import { RemoteImage } from '../branded/RemoteImage';
import { clubShirtUrl } from '@/api/assets';
import { useLayout } from '@/hooks/useLayout';
import { darkColors } from '@/theme';

export interface PitchPlayer {
  id: string;
  name: string;
  /** Generic shirt fill: a plain colour, no kit art. */
  kit: string;
  /** Club code: the real shirt while branded images are on (Shirt). */
  club?: string;
  /** The goalkeeper's shirt. */
  goalkeeper?: boolean;
  /** Variant 'lineup': the line under the name (the fixture). */
  detail?: string;
  /** Variant 'points': the score in the box under the name. */
  points?: number;
  /** A warning mark by the shirt. */
  flag?: 'doubtful' | 'out';
  autoSub?: { kind: 'in' | 'out'; projected: boolean };
  /** Faded, e.g. not a legal swap for the selected player. */
  dimmed?: boolean;
  /** Extra words for screen readers (position, fixture, status). */
  description?: string;
}

export interface PitchLine {
  /** Stable key, also the line's testID suffix (`pitch-line-<key>`). */
  key: string;
  /** Screen reader name for the line, e.g. "Defenders". */
  label?: string;
  players: PitchPlayer[];
}

export type PitchVariant = 'lineup' | 'points';

/** What tapping a player does: 'swap' selects them (a toggle), 'open' opens their card. */
export type PitchPressAction = 'swap' | 'open';

interface Props {
  /** The XI in lines, keeper's line first: it is drawn at the bottom, attacking upward. */
  lines: PitchLine[];
  /** 'lineup': name and fixture in the chip. 'points': name chip over a bordered points box. */
  variant?: PitchVariant;
  /** Tapping a player (e.g. to swap). Without it players are not buttons. */
  onPress?: (id: string) => void;
  /** How a tap is announced: a swap toggle (default) or opening the player card. */
  pressAction?: PitchPressAction;
  /** The round ⓘ button at each player's top right (the player card). */
  onInfo?: (id: string) => void;
  /** Ringed in amber. */
  selectedId?: string | null;
  /** Pitch height; below what full-size players need, they turn compact. */
  height?: number;
  maxWidth?: number;
  /** testID prefix of each player box. */
  playerTestID?: string;
  testID?: string;
}

/**
 * A formation on our drawn half pitch: stripes, the halfway line along the
 * top, our box and goal at the bottom, players in lines attacking upward
 * (forwards at the top edge, each line spread evenly across the width). Each
 * player is a plain shirt over a dark name chip; the lineup variant shows
 * the fixture, the points variant a points box. Used by the team page and
 * shareable graphics.
 */
export function FormationPitch({
  lines,
  variant = 'lineup',
  onPress,
  pressAction = 'swap',
  onInfo,
  selectedId = null,
  height = PITCH_HEIGHT,
  maxWidth = 520,
  playerTestID = 'pitch-player-',
  testID = 'pitch-xi',
}: Props) {
  const { width: window } = useLayout();
  const [measured, setMeasured] = useState<number | null>(null);
  const w = measured ?? Math.min(maxWidth, window - 32);
  const { fontScale } = useWindowDimensions();
  const compact = !fitsFull(height, lines.length, variant, fontScale);
  const tokenH = tokenHeight(variant, compact, fontScale);
  const placed = placeLines(
    lines.map((l) => l.players),
    w,
    height,
    tokenH,
  );
  return (
    <View
      testID={testID}
      onLayout={(e) => {
        const lw = e.nativeEvent.layout.width;
        if (lw > 0 && (measured === null || Math.abs(measured - lw) > 0.5)) setMeasured(lw);
      }}
      style={{
        width: '100%',
        maxWidth,
        alignSelf: 'center',
        height,
        borderRadius: 20,
        overflow: 'hidden',
        backgroundColor: PITCH.grass,
      }}
    >
      <View
        aria-hidden
        pointerEvents="none"
        testID="pitch-background"
        style={StyleSheet.absoluteFill}
      >
        <Markings w={w} h={height} />
      </View>
      {/* Top line first, so reading and tab order follow the screen. */}
      {lines
        .map((line, i) => ({ line, i }))
        .reverse()
        .map(({ line, i }) => {
          const inLine = placed.filter((p) => p.line === i);
          const top = inLine[0]?.top ?? 0;
          return (
            <View
              key={line.key}
              testID={`pitch-line-${line.key}`}
              accessibilityLabel={line.label}
              pointerEvents="box-none"
              style={{ position: 'absolute', left: 0, right: 0, top }}
            >
              {inLine.map((p) => (
                <PitchToken
                  key={p.item.id}
                  player={p.item}
                  variant={variant}
                  left={p.left}
                  width={p.width}
                  compact={compact}
                  height={tokenH}
                  selected={selectedId === p.item.id}
                  onPress={onPress}
                  pressAction={pressAction}
                  onInfo={onInfo}
                  testID={`${playerTestID}${p.item.id}`}
                />
              ))}
            </View>
          );
        })}
    </View>
  );
}

function Markings({ w, h }: { w: number; h: number }) {
  const m = pitchMarkings(w, h);
  const mid = w / 2;
  const line = { stroke: PITCH.line, strokeWidth: 2, fill: 'none' } as const;
  return (
    <Svg width={w} height={h}>
      {m.stripes.map((s) => (
        <Rect key={s.y} x={0} y={s.y} width={w} height={s.h} fill={PITCH.stripe} />
      ))}
      <Rect x={m.inset} y={m.inset} width={w - 2 * m.inset} height={h - 2 * m.inset} {...line} />
      <Path
        d={`M${mid - m.circleR} ${m.halfway}a${m.circleR} ${m.circleR} 0 0 0 ${2 * m.circleR} 0`}
        {...line}
      />
      <Rect x={m.box.x} y={m.box.y} width={m.box.w} height={m.box.h} {...line} />
      <Rect x={m.six.x} y={m.six.y} width={m.six.w} height={m.six.h} {...line} />
      <Path
        d={`M${mid - m.arc.half} ${m.box.y}a${m.arc.r} ${m.arc.r} 0 0 1 ${2 * m.arc.half} 0`}
        {...line}
      />
      <Rect x={m.goal.x} y={m.goal.y} width={m.goal.w} height={m.goal.h} {...line} />
      <Circle cx={mid} cy={m.penaltySpot} r={2.5} fill={PITCH.spot} />
      <Circle cx={mid} cy={m.halfway} r={2.5} fill={PITCH.spot} />
    </Svg>
  );
}

/**
 * The club's real shirt (the goalkeeper's for keepers) while branded images
 * are on; otherwise, or if it fails to load, a plain shirt shape with a
 * collar line in `kit`. Same box either way.
 */
export function Shirt({
  kit,
  size,
  club,
  goalkeeper,
}: {
  kit: string;
  size: number;
  club?: string;
  goalkeeper?: boolean;
}) {
  const branded = useBrandedAssets();
  const generic = <GenericShirt kit={kit} size={size} />;
  if (!branded || !club) return generic;
  return (
    <RemoteImage
      kind="shirt"
      uri={clubShirtUrl(club, goalkeeper)}
      width={size}
      height={size}
      placeholder="rgba(255,255,255,0.12)"
      fallback={generic}
    />
  );
}

/** A plain shirt shape with a collar line. */
function GenericShirt({ kit, size }: { kit: string; size: number }) {
  return (
    <Svg testID="generic-shirt" width={size} height={size} viewBox="0 0 48 48" aria-hidden>
      <Path d="M17 6h14l10 6-4 9-4-2v23H15V19l-4 2-4-9z" fill={kit} />
      <Path d="M19 6c1 3 3 4 5 4s4-1 5-4" fill="none" stroke={PITCH.text} strokeWidth={2} />
    </Svg>
  );
}

/** The round ⓘ button that opens a player's card. */
export function InfoButton({
  name,
  onPress,
  size = 26,
  background = PITCH.chip,
  color = PITCH.text,
  style,
  testID,
}: {
  name: string;
  onPress: () => void;
  size?: number;
  background?: string;
  color?: string;
  style?: ViewStyle;
  testID?: string;
}) {
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={`Player card for ${name}`}
      onPress={onPress}
      hitSlop={6}
      style={(state: PressState) => [
        {
          position: 'absolute',
          width: size,
          height: size,
          borderRadius: size / 2,
          backgroundColor: background,
          alignItems: 'center',
          justifyContent: 'center',
        },
        style,
        ...pressFeedback(darkColors, state, { hoverFill: false }),
      ]}
    >
      <Svg
        width={size / 2}
        height={size / 2}
        viewBox="0 0 24 24"
        fill="none"
        stroke={color}
        strokeWidth={2.4}
        strokeLinecap="round"
        aria-hidden
      >
        <Path d="M12 11v6M12 7.5v.5" />
      </Svg>
    </Pressable>
  );
}

/** A small arrow on the shirt: on (green, up) or off (red, down); dashed while projected. */
function AutoSubBadge({ kind, projected }: { kind: 'in' | 'out'; projected: boolean }) {
  const fg = kind === 'in' ? darkColors.win : darkColors.loss;
  const Icon = kind === 'in' ? ArrowUp : ArrowDown;
  return (
    <View
      testID={`autosub-${kind}`}
      style={{
        position: 'absolute',
        left: -6,
        bottom: -2,
        width: 18,
        height: 18,
        borderRadius: 9,
        borderWidth: 1,
        borderStyle: projected ? 'dashed' : 'solid',
        borderColor: fg,
        backgroundColor: PITCH.chip,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Icon size={11} color={fg} aria-hidden />
    </View>
  );
}

function PitchToken({
  player,
  variant,
  left,
  width,
  compact,
  height,
  selected,
  onPress,
  pressAction,
  onInfo,
  testID,
}: {
  player: PitchPlayer;
  variant: PitchVariant;
  left: number;
  width: number;
  /** Shirt and name only, the points on the shirt: for a pitch fitted to a short screen. */
  compact: boolean;
  /** The box's height (tokenHeight at the current font scale). */
  height: number;
  selected: boolean;
  onPress?: (id: string) => void;
  pressAction: PitchPressAction;
  onInfo?: (id: string) => void;
  testID: string;
}) {
  const { name } = player;
  const ring = compact ? SLOT.compactShirt : SLOT.shirt;
  const showPoints = variant === 'points' && player.points !== undefined;
  const pointsBox = (
    <View
      testID="pitch-points"
      style={{
        minWidth: compact ? 22 : 30,
        paddingHorizontal: compact ? 4 : 6,
        borderRadius: 6,
        borderWidth: 1.5,
        borderStyle: 'solid',
        borderColor: PITCH.text,
        backgroundColor: PITCH.chip,
        alignItems: 'center',
        ...(compact
          ? {
              position: 'absolute',
              ...compactPointsBadge(width, onInfo !== undefined),
              minWidth: COMPACT_POINTS.width,
              height: COMPACT_POINTS.height,
              justifyContent: 'center',
            }
          : null),
      }}
    >
      <AppText
        numberOfLines={1}
        maxFontSizeMultiplier={PITCH_FONT_SCALE_MAX}
        weight="bold"
        numeric
        color={PITCH.text}
        style={compact ? { fontSize: 11, lineHeight: 15 } : { fontSize: 13, lineHeight: 18 }}
      >
        {String(player.points)}
      </AppText>
    </View>
  );
  const body = (
    <>
      <View
        style={{
          width: ring,
          height: ring,
          borderRadius: compact ? 9 : 12,
          borderWidth: 2,
          borderColor: selected ? PITCH.ring : 'transparent',
          backgroundColor: selected ? 'rgba(255,176,32,0.2)' : 'transparent',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Shirt
          kit={player.kit}
          club={player.club}
          goalkeeper={player.goalkeeper}
          size={compact ? 24 : 34}
        />
        {player.flag ? (
          <View style={{ position: 'absolute', left: -4, top: -2 }}>
            <TriangleAlert
              size={12}
              color={player.flag === 'doubtful' ? darkColors.accent : darkColors.loss}
              aria-hidden
            />
          </View>
        ) : null}
        {player.autoSub ? <AutoSubBadge {...player.autoSub} /> : null}
      </View>
      <View
        style={{
          maxWidth: width,
          paddingHorizontal: width < 72 ? 4 : 7,
          paddingVertical: compact ? 0 : 2,
          borderRadius: 8,
          backgroundColor: PITCH.chip,
          alignItems: 'center',
        }}
      >
        <AppText
          weight="semibold"
          numberOfLines={1}
          maxFontSizeMultiplier={PITCH_FONT_SCALE_MAX}
          color={PITCH.text}
          testID="pitch-name"
          style={{ fontSize: compact ? 11 : 12, lineHeight: 16, textAlign: 'center' }}
        >
          {name}
        </AppText>
        {variant === 'lineup' && !compact && player.detail ? (
          <AppText
            numeric
            numberOfLines={1}
            maxFontSizeMultiplier={PITCH_FONT_SCALE_MAX}
            color={PITCH.textMuted}
            testID="pitch-detail"
            style={{ fontSize: 11, lineHeight: 15, textAlign: 'center' }}
          >
            {player.detail}
          </AppText>
        ) : null}
      </View>
      {showPoints ? pointsBox : null}
    </>
  );
  const box: ViewStyle = {
    width,
    height,
    alignItems: 'center',
    gap: compact ? 2 : 4,
    borderRadius: 12,
    opacity: player.dimmed ? 0.5 : 1,
  };
  const describe = [
    name,
    variant === 'points' && player.points !== undefined ? `${player.points} points` : '',
    player.description,
  ]
    .filter(Boolean)
    .join(', ');
  return (
    <View style={{ position: 'absolute', left, top: 0, width }} pointerEvents="box-none">
      {onPress ? (
        <Pressable
          testID={testID}
          accessibilityRole="button"
          {...(pressAction === 'open'
            ? { accessibilityLabel: `Player card for ${describe}` }
            : {
                accessibilityLabel: `Swap ${name}`,
                accessibilityHint: player.description,
                accessibilityState: { selected },
                'aria-pressed': selected,
              })}
          onPress={() => onPress(player.id)}
          style={(state: PressState) => [
            box,
            ...pressFeedback(darkColors, state, { hoverFill: false }),
          ]}
        >
          {body}
        </Pressable>
      ) : (
        <View testID={testID} accessibilityLabel={describe} style={box}>
          {body}
        </View>
      )}
      {onInfo ? (
        <InfoButton
          name={name}
          onPress={() => onInfo(player.id)}
          testID={`player-info-${player.id}`}
          // Over the shirt's top-right corner, as in the design.
          size={compact ? 22 : 26}
          style={{
            top: compact ? -4 : -8,
            left: Math.min(width / 2 + ring / 2 - 6, width - (compact ? 20 : 22)),
          }}
        />
      ) : null}
    </View>
  );
}
