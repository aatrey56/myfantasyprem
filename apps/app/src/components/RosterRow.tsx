import type { Schemas } from '@myfantasyprem/api-client';
import { Pressable, View, type ViewStyle } from 'react-native';

import { AppText } from './AppText';
import { ClubCrest, PlayerPhoto, PositionBadge, positionLabel } from './Badges';
import { Newspaper } from './icons';
import { marksA11y, PlayerMarks } from './players/PlayerMarks';
import { availabilityText, FixtureStrip, fixtureStatusText, statusTag, SubChip } from './PlayerRow';
import type { TagMeta } from '@/lib/tags';
import { pressFeedback, type PressState } from './pressable';
import { FixtureRank } from './projections/Difficulty';
import {
  expandRowFixtures,
  fixtureA11y,
  fixtureLabel,
  formatNextFixtures,
  type FixtureLike,
} from '@/lib/fixtures';
import { expectedText, showsDifficulty } from '@/lib/projections';
import { positionColorKey, tint, useTheme } from '@/theme';

type PlayerRef = Schemas['PlayerRef'];

export { statusTag, type StatusTag } from './PlayerRow';

/** `Form 6.3 · 41 pts · Played 6/7` as parts, so the numbers can be bold. */
export function seasonParts(season: Schemas['LineupEntrySeason']): [string, string][] {
  return [
    ['Form', season.form === null ? '–' : season.form.toFixed(1)],
    ['pts', String(season.totalPoints)],
    ['Played', `${season.appearances}/${Math.max(season.clubMatches, season.appearances)}`],
  ];
}

/**
 * A round avatar for a player: the photo while branded images are on,
 * otherwise our shirt glyph in the club's colour.
 */
export function PlayerAvatar({
  player,
  size = 40,
}: {
  player: Pick<PlayerRef, 'id' | 'club'>;
  size?: number;
}) {
  const { colors } = useTheme();
  return (
    <View
      aria-hidden
      style={{
        width: size,
        height: size,
        borderRadius: size / 2,
        backgroundColor: colors.surfaceRaised,
        borderWidth: 1,
        borderColor: colors.border,
        alignItems: 'center',
        justifyContent: 'center',
        overflow: 'hidden',
      }}
    >
      <PlayerPhoto
        playerId={player.id}
        code={player.club.code}
        size={size}
        fallbackSize={Math.round(size * 0.58)}
        placeholder={colors.surfaceRaised}
      />
    </View>
  );
}

export interface LineupRowProps {
  player: PlayerRef;
  /** The gameweek's fixtures (two in a double, none in a blank). Omit to hide them. */
  fixtures?: readonly FixtureLike[];
  /** Points this gameweek; null shows a dash (before kickoff). Omit to hide. */
  points?: number | null;
  /** Expected points this gameweek (ADR 0014), under the points; null shows a dash. Omit to hide. */
  expected?: number | null;
  autoSub?: Schemas['AutoSubState'];
  /** The season so far: draws the `Form 6.3 · 41 pts · Played 6/7` line. */
  season?: Schemas['LineupEntrySeason'];
  /** Percent chance of playing next, for a doubtful player's tag. */
  chanceOfPlaying?: number | null;
  /**
   * Show a news icon after the status tag when the player has news. Only
   * where the row's body opens the player card, which shows the news.
   */
  newsIcon?: boolean;
  /** A bench player: the position pill is quiet grey. */
  bench?: boolean;
  /** Spoken place on the bench, e.g. "bench 1". */
  place?: string;
  /** Makes the position pill a button that starts a swap. */
  onSwap?: () => void;
  /** The row body's action (the player card, or "swap with" in the swap sheet). */
  onPress?: () => void;
  /** The body button's label; defaults to "Player card for X". */
  pressLabel?: string;
  /** Extra meta after the fixtures (squad lists). */
  detail?: string;
  selected?: boolean;
  divider?: boolean;
  /** Horizontal padding (0 inside a sheet, which pads itself). */
  inset?: number;
  testID?: string;
  /** testID of the pill when it is a button. */
  swapTestID?: string;
  /** testID of the body when it is a button. */
  pressTestID?: string;
  /** My personal tag on the player. */
  myTag?: TagMeta | null;
  /** He is on my trade block. */
  onBlock?: boolean;
}

/**
 * A roster row in the Sleeper layout, with no box or divider: the position
 * pill (the swap button while the lineup is editable), a round avatar, the
 * name in bold with `MID - ARS` beside it (the position in its colour), the
 * season line (`Form 6.3 · 41 pts · Played 6/7`) when given, then the
 * fixture and kickoff with a status tag (`75%`, `OUT`, `SUSP`), a news icon
 * and auto-subs, and the points on the right (a dash before kickoff) with
 * Expected under them when given.
 */
export function LineupRow({
  player,
  fixtures,
  points,
  expected,
  autoSub,
  season,
  chanceOfPlaying,
  newsIcon,
  bench,
  place,
  onSwap,
  onPress,
  pressLabel,
  detail,
  selected,
  divider,
  inset,
  testID,
  swapTestID,
  pressTestID,
  myTag,
  onBlock,
}: LineupRowProps) {
  const { colors, spacing, radii, fontFamilies } = useTheme();
  const status = availabilityText(player.availability);
  const tag = statusTag(player.availability, chanceOfPlaying);
  const statusColor = tag?.tone === 'out' ? colors.loss : colors.warning;
  const showNews = Boolean(newsIcon && player.news);
  const subOut = autoSub === 'projected_out' || autoSub === 'confirmed_out';
  const subIn = autoSub === 'projected_in' || autoSub === 'confirmed_in';
  const projected = autoSub === 'projected_out' || autoSub === 'projected_in';
  const pos = positionLabel(player.position);
  const posKey = positionColorKey(player.position);

  const description = [
    pos,
    player.club.name,
    place ?? '',
    status ? `${status}${tag && tag.label.endsWith('%') ? ` (${tag.label} chance)` : ''}` : '',
    player.news ? `news: ${player.news}` : '',
    season
      ? seasonParts(season)
          .map(([label, value]) => (label === 'pts' ? `${value} points` : `${label} ${value}`))
          .join(', ')
      : '',
    fixtures === undefined
      ? ''
      : fixtures.length === 0
        ? 'no fixture this gameweek'
        : fixtures.map(fixtureA11y).join(' and '),
    points === undefined ? '' : points === null ? 'no points yet' : `${points} points`,
    expected === undefined || expected === null ? '' : `expected ${expectedText(expected)}`,
    subOut ? `auto-sub out${projected ? ', projected' : ''}` : '',
    subIn ? `auto-sub in${projected ? ', projected' : ''}` : '',
    marksA11y(myTag, onBlock),
    detail ?? '',
  ]
    .filter(Boolean)
    .join(', ');

  const pill = <PositionBadge position={player.position} size="lg" muted={bench} />;

  const body = (
    <>
      <PlayerAvatar player={player} />
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <AppText numberOfLines={1}>
          <AppText style={{ fontFamily: fontFamilies.bold, fontSize: 16, lineHeight: 21 }}>
            {player.displayName}
          </AppText>
          <AppText variant="meta" weight="semibold" color={colors[posKey]}>
            {`  ${pos}`}
          </AppText>
          <AppText variant="meta" muted>
            {` - ${player.club.code.toUpperCase()}`}
          </AppText>
        </AppText>
        {season ? (
          <AppText variant="meta" muted numberOfLines={1} testID="player-season">
            {seasonParts(season).map(([label, value], i) => (
              <AppText key={label} variant="meta" muted>
                {i > 0 ? ' · ' : ''}
                {label === 'pts' ? null : `${label} `}
                <AppText variant="meta" weight="semibold" color={colors.text}>
                  {value}
                </AppText>
                {label === 'pts' ? ' pts' : null}
              </AppText>
            ))}
          </AppText>
        ) : null}
        <View
          style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' }}
        >
          {fixtures === undefined ? null : fixtures.length === 0 ? (
            <AppText variant="meta" subtle testID="next-fixtures">
              No fixture
            </AppText>
          ) : (
            <View
              testID="next-fixtures"
              // A double gameweek's two chips wrap rather than push past a 320px screen.
              style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs, flexShrink: 1 }}
            >
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
          )}
          {tag || showNews ? (
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
              {tag ? (
                <AppText
                  variant="meta"
                  weight="bold"
                  numeric
                  color={statusColor}
                  testID="player-status"
                >
                  {tag.label}
                </AppText>
              ) : null}
              {showNews ? (
                <View testID="player-news-icon" aria-hidden>
                  <Newspaper size={14} color={tag ? statusColor : colors.textMuted} />
                </View>
              ) : null}
            </View>
          ) : null}
          <PlayerMarks tag={myTag} onBlock={onBlock} />
          {subOut ? <SubChip kind="out" projected={projected} /> : null}
          {subIn ? <SubChip kind="in" projected={projected} /> : null}
          {detail ? (
            <AppText variant="meta" muted numberOfLines={1}>
              {detail}
            </AppText>
          ) : null}
        </View>
      </View>
      {points !== undefined ? (
        <View style={{ alignItems: 'flex-end', minWidth: 32 }}>
          <View>
            <AppText
              numeric
              testID="player-points"
              style={{ fontSize: 17, lineHeight: 22 }}
              weight="semibold"
            >
              {points === null ? '–' : String(points)}
            </AppText>
          </View>
          {expected !== undefined ? (
            <AppText
              variant="meta"
              subtle
              numeric
              testID="player-expected"
              style={{ fontSize: 11, lineHeight: 14 }}
            >
              {`Exp ${expectedText(expected)}`}
            </AppText>
          ) : null}
        </View>
      ) : null}
    </>
  );

  const bodyStyle: ViewStyle = {
    flex: 1,
    minWidth: 0,
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.md,
    paddingVertical: spacing.md,
    borderRadius: radii.md,
  };

  return (
    <View
      testID={testID}
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.md,
        paddingHorizontal: inset ?? 0,
        borderRadius: radii.md,
        borderTopWidth: divider ? 1 : 0,
        borderTopColor: colors.border,
        backgroundColor: selected ? tint(colors.accent, colors.surface) : 'transparent',
      }}
    >
      {onSwap ? (
        <Pressable
          testID={swapTestID}
          accessibilityRole="button"
          accessibilityLabel={`Swap ${player.displayName}`}
          accessibilityHint={place ? `${pos}, ${place}` : pos}
          accessibilityState={{ selected: Boolean(selected) }}
          aria-pressed={Boolean(selected)}
          hitSlop={8}
          onPress={onSwap}
          style={(state: PressState) => [
            { borderRadius: radii.sm },
            ...pressFeedback(colors, state, { hoverFill: false }),
          ]}
        >
          {pill}
        </Pressable>
      ) : (
        pill
      )}
      {onPress ? (
        <Pressable
          testID={pressTestID}
          accessibilityRole="button"
          accessibilityLabel={pressLabel ?? `Player card for ${player.displayName}`}
          accessibilityHint={description}
          onPress={onPress}
          style={(state: PressState) => [bodyStyle, ...pressFeedback(colors, state)]}
        >
          {body}
        </Pressable>
      ) : (
        <View accessibilityLabel={`${player.displayName}, ${description}`} style={bodyStyle}>
          {body}
        </View>
      )}
    </View>
  );
}

/** One squad player with next gameweek's opponents (`ARS, CHE`, or `No game`). */
export function RosterRow({
  entry,
  clubs,
}: {
  entry: Schemas['RosterEntry'];
  /** The club list (/v1/clubs), for the fixtures' opponent names. */
  clubs?: readonly Schemas['ClubRef'][];
}) {
  const nextFixtures = expandRowFixtures(entry.nextFixtures, clubs);
  const { colors, spacing, fontFamilies } = useTheme();
  const { player } = entry;
  return (
    <View
      testID={`roster-${entry.slot}`}
      style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}
    >
      <PositionBadge position={player.position} size="lg" />
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <AppText numberOfLines={1}>
          <AppText style={{ fontFamily: fontFamilies.bold, fontSize: 16, lineHeight: 21 }}>
            {player.displayName}
          </AppText>
          <AppText
            variant="meta"
            weight="semibold"
            color={colors[positionColorKey(player.position)]}
          >
            {`  ${positionLabel(player.position)}`}
          </AppText>
          <AppText variant="meta" muted>
            {` - ${player.club.code.toUpperCase()}`}
          </AppText>
        </AppText>
        {nextFixtures.length === 0 ? (
          <AppText variant="meta" muted testID="next-fixtures">
            {formatNextFixtures(nextFixtures)}
          </AppText>
        ) : (
          <FixtureStrip fixtures={nextFixtures} position={player.position} testID="next-fixtures" />
        )}
      </View>
    </View>
  );
}
