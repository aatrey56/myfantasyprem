import type { Schemas } from '@myfantasyprem/api-client';
import type { ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest, PositionBadge, positionLabel } from '../Badges';
import { TriangleAlert } from '../icons';
import { availabilityText, SubChip } from '../PlayerRow';
import { playerCardLabel, usePlayerCard } from '../players/PlayerCardContext';
import { pressFeedback, type PressState } from '../pressable';
import { shortName } from '../team/LineupViews';
import { useLayout } from '@/hooks/useLayout';
import { fixtureLabel } from '@/lib/fixtures';
import { autoSubOf, autoSubSummary, beforeKickoff, fixtureState, pairBench } from '@/lib/matchup';
import { pairInOrder } from '@/lib/h2h-pairing';
import { positionColorKey, useTheme } from '@/theme';

type Slot = Schemas['LineupSlot'];

interface Props {
  left: Schemas['TeamLineup'];
  right: Schemas['TeamLineup'];
  /** The matchup's score state: auto-subs are projected until every game is played. */
  state: string;
  /** Opens the points breakdown. */
  onPick: (slot: Slot, teamName: string) => void;
  /** On the right of the Starters heading: the gameweek stepper. */
  stepper?: ReactNode;
}

/**
 * Both XIs side by side, GK→FWD, as tall rows floating on the page (no card
 * or row borders): name, `POS • CLUB` and the fixture line at the outer
 * edge, points at the inner edge (a dash before kickoff) and a square
 * position pill dead centre; then both benches, muted and numbered 1–4 in
 * auto-sub order. A player's name opens their card; their points open the
 * points breakdown.
 */
export function H2HLineups({ left, right, state, onPick, stepper }: Props) {
  const { spacing } = useTheme();
  const starters = pairInOrder(left.starters, right.starters);
  const bench = pairBench(left.bench, right.bench);
  // The pressable cells pad their text: pull them out so names line up with the heading.
  const rows = { gap: spacing.sm, marginHorizontal: -spacing.sm };
  const subs = [autoSubSummary(left, state), autoSubSummary(right, state)].filter(
    Boolean,
  ) as string[];
  const pick = (s: Slot, side: 'left' | 'right') =>
    onPick(s, side === 'left' ? left.team.name : right.team.name);
  return (
    <View style={{ gap: spacing.lg }} testID="h2h-lineups">
      <View style={{ gap: spacing.sm }}>
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
          <Heading title="Starters" testID="starters-header" />
          <View style={{ flex: 1 }} />
          {stepper}
        </View>
        <View style={rows} testID="h2h-starters">
          {starters.map((row, i) => (
            <Row
              key={i}
              left={row.left}
              right={row.right}
              center={<PairBadge left={row.left} right={row.right} />}
              onPick={pick}
            />
          ))}
        </View>
      </View>
      {subs.length > 0 ? (
        <View style={{ gap: 2 }} testID="autosub-summary">
          {subs.map((s) => (
            <AppText key={s} variant="meta" muted>
              {s}
            </AppText>
          ))}
        </View>
      ) : null}
      <View style={{ gap: spacing.sm }}>
        <Heading title="Bench" testID="bench-header" />
        <View style={rows} testID="h2h-bench">
          {bench.map((row) => (
            <Row
              key={row.order}
              left={row.left}
              right={row.right}
              muted
              center={<BenchOrder order={row.order} />}
              onPick={pick}
            />
          ))}
        </View>
      </View>
    </View>
  );
}

/** A bold section heading, like "Starters". */
function Heading({ title, testID }: { title: string; testID: string }) {
  return (
    <View testID={testID} style={{ minHeight: 28, justifyContent: 'center' }}>
      <AppText accessibilityRole="header" weight="bold" style={{ fontSize: 18, lineHeight: 24 }}>
        {title}
      </AppText>
    </View>
  );
}

type Positioned = { player: { position: string } } | null;

/** The row's position; split in two position colours when the players' positions differ. */
export function PairBadge({ left, right }: { left: Positioned; right: Positioned }) {
  const { colors, radii, fontFamilies } = useTheme();
  const a = left?.player.position ?? right?.player.position ?? '?';
  const b = right?.player.position ?? a;
  if (a === b) return <PositionBadge position={a} size="square" />;
  const half = (position: string, side: 'left' | 'right') => {
    const key = positionColorKey(position);
    const known = key !== 'textMuted';
    return (
      <View
        testID={`pair-badge-${side}`}
        style={{
          flex: 1,
          alignItems: 'center',
          justifyContent: 'center',
          backgroundColor: known ? colors[key] : colors.surfaceSelected,
        }}
      >
        <AppText
          color={known ? colors.onPosition : colors.textMuted}
          style={{ fontFamily: fontFamilies.bold, fontSize: 9, lineHeight: 12, letterSpacing: 0 }}
        >
          {positionLabel(position)}
        </AppText>
      </View>
    );
  };
  return (
    <View
      testID="pair-badge-split"
      accessibilityLabel={`Positions ${positionLabel(a)} and ${positionLabel(b)}`}
      style={{
        flexDirection: 'row',
        width: 40,
        height: 30,
        borderRadius: radii.sm,
        overflow: 'hidden',
      }}
    >
      {half(a, 'left')}
      {half(b, 'right')}
    </View>
  );
}

/** The bench's auto-sub order in a quiet square where starters show the position. */
function BenchOrder({ order }: { order: number }) {
  const { colors, radii } = useTheme();
  return (
    <View
      accessibilityLabel={`Bench ${order}`}
      style={{
        minWidth: 30,
        height: 30,
        borderRadius: radii.sm,
        alignItems: 'center',
        justifyContent: 'center',
        backgroundColor: colors.surfaceRaised,
      }}
    >
      <AppText variant="meta" muted numeric weight="semibold">
        {String(order)}
      </AppText>
    </View>
  );
}

function Row({
  left,
  right,
  center,
  muted,
  onPick,
}: {
  left: Slot | null;
  right: Slot | null;
  center: ReactNode;
  muted?: boolean;
  onPick: (slot: Slot, side: 'left' | 'right') => void;
}) {
  return (
    <View
      style={{
        flexDirection: 'row',
        alignItems: 'stretch',
        minHeight: muted ? 72 : 84,
      }}
    >
      <Cell slot={left} align="left" muted={muted} onPick={(s) => onPick(s, 'left')} />
      <View style={{ justifyContent: 'center', alignItems: 'center', width: 40 }}>{center}</View>
      <Cell slot={right} align="right" muted={muted} onPick={(s) => onPick(s, 'right')} />
    </View>
  );
}

/**
 * One player: bold name (with availability flag and auto-sub chip), then
 * `POS • CLUB` with the position in its colour, then the fixture line, at
 * the outer edge; points on the inner edge. Mirrored on the right.
 */
function Cell({
  slot,
  align,
  muted,
  onPick,
}: {
  slot: Slot | null;
  align: 'left' | 'right';
  muted?: boolean;
  onPick: (slot: Slot) => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const { width } = useLayout();
  const openCard = usePlayerCard();
  if (!slot) return <View style={{ flex: 1 }} />;
  const right = align === 'right';
  const sub = autoSubOf(slot.autoSub);
  const status = availabilityText(slot.player.availability);
  const subbedOut = sub.kind === 'out';
  const dim = muted || subbedOut;
  const name = width < 600 ? shortName(slot.player.displayName) : slot.player.displayName;
  const posKey = positionColorKey(slot.player.position);
  const fixtures =
    slot.fixtures.length === 0
      ? [{ key: 'blank', text: 'No fixture', live: false }]
      : slot.fixtures.map((f) => {
          const st = fixtureState(f);
          return {
            key: f.fixtureId,
            text: `${st.text} ${fixtureLabel(f)}`,
            live: st.live,
          };
        });
  const a11y = [
    slot.player.displayName,
    positionLabel(slot.player.position),
    slot.player.club.name,
    status,
    slot.fixtures.length === 0
      ? 'no fixture'
      : slot.fixtures.map((f) => `${fixtureLabel(f)} ${fixtureState(f).text}`).join(' and '),
    beforeKickoff(slot) && slot.points === 0 ? 'not played yet' : `${slot.points} points`,
    sub.kind ? `auto-sub ${sub.kind}${sub.projected ? ', projected' : ''}` : '',
  ]
    .filter(Boolean)
    .join(', ');
  const lineAlign = right ? 'right' : 'left';

  const info = (
    <View style={{ flex: 1, minWidth: 0, gap: 2, alignItems: right ? 'flex-end' : 'flex-start' }}>
      <View
        style={{
          flexDirection: right ? 'row-reverse' : 'row',
          alignItems: 'center',
          gap: spacing.xs,
          maxWidth: '100%',
        }}
      >
        <AppText
          weight="bold"
          numberOfLines={1}
          color={dim ? colors.textMuted : colors.text}
          style={{
            fontSize: 16,
            lineHeight: 21,
            flexShrink: 1,
            textDecorationLine: subbedOut ? 'line-through' : 'none',
          }}
        >
          {name}
        </AppText>
        {status ? (
          <TriangleAlert
            size={12}
            color={slot.player.availability === 'doubtful' ? colors.accent : colors.loss}
            aria-hidden
          />
        ) : null}
        {sub.kind ? <SubChip kind={sub.kind} projected={sub.projected} /> : null}
      </View>
      <View
        style={{
          flexDirection: right ? 'row-reverse' : 'row',
          alignItems: 'center',
          gap: spacing.xs,
          maxWidth: '100%',
        }}
      >
        <ClubCrest code={slot.player.club.code} size={12} />
        <AppText
          variant="meta"
          numberOfLines={1}
          muted
          style={{ textAlign: lineAlign, flexShrink: 1 }}
        >
          <AppText
            variant="meta"
            weight="semibold"
            color={muted || posKey === 'textMuted' ? colors.textMuted : colors[posKey]}
          >
            {positionLabel(slot.player.position)}
          </AppText>
          {` • ${slot.player.club.code.toUpperCase()}`}
        </AppText>
      </View>
      <View
        style={{
          flexDirection: right ? 'row-reverse' : 'row',
          alignItems: 'center',
          columnGap: spacing.sm,
          flexWrap: 'wrap',
          maxWidth: '100%',
        }}
      >
        {fixtures.map((f) => (
          <AppText
            key={f.key}
            variant="meta"
            numberOfLines={1}
            color={f.live ? colors.accent : colors.textSubtle}
            weight={f.live ? 'semibold' : undefined}
            testID="fixture-state"
            style={{ flexShrink: 1 }}
          >
            {f.live ? `● ${f.text}` : f.text}
          </AppText>
        ))}
      </View>
    </View>
  );
  // Two hit targets side by side: the name (card) and the points (breakdown).
  return (
    <View
      testID={`h2h-player-${slot.player.id}`}
      style={{
        flex: 1,
        minWidth: 0,
        flexDirection: right ? 'row-reverse' : 'row',
        alignItems: 'center',
        gap: spacing.xs,
        opacity: muted ? 0.85 : 1,
      }}
    >
      <Pressable
        testID={`open-card-${slot.player.id}`}
        disabled={!openCard}
        accessibilityRole={openCard ? 'button' : undefined}
        accessibilityLabel={openCard ? playerCardLabel(slot.player.displayName) : a11y}
        accessibilityHint={openCard ? a11y : undefined}
        onPress={() => openCard?.(slot.player.id)}
        style={(state: PressState) => [
          {
            flex: 1,
            minWidth: 0,
            flexDirection: 'row',
            alignSelf: 'stretch',
            alignItems: 'center',
            paddingVertical: spacing.sm,
            paddingHorizontal: spacing.sm,
            borderRadius: radii.md,
          },
          ...pressFeedback(colors, state),
        ]}
      >
        {info}
      </Pressable>
      <BreakdownPoints slot={slot} muted={dim} raised={false} onPress={() => onPick(slot)} />
    </View>
  );
}

/**
 * A player's points as the button that opens their points breakdown: bold,
 * a dash before kickoff.
 */
export function BreakdownPoints({
  slot,
  muted,
  raised = true,
  onPress,
}: {
  slot: Slot;
  /** Grey figures (bench, auto-subbed out). */
  muted?: boolean;
  /** A raised fill behind the figures. */
  raised?: boolean;
  onPress: () => void;
}) {
  const { colors, radii } = useTheme();
  // A dash until his first game kicks off (and nothing has scored).
  const waiting = beforeKickoff(slot) && slot.points === 0;
  return (
    <Pressable
      testID={`h2h-breakdown-${slot.player.id}`}
      accessibilityRole="button"
      accessibilityLabel={`Points breakdown for ${slot.player.displayName}: ${
        waiting ? 'not played yet' : `${slot.points} points`
      }`}
      onPress={onPress}
      hitSlop={{ top: 8, bottom: 8, left: 4, right: 4 }}
      style={(state: PressState) => [
        {
          minWidth: 36,
          minHeight: 36,
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: radii.sm,
          paddingHorizontal: 4,
          paddingVertical: 2,
          backgroundColor: raised ? colors.surfaceRaised : 'transparent',
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <AppText
        variant="heading"
        numeric
        weight="bold"
        testID="h2h-points"
        color={muted || waiting ? colors.textMuted : colors.text}
      >
        {waiting ? '–' : String(slot.points)}
      </AppText>
    </Pressable>
  );
}
