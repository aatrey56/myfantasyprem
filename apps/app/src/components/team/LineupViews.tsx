import type { Schemas } from '@myfantasyprem/api-client';
import { useCallback, useEffect, useRef, useState, type RefObject } from 'react';
import { Pressable, useWindowDimensions, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from '../AppText';
import { positionLabel } from '../Badges';
import {
  FormationPitch,
  InfoButton,
  Shirt,
  type PitchLine,
  type PitchPlayer,
} from '../pitch/FormationPitch';
import { minPitchHeight, PITCH_HEIGHT, PITCH_MIN_HEIGHT } from '../pitch/pitchGeometry';
import { SubChip, availabilityText } from '../PlayerRow';
import { useScreenScroll } from '../screen-scroll-context';
import { useHasBottomTabBar } from '../tab-bar-context';
import { pressFeedback, type PressState } from '../pressable';
import { LineupRow } from '../RosterRow';
import { fixtureLabel, type FixtureLike } from '@/lib/fixtures';
import { clubColors } from '@/lib/identity';
import { linesOf, type LineupState, type SwapCheck } from '@/lib/lineup';
import { useLayout } from '@/hooks/useLayout';
import { tint, useTheme } from '@/theme';
import type { TagMeta } from '@/lib/tags';

type Entry = Schemas['LineupEntry'];

/** Points, auto-sub state and played fixtures for a player in a started gameweek. */
export interface PlayerScore {
  points: number;
  autoSub: Schemas['AutoSubState'];
}

/** Scores by player id from a started gameweek's lineup; null before kickoff. */
export function scoresOf(lineup: Schemas['Lineup'] | undefined): Map<string, PlayerScore> | null {
  if (!lineup || lineup.totalPoints === undefined) return null;
  const m = new Map<string, PlayerScore>();
  for (const e of [...lineup.starters, ...lineup.bench]) {
    if (e.points === undefined) continue;
    m.set(e.player.id, {
      points: e.points,
      autoSub: e.autoSub ?? 'none',
    });
  }
  return m;
}

export interface LineupViewProps {
  state: LineupState;
  scores: ReadonlyMap<string, PlayerScore> | null;
  editable: boolean;
  selectedId: string | null;
  /** For the selected player: what tapping each other player would do. */
  check: (targetId: string) => SwapCheck;
  onPress: (playerId: string) => void;
  /** Opens the player card. */
  onInfo: (playerId: string) => void;
}

/** Bench places, in auto-sub order (the first is the backup keeper). */
const BENCH_LABELS = ['1', '2', '3', '4'];

/** "A. Gustavsen" → "Gustavsen" for narrow pitches. */
export function shortName(name: string): string {
  const m = name.match(/^\p{L}{1,2}\.\s+(.+)$/u);
  return m ? m[1]! : name;
}

function fixturesOf(entry: Entry): FixtureLike[] {
  return entry.fixtures;
}

const LINE_NAMES: Record<string, string> = {
  GKP: 'Goalkeeper',
  DEF: 'Defenders',
  MID: 'Midfielders',
  FWD: 'Forwards',
};

/** The line under a name: the fixture, "+1" for a double, or no fixture. */
function fixtureLine(entry: Entry): string {
  const fixtures = fixturesOf(entry);
  if (fixtures.length === 0) return 'No fixture';
  return fixtures.length > 1 ? `${fixtureLabel(fixtures[0]!)} +1` : fixtureLabel(fixtures[0]!);
}

/** One lineup entry as a pitch player. */
export function pitchPlayer(
  entry: Entry,
  score: PlayerScore | undefined,
  options: { short?: boolean; dimmed?: boolean; bench?: string } = {},
): PitchPlayer {
  const { player } = entry;
  const status = availabilityText(player.availability);
  const subOut = score?.autoSub === 'projected_out' || score?.autoSub === 'confirmed_out';
  const subIn = score?.autoSub === 'projected_in' || score?.autoSub === 'confirmed_in';
  const projected = score?.autoSub === 'projected_out' || score?.autoSub === 'projected_in';
  const fixtures = fixturesOf(entry);
  return {
    id: player.id,
    name: options.short ? shortName(player.displayName) : player.displayName,
    kit: clubColors(player.club.code).body,
    club: player.club.code,
    goalkeeper: player.position === 'GKP',
    detail: fixtureLine(entry),
    points: score?.points,
    flag: status ? (player.availability === 'doubtful' ? 'doubtful' : 'out') : undefined,
    autoSub: subOut || subIn ? { kind: subIn ? 'in' : 'out', projected } : undefined,
    dimmed: options.dimmed,
    description: [
      positionLabel(player.position),
      player.club.name,
      options.bench ? `bench ${options.bench}` : 'starting',
      status,
      score
        ? `${score.points} points`
        : fixtures.length === 0
          ? 'no fixture'
          : fixtures.map(fixtureLabel).join(' and '),
      subOut ? 'auto-sub out' : subIn ? 'auto-sub in' : '',
    ]
      .filter(Boolean)
      .join(', '),
  };
}

/** Gap between the pitch and the bench, and kept free under the bench. */
const FIT_GAP = 8;

/**
 * Pitch height that leaves the pitch and the bench on one screen: the
 * window, less what sits above the pitch (top), the bench, and the bottom
 * bar; clamped between a readable minimum (taller with larger text) and
 * the full design height.
 */
export function fitPitchHeight(
  window: number,
  top: number,
  bench: number,
  bottom: number,
  minHeight: number = PITCH_MIN_HEIGHT,
): number {
  const free = window - top - FIT_GAP - bench - FIT_GAP - bottom;
  return Math.round(Math.max(minHeight, Math.min(PITCH_HEIGHT, free)));
}

/**
 * Where the pitch starts in the scroll content: its top on screen plus how
 * far the content is scrolled. The same before and after any scrolling.
 */
export function pitchTopInContent(windowY: number, scrollY: number): number {
  return windowY + scrollY;
}

/**
 * Measures where the pitch starts in the scroll content and the bench's
 * height, and sizes the pitch so both fit on one screen (scrolled to the
 * top). Re-measures whenever the pitch, the content above it (an errors
 * banner, the header) or the viewport (rotation) changes.
 */
function useFittedPitch(ref: RefObject<View | null>) {
  const { height: window, width } = useLayout();
  const { layout } = useTheme();
  const insets = useSafeAreaInsets();
  const hasBar = useHasBottomTabBar();
  const scroll = useScreenScroll();
  const { fontScale } = useWindowDimensions();
  const [top, setTop] = useState<number | null>(null);
  const [bench, setBench] = useState(64);
  const bottom = hasBar ? layout.bottomTabsHeight + 1 + insets.bottom : insets.bottom;
  const height =
    top === null
      ? PITCH_HEIGHT
      : fitPitchHeight(window, top, bench, bottom, minPitchHeight(4, fontScale));
  const measure = useCallback(() => {
    ref.current?.measureInWindow((_x, y) => {
      if (!Number.isFinite(y)) return;
      const next = pitchTopInContent(y, scroll?.offset() ?? 0);
      setTop((prev) => (prev !== null && Math.abs(prev - next) < 0.5 ? prev : next));
    });
  }, [ref, scroll]);
  // A new scroll context value means the content or the viewport changed size.
  useEffect(() => measure(), [measure, window, width]);
  const onBenchLayout = (h: number) => {
    if (h > 0 && Math.abs(h - bench) > 0.5) setBench(h);
  };
  return { height, measure, onBenchLayout };
}

/**
 * The pitch: the XI in lines on our drawn half pitch, attacking upward,
 * sized so the bench (numbered 1–4, one row) fits under it on one screen. Tap a player, then another, to swap; the ⓘ
 * opens the player card. When the lineup can't be edited, a tap opens it. Once the gameweek has started, points replace
 * the fixtures.
 */
export function PitchView(props: LineupViewProps) {
  const { state, scores, editable, selectedId, check } = props;
  const ref = useRef<View>(null);
  const fit = useFittedPitch(ref);
  const dimmed = (id: string) =>
    selectedId !== null && selectedId !== id && check(id).kind === 'invalid';
  const lines: PitchLine[] = linesOf(state.starters).map((line) => ({
    key: line.position,
    label: LINE_NAMES[line.position] ?? 'Other',
    players: line.entries.map((e) =>
      pitchPlayer(e, scores?.get(e.player.id), { short: true, dimmed: dimmed(e.player.id) }),
    ),
  }));
  return (
    <View
      ref={ref}
      onLayout={fit.measure}
      testID="pitch-view"
      style={{ gap: FIT_GAP, width: '100%', alignSelf: 'center' }}
    >
      <FormationPitch
        height={fit.height}
        lines={lines}
        variant={scores ? 'points' : 'lineup'}
        selectedId={selectedId}
        // Editing: a tap selects for a swap and the ⓘ opens the card. Viewing: a tap opens the card.
        onPress={editable ? props.onPress : props.onInfo}
        pressAction={editable ? 'swap' : 'open'}
        onInfo={editable ? props.onInfo : undefined}
        playerTestID="lineup-player-"
      />
      <Bench {...props} onLayout={fit.onBenchLayout} />
    </View>
  );
}

/** The bench: one compact row of four under the pitch, numbered in auto-sub order. */
function Bench({
  state,
  scores,
  editable,
  selectedId,
  check,
  onPress,
  onInfo,
  onLayout,
}: LineupViewProps & { onLayout: (height: number) => void }) {
  const { colors, radii } = useTheme();
  return (
    <View
      accessibilityLabel="Bench"
      testID="bench"
      onLayout={(e) => onLayout(e.nativeEvent.layout.height)}
      style={{
        width: '100%',
        maxWidth: 520,
        alignSelf: 'center',
        borderRadius: 16,
        backgroundColor: colors.surface,
        padding: 4,
      }}
    >
      <View style={{ flexDirection: 'row', gap: 4 }} testID="bench-row">
        {state.bench.map((e, i) => {
          const id = e.player.id;
          const p = pitchPlayer(e, scores?.get(id), {
            short: true,
            bench: BENCH_LABELS[i],
            dimmed: selectedId !== null && selectedId !== id && check(id).kind === 'invalid',
          });
          const selected = selectedId === id;
          const body = (
            <>
              <AppText
                variant="meta"
                subtle
                numeric
                style={{ position: 'absolute', left: 6, top: 3, fontSize: 11, lineHeight: 14 }}
              >
                {BENCH_LABELS[i]}
              </AppText>
              <Shirt kit={p.kit} club={p.club} goalkeeper={p.goalkeeper} size={22} />
              <AppText
                variant="meta"
                weight="semibold"
                numberOfLines={1}
                style={{ fontSize: 12, lineHeight: 14, maxWidth: '100%' }}
              >
                {p.name}
              </AppText>
              <AppText
                variant="meta"
                muted
                numeric
                numberOfLines={1}
                style={{ fontSize: 10, lineHeight: 13 }}
              >
                {p.points !== undefined ? `${p.points} pts` : p.detail}
              </AppText>
              {p.autoSub ? <SubChip kind={p.autoSub.kind} projected={p.autoSub.projected} /> : null}
            </>
          );
          const box = {
            alignItems: 'center' as const,
            minHeight: 44,
            gap: 1,
            paddingVertical: 2,
            paddingHorizontal: 2,
            borderRadius: radii.md,
            borderWidth: 2,
            borderColor: selected ? colors.accent : 'transparent',
            backgroundColor: selected ? tint(colors.accent, colors.surface) : 'transparent',
            opacity: p.dimmed ? 0.5 : 1,
          };
          return (
            <View key={id} style={{ flex: 1, minWidth: 0 }}>
              {editable ? (
                <Pressable
                  testID={`lineup-player-${id}`}
                  accessibilityRole="button"
                  accessibilityLabel={`Swap ${p.name}`}
                  accessibilityHint={p.description}
                  accessibilityState={{ selected }}
                  aria-pressed={selected}
                  onPress={() => onPress(id)}
                  style={(s: PressState) => [
                    box,
                    ...pressFeedback(colors, s, { hoverFill: false }),
                  ]}
                >
                  {body}
                </Pressable>
              ) : (
                <Pressable
                  testID={`lineup-player-${id}`}
                  accessibilityRole="button"
                  accessibilityLabel={`Player card for ${p.name}`}
                  accessibilityHint={p.description}
                  onPress={() => onInfo(id)}
                  style={(s: PressState) => [
                    box,
                    ...pressFeedback(colors, s, { hoverFill: false }),
                  ]}
                >
                  {body}
                </Pressable>
              )}
              {editable ? (
                <InfoButton
                  name={p.name}
                  onPress={() => onInfo(id)}
                  size={22}
                  background={colors.surfaceRaised}
                  color={colors.text}
                  testID={`player-info-${id}`}
                  style={{ top: 2, right: 2 }}
                />
              ) : null}
            </View>
          );
        })}
      </View>
    </View>
  );
}

export interface ListViewProps extends LineupViewProps {
  /** Opens the swap sheet for a player (the position pill). */
  onSwap: (playerId: string) => void;
  /** Expected points by player id (ADR 0014); omit to hide them. */
  expected?: ReadonlyMap<string, number | null>;
  /** My tags by player id (an emoji after the name). */
  myTags?: ReadonlyMap<string, TagMeta>;
  /** Players on the trade block (a BLOCK chip). */
  blocked?: ReadonlyMap<string, unknown>;
}

/**
 * The list (the default view): the starters, then "Bench", one roster row
 * per player with no box around them (the "Starters" heading sits in the
 * team header above). While the lineup is editable the position pill is a
 * button that opens the swap sheet; the rest of the row opens the player card.
 */
export function ListView({
  state,
  scores,
  expected,
  editable,
  selectedId,
  onSwap,
  onInfo,
  myTags,
  blocked,
}: ListViewProps) {
  const { spacing } = useTheme();
  const row = (e: Entry, bench?: string) => {
    const id = e.player.id;
    const score = scores?.get(id);
    return (
      <LineupRow
        key={id}
        testID={`lineup-row-${id}`}
        swapTestID={`lineup-swap-${id}`}
        pressTestID={`open-card-${id}`}
        player={e.player}
        fixtures={fixturesOf(e)}
        season={e.season}
        chanceOfPlaying={e.chanceOfPlaying}
        newsIcon
        myTag={myTags?.get(id)}
        onBlock={blocked?.has(id)}
        points={score ? score.points : null}
        expected={expected ? (expected.get(id) ?? null) : undefined}
        autoSub={score?.autoSub}
        bench={bench !== undefined}
        place={bench !== undefined ? `bench ${bench}` : 'starting'}
        selected={selectedId === id}
        onSwap={editable ? () => onSwap(id) : undefined}
        onPress={() => onInfo(id)}
      />
    );
  };
  return (
    <View testID="list-view" style={{ gap: spacing.md }}>
      <View testID="list-starters">
        {linesOf(state.starters)
          .flatMap((l) => l.entries)
          .map((e) => row(e))}
      </View>
      <View>
        <AppText variant="heading" weight="bold" accessibilityRole="header">
          Bench
        </AppText>
        <View testID="list-bench">{state.bench.map((e, i) => row(e, BENCH_LABELS[i]))}</View>
      </View>
    </View>
  );
}
