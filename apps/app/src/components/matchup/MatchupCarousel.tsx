import type { Schemas } from '@myfantasyprem/api-client';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  Platform,
  Pressable,
  ScrollView,
  View,
  type LayoutChangeEvent,
  type NativeScrollEvent,
  type NativeSyntheticEvent,
} from 'react-native';

import { AppText } from '../AppText';
import { ChevronLeft, ChevronRight, Users } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { ScoreHeader } from './ScoreHeader';
import { useLayout } from '@/hooks/useLayout';
import { orient, sideName, teamOf, type TeamRecord } from '@/lib/matchup';
import { accuracyLabel, averageLabel, seasonLineOf, type SeasonLine } from '@/lib/matchup-carousel';
import { useTheme } from '@/theme';

interface Props {
  leagueId: string;
  /** Every matchup in the gameweek, mine first (lib/matchup-carousel carouselOrder). */
  order: readonly Schemas['MatchupSummary'][];
  /** The page on screen. */
  page: number;
  onPageChange: (page: number) => void;
  /** The open matchup's detail: its page shows the live score and state from it. */
  detail?: Schemas['MatchupDetail'];
  myTeamId?: string;
  records?: ReadonlyMap<string, TeamRecord>;
  seasonLines: ReadonlyMap<string, SeasonLine>;
  /** The badge between the avatars (the gameweek picker); defaults to `GW n`. */
  emblem?: (gameweek: number, live: boolean) => ReactNode;
  /** A future gameweek: no scores, so no points-share bar. */
  preview?: boolean;
}

/**
 * Sleeper's matchup header: the gameweek's matchups as a row of score
 * headers, one a page, swiped sideways (scroll snapping on web), with a
 * dot a page under it (an avatar pair for mine, the first). Desktop adds
 * arrows; on web the left and right keys step it once it has focus. Each
 * card ends with both sides' season start/sit accuracy and average points
 * for and against a gameweek.
 */
export function MatchupCarousel({
  leagueId,
  order,
  page,
  onPageChange,
  detail,
  myTeamId,
  records,
  seasonLines,
  emblem,
  preview = false,
}: Props) {
  const { spacing } = useTheme();
  const { isDesktop } = useLayout();
  const scroller = useRef<ScrollView>(null);
  const [width, setWidth] = useState(0);
  // The page the scroll position shows; a page set from outside scrolls to it.
  const shown = useRef(page);
  const settle = useRef<ReturnType<typeof setTimeout> | null>(null);
  const count = order.length;
  const mineFirst =
    myTeamId !== undefined &&
    count > 0 &&
    [order[0].home, order[0].away].some((s) => teamOf(s)?.id === myTeamId);

  useEffect(() => {
    if (width === 0) return;
    if (shown.current === page) {
      // A new width (rotation, resize) keeps the page.
      scroller.current?.scrollTo({ x: page * width, animated: false });
      return;
    }
    shown.current = page;
    scroller.current?.scrollTo({ x: page * width, animated: true });
  }, [page, width]);
  useEffect(() => () => void (settle.current && clearTimeout(settle.current)), []);

  const land = (x: number) => {
    if (width === 0) return;
    const next = Math.min(Math.max(Math.round(x / width), 0), count - 1);
    if (next === shown.current) return;
    shown.current = next;
    onPageChange(next);
  };
  // Native reports the end of a fling; web only scrolls, so wait for it to stop.
  const onScroll = (e: NativeSyntheticEvent<NativeScrollEvent>) => {
    if (Platform.OS !== 'web') return;
    const x = e.nativeEvent.contentOffset.x;
    if (settle.current) clearTimeout(settle.current);
    settle.current = setTimeout(() => land(x), 120);
  };
  const go = (next: number) => {
    if (next < 0 || next >= count || next === page) return;
    onPageChange(next);
  };
  const keys =
    Platform.OS === 'web'
      ? {
          focusable: true,
          onKeyDown: (e: { key: string; preventDefault: () => void }) => {
            if (e.key === 'ArrowLeft') {
              e.preventDefault();
              go(page - 1);
            } else if (e.key === 'ArrowRight') {
              e.preventDefault();
              go(page + 1);
            }
          },
        }
      : {};

  const label = (m: Schemas['MatchupSummary'], i: number) => {
    const { left, right } = orient(m, myTeamId);
    return `Matchup ${i + 1} of ${count}: ${sideName(left)} versus ${sideName(right)}${
      i === 0 && mineFirst ? ', your matchup' : ''
    }`;
  };

  return (
    <View style={{ gap: spacing.sm }} testID="matchup-carousel">
      <View
        onLayout={(e: LayoutChangeEvent) => setWidth(Math.round(e.nativeEvent.layout.width))}
        accessibilityLabel={`Matchups, ${page + 1} of ${count}. Left and right arrow keys switch matchup`}
        {...keys}
      >
        <ScrollView
          ref={scroller}
          testID="matchup-carousel-scroller"
          horizontal
          pagingEnabled
          showsHorizontalScrollIndicator={false}
          scrollEventThrottle={32}
          onScroll={onScroll}
          onMomentumScrollEnd={(e) => land(e.nativeEvent.contentOffset.x)}
          contentOffset={{ x: page * width, y: 0 }}
        >
          {order.map((m, i) => {
            const source = detail && detail.id === m.id ? detail : m;
            const { left, right } = orient<Schemas['MatchupDetailSide'] | Schemas['MatchupSide']>(
              source,
              myTeamId,
            );
            const current = i === page;
            return (
              <View
                key={m.id}
                testID={`carousel-page-${i}`}
                role="group"
                aria-label={label(m, i)}
                aria-current={current ? 'true' : undefined}
                style={{ width: width || undefined }}
              >
                <ScoreHeader
                  leagueId={leagueId}
                  left={left}
                  right={right}
                  state={source.state}
                  gameweek={m.gameweek}
                  records={records}
                  toPlay={false}
                  peek={!current}
                  testID={current ? 'score-header' : `peek-score-header-${i}`}
                  emblem={emblem?.(m.gameweek, source.state === 'live')}
                  shareBar={!preview}
                  footer={<SeasonStrip left={left} right={right} lines={seasonLines} />}
                />
              </View>
            );
          })}
        </ScrollView>
      </View>
      {count > 1 ? (
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: 'center',
            gap: spacing.sm,
          }}
        >
          {isDesktop ? (
            <Arrow dir="prev" disabled={page === 0} onPress={() => go(page - 1)} />
          ) : null}
          <View
            style={{ flexDirection: 'row', alignItems: 'center', gap: 2 }}
            testID="carousel-dots"
          >
            {order.map((m, i) => (
              <Dot
                key={m.id}
                index={i}
                active={i === page}
                mine={i === 0 && mineFirst}
                label={label(m, i)}
                onPress={() => go(i)}
              />
            ))}
          </View>
          {isDesktop ? (
            <Arrow dir="next" disabled={page === count - 1} onPress={() => go(page + 1)} />
          ) : null}
        </View>
      ) : null}
    </View>
  );
}

function Dot({
  index,
  active,
  mine,
  label,
  onPress,
}: {
  index: number;
  active: boolean;
  mine: boolean;
  label: string;
  onPress: () => void;
}) {
  const { colors } = useTheme();
  const tint = active ? colors.accent : colors.textSubtle;
  return (
    <Pressable
      testID={`carousel-dot-${index}`}
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ selected: active }}
      aria-current={active ? 'true' : undefined}
      hitSlop={6}
      onPress={onPress}
      style={(s: PressState) => [
        {
          minWidth: 20,
          height: 20,
          alignItems: 'center',
          justifyContent: 'center',
          borderRadius: 10,
        },
        ...pressFeedback(colors, s),
      ]}
    >
      {mine ? (
        <Users size={14} color={tint} strokeWidth={active ? 2.5 : 2} />
      ) : (
        <View
          aria-hidden
          style={{
            width: active ? 8 : 6,
            height: active ? 8 : 6,
            borderRadius: 4,
            backgroundColor: tint,
          }}
        />
      )}
    </Pressable>
  );
}

function Arrow({
  dir,
  disabled,
  onPress,
}: {
  dir: 'prev' | 'next';
  disabled: boolean;
  onPress: () => void;
}) {
  const { colors } = useTheme();
  const Icon = dir === 'prev' ? ChevronLeft : ChevronRight;
  return (
    <Pressable
      testID={`carousel-${dir}`}
      accessibilityRole="button"
      accessibilityLabel={dir === 'prev' ? 'Previous matchup' : 'Next matchup'}
      accessibilityState={{ disabled }}
      disabled={disabled}
      onPress={onPress}
      style={(s: PressState) => [
        {
          width: 28,
          height: 28,
          borderRadius: 14,
          alignItems: 'center',
          justifyContent: 'center',
          opacity: disabled ? 0.35 : 1,
        },
        ...pressFeedback(colors, s),
      ]}
    >
      <Icon size={18} color={colors.textMuted} />
    </Pressable>
  );
}

/** Sleeper's bottom strip: start/sit accuracy and average points for and against, each side's value either side of the label. */
function SeasonStrip({
  left,
  right,
  lines,
}: {
  left: Schemas['MatchupDetailSide'] | Schemas['MatchupSide'];
  right: Schemas['MatchupDetailSide'] | Schemas['MatchupSide'];
  lines: ReadonlyMap<string, SeasonLine>;
}) {
  const { colors, spacing } = useTheme();
  const l = seasonLineOf(left, lines);
  const r = seasonLineOf(right, lines);
  const row = (name: string, lv: string, rv: string, id: string) => (
    <View
      style={{ flexDirection: 'row', alignItems: 'center' }}
      accessibilityLabel={`${name}: ${sideName(left)} ${lv}, ${sideName(right)} ${rv}`}
      testID={id}
    >
      <AppText variant="meta" weight="bold" numeric style={{ flex: 1, textAlign: 'left' }}>
        {lv}
      </AppText>
      <AppText variant="eyebrow" muted style={{ textAlign: 'center' }}>
        {name}
      </AppText>
      <AppText variant="meta" weight="bold" numeric style={{ flex: 1, textAlign: 'right' }}>
        {rv}
      </AppText>
    </View>
  );
  return (
    <View
      style={{
        borderTopWidth: 1,
        borderTopColor: colors.border,
        paddingHorizontal: spacing.md,
        paddingVertical: spacing.sm,
        gap: spacing.xs,
      }}
    >
      {row(
        'Start/sit',
        left.kind === 'team' ? accuracyLabel(l?.accuracy) : '–',
        right.kind === 'team' ? accuracyLabel(r?.accuracy) : '–',
        'season-start-sit',
      )}
      {row(
        'Avg pts',
        left.kind === 'team' ? averageLabel(l?.average) : '–',
        right.kind === 'team' ? averageLabel(r?.average) : '–',
        'season-avg',
      )}
      {row(
        'Avg pts against',
        left.kind === 'team' ? averageLabel(l?.against) : '–',
        right.kind === 'team' ? averageLabel(r?.against) : '–',
        'season-avg-against',
      )}
    </View>
  );
}
