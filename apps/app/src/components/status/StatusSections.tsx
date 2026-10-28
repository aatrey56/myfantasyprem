import type { Schemas } from '@myfantasyprem/api-client';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import { Platform, Pressable, ScrollView, View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest, PlayerPhoto, PositionBadge, positionLabel } from '../Badges';
import { useBrandedAssets } from '../branded/BrandedAssets';
import { Button, IconButton } from '../Button';
import { Card } from '../Card';
import { Chip, Pill } from '../Chip';
import { EmptyState } from '../EmptyState';
import { ChevronLeft, ChevronRight, TriangleAlert, Users } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { PressableLink } from '../PressableLink';
import { FormationPitch, type PitchLine } from '../pitch/FormationPitch';
import { GameweekSwitcher } from '../team/GameweekSwitcher';
import { shortName } from '../team/LineupViews';
import { HazardIcon, STATUS_COLORS, StopIcon } from './StatusIcons';
import { useLayout } from '@/hooks/useLayout';
import { formText } from '@/lib/form';
import { clubColors, initialsOf } from '@/lib/identity';
import { scoreStateLabel } from '@/lib/matchup';
import {
  attentionHref,
  clampOffset,
  fixtureCell,
  injuryBadge,
  ordinal,
  POTW_COMPACT_BELOW,
  POTW_SHOWN,
  potwCardWidth,
  potwFirstShown,
  rankChangeLabel,
  sortAttention,
  topAvailableFor,
  totwLines,
  upcomingDeadlines,
  type FixtureLine,
  type GameweekSummary,
  type PositionFilter,
  type TableRow,
} from '@/lib/status';
import { formatDateTime, formatRelative } from '@/lib/time';
import { waiversDueLabel } from '@/lib/waivers';
import { tint, useTheme } from '@/theme';

/** A section title (h2). */
function Heading({ children, right }: { children: string; right?: ReactNode }) {
  const { spacing } = useTheme();
  return (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        justifyContent: 'space-between',
        flexWrap: 'wrap',
        gap: spacing.sm,
      }}
    >
      <AppText variant="heading" accessibilityRole="header" aria-level={2}>
        {children}
      </AppText>
      {right}
    </View>
  );
}

// ---------------------------------------------------------------- attention

export function AttentionStrip({
  items,
  leagueId,
}: {
  items: readonly Schemas['StatusAttentionItem'][];
  leagueId: string;
}) {
  const { colors, spacing, radii } = useTheme();
  if (items.length === 0) return null;
  return (
    <View
      role="region"
      aria-label="Needs your attention"
      testID="attention-strip"
      style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}
    >
      {sortAttention(items).map((item, i) => {
        const href = attentionHref(item, leagueId);
        const critical = item.severity === 'critical';
        const warning = item.severity === 'warning';
        const fg = critical ? STATUS_COLORS.onStop : warning ? colors.accent : colors.textMuted;
        const bg = critical
          ? STATUS_COLORS.stop
          : warning
            ? tint(colors.accent, colors.background, 0.12)
            : colors.surfaceRaised;
        const body = (
          <>
            {critical ? (
              <StopIcon size={16} />
            ) : (
              <View
                aria-hidden
                style={{ width: 8, height: 8, borderRadius: 4, backgroundColor: fg }}
              />
            )}
            <AppText weight="semibold" color={fg} numberOfLines={2} style={{ flexShrink: 1 }}>
              {item.message}
            </AppText>
          </>
        );
        const style = {
          flexDirection: 'row' as const,
          alignItems: 'center' as const,
          gap: spacing.sm,
          minHeight: 44,
          maxWidth: '100%' as const,
          paddingHorizontal: spacing.lg,
          paddingVertical: spacing.xs,
          borderRadius: radii.md,
          backgroundColor: bg,
        };
        return href ? (
          <PressableLink
            key={`${item.kind}-${i}`}
            href={href}
            testID={`attention-${item.kind}`}
            accessibilityLabel={item.message}
            style={(state: PressState) => [
              style,
              ...pressFeedback(colors, state, { hoverFill: false }),
            ]}
          >
            {body}
          </PressableLink>
        ) : (
          <View key={`${item.kind}-${i}`} testID={`attention-${item.kind}`} style={style}>
            {body}
          </View>
        );
      })}
    </View>
  );
}

// ---------------------------------------------------------------- summary

export function GameweekSummaryCard({ summary }: { summary: GameweekSummary }) {
  const { colors, spacing, radii } = useTheme();
  const st = scoreStateLabel(summary.state);
  const change = rankChangeLabel(summary.rankChange);
  const { isPhone } = useLayout();
  const big = isPhone
    ? ({ fontSize: 30, lineHeight: 36, letterSpacing: -0.5 } as const)
    : ({ fontSize: 40, lineHeight: 46, letterSpacing: -1 } as const);
  const resultColor =
    summary.result === 'W' ? colors.win : summary.result === 'L' ? colors.loss : colors.textMuted;
  const column = (label: string, children: ReactNode, testID: string) => (
    <View style={{ flex: 1, minWidth: 0, gap: spacing.xs }} testID={testID}>
      <AppText variant="eyebrow" muted>
        {label}
      </AppText>
      {children}
    </View>
  );
  return (
    <Card testID="gw-summary" style={{ padding: isPhone ? spacing.lg : spacing.xl }}>
      <View
        role="region"
        aria-label={`Gameweek ${summary.gameweek} summary`}
        style={{
          flexDirection: 'row',
          gap: isPhone ? spacing.md : spacing.lg,
          alignItems: 'flex-start',
        }}
      >
        {column(
          `Gameweek ${summary.gameweek}`,
          <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm }}>
            <AppText weight="semibold" numeric style={big} testID="summary-points">
              {summary.points === null ? '–' : String(summary.points)}
            </AppText>
            <AppText muted>points</AppText>
          </View>,
          'summary-gw',
        )}
        {column(
          'Result',
          <View
            style={{
              flexDirection: isPhone ? 'column' : 'row',
              alignItems: isPhone ? 'flex-start' : 'center',
              gap: isPhone ? spacing.xs : spacing.md,
            }}
          >
            {summary.result ? (
              <View
                testID="summary-result"
                accessibilityLabel={
                  summary.result === 'W' ? 'Won' : summary.result === 'L' ? 'Lost' : 'Drew'
                }
                style={{
                  width: 40,
                  height: 40,
                  borderRadius: radii.md,
                  alignItems: 'center',
                  justifyContent: 'center',
                  backgroundColor: tint(resultColor, colors.surface),
                }}
              >
                <AppText weight="bold" color={resultColor}>
                  {summary.result}
                </AppText>
              </View>
            ) : (
              <View testID="summary-result" style={{ alignSelf: 'center' }}>
                <Pill label={st.label} tone={st.tone} dot={st.dot} />
              </View>
            )}
            <AppText
              numeric
              numberOfLines={1}
              style={isPhone ? { fontSize: 18, lineHeight: 24 } : { fontSize: 22, lineHeight: 28 }}
            >
              {summary.points === null || summary.opponentPoints === null
                ? '–'
                : `${summary.points} – ${summary.opponentPoints}`}
            </AppText>
          </View>,
          'summary-score',
        )}
        {column(
          'League position',
          <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm }}>
            <AppText weight="semibold" numeric style={big} testID="summary-rank">
              {summary.rank === null ? '–' : ordinal(summary.rank)}
            </AppText>
            <AppText
              numeric
              accessibilityLabel={change.a11y}
              color={
                change.tone === 'up'
                  ? colors.win
                  : change.tone === 'down'
                    ? colors.loss
                    : colors.textMuted
              }
            >
              {change.text}
            </AppText>
          </View>,
          'summary-position',
        )}
      </View>
    </Card>
  );
}

// ---------------------------------------------------------------- deadlines

/** The device clock, ticking once a minute (deadlines show days and hours). */
function useMinuteClock(): number {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 60_000);
    return () => clearInterval(id);
  }, []);
  return now;
}

export function DeadlineCards({
  gameweeks,
  waivers,
  myTeamId,
}: {
  gameweeks: Schemas['GameweekStatus'];
  waivers?: Schemas['WaiverState'];
  myTeamId?: string;
}) {
  const { spacing } = useTheme();
  const now = useMinuteClock();
  const deadlines = upcomingDeadlines(gameweeks, now, waivers, myTeamId);
  if (deadlines.length === 0) return null;
  return (
    <View style={{ gap: spacing.md }} testID="status-deadlines">
      <Heading>Upcoming Deadlines</Heading>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md }}>
        {deadlines.map((d) => (
          <Card key={d.key} testID={`deadline-${d.key}`} style={{ flex: 1, minWidth: 220 }}>
            <AppText muted variant="meta">
              {d.label}
            </AppText>
            <AppText weight="semibold" style={{ fontSize: 20, lineHeight: 26 }}>
              {formatDateTime(d.at)}
            </AppText>
            <AppText accent variant="meta">
              {formatRelative(d.at, now)}
            </AppText>
          </Card>
        ))}
      </View>
    </View>
  );
}

// ---------------------------------------------------------------- top available

const FILTERS: { value: PositionFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'GKP', label: 'GK' },
  { value: 'DEF', label: 'DEF' },
  { value: 'MID', label: 'MID' },
  { value: 'FWD', label: 'FWD' },
];

export function TopAvailable({
  players,
  canSign,
  onOpen,
  onSign,
}: {
  players: readonly Schemas['StatusAvailablePlayer'][];
  canSign: boolean;
  onOpen: (playerId: string) => void;
  onSign: (playerId: string) => void;
}) {
  const { colors, spacing } = useTheme();
  const branded = useBrandedAssets();
  const [filter, setFilter] = useState<PositionFilter>('all');
  const [width, setWidth] = useState(0);
  const rows = topAvailableFor(players, filter);
  // Three fixture columns where they fit, one on a small tablet, none on a phone.
  const weeks = width >= 600 ? 3 : width >= 440 ? 1 : 0;
  const gameweeks = (players[0]?.upcoming ?? []).slice(0, weeks).map((u) => u.gameweek);
  return (
    <Card flush testID="top-available">
      <View
        style={{ padding: spacing.lg, paddingBottom: spacing.sm, gap: spacing.sm }}
        onLayout={(e) => setWidth(e.nativeEvent.layout.width)}
      >
        <Heading
          right={
            <View
              accessibilityLabel="Position"
              style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}
            >
              {FILTERS.map((f) => (
                <Chip
                  key={f.value}
                  label={f.label}
                  selected={filter === f.value}
                  onPress={() => setFilter(f.value)}
                  testID={`top-filter-${f.value}`}
                />
              ))}
            </View>
          }
        >
          Top Available
        </Heading>
      </View>
      {rows.length === 0 ? (
        <EmptyState icon={Users} title="Nobody available" body="Every player is on a team." />
      ) : (
        <View>
          <View
            style={{
              flexDirection: 'row',
              alignItems: 'center',
              gap: spacing.sm,
              paddingHorizontal: spacing.lg,
              paddingVertical: spacing.xs,
            }}
          >
            <AppText variant="eyebrow" subtle style={{ flex: 1 }}>
              Player
            </AppText>
            <AppText variant="eyebrow" subtle style={{ width: 44, textAlign: 'right' }}>
              Form
            </AppText>
            {gameweeks.map((gw) => (
              <AppText key={gw} variant="eyebrow" subtle style={{ width: 76, textAlign: 'center' }}>
                {`GW ${gw}`}
              </AppText>
            ))}
            <View style={{ width: 64 }} />
          </View>
          {rows.map((p) => (
            <View
              key={p.player.id}
              testID={`top-${p.player.id}`}
              style={{
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.sm,
                paddingHorizontal: spacing.lg,
                paddingVertical: spacing.sm,
                borderTopWidth: 1,
                borderTopColor: colors.border,
              }}
            >
              <Pressable
                accessibilityRole="button"
                accessibilityLabel={`Player card for ${p.player.displayName}`}
                onPress={() => onOpen(p.player.id)}
                style={(state: PressState) => [
                  {
                    flex: 1,
                    minWidth: 0,
                    flexDirection: 'row',
                    alignItems: 'center',
                    gap: spacing.sm,
                    borderRadius: 8,
                  },
                  ...pressFeedback(colors, state, { hoverFill: false }),
                ]}
              >
                <PositionBadge position={p.player.position} size="sm" />
                <View style={{ flex: 1, minWidth: 0 }}>
                  <AppText weight="semibold" numberOfLines={1}>
                    {p.player.displayName}
                  </AppText>
                  <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
                    <ClubCrest code={p.player.club.code} size={12} />
                    <AppText variant="meta" subtle numberOfLines={1} style={{ flexShrink: 1 }}>
                      {p.rosterStatus === 'waivers'
                        ? `${p.player.club.code} · Waivers`
                        : p.rosterStatus === 'locked'
                          ? `${p.player.club.code} · ${waiversDueLabel(p.waiverClearsAt)}`
                          : p.player.club.code}
                    </AppText>
                  </View>
                </View>
              </Pressable>
              <AppText numeric weight="semibold" style={{ width: 44, textAlign: 'right' }}>
                {formText(p.form)}
              </AppText>
              {p.upcoming.slice(0, weeks).map((u) =>
                branded && u.fixtures.length > 0 ? (
                  <View
                    key={u.gameweek}
                    accessibilityLabel={fixtureCell(u)}
                    style={{
                      width: 76,
                      flexDirection: 'row',
                      flexWrap: 'wrap',
                      justifyContent: 'center',
                      alignItems: 'center',
                      columnGap: 4,
                    }}
                  >
                    {u.fixtures.map((f, i) => (
                      <View
                        key={`${f.opponent.code}-${i}`}
                        style={{ flexDirection: 'row', alignItems: 'center', gap: 3 }}
                      >
                        <ClubCrest code={f.opponent.code} size={12} />
                        <AppText variant="meta" muted numberOfLines={1}>
                          {fixtureCell({ fixtures: [f] })}
                        </AppText>
                      </View>
                    ))}
                  </View>
                ) : (
                  <AppText
                    key={u.gameweek}
                    variant="meta"
                    muted
                    numberOfLines={2}
                    style={{ width: 76, textAlign: 'center' }}
                  >
                    {fixtureCell(u)}
                  </AppText>
                ),
              )}
              <View style={{ width: 64, alignItems: 'flex-end' }}>
                {canSign && p.rosterStatus !== 'locked' ? (
                  <Button
                    label="Sign"
                    size="sm"
                    variant="secondary"
                    onPress={() => onSign(p.player.id)}
                    accessibilityLabel={`Sign ${p.player.displayName}`}
                    testID={`sign-${p.player.id}`}
                  />
                ) : null}
              </View>
            </View>
          ))}
        </View>
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- player of the week

/** Web only: CSS scroll snap, one card per swipe (native uses snapToInterval). */
const WEB_SNAP_ROW = Platform.OS === 'web' ? ({ scrollSnapType: 'x mandatory' } as object) : null;
const WEB_SNAP_CARD =
  Platform.OS === 'web' ? ({ scrollSnapAlign: 'start', scrollSnapStop: 'always' } as object) : null;

/**
 * Five gameweeks at a time, the latest rightmost, in a row that scrolls and
 * snaps one card at a time; the arrows move one gameweek. Gameweeks before
 * the first have no card. Compact on phones (about 65px cards at 390px).
 */
export function PlayersOfTheWeek({
  weeks,
  onOpen,
}: {
  weeks: readonly Schemas['PlayerOfTheWeek'][];
  onOpen: (playerId: string) => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const [width, setWidth] = useState(0);
  const total = weeks.length;
  const [first, setFirst] = useState(() => Math.max(0, total - POTW_SHOWN));
  const scroller = useRef<ScrollView>(null);
  const compact = width < POTW_COMPACT_BELOW;
  // Compact rows bleed into the card's padding to give the cards room.
  const bleed = compact ? spacing.sm : 0;
  const gap = compact ? spacing.xs : 10;
  const cardW = potwCardWidth(width + 2 * bleed, gap);
  const step = cardW + gap;
  // Where an arrow is scrolling to, until it gets there: taps during the
  // animation count from the target, not from the card passing by.
  const [target, setTarget] = useState<number | null>(null);
  const at = clampOffset(target ?? first, total, POTW_SHOWN);
  const atStart = at === 0;
  const atEnd = at + POTW_SHOWN >= total;

  const place = (index: number, animated: boolean) =>
    scroller.current?.scrollTo({ x: index * step, y: 0, animated });
  // Keep the shown gameweeks when the row is laid out or resized.
  useEffect(() => {
    if (step > 0) place(at, false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [step]);
  const move = (by: number) => {
    const to = clampOffset(at + by, total, POTW_SHOWN);
    setTarget(to);
    place(to, true);
  };
  const onScroll = (x: number) => {
    if (target !== null && Math.abs(x - target * step) < 1) setTarget(null);
    setFirst(potwFirstShown(x, step, total));
  };

  return (
    <Card testID="potw" style={{ gap: spacing.md }}>
      <Heading
        right={
          <View style={{ flexDirection: 'row', gap: spacing.sm }}>
            <IconButton
              icon={ChevronLeft}
              label="Earlier gameweek"
              variant="filled"
              disabled={atStart}
              onPress={() => move(-1)}
              testID="potw-prev"
            />
            <IconButton
              icon={ChevronRight}
              label="Later gameweek"
              variant="filled"
              disabled={atEnd}
              onPress={() => move(1)}
              testID="potw-next"
            />
          </View>
        }
      >
        Player of the Week
      </Heading>
      <View testID="potw-frame" onLayout={(e) => setWidth(e.nativeEvent.layout.width)}>
        {width === 0 ? null : (
          <ScrollView
            ref={scroller}
            horizontal
            testID="potw-row"
            showsHorizontalScrollIndicator={false}
            snapToInterval={step}
            snapToAlignment="start"
            decelerationRate="fast"
            disableIntervalMomentum
            scrollEventThrottle={16}
            onScroll={(e) => onScroll(e.nativeEvent.contentOffset.x)}
            onContentSizeChange={() => place(at, false)}
            style={[{ marginHorizontal: -bleed }, WEB_SNAP_ROW]}
            // Fewer than five gameweeks still end on the right.
            contentContainerStyle={{ flexGrow: 1, justifyContent: 'flex-end', gap }}
          >
            {weeks.map((w) => (
              <Pressable
                key={w.gameweek}
                testID={`potw-card-${w.gameweek}`}
                accessibilityRole="button"
                accessibilityLabel={`Player card for ${w.award.player.displayName}, gameweek ${w.gameweek}, ${w.award.stats.points} points`}
                onPress={() => onOpen(w.award.player.id)}
                style={(state: PressState) => [
                  {
                    width: cardW,
                    alignItems: 'center',
                    gap: compact ? spacing.xs : spacing.sm,
                    paddingVertical: compact ? spacing.sm : spacing.md,
                    paddingHorizontal: compact ? spacing.xs : spacing.sm,
                    borderRadius: compact ? radii.md : radii.lg,
                    backgroundColor: colors.surfaceRaised,
                  },
                  WEB_SNAP_CARD,
                  ...pressFeedback(colors, state, { hoverFill: false }),
                ]}
              >
                <AppText variant="meta" muted numeric style={compact ? { fontSize: 12 } : null}>
                  {compact ? `GW${w.gameweek}` : `GW ${w.gameweek}`}
                </AppText>
                <PlayerPhoto
                  playerId={w.award.player.id}
                  code={w.award.player.club.code}
                  size={compact ? 28 : 38}
                />
                <View
                  style={{
                    maxWidth: '100%',
                    paddingHorizontal: compact ? 3 : spacing.sm,
                    paddingVertical: 2,
                    borderRadius: compact ? 6 : radii.sm,
                    borderWidth: 1,
                    borderColor: colors.border,
                    backgroundColor: colors.background,
                  }}
                >
                  <AppText
                    weight="semibold"
                    numberOfLines={1}
                    ellipsizeMode="tail"
                    style={compact ? { fontSize: 11, lineHeight: 15 } : null}
                    testID="potw-name"
                  >
                    {/* Phones drop the initial before truncating ("Delacroix"). */}
                    {compact ? shortName(w.award.player.displayName) : w.award.player.displayName}
                  </AppText>
                </View>
                <View
                  style={{
                    paddingHorizontal: compact ? spacing.sm : spacing.md,
                    borderRadius: compact ? 6 : radii.sm,
                    borderWidth: 1,
                    borderColor: colors.accent,
                  }}
                >
                  <AppText
                    accent
                    weight="bold"
                    numeric
                    style={
                      compact ? { fontSize: 15, lineHeight: 20 } : { fontSize: 18, lineHeight: 24 }
                    }
                    testID="potw-points"
                  >
                    {String(w.award.stats.points)}
                  </AppText>
                </View>
              </Pressable>
            ))}
          </ScrollView>
        )}
      </View>
    </Card>
  );
}

// ---------------------------------------------------------------- team of the week

const LINE_NAMES: Record<string, string> = {
  GKP: 'Goalkeeper',
  DEF: 'Defenders',
  MID: 'Midfielders',
  FWD: 'Forwards',
};

/** The Team of the Week as pitch lines, keeper's line first, each with its points. */
export function totwPitchLines(team: readonly Schemas['AwardPlayer'][]): PitchLine[] {
  return totwLines(team).map((line) => {
    const pos = line[0]!.player.position;
    return {
      key: pos,
      label: LINE_NAMES[pos] ?? 'Other',
      players: line.map(({ player, stats }) => ({
        id: player.id,
        name: shortName(player.displayName),
        kit: clubColors(player.club.code).body,
        club: player.club.code,
        goalkeeper: player.position === 'GKP',
        points: stats.points,
        description: `${positionLabel(player.position)}, ${player.club.name}`,
      })),
    };
  });
}

/** The gameweek's best XI on the Team page's pitch; tap a player for their card. */
export function TeamOfTheWeek({
  awards,
  gameweek,
  maxGameweek,
  onChange,
  onOpen,
  loading,
}: {
  awards: Schemas['GameweekAwards'] | undefined;
  gameweek: number;
  maxGameweek: number;
  onChange: (gw: number) => void;
  onOpen: (playerId: string) => void;
  loading?: ReactNode;
}) {
  const { spacing } = useTheme();
  return (
    <Card testID="totw" style={{ gap: spacing.md }}>
      <Heading
        right={
          <GameweekSwitcher gameweek={gameweek} min={1} max={maxGameweek} onChange={onChange} />
        }
      >
        Team of the Week
      </Heading>
      {awards ? (
        <FormationPitch
          lines={totwPitchLines(awards.teamOfTheWeek)}
          variant="points"
          onPress={onOpen}
          pressAction="open"
          playerTestID="totw-player-"
          testID="totw-pitch"
        />
      ) : (
        loading
      )}
    </Card>
  );
}

// ---------------------------------------------------------------- league table

export function LeagueTableCard({
  rows,
  leagueId,
}: {
  rows: readonly TableRow[];
  leagueId: string;
}) {
  const { colors, spacing, radii } = useTheme();
  return (
    <Card flush testID="status-table">
      <View style={{ padding: spacing.lg, paddingBottom: spacing.sm }}>
        <Heading
          right={
            <PressableLink
              href={`/leagues/${leagueId}/league`}
              testID="status-full-table"
              style={(state: PressState) => [
                { paddingHorizontal: spacing.xs, borderRadius: radii.sm },
                ...pressFeedback(colors, state),
              ]}
            >
              <AppText accent weight="semibold" variant="meta">
                Full table
              </AppText>
            </PressableLink>
          }
        >
          League Table
        </Heading>
      </View>
      {rows.map((r) => {
        const c = r.last === 'W' ? colors.win : r.last === 'L' ? colors.loss : colors.textMuted;
        return (
          <View
            key={r.team.id}
            testID={`table-row-${r.team.id}`}
            style={{
              flexDirection: 'row',
              alignItems: 'center',
              gap: spacing.sm,
              paddingHorizontal: spacing.lg,
              paddingVertical: spacing.sm,
              borderTopWidth: 1,
              borderTopColor: colors.border,
              backgroundColor: r.mine ? tint(colors.accent, colors.surface, 0.07) : undefined,
            }}
          >
            <AppText numeric subtle style={{ width: 22 }}>
              {String(r.rank)}
            </AppText>
            <View style={{ flex: 1, minWidth: 0 }}>
              <AppText weight="semibold" numberOfLines={1}>
                {r.team.name}
              </AppText>
              <AppText variant="meta" subtle numberOfLines={1}>
                {r.mine ? 'You' : r.team.managerName}
              </AppText>
            </View>
            {r.last ? (
              <View
                accessibilityLabel={`Last result ${r.last === 'W' ? 'won' : r.last === 'L' ? 'lost' : 'drew'}`}
                style={{
                  width: 26,
                  height: 26,
                  borderRadius: 8,
                  alignItems: 'center',
                  justifyContent: 'center',
                  backgroundColor: tint(c, colors.surface),
                }}
              >
                <AppText variant="meta" weight="bold" color={c}>
                  {r.last}
                </AppText>
              </View>
            ) : (
              <View style={{ width: 26 }} />
            )}
            <AppText numeric style={{ width: 32, textAlign: 'right' }}>
              {r.gameweekPoints === null ? '–' : String(r.gameweekPoints)}
            </AppText>
          </View>
        );
      })}
    </Card>
  );
}

// ---------------------------------------------------------------- fixtures

export function FixturesCard({
  current,
  next,
}: {
  current: FixtureLine | null;
  next: FixtureLine | null;
}) {
  const { colors, spacing } = useTheme();
  if (!current && !next) return null;
  const line = (label: string, f: FixtureLine, testID: string) => (
    <View style={{ gap: spacing.sm }} testID={testID}>
      <AppText variant="eyebrow" muted>
        {label}
      </AppText>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText weight="semibold" numberOfLines={2} style={{ flex: 1, minWidth: 0 }}>
          {f.left}
        </AppText>
        <AppText numeric subtle={!f.score}>
          {f.score ?? 'v'}
        </AppText>
        <AppText
          weight="semibold"
          numberOfLines={2}
          style={{ flex: 1, minWidth: 0, textAlign: 'right' }}
        >
          {f.right}
        </AppText>
      </View>
    </View>
  );
  return (
    <Card testID="status-fixtures" style={{ gap: spacing.md }}>
      <View role="region" aria-label="Fixtures" style={{ gap: spacing.md }}>
        {current ? line(`This gameweek · GW ${current.gameweek}`, current, 'fixture-this') : null}
        {current && next ? (
          <View aria-hidden style={{ height: 1, backgroundColor: colors.border }} />
        ) : null}
        {next ? line(`Next gameweek · GW ${next.gameweek}`, next, 'fixture-next') : null}
      </View>
    </Card>
  );
}

// ---------------------------------------------------------------- injuries

export function InjuriesCard({
  players,
  onOpen,
}: {
  players: readonly Schemas['StatusFlaggedPlayer'][];
  onOpen: (playerId: string) => void;
}) {
  const { colors, spacing, radii } = useTheme();
  return (
    <Card testID="status-injuries" style={{ gap: spacing.sm }}>
      <Heading>Injuries and Suspensions</Heading>
      {players.length === 0 ? (
        <EmptyState icon={TriangleAlert} title="Nobody flagged" body="Every player is fit." />
      ) : (
        <ScrollView
          style={{ maxHeight: 560 }}
          nestedScrollEnabled
          accessibilityLabel="All flagged players"
          {...({ tabIndex: 0 } as object)}
        >
          {players.map((f, i) => {
            const badge = injuryBadge(f);
            const out = badge.kind === 'out';
            const open = () => onOpen(f.player.id);
            return (
              <View
                key={f.player.id}
                testID={`injury-${f.player.id}`}
                style={{
                  flexDirection: 'row',
                  alignItems: 'center',
                  gap: spacing.sm,
                  paddingVertical: spacing.sm,
                  borderTopWidth: i > 0 ? 1 : 0,
                  borderTopColor: colors.border,
                }}
              >
                <Pressable
                  testID={`injury-badge-${f.player.id}`}
                  accessibilityRole="button"
                  accessibilityLabel={`Player card for ${f.player.displayName}, ${badge.a11y}`}
                  onPress={open}
                  style={(state: PressState) => [
                    {
                      width: 68,
                      height: 28,
                      flexDirection: 'row',
                      alignItems: 'center',
                      justifyContent: 'center',
                      gap: 4,
                      borderRadius: radii.sm,
                      backgroundColor: out ? STATUS_COLORS.stop : STATUS_COLORS.hazard,
                    },
                    ...pressFeedback(colors, state, { hoverFill: false }),
                  ]}
                >
                  {out ? <StopIcon size={13} /> : <HazardIcon size={14} />}
                  <AppText
                    variant="meta"
                    weight="bold"
                    color={out ? STATUS_COLORS.onStop : STATUS_COLORS.onHazard}
                  >
                    {badge.label}
                  </AppText>
                </Pressable>
                <Pressable
                  accessibilityRole="button"
                  accessibilityLabel={`Player card for ${f.player.displayName}`}
                  onPress={open}
                  style={(state: PressState) => [
                    { flex: 1, minWidth: 0, borderRadius: radii.sm },
                    ...pressFeedback(colors, state, { hoverFill: false }),
                  ]}
                >
                  <AppText weight="semibold" numberOfLines={1}>
                    {f.player.displayName}
                  </AppText>
                </Pressable>
                <AppText variant="meta" weight="semibold" muted style={{ width: 36 }}>
                  {positionLabel(f.player.position)}
                </AppText>
                <AppText variant="meta" muted style={{ width: 40 }}>
                  {f.player.club.code}
                </AppText>
                <AppText
                  variant="meta"
                  weight="semibold"
                  muted
                  accessibilityLabel={f.owner ? `Owned by ${f.owner.name}` : 'Not owned'}
                  {...({ title: f.owner?.name ?? 'Not owned' } as object)}
                  style={{ width: 32, textAlign: 'right' }}
                >
                  {f.owner ? initialsOf(f.owner.name) : '–'}
                </AppText>
              </View>
            );
          })}
        </ScrollView>
      )}
    </Card>
  );
}
