import type { Schemas } from '@myfantasyprem/api-client';
import type { ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar } from '../Badges';
import { ShareBar } from './ShareBar';
import {
  leadOf,
  pointsShare,
  recordLine,
  streakArrow,
  streakLabel,
  teamOf,
  type AnySide,
  type TeamRecord,
} from '@/lib/matchup';
import { showProjections } from '@/lib/projections';
import { useTheme } from '@/theme';

export type HeroSize = 'lg' | 'sm';

const DIMS = {
  lg: { avatar: 40, emblem: 48, gap: 12 },
  sm: { avatar: 40, emblem: 36, gap: 6 },
} as const;

/** Medal fills for the league's top three: amber gold, silver, bronze. */
const BRONZE = '#C8875A';

/**
 * A team's round avatar (its colour and initials) with a small medal
 * numbered 1–3 at the lower left for the league's top three. The league
 * average gets a plain avatar.
 */
export function TeamAvatar({
  name,
  rank,
  size = 40,
  testID,
}: {
  name: string;
  rank?: number;
  size?: number;
  testID?: string;
}) {
  const { colors } = useTheme();
  const medal =
    rank === 1
      ? colors.accentBright
      : rank === 2
        ? colors.textMuted
        : rank === 3
          ? BRONZE
          : undefined;
  const m = Math.round(size * 0.45);
  return (
    <View style={{ width: size, height: size }} testID={testID}>
      <Avatar name={name} size={size} />
      {medal ? (
        <View
          aria-hidden
          testID={testID ? `${testID}-medal` : undefined}
          style={{
            position: 'absolute',
            left: -3,
            bottom: -3,
            width: m,
            height: m,
            borderRadius: m / 2,
            borderWidth: 2,
            borderColor: colors.background,
            backgroundColor: medal,
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <AppText
            weight="bold"
            numeric
            color={colors.onAccent}
            style={{ fontSize: 9, lineHeight: 11 }}
          >
            {String(rank)}
          </AppText>
        </View>
      ) : null}
    </View>
  );
}

/** The width of the centre column, so rows under the bars line up with them. */
export const heroCentreWidth = (size: HeroSize) => DIMS[size].emblem + 2 * DIMS[size].gap;

/**
 * Lead colour for one side: win while ahead, loss while behind, muted when
 * level or without scores. Always paired with text (▲/▼ and the share).
 */
export function useLeadColor(mine: number | null, theirs: number | null): string {
  const { colors } = useTheme();
  const lead = leadOf(mine, theirs);
  return lead === 'lead' ? colors.win : lead === 'trail' ? colors.loss : colors.textMuted;
}

/**
 * `▲ 54% of pts`, `▼ 46% of pts`, `50% of pts` (`▲ 54%` when short), `–`
 * at 0-0, and nothing before either side has a score.
 */
export function shareLabel(
  percent: number | null,
  mine: number | null,
  theirs: number | null,
  short = false,
): string {
  if (mine === null && theirs === null) return '';
  if (percent === null) return '–';
  const lead = leadOf(mine, theirs);
  const arrow = lead === 'lead' ? '▲ ' : lead === 'trail' ? '▼ ' : '';
  return `${arrow}${percent}%${short ? '' : ' of pts'}`;
}

interface BarsProps {
  left: AnySide;
  right: AnySide;
  leftPoints: number | null;
  rightPoints: number | null;
  size: HeroSize;
  /** The square between the bars: the gameweek, VS, ... */
  emblem: ReactNode;
  /** Extra content at the inner end of each side's label row (a score, a W/L chip). */
  leftInner?: ReactNode;
  rightInner?: ReactNode;
  /** Records by team id: the avatars' top-three medals. */
  records?: ReadonlyMap<string, TeamRecord>;
}

/**
 * The top of a matchup hero: avatars pinned to the outer edges with each
 * side's share of the combined live points beside them, then two thick
 * bars growing toward a square emblem in the centre. The API has no
 * projections, so the bars are the live points split, never a win chance.
 */
export function HeroBars({
  left,
  right,
  leftPoints,
  rightPoints,
  size,
  emblem,
  leftInner,
  rightInner,
  records,
}: BarsProps) {
  const { spacing } = useTheme();
  const d = DIMS[size];
  // The owner reads the split as a win chance: it hides with projections (the
  // emblem stays, centred, with no empty bars beside it).
  const split = showProjections();
  const share = split ? pointsShare(leftPoints, rightPoints) : null;
  const leftColor = useLeadColor(leftPoints, rightPoints);
  const rightColor = useLeadColor(rightPoints, leftPoints);
  const labelRow = (
    side: AnySide,
    align: 'left' | 'right',
    percent: number | null,
    mine: number | null,
    theirs: number | null,
    color: string,
    inner: ReactNode,
  ) => (
    <View
      style={{
        flex: 1,
        minWidth: 0,
        flexDirection: align === 'left' ? 'row' : 'row-reverse',
        alignItems: 'flex-end',
        gap: spacing.sm,
      }}
    >
      <TeamAvatar
        name={teamOf(side)?.name ?? 'League Average'}
        rank={records?.get(teamOf(side)?.id ?? '')?.rank}
        size={d.avatar}
        testID={`avatar-${align}`}
      />
      {split ? (
        <AppText
          variant="meta"
          weight="semibold"
          numberOfLines={1}
          color={percent === null ? undefined : color}
          muted={percent === null}
          accessibilityLabel={
            percent === null ? undefined : `${percent}% of the points scored in this matchup`
          }
          style={{ flexShrink: 1, textAlign: align }}
          testID={size === 'lg' ? `share-${align}` : undefined}
        >
          {shareLabel(percent, mine, theirs, true)}
        </AppText>
      ) : null}
      <View style={{ flex: 1 }} />
      {inner}
    </View>
  );
  return (
    <View style={{ gap: spacing.xs }}>
      <View style={{ flexDirection: 'row', alignItems: 'flex-end' }}>
        {labelRow(left, 'left', share?.left ?? null, leftPoints, rightPoints, leftColor, leftInner)}
        <View style={{ width: heroCentreWidth(size) }} />
        {labelRow(
          right,
          'right',
          share?.right ?? null,
          rightPoints,
          leftPoints,
          rightColor,
          rightInner,
        )}
      </View>
      <View style={{ flexDirection: 'row', alignItems: 'center' }}>
        <View style={{ flex: 1 }}>
          {split ? <ShareBar percent={share?.left ?? null} from="left" color={leftColor} /> : null}
        </View>
        <View style={{ width: heroCentreWidth(size), alignItems: 'center' }}>{emblem}</View>
        <View style={{ flex: 1 }}>
          {split ? (
            <ShareBar percent={share?.right ?? null} from="right" color={rightColor} />
          ) : null}
        </View>
      </View>
    </View>
  );
}

/** The rounded square between the bars. */
export function Emblem({
  size,
  highlight,
  children,
  testID,
}: {
  size: HeroSize;
  /** Accent border, e.g. while scores are live. */
  highlight?: boolean;
  children: ReactNode;
  testID?: string;
}) {
  const { colors } = useTheme();
  const s = DIMS[size].emblem;
  return (
    <View
      testID={testID}
      style={{
        width: s,
        height: s,
        borderRadius: Math.round(s * 0.28),
        borderWidth: 1,
        borderColor: highlight ? colors.accent : colors.border,
        backgroundColor: colors.surfaceRaised,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      {children}
    </View>
  );
}

/**
 * `Manager • 3-2-1 ▼2L (#5)` under a team name; mirrored on the right
 * (`3-2-1 ▼2L (#5) • Manager`). The league average explains itself instead.
 */
export function sublineText(
  side: AnySide,
  records: ReadonlyMap<string, TeamRecord> | undefined,
  align: 'left' | 'right',
): string {
  const team = teamOf(side);
  if (!team) return side.kind === 'average' ? 'Average of all teams' : '';
  const rec = records?.get(team.id);
  const parts = [team.managerName, rec ? recordLine(rec) : ''].filter(Boolean);
  return (align === 'left' ? parts : parts.reverse()).join(' • ');
}

/**
 * The subline on one line: the manager's name (cut short first when space
 * runs out) and the record, whose streak arrow is in the win or loss colour
 * (the letters after it say the same in words: `▼2L`). Mirrored on the right.
 */
export function Subline({
  side,
  records,
  align,
  testID = `subline-${align}`,
}: {
  side: AnySide;
  records: ReadonlyMap<string, TeamRecord> | undefined;
  align: 'left' | 'right';
  testID?: string;
}) {
  const { colors } = useTheme();
  const team = teamOf(side);
  const rec = team ? records?.get(team.id) : undefined;
  if (!team || !rec) {
    const text = sublineText(side, records, align);
    return text ? (
      <AppText
        variant="meta"
        muted
        numberOfLines={1}
        style={{ textAlign: align, maxWidth: '100%' }}
        testID={testID}
      >
        {text}
      </AppText>
    ) : null;
  }
  const streak = streakLabel(rec.streak);
  const arrow = rec.streak ? streakArrow(rec.streak.result) : '';
  const color =
    rec.streak?.result === 'W'
      ? colors.win
      : rec.streak?.result === 'L'
        ? colors.loss
        : colors.textMuted;
  const record = (
    <AppText variant="meta" muted numberOfLines={1} style={{ flexShrink: 0 }}>
      {`${rec.record} `}
      {streak ? (
        <>
          <AppText variant="meta" color={color}>
            {arrow}
          </AppText>
          {`${streak.slice(arrow.length)} `}
        </>
      ) : null}
      {`(#${rec.rank})`}
    </AppText>
  );
  const manager = (
    <AppText variant="meta" muted numberOfLines={1} style={{ flexShrink: 1 }}>
      {team.managerName}
    </AppText>
  );
  const dot = (
    <AppText variant="meta" muted style={{ flexShrink: 0 }}>
      {' • '}
    </AppText>
  );
  // Manager • record in reading order on both sides; row-reverse mirrors the
  // right one to `record • manager`.
  return (
    <View
      testID={testID}
      style={{
        flexDirection: align === 'left' ? 'row' : 'row-reverse',
        alignItems: 'baseline',
        maxWidth: '100%',
      }}
    >
      {manager}
      {dot}
      {record}
    </View>
  );
}

/** W / D / L chip once a result is final. */
export function ResultChip({
  result,
  testID,
}: {
  result: Schemas['FormResult'] | null;
  testID: string;
}) {
  const { colors, radii } = useTheme();
  if (!result) return null;
  const color = result === 'W' ? colors.win : result === 'L' ? colors.loss : colors.textMuted;
  return (
    <View
      testID={testID}
      accessibilityLabel={result === 'W' ? 'Won' : result === 'L' ? 'Lost' : 'Drew'}
      style={{
        minWidth: 22,
        height: 22,
        borderRadius: radii.sm - 2,
        alignItems: 'center',
        justifyContent: 'center',
        borderWidth: 1,
        borderColor: color,
      }}
    >
      <AppText variant="eyebrow" color={color}>
        {result}
      </AppText>
    </View>
  );
}
