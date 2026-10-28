import type { Schemas } from '@myfantasyprem/api-client';
import { FlashList } from '@shopify/flash-list';
import { useEffect, useMemo, useState } from 'react';
import {
  FlatList,
  Platform,
  ScrollView,
  TextInput,
  View,
  type StyleProp,
  type ViewStyle,
} from 'react-native';

import { AppText, INPUT_MAX_FONT_SCALE } from '../AppText';
import { Button, IconButton } from '../Button';
import { Chip } from '../Chip';
import { EmptyState } from '../EmptyState';
import { Check, ListPlus, Search } from '../icons';
import { PlayerRow } from '../PlayerRow';
import { usePlayerCard } from '../players/PlayerCardContext';
import { SegmentedControl } from '../SegmentedControl';
import { SkeletonRows } from '../Skeleton';
import { useLeaguePlayers, type PlayersFilter } from '@/api/hooks';
import { useLayout } from '@/hooks/useLayout';
import { describeError } from '@/lib/errors';
import { hasRoomFor, POSITIONS } from '@/lib/draft';
import { useTheme } from '@/theme';
import { flattenUnique } from '@/lib/pages';

type SortKey = 'draft_rank' | 'total_points' | 'form';

/**
 * The available-players list and its filters: position, name search
 * (debounced), sort. Players already on the board are hidden at once,
 * before the list refetches. Fetches only while `enabled`.
 */
export function useDraftPlayers(leagueId: string, draftedIds: ReadonlySet<string>, enabled = true) {
  const [position, setPosition] = useState<Schemas['Position'] | undefined>();
  const [text, setText] = useState('');
  const [q, setQ] = useState('');
  const [sort, setSort] = useState<SortKey>('draft_rank');

  useEffect(() => {
    const id = setTimeout(() => setQ(text.trim()), 250);
    return () => clearTimeout(id);
  }, [text]);

  const filter: PlayersFilter = {
    status: 'available',
    sort,
    limit: 40,
    ...(position ? { position } : {}),
    ...(q ? { q } : {}),
  };
  const players = useLeaguePlayers(leagueId, filter, { enabled });
  const rows = useMemo(
    () =>
      flattenUnique(players.data?.pages, (p) => p.player.id).filter(
        (item) => !draftedIds.has(item.player.id),
      ),
    [players.data, draftedIds],
  );
  const loadMore = () => {
    if (players.hasNextPage && !players.isFetchingNextPage) void players.fetchNextPage();
  };
  const clear = () => {
    setPosition(undefined);
    setText('');
  };
  return { position, setPosition, text, setText, sort, setSort, players, rows, loadMore, clear };
}

export type DraftPlayersState = ReturnType<typeof useDraftPlayers>;

interface RowProps {
  queuedIds: ReadonlySet<string>;
  myCounts: Record<'GKP' | 'DEF' | 'MID' | 'FWD', number> | null;
  canPick: boolean;
  pickingId: string | null;
  onPick: (player: Schemas['PlayerRef']) => void;
  onToggleQueue: (item: Schemas['PlayerListItem']) => void;
}

/**
 * Search, position chips and sort, as a solid block: rows never scroll
 * underneath or show through between the controls.
 */
export function DraftPlayersControls({
  state,
  style,
}: {
  state: DraftPlayersState;
  style?: StyleProp<ViewStyle>;
}) {
  const { colors, spacing, radii, fontSizes } = useTheme();
  const { position, setPosition, text, setText, sort, setSort } = state;
  return (
    <View
      testID="draft-players-controls"
      style={[
        {
          gap: spacing.sm,
          padding: spacing.md,
          borderBottomWidth: 1,
          borderBottomColor: colors.border,
          backgroundColor: colors.surface,
        },
        style,
      ]}
    >
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.sm,
          minHeight: 44,
          paddingHorizontal: spacing.md,
          borderRadius: radii.md,
          borderWidth: 1,
          borderColor: colors.border,
          backgroundColor: colors.background,
        }}
      >
        <Search size={16} color={colors.textMuted} aria-hidden />
        <TextInput
          maxFontSizeMultiplier={INPUT_MAX_FONT_SCALE}
          value={text}
          onChangeText={setText}
          placeholder="Search players"
          placeholderTextColor={colors.textSubtle}
          accessibilityLabel="Search players"
          testID="draft-search"
          autoCorrect={false}
          style={{
            flex: 1,
            color: colors.text,
            fontSize: fontSizes.md,
            minHeight: 40,
            ...({ outlineStyle: 'none' } as object),
          }}
        />
      </View>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={{ gap: spacing.xs }}
      >
        <Chip
          label="All"
          selected={!position}
          onPress={() => setPosition(undefined)}
          testID="filter-all"
        />
        {POSITIONS.map((p) => (
          <Chip
            key={p}
            label={p === 'GKP' ? 'GK' : p}
            selected={position === p}
            onPress={() => setPosition(position === p ? undefined : p)}
            testID={`filter-${p}`}
            accessibilityLabel={`Only ${p === 'GKP' ? 'goalkeepers' : p}`}
          />
        ))}
      </ScrollView>
      <SegmentedControl
        kind="radio"
        accessibilityLabel="Sort players"
        value={sort}
        onChange={setSort}
        segments={[
          { value: 'draft_rank', label: 'Rank' },
          { value: 'total_points', label: 'Points' },
          { value: 'form', label: 'Form' },
        ]}
      />
    </View>
  );
}

/**
 * The rows, then draft or queue. `scroll`: a virtualised list that scrolls
 * itself (the desktop panel); otherwise plain rows for a scrolling page,
 * which calls `state.loadMore` near its end.
 */
export function DraftPlayersList({
  state,
  scroll,
  queuedIds,
  myCounts,
  canPick,
  pickingId,
  onPick,
  onToggleQueue,
}: RowProps & { state: DraftPlayersState; scroll: boolean }) {
  const { colors, spacing } = useTheme();
  const { width } = useLayout();
  const openCard = usePlayerCard();
  const compact = width < 480;
  const { players, rows, sort } = state;

  if (players.isPending) return <SkeletonRows count={8} />;
  if (players.isError && rows.length === 0) {
    return (
      <EmptyState title="Couldn't load players" body={describeError(players.error)}>
        <Button label="Try again" variant="secondary" onPress={() => void players.refetch()} />
      </EmptyState>
    );
  }

  const empty = (
    <EmptyState
      icon={Search}
      title="No players match"
      body="Try another name or position."
      testID="draft-players-empty"
    >
      <Button label="Clear filters" variant="secondary" onPress={state.clear} />
    </EmptyState>
  );
  const footer = players.isFetchingNextPage ? (
    <SkeletonRows count={2} testID="loading-more" />
  ) : players.hasNextPage ? (
    <View style={{ padding: spacing.md }}>
      <Button label="Load more" variant="ghost" onPress={state.loadMore} />
    </View>
  ) : null;
  const renderRow = (item: Schemas['PlayerListItem'], index: number) => {
    const queued = queuedIds.has(item.player.id);
    const room = myCounts ? hasRoomFor(myCounts, item.player.position) : true;
    const stat =
      sort === 'form'
        ? item.form === null
          ? null
          : Number(item.form.toFixed(1))
        : item.totalPoints;
    return (
      <PlayerRow
        key={item.player.id}
        testID={`draft-player-${item.player.id}`}
        divider={index > 0}
        onOpenCard={openCard ? () => openCard(item.player.id) : undefined}
        player={item.player}
        points={compact ? undefined : stat}
        pointsLabel={sort === 'form' ? 'form' : 'pts'}
        detail={
          compact
            ? `#${item.draftRank} · ${stat ?? '–'}${sort === 'form' ? ' form' : ' pts'}`
            : `Rank ${item.draftRank}`
        }
        dimmed={!room}
        leading={
          compact ? undefined : (
            <AppText variant="meta" subtle numeric style={{ width: 28, textAlign: 'right' }}>
              {item.draftRank}
            </AppText>
          )
        }
        trailing={
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
            <IconButton
              icon={queued ? Check : ListPlus}
              label={
                queued
                  ? `Remove ${item.player.displayName} from queue`
                  : `Queue ${item.player.displayName}`
              }
              onPress={() => onToggleQueue(item)}
              selected={queued}
              color={queued ? colors.accent : colors.textMuted}
              testID={`queue-toggle-${item.player.id}`}
            />
            {canPick ? (
              <Button
                label="Draft"
                size="sm"
                disabled={!room || (pickingId !== null && pickingId !== item.player.id)}
                loading={pickingId === item.player.id}
                onPress={() => onPick(item.player)}
                accessibilityLabel={`Draft ${item.player.displayName}`}
                accessibilityHint={
                  room ? undefined : 'Your squad has no place left at this position'
                }
                testID={`draft-pick-${item.player.id}`}
              />
            ) : null}
          </View>
        }
      />
    );
  };

  if (!scroll) {
    return (
      <View testID="draft-players">
        {rows.length === 0 ? empty : rows.map(renderRow)}
        {footer}
      </View>
    );
  }
  const list = {
    testID: 'draft-players',
    data: rows,
    keyExtractor: (item: (typeof rows)[number]) => item.player.id,
    keyboardShouldPersistTaps: 'handled',
    onEndReachedThreshold: 0.5,
    onEndReached: state.loadMore,
    ListEmptyComponent: empty,
    ListFooterComponent: footer,
    renderItem: ({ item, index }: { item: (typeof rows)[number]; index: number }) =>
      renderRow(item, index),
  } as const;
  // Native: FlashList draws only the rows on screen and reuses them. The web
  // keeps FlatList, whose DOM stays in list order for the Tab key and screen
  // readers (FlashList reuses its cells in place).
  return Platform.OS === 'web' ? (
    <FlatList {...list} style={{ flex: 1 }} />
  ) : (
    <View style={{ flex: 1 }}>
      <FlashList {...list} />
    </View>
  );
}
