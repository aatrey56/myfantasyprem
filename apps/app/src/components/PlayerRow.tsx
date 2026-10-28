import type { Schemas } from '@myfantasyprem/api-client';
import { useId, type ReactNode } from 'react';
import { Pressable, ScrollView, View, type ViewStyle } from 'react-native';

import { AppText } from './AppText';
import { ClubCrest, ClubTag, PlayerPhoto, PositionBadge, positionLabel } from './Badges';
import { IconButton } from './Button';
import { ArrowDown, ArrowLeftRight, ArrowUp, Info, Plus, TriangleAlert } from './icons';
import { chanceFromNews } from './players/heat';
import { marksA11y, PlayerMarks } from './players/PlayerMarks';
import { pressFeedback, type PressState } from './pressable';
import { usePlayerPressPrefetch } from '@/api/prefetch';
import { FixtureRank } from './projections/Difficulty';
import type { TagMeta } from '@/lib/tags';
import { fixtureA11y, fixtureLabel, type FixtureLike } from '@/lib/fixtures';
import { showsDifficulty } from '@/lib/projections';
import { formatKickoff } from '@/lib/time';
import { positionColorKey, tint, useTheme, type ColorTokens } from '@/theme';

type PlayerRef = Schemas['PlayerRef'];

export interface PlayerRowProps {
  player: PlayerRef;
  /** The gameweek's fixtures: two in a double, none in a blank. Omit to hide the fixture line. */
  fixtures?: readonly FixtureLike[];
  /** Points this gameweek (or the sorted stat). Null shows a dash. Omit to hide. */
  points?: number | null;
  /** Label under the points, e.g. "pts" or "rank". */
  pointsLabel?: string;
  /** Text shown instead of `points` (a signed change, `+2.3`). */
  pointsText?: string;
  /** What a screen reader says for the points (default: label and text, or `n points`). */
  pointsA11y?: string;
  /** Extra meta at the end of the second line (owner, rank). */
  detail?: string;
  leading?: ReactNode;
  trailing?: ReactNode;
  autoSub?: Schemas['AutoSubState'];
  /** The row's own action (select, swap). With `onOpenCard`, the card gets an info button. */
  onPress?: () => void;
  /**
   * Opens the player's card. Alone, the row body (`leading`, badge, name,
   * points) is a "Player card for X" button; `trailing` stays a separate hit
   * target beside it, so its actions never also open the card. `leading`
   * must hold no controls.
   */
  onOpenCard?: () => void;
  selected?: boolean;
  /** Dims the row (not a valid swap target, already drafted). */
  dimmed?: boolean;
  divider?: boolean;
  /** List rank shown muted before the player (the Players list). */
  rank?: number;
  /**
   * List look (Players tab): a round shirt avatar instead of the position
   * pill, `• POS • CLUB` under the name, and the points in a labelled box.
   */
  avatar?: boolean;
  /** My personal tag on the player (an emoji after the name). */
  myTag?: TagMeta | null;
  /** He is on his team's trade block (a BLOCK chip after the name). */
  onBlock?: boolean;
  testID?: string;
  accessibilityHint?: string;
  style?: ViewStyle;
}

/** A row's status tag: `75%` (doubtful), `OUT`, `SUSP`; null when available. */
export interface StatusTag {
  label: string;
  /** `doubt` draws in the warning colour, `out` in the loss colour. */
  tone: 'doubt' | 'out';
}

/**
 * The short status tag: a doubtful player's chance of playing (`75%`, or
 * `DOUBT` when the news gives none), `SUSP` for a suspension and `OUT` for
 * an injured or unavailable player. Open enum: an unknown status reads `?`
 * in the warning colour.
 */
export function statusTag(availability: string, chance?: number | null): StatusTag | null {
  switch (availability) {
    case 'available':
      return null;
    case 'doubtful':
      return { label: chance != null ? `${chance}%` : 'DOUBT', tone: 'doubt' };
    case 'suspended':
      return { label: 'SUSP', tone: 'out' };
    case 'injured':
    case 'unavailable':
      return { label: 'OUT', tone: 'out' };
    default:
      return { label: '?', tone: 'doubt' };
  }
}

const AVAILABILITY: Record<string, string> = {
  doubtful: 'Doubtful',
  injured: 'Injured',
  suspended: 'Suspended',
  unavailable: 'Unavailable',
};

/** Human text for an availability value; '' for available, neutral text for unknown values. */
export function availabilityText(availability: string): string {
  if (availability === 'available') return '';
  return AVAILABILITY[availability] ?? 'Status unknown';
}

/** Kickoff time, LIVE, FT, postponed or TBC (no date yet) for one fixture. */
export function fixtureStatusText(f: FixtureLike): string {
  switch (f.status) {
    case 'live':
      return 'LIVE';
    case 'finished':
    case 'finished_provisional':
      return 'FT';
    case 'postponed':
      return 'PP';
    default:
      return f.kickoffAt === null ? 'TBC' : formatKickoff(Date.parse(f.kickoffAt));
  }
}

/**
 * The standard player row: position badge, name with an availability flag,
 * club shirt + code, the gameweek's fixture(s), and points on the right.
 */
export function PlayerRow({
  player,
  fixtures,
  points,
  pointsLabel,
  pointsText,
  pointsA11y,
  detail,
  leading,
  trailing,
  autoSub,
  onPress,
  onOpenCard,
  selected,
  dimmed,
  divider,
  rank,
  avatar,
  myTag,
  onBlock,
  testID,
  accessibilityHint,
  style,
}: PlayerRowProps) {
  const { colors, spacing, layout, radii } = useTheme();
  // A finger on the row starts loading the card it opens.
  const prefetchCard = usePlayerPressPrefetch(player.id);
  const status = availabilityText(player.availability);
  const statusColor = player.availability === 'doubtful' ? colors.accent : colors.loss;
  const sTag = statusTag(player.availability, chanceFromNews(player.news));
  const subOut = autoSub === 'projected_out' || autoSub === 'confirmed_out';
  const subIn = autoSub === 'projected_in' || autoSub === 'confirmed_in';
  const projected = autoSub === 'projected_out' || autoSub === 'projected_in';

  const a11y = [
    player.displayName,
    positionLabel(player.position),
    player.club.name,
    status ? `${status}${player.news ? `: ${player.news}` : ''}` : '',
    fixtures === undefined
      ? ''
      : fixtures.length === 0
        ? 'no fixture this gameweek'
        : fixturesA11y(fixtures),
    points === undefined
      ? ''
      : pointsA11y !== undefined
        ? pointsA11y
        : pointsText !== undefined
          ? `${pointsLabel ?? ''} ${pointsText}`.trim()
          : points === null
            ? 'no points yet'
            : `${points} points`,
    subOut ? `auto-sub out${projected ? ', projected' : ''}` : '',
    subIn ? `auto-sub in${projected ? ', projected' : ''}` : '',
    marksA11y(myTag, onBlock),
    detail ?? '',
  ]
    .filter(Boolean)
    .join(', ');

  const rowGap = avatar ? spacing.sm : spacing.md;
  const base: ViewStyle = {
    minHeight: layout.rowHeight,
    flexDirection: 'row',
    alignItems: 'center',
    gap: rowGap,
    paddingHorizontal: spacing.lg,
    paddingVertical: spacing.sm,
    borderTopWidth: divider ? 1 : 0,
    borderTopColor: colors.border,
    backgroundColor: selected ? tint(colors.accent, colors.surface) : 'transparent',
    opacity: dimmed ? 0.55 : 1,
  };

  const posColor = colors[positionColorKey(player.position)];
  const fixtureLine =
    fixtures === undefined ? null : fixtures.length === 0 ? (
      <AppText variant="meta" subtle testID="next-fixtures">
        No fixture
      </AppText>
    ) : (
      <View testID="next-fixtures" style={{ flexDirection: 'row', gap: spacing.xs, flexShrink: 1 }}>
        {fixtures.map((f, i) => (
          <View
            key={`${f.opponent.code}-${i}`}
            style={{
              flexDirection: 'row',
              gap: 4,
              paddingHorizontal: fixtures.length > 1 ? 4 : 0,
              borderRadius: radii.sm,
              backgroundColor: fixtures.length > 1 ? colors.surfaceRaised : 'transparent',
              alignItems: 'center',
            }}
          >
            <ClubCrest code={f.opponent.code} size={12} />
            <AppText variant="meta" muted numberOfLines={1}>
              {fixtureLabel(f)}
            </AppText>
            <AppText
              variant="meta"
              numberOfLines={1}
              color={f.status === 'live' ? colors.accent : colors.textMuted}
              weight={f.status === 'live' ? 'semibold' : undefined}
            >
              {fixtureStatusText(f)}
            </AppText>
            {showsDifficulty(f) ? (
              <FixtureRank position={player.position} opponent={f.opponent.code} />
            ) : null}
          </View>
        ))}
      </View>
    );
  const pointsView =
    points === undefined ? null : avatar ? (
      <View
        style={{
          alignItems: 'center',
          minWidth: 52,
          paddingHorizontal: spacing.sm,
          paddingVertical: spacing.xs,
          borderRadius: radii.sm,
          backgroundColor: colors.surfaceRaised,
        }}
      >
        {pointsLabel ? (
          <AppText muted style={{ fontSize: 10, lineHeight: 12, letterSpacing: 0.6 }}>
            {pointsLabel}
          </AppText>
        ) : null}
        <AppText weight="bold" numeric testID="player-points" style={{ fontSize: 17 }}>
          {pointsText ?? (points === null ? '–' : String(points))}
        </AppText>
      </View>
    ) : (
      <View style={{ alignItems: 'flex-end', minWidth: 32 }}>
        <View>
          <AppText variant="heading" numeric testID="player-points">
            {points === null ? '–' : String(points)}
          </AppText>
        </View>
        {pointsLabel ? (
          <AppText variant="eyebrow" subtle>
            {pointsLabel}
          </AppText>
        ) : null}
      </View>
    );
  const flags = (
    <>
      {status ? <TriangleAlert size={14} color={statusColor} aria-hidden /> : null}
      {sTag ? (
        <AppText variant="meta" weight="bold" numeric color={statusColor} testID="row-status">
          {sTag.label}
        </AppText>
      ) : null}
      <PlayerMarks tag={myTag} onBlock={onBlock} />
      {subOut ? <SubChip kind="out" projected={projected} /> : null}
      {subIn ? <SubChip kind="in" projected={projected} /> : null}
    </>
  );

  const main = avatar ? (
    <>
      {rank !== undefined ? (
        <AppText
          muted
          numeric
          weight="semibold"
          testID="player-rank"
          style={{ minWidth: 18, textAlign: 'center' }}
        >
          {String(rank)}
        </AppText>
      ) : null}
      <PlayerAvatar player={player} />
      <View style={{ flex: 1, minWidth: 0 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
          <AppText
            weight="bold"
            numberOfLines={1}
            style={{ flexShrink: 1, fontSize: 16, lineHeight: 21 }}
          >
            {player.displayName}
          </AppText>
          {flags}
        </View>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
          <View
            aria-hidden
            style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: posColor }}
          />
          <AppText variant="meta" weight="semibold" color={posColor}>
            {positionLabel(player.position)}
          </AppText>
          <AppText variant="meta" muted>
            •
          </AppText>
          <ClubCrest code={player.club.code} size={12} />
          <AppText variant="meta" muted numberOfLines={1} style={{ flexShrink: 0 }}>
            {player.club.code.toUpperCase()}
          </AppText>
          {detail ? (
            <AppText
              variant="meta"
              subtle
              numberOfLines={1}
              testID="player-detail"
              style={{ flexShrink: 1, marginLeft: 4 }}
            >
              {detail}
            </AppText>
          ) : null}
        </View>
        {fixtures === undefined ? null : (
          <CompactFixtures fixtures={fixtures} position={player.position} />
        )}
      </View>
      {pointsView}
    </>
  ) : (
    <>
      <PositionBadge position={player.position} />
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
          <AppText weight="semibold" numberOfLines={1} style={{ flexShrink: 1 }}>
            {player.displayName}
          </AppText>
          {flags}
        </View>
        <View
          style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' }}
        >
          <ClubTag code={player.club.code} name={player.club.name} />
          {fixtureLine}
          {detail ? (
            <AppText variant="meta" muted numberOfLines={1}>
              {detail}
            </AppText>
          ) : null}
        </View>
      </View>
      {pointsView}
    </>
  );
  const body = (
    <>
      {leading}
      {main}
      {trailing}
    </>
  );
  const cardLabel = `Player card for ${player.displayName}`;
  const describedBy = `row-${useId().replace(/:/g, '')}`;

  if (onOpenCard && onPress) {
    // The row is its own action (a swap): the card is a separate info button.
    return (
      <View testID={testID} style={[base, { paddingVertical: 0 }, style]}>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={a11y}
          accessibilityHint={accessibilityHint}
          accessibilityState={{ selected: Boolean(selected) }}
          aria-pressed={Boolean(selected)}
          onPress={onPress}
          style={(state: PressState) => [
            {
              flex: 1,
              minWidth: 0,
              minHeight: layout.rowHeight,
              flexDirection: 'row',
              alignItems: 'center',
              gap: rowGap,
            },
            ...pressFeedback(colors, state),
          ]}
        >
          {leading}
          {main}
        </Pressable>
        <IconButton
          icon={Info}
          label={cardLabel}
          onPress={onOpenCard}
          size={18}
          testID={`open-card-${player.id}`}
        />
        {trailing}
      </View>
    );
  }
  if (onOpenCard) {
    return (
      <View testID={testID} style={[base, { paddingVertical: 0 }, style]}>
        <Pressable
          testID={`open-card-${player.id}`}
          accessibilityRole="button"
          accessibilityLabel={cardLabel}
          accessibilityHint={a11y}
          // The web has no hint: the same words are the button's description.
          aria-describedby={describedBy}
          onPressIn={prefetchCard}
          onPress={onOpenCard}
          style={(state: PressState) => [
            {
              flex: 1,
              minWidth: 0,
              minHeight: layout.rowHeight,
              flexDirection: 'row',
              alignItems: 'center',
              gap: rowGap,
              paddingVertical: spacing.sm,
              borderRadius: radii.md,
            },
            ...pressFeedback(colors, state),
          ]}
        >
          {leading}
          {main}
        </Pressable>
        {trailing}
        <AppText nativeID={describedBy} style={{ display: 'none' }}>
          {a11y}
        </AppText>
      </View>
    );
  }
  if (!onPress) {
    return (
      <View testID={testID} accessibilityLabel={a11y} style={[base, style]}>
        {body}
      </View>
    );
  }
  return (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={a11y}
      accessibilityHint={accessibilityHint}
      accessibilityState={{ selected: Boolean(selected) }}
      onPress={onPress}
      style={(state: PressState) => [base, ...pressFeedback(colors, state), style ?? {}]}
    >
      {body}
    </Pressable>
  );
}

/** Fixtures for a screen reader: all of a gameweek's, or the next and a count for a longer list. */
function fixturesA11y(fixtures: readonly FixtureLike[]): string {
  if (fixtures.length <= 3) return fixtures.map(fixtureA11y).join(' and ');
  return `next ${fixtureA11y(fixtures[0]!)}, then ${fixtures.length - 1} more fixtures`;
}

/**
 * The Players list's fixture line: every remaining fixture as a strip
 * (difficulty chip and opponent each, the first with its kickoff) that
 * scrolls sideways so the row stays one line tall; "No fixture" when none.
 */
function CompactFixtures({
  fixtures,
  position,
}: {
  fixtures: readonly FixtureLike[];
  position: Schemas['Position'];
}) {
  if (fixtures.length === 0) {
    return (
      <AppText variant="meta" subtle testID="next-fixtures">
        No fixture
      </AppText>
    );
  }
  return <FixtureStrip fixtures={fixtures} position={position} testID="next-fixtures" />;
}

/**
 * A one-line, sideways-scrolling strip of fixtures: per fixture its
 * difficulty for the position (dated fixtures only), crest and `v ABC`;
 * the first also shows its kickoff (or LIVE), an undated one "TBC".
 */
export function FixtureStrip({
  fixtures,
  position,
  testID,
}: {
  fixtures: readonly FixtureLike[];
  position: Schemas['Position'];
  testID?: string;
}) {
  const { colors, spacing } = useTheme();
  return (
    <ScrollView
      horizontal
      nestedScrollEnabled
      showsHorizontalScrollIndicator={false}
      // A picture of what the row's label already says (the next fixture and
      // how many follow); the player card lists every one. Nothing in it to
      // focus inside the row's button.
      aria-hidden
      focusable={false}
      tabIndex={-1}
      testID={testID}
      style={{ flexGrow: 0, flexShrink: 1, minWidth: 0 }}
      contentContainerStyle={{ alignItems: 'center', gap: spacing.sm }}
    >
      {fixtures.map((f, i) => {
        const live = f.status === 'live';
        const when = i === 0 || f.kickoffAt === null ? fixtureStatusText(f) : null;
        return (
          <View
            key={`${f.opponent.code}-${i}`}
            testID="fixture-strip-item"
            style={{ flexDirection: 'row', alignItems: 'center', gap: 3 }}
          >
            {showsDifficulty(f) ? (
              <FixtureRank position={position} opponent={f.opponent.code} />
            ) : null}
            <ClubCrest code={f.opponent.code} size={12} />
            <AppText variant="meta" muted numberOfLines={1}>
              {fixtureLabel(f)}
            </AppText>
            {when ? (
              <AppText
                variant="meta"
                numberOfLines={1}
                color={live ? colors.accent : colors.textMuted}
                weight={live ? 'semibold' : undefined}
              >
                {when}
              </AppText>
            ) : null}
          </View>
        );
      })}
    </ScrollView>
  );
}

/** OUT / IN arrow chip for auto-subs; dashed while projected. */
export function SubChip({
  kind,
  projected,
  palette,
}: {
  kind: 'in' | 'out';
  projected?: boolean;
  /** Colours to draw with instead of the theme's (the pitch always uses the dark set). */
  palette?: ColorTokens;
}) {
  const theme = useTheme();
  const { radii } = theme;
  const colors = palette ?? theme.colors;
  const fg = kind === 'in' ? colors.win : colors.loss;
  const Icon = kind === 'in' ? ArrowUp : ArrowDown;
  return (
    <View
      testID={`autosub-${kind}`}
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: 2,
        paddingHorizontal: 4,
        borderRadius: radii.sm,
        borderWidth: 1,
        borderStyle: projected ? 'dashed' : 'solid',
        borderColor: fg,
      }}
    >
      <Icon size={10} color={fg} aria-hidden />
      <AppText variant="eyebrow" color={fg}>
        {kind === 'in' ? 'IN' : 'OUT'}
      </AppText>
    </View>
  );
}

/**
 * A round avatar for a player: our shirt glyph in a circle ringed in the
 * position colour. No photos (AGENTS.md). Decorative: the name carries the meaning.
 */
export function PlayerAvatar({
  player,
  size = 36,
}: {
  player: Pick<PlayerRef, 'id' | 'club' | 'position'>;
  size?: number;
}) {
  const { colors } = useTheme();
  return (
    <View
      aria-hidden
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={{
        width: size,
        height: size,
        borderRadius: size / 2,
        borderWidth: 1.5,
        borderColor: colors[positionColorKey(player.position)],
        backgroundColor: colors.surfaceRaised,
        alignItems: 'center',
        justifyContent: 'center',
        overflow: 'hidden',
      }}
    >
      <PlayerPhoto
        playerId={player.id}
        code={player.club.code}
        size={size}
        fallbackSize={Math.round(size * 0.56)}
        placeholder={colors.surfaceRaised}
      />
    </View>
  );
}

export interface MicroStat {
  label: string;
  value: string;
}

/**
 * A full-width band of labelled micro stats under a player row (tiny caps
 * label over a bold number), on the raised surface.
 */
export function PlayerStatStrip({
  stats,
  testID,
}: {
  stats: readonly MicroStat[];
  testID?: string;
}) {
  const { colors, spacing } = useTheme();
  return (
    <View
      testID={testID}
      style={{
        flexDirection: 'row',
        paddingHorizontal: spacing.lg,
        paddingVertical: spacing.xs,
        gap: spacing.sm,
        backgroundColor: colors.surfaceRaised,
      }}
    >
      {stats.map((s) => (
        <View
          key={s.label}
          accessibilityLabel={`${s.label} ${s.value}`}
          style={{ flex: 1, minWidth: 0 }}
        >
          <AppText
            muted
            numberOfLines={1}
            style={{ fontSize: 10, lineHeight: 12, letterSpacing: 0.4 }}
          >
            {s.label}
          </AppText>
          <AppText
            numeric
            weight="semibold"
            numberOfLines={1}
            style={{ fontSize: 13, lineHeight: 17 }}
          >
            {s.value}
          </AppText>
        </View>
      ))}
    </View>
  );
}

/** The sortable stats of a Players list row. */
export type ListSort = 'total_points' | 'form' | 'gameweek_points';

type ListItem = Pick<
  Schemas['PlayerListItem'],
  'totalPoints' | 'gameweekPoints' | 'form' | 'minutes'
>;

/** The sorted stat for a list row's box: form to one decimal, null before any form. */
export function sortedStat(item: ListItem, sort: ListSort): number | null {
  if (sort === 'form') return item.form === null ? null : Number(item.form.toFixed(1));
  return sort === 'gameweek_points' ? item.gameweekPoints : item.totalPoints;
}

/** Short caps label for the sorted stat's box. */
export function sortLabel(sort: ListSort): string {
  return sort === 'form' ? 'FORM' : sort === 'gameweek_points' ? 'GW' : 'PTS';
}

/**
 * The micro stats under a list row: points, GW points, form, minutes and
 * points per 90, without the one already shown in the box.
 */
export function listMicroStats(item: ListItem, sort: ListSort): MicroStat[] {
  const per90 = item.minutes > 0 ? ((item.totalPoints * 90) / item.minutes).toFixed(1) : '–';
  const all: (MicroStat & { sort?: ListSort })[] = [
    { label: 'PTS', value: String(item.totalPoints), sort: 'total_points' },
    { label: 'GW PTS', value: String(item.gameweekPoints), sort: 'gameweek_points' },
    { label: 'FORM', value: item.form === null ? '–' : item.form.toFixed(1), sort: 'form' },
    { label: 'MINS', value: String(item.minutes) },
    { label: 'PTS/90', value: per90 },
  ];
  return all.filter((s) => s.sort !== sort).map(({ label, value }) => ({ label, value }));
}

type SeasonItem = Pick<
  Schemas['PlayerListItem'],
  'player' | 'totalPoints' | 'gameweekPoints' | 'form' | 'seasonStats'
>;

/**
 * The stat strip under a Players list row, by position (as Sleeper's per
 * position strips): keepers SV, CS, PTS; defenders CS, DC, PTS; midfielders
 * and forwards xGI, G, A, PTS. Season totals. When the box already shows
 * the season's points, the strip ends with form instead.
 */
export function positionStats(item: SeasonItem, sort?: ListSort): MicroStat[] {
  const s = item.seasonStats;
  const xgi = s.expectedGoalInvolvements;
  const lead: MicroStat[] =
    item.player.position === 'GKP'
      ? [
          { label: 'SV', value: String(s.saves) },
          { label: 'CS', value: String(s.cleanSheets) },
        ]
      : item.player.position === 'DEF'
        ? [
            { label: 'CS', value: String(s.cleanSheets) },
            { label: 'DC', value: String(s.defensiveContributionPoints) },
          ]
        : [
            { label: 'xGI', value: xgi === null ? '–' : xgi.toFixed(1) },
            { label: 'G', value: String(s.goals) },
            { label: 'A', value: String(s.assists) },
          ];
  const last: MicroStat =
    sort === 'total_points'
      ? { label: 'FORM', value: item.form === null ? '–' : item.form.toFixed(1) }
      : { label: 'PTS', value: String(item.totalPoints) };
  return [...lead, last];
}

/** A form change with its sign (`+2.3`, `-1.0`, `0.0`), or a dash. */
export function formChangeText(change: number | null): string {
  if (change === null) return '–';
  return `${change > 0 ? '+' : ''}${change.toFixed(1)}`;
}

/**
 * The action at a Players list row's left edge (SLEEPER-OBSERVED): an
 * outlined round + (add a free agent), a W badge with the waiver run's
 * day (claim), or ⇄ (trade with the team that owns him). Named "Add X",
 * "Claim X" or "Trade for X".
 */
export function RowAction({
  kind,
  day,
  name,
  onPress,
  testID,
}: {
  kind: 'add' | 'claim' | 'trade';
  /** The waiver run's weekday (`Tue`), for the W badge. */
  day?: string;
  name: string;
  onPress: () => void;
  testID?: string;
}) {
  const { colors } = useTheme();
  const ring =
    kind === 'claim' ? colors.accentBright : kind === 'trade' ? colors.textMuted : colors.accent;
  const verb = kind === 'claim' ? 'Claim' : kind === 'trade' ? 'Trade for' : 'Add';
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`${verb} ${name}${kind === 'claim' && day ? `, waivers run ${day}` : ''}`}
      onPress={onPress}
      testID={testID}
      hitSlop={6}
      style={(state: PressState) => [
        { alignItems: 'center', borderRadius: 20 },
        ...pressFeedback(colors, state, { hoverFill: false }),
      ]}
    >
      <View
        style={{
          width: 36,
          height: 36,
          borderRadius: 18,
          borderWidth: 1.5,
          borderColor: ring,
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        {kind === 'claim' ? (
          <>
            <AppText weight="bold" color={ring} style={{ fontSize: 12, lineHeight: 13 }}>
              W
            </AppText>
            {day ? (
              <AppText testID="waiver-day" color={ring} style={{ fontSize: 8, lineHeight: 10 }}>
                {`(${day})`}
              </AppText>
            ) : null}
          </>
        ) : kind === 'trade' ? (
          <ArrowLeftRight size={16} color={ring} strokeWidth={2.4} aria-hidden />
        ) : (
          <Plus size={18} color={ring} strokeWidth={2.4} aria-hidden />
        )}
      </View>
    </Pressable>
  );
}

/** The weekday a waiver clears (`Sat`), in the device time zone, for the W badge. */
export function waiverDay(clearsAt: string | null, now = Date.now(), timeZone?: string): string {
  if (!clearsAt) return '';
  const ms = Date.parse(clearsAt);
  if (Number.isNaN(ms)) return '';
  return formatKickoff(ms, now, timeZone).split(' ')[0] ?? '';
}
