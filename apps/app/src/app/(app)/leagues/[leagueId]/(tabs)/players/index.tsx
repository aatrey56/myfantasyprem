import type { Schemas } from '@myfantasyprem/api-client';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useEffect, useMemo, useState } from 'react';
import { Pressable, ScrollView, View } from 'react-native';

import { useFreshness, useNow } from '@/api/freshness';
import {
  PLAYERS_FIRST_FILTER,
  type PlayersFilter,
  useClubs,
  useLeagueContext,
  useLeaguePlayers,
} from '@/api/hooks';
import { useWaiverClaims, useWaiverState } from '@/api/moves-hooks';
import { useTagByPlayer } from '@/api/tag-hooks';
import { useBlockByPlayer } from '@/api/trade-block-hooks';
import { TAGS, tagMeta, type PlayerTag } from '@/lib/tags';
import { AppText } from '@/components/AppText';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import {
  Check,
  ChevronDown,
  Star,
  TrendingDown,
  TrendingUp,
  Users,
  type LucideIcon,
} from '@/components/icons';
import { ListRow } from '@/components/ListRow';
import { usePlayerCard } from '@/components/players/PlayerCardContext';
import { parseView, PlayersNav, type PlayersView } from '@/components/players/PlayersNav';
import {
  formChangeText,
  PlayerRow,
  PlayerStatStrip,
  positionStats,
  RowAction,
  waiverDay,
  type ListSort,
} from '@/components/PlayerRow';
import { pressFeedback, type PressState } from '@/components/pressable';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SortSheet } from '@/components/players/SortSheet';
import { ViewSheet } from '@/components/players/ViewSheet';
import { SkeletonRows } from '@/components/Skeleton';
import { TextField } from '@/components/TextField';
import { useLayout } from '@/hooks/useLayout';
import { useLeagueId } from '@/hooks/useLeagueId';
import { usePlayerSort } from '@/hooks/usePlayerSort';
import { usePlayerView } from '@/hooks/usePlayerView';
import {
  formatSortValue,
  sortChipA11y,
  sortChipText,
  sortShort,
  sortValueA11y,
} from '@/lib/playerSort';
import { expandRowFixtures } from '@/lib/fixtures';
import { flattenUnique } from '@/lib/pages';
import { ALL_VIEW, knownView, viewButtonText, viewWhere } from '@/lib/playerView';
import { formatDateTime } from '@/lib/time';
import { waiversDueLabel } from '@/lib/waivers';
import { tint, useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Debounces a value (search as you type without a request per key). */
function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms);
    return () => clearTimeout(id);
  }, [value, ms]);
  return v;
}

/**
 * Players tab (SLEEPER-OBSERVED): the nav card under the top tabs, then one
 * of four views. Available (and Search) list every player with filter
 * chips, a sort and an action per row: + adds a free agent, the W badge
 * claims on waivers, ⇄ opens a trade with the owner. Trend lists 2-GW form
 * risers or fallers; Leaders the top scorers for the season or gameweek.
 */
export default function PlayersScreen() {
  const leagueId = useLeagueId();
  const router = useRouter();
  const params = useLocalSearchParams<{ view?: string }>();
  const { spacing, colors } = useTheme();
  // The chip row scrolls edge to edge: it bleeds through the screen gutter.
  const bleed = useLayout().isDesktop ? spacing.xl : spacing.lg;
  const home = useLeagueContext(leagueId);
  const myTeamId = home.data?.data.viewer.team?.id;
  const inSeason =
    home.data?.data.league.status === 'in_season' || home.data?.data.league.status === 'complete';
  const [view, setView] = useState<PlayersView>(parseView(params.view));
  // A link with ?view= (the trades screen's nav card) switches the view.
  const [linkedView, setLinkedView] = useState(params.view);
  if (params.view !== linkedView) {
    setLinkedView(params.view);
    if (params.view) setView(parseView(params.view));
  }
  const [text, setText] = useState('');
  const q = useDebounced(text.trim(), 250);
  const [watchlist, setWatchlist] = useState(false);
  const [tag, setTag] = useState<PlayerTag | null>(null);
  const tagShown = tagMeta(tag);
  const myTags = useTagByPlayer(leagueId);
  const blocked = useBlockByPlayer(leagueId);
  const [viewOpen, setViewOpen] = useState(false);
  const [sortOpen, setSortOpen] = useState(false);
  const [{ sort }, setSortChoice, sortReady] = usePlayerSort(leagueId);
  const [viewSettings, setViewSettings, viewReady] = usePlayerView(leagueId);
  const clubsQuery = useClubs();
  const clubs = clubsQuery.data?.data;
  // A stored club the club list no longer has is All players; so is any
  // club once that list fails (no wait for its retries). Until then a club waits.
  const scope =
    viewSettings.view.kind === 'club' && !clubs && clubsQuery.failureCount > 0
      ? ALL_VIEW
      : knownView(viewSettings.view, clubs);
  const scopeReady = scope.kind !== 'club' || clubs !== undefined;
  const { available } = viewSettings;
  const [rising, setRising] = useState(true);
  const [season, setSeason] = useState(true);
  const openCard = usePlayerCard();
  const browsing = view === 'available' || view === 'search';
  const filter = useMemo<PlayersFilter>(() => {
    const where = viewWhere(scope);
    if (view === 'trend') {
      return {
        status: 'all',
        sort: rising ? 'form_rise' : 'form_fall',
        limit: PLAYERS_FIRST_FILTER.limit,
        ...where,
      };
    }
    if (view === 'leaders') {
      return {
        status: 'all',
        sort: season ? 'total_points' : 'gameweek_points',
        limit: PLAYERS_FIRST_FILTER.limit,
        ...where,
      };
    }
    return {
      // Available: only players that can be signed or claimed (unowned).
      // Search looks through everyone: the toggle is for browsing.
      status: view === 'available' && available ? 'available' : 'all',
      sort,
      // desc is the API's default: left out, the first list is the query
      // the league prefetches (PLAYERS_FIRST_FILTER).
      limit: PLAYERS_FIRST_FILTER.limit,
      ...where,
      ...(watchlist ? { watchlist: true } : {}),
      ...(tag ? { tag } : {}),
      ...(view === 'search' && q ? { q } : {}),
    };
  }, [view, available, sort, scope, q, watchlist, tag, rising, season]);
  // Held until the stored sort and view are read (and a club checked): no wasted first request.
  const players = useLeaguePlayers(leagueId, filter, {
    enabled: sortReady && viewReady && scopeReady,
  });
  // While a new sort loads the rows still show the last list (placeholder
  // data): they keep that list's sort for their label and value.
  const [shownSort, setShownSort] = useState(sort);
  if (!players.isPlaceholderData && players.data && shownSort !== sort) setShownSort(sort);
  const claims = useWaiverClaims(leagueId);
  const waiverState = useWaiverState(leagueId);
  const first = players.data?.pages[0];
  const freshness = useFreshness({ ...players, data: first });
  const items = flattenUnique(players.data?.pages, (p) => p.player.id);
  const filtered =
    (view === 'available' && !available) ||
    (browsing && (watchlist || tag !== null || (view === 'search' && q !== ''))) ||
    scope.kind !== 'all';
  const pending = claims.data?.data.length ?? 0;
  const now = useNow(60_000);
  const nextRunAt = waiverState.data?.data.nextRunAt ?? null;
  const actionOf = (p: Schemas['PlayerListItem']): 'add' | 'claim' | 'trade' | null => {
    if (!inSeason || !myTeamId) return null;
    if (p.rosterStatus === 'free_agent') return 'add';
    if (p.rosterStatus === 'waivers') return 'claim';
    if (p.rosterStatus === 'rostered' && p.owner && p.owner.id !== myTeamId) return 'trade';
    return null;
  };
  // The action column only takes room when some row has an action.
  const actionWidth = items.some((p) => actionOf(p) !== null) ? 52 : spacing.sm;

  const loadMore = () => {
    if (players.hasNextPage && !players.isFetchingNextPage) void players.fetchNextPage();
  };
  const clear = () => {
    setText('');
    setWatchlist(false);
    setTag(null);
    setViewSettings({ view: ALL_VIEW, available: true });
  };
  const sortChip = sortChipText(sort);
  const title =
    view === 'trend'
      ? 'Trending'
      : view === 'leaders'
        ? 'Leaders'
        : watchlist
          ? 'Watchlist'
          : tagShown
            ? `${tagShown.emoji} ${tagShown.label}`
            : view === 'search'
              ? 'Search'
              : available
                ? 'Available'
                : 'All players';
  const box = (p: Schemas['PlayerListItem']) =>
    view === 'trend'
      ? {
          points: p.formChange,
          label: 'Form ±',
          text: formChangeText(p.formChange),
          a11y:
            p.formChange === null
              ? 'Form change, no value'
              : `Form change ${formChangeText(p.formChange)}`,
        }
      : view === 'leaders'
        ? season
          ? { points: p.totalPoints, label: 'PTS' }
          : { points: p.gameweekPoints, label: 'GW' }
        : shownSort === 'total_points'
          ? { points: p.totalPoints, label: 'PTS' }
          : {
              points: p.sortValue,
              label: sortShort(shownSort),
              text: formatSortValue(shownSort, p.sortValue),
              a11y: sortValueA11y(shownSort, p.sortValue),
            };
  const stripSort: ListSort | undefined =
    view === 'leaders'
      ? season
        ? 'total_points'
        : 'gameweek_points'
      : browsing &&
          (shownSort === 'total_points' || shownSort === 'form' || shownSort === 'gameweek_points')
        ? shownSort
        : undefined;

  const renderRow = (p: Schemas['PlayerListItem'], i: number) => {
    const mine = p.owner?.id === myTeamId;
    const detail =
      p.rosterStatus === 'rostered'
        ? mine
          ? 'Your team'
          : (p.owner?.name ?? 'Owned')
        : p.rosterStatus === 'locked'
          ? waiversDueLabel(p.waiverClearsAt, now)
          : undefined;
    const action = actionOf(p);
    const b = box(p);
    return (
      <View
        key={p.player.id}
        style={{ borderTopWidth: i > 0 ? 1 : 0, borderTopColor: colors.border }}
      >
        {/* The action sits beside the row, not inside it: no nested buttons. */}
        <View style={{ flexDirection: 'row', alignItems: 'center' }}>
          <View style={{ width: actionWidth, alignItems: 'flex-end' }}>
            {action === 'trade' ? (
              <RowAction
                kind="trade"
                name={p.player.displayName}
                onPress={() =>
                  router.push(
                    `/leagues/${leagueId}/players/trades/new?teamId=${p.owner!.id}&playerId=${p.player.id}`,
                  )
                }
                testID={`trade-for-${p.player.id}`}
              />
            ) : action ? (
              <RowAction
                kind={action}
                day={waiverDay(nextRunAt ?? p.waiverClearsAt, now)}
                name={p.player.displayName}
                onPress={() => openCard?.(p.player.id, { drop: true })}
                testID={`move-${p.player.id}`}
              />
            ) : null}
          </View>
          <PlayerRow
            testID={`player-${p.player.id}`}
            player={p.player}
            avatar
            rank={i + 1}
            fixtures={expandRowFixtures(p.nextFixtures, clubs)}
            points={b.points}
            pointsText={b.text}
            pointsLabel={b.label}
            pointsA11y={'a11y' in b ? b.a11y : undefined}
            detail={detail}
            myTag={myTags.get(p.player.id)}
            onBlock={blocked.has(p.player.id)}
            onOpenCard={openCard ? () => openCard(p.player.id) : undefined}
            style={{ flex: 1, minWidth: 0, paddingLeft: spacing.sm }}
          />
        </View>
        <PlayerStatStrip
          stats={positionStats(p, stripSort)}
          testID={`player-stats-${p.player.id}`}
        />
      </View>
    );
  };

  return (
    <Screen
      title="Players"
      hideHeader
      freshness={freshness}
      onEndReached={loadMore}
      // Long: drawn as you scroll on native (FlashList).
      list={{
        data: first ? items : [],
        keyExtractor: (p) => p.player.id,
        renderItem: renderRow,
        card: true,
        testID: 'player-list',
        footer: players.hasNextPage ? (
          <Button
            label="Show more players"
            variant="secondary"
            onPress={loadMore}
            loading={players.isFetchingNextPage}
            testID="players-more"
          />
        ) : items.length > 0 ? (
          <AppText variant="meta" subtle style={{ textAlign: 'center' }}>
            {`${items.length} player${items.length === 1 ? '' : 's'}`}
          </AppText>
        ) : null,
      }}
    >
      <PlayersNav
        leagueId={leagueId}
        active={view}
        onSelect={setView}
        onTrades={() => router.push(`/leagues/${leagueId}/players/trades`)}
      />
      {view === 'search' ? (
        <TextField
          label="Search players"
          value={text}
          onChangeText={setText}
          placeholder="Name"
          autoCorrect={false}
          autoCapitalize="none"
          autoFocus
          returnKeyType="search"
          testID="player-search"
        />
      ) : null}
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText
          variant="heading"
          weight="bold"
          numberOfLines={1}
          style={{ flexShrink: 1 }}
          testID="players-scope-title"
        >
          {title}
        </AppText>
        {view === 'trend' ? (
          <TwoWay
            label="Trend direction"
            value={rising}
            onChange={setRising}
            on={{ icon: TrendingUp, label: 'Rising', testID: 'trend-up' }}
            off={{ icon: TrendingDown, label: 'Falling', testID: 'trend-down' }}
          />
        ) : view === 'leaders' ? (
          <TwoWay
            label="Leaders scope"
            value={season}
            onChange={setSeason}
            on={{ text: 'Season', label: 'Season', testID: 'leaders-season' }}
            off={{ text: 'GW', label: 'This gameweek', testID: 'leaders-gw' }}
          />
        ) : null}
        <View style={{ flex: 1 }} />
        {browsing ? (
          <Pressable
            accessibilityRole="button"
            accessibilityLabel={`${sortChipA11y(sort)}. Change`}
            onPress={() => setSortOpen(true)}
            hitSlop={8}
            testID="sort-menu"
            style={(state: PressState) => [
              {
                flexDirection: 'row',
                alignItems: 'center',
                gap: 4,
                minHeight: 32,
                paddingHorizontal: spacing.xs,
                borderRadius: 8,
              },
              ...pressFeedback(colors, state, { hoverFill: false }),
            ]}
          >
            <AppText variant="eyebrow" color={colors.accentBright}>
              {sortChip}
            </AppText>
            <ChevronDown size={14} color={colors.accentBright} aria-hidden />
          </Pressable>
        ) : null}
      </View>
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        testID="player-filters"
        style={{ flexGrow: 0, marginHorizontal: -bleed }}
        contentContainerStyle={{
          paddingHorizontal: bleed,
          gap: spacing.sm,
          alignItems: 'center',
          // Room for the chips' keyboard focus ring.
          paddingVertical: 4,
        }}
      >
        <FilterChip
          label={viewButtonText(scope, clubs)}
          trailingIcon={ChevronDown}
          selected={scope.kind !== 'all'}
          onPress={() => setViewOpen(true)}
          accessibilityLabel={`${viewButtonText(scope, clubs)}. Change`}
          testID="view-menu"
        />
        {browsing ? (
          <>
            {view === 'available' ? (
              <>
                <FilterChip
                  icon={available ? Check : undefined}
                  label="Available"
                  accessibilityLabel="Available players only"
                  selected={available}
                  onPress={() => setViewSettings({ ...viewSettings, available: !available })}
                  testID="filter-available"
                />
                <View
                  aria-hidden
                  style={{
                    width: 1,
                    height: 20,
                    backgroundColor: colors.border,
                    marginHorizontal: 2,
                  }}
                />
              </>
            ) : null}
            <FilterChip
              icon={Star}
              iconFilled={watchlist}
              accessibilityLabel="Watchlist"
              selected={watchlist}
              onPress={() => setWatchlist(!watchlist)}
              testID="filter-watchlist"
            />
            <View
              style={{ flexDirection: 'row', gap: spacing.sm }}
              accessibilityLabel="My tags"
              role="group"
            >
              {TAGS.map((t) => (
                <FilterChip
                  key={t.tag}
                  label={t.emoji}
                  accessibilityLabel={`Tagged ${t.label}`}
                  selected={tag === t.tag}
                  onPress={() => setTag(tag === t.tag ? null : t.tag)}
                  testID={`filter-tag-${t.tag}`}
                />
              ))}
            </View>
          </>
        ) : null}
      </ScrollView>
      {view === 'available' && inSeason ? (
        <Card flush>
          <ListRow
            title="Waiver claims"
            subtitle={`${pending === 0 ? 'None pending' : `${pending} pending`}${
              nextRunAt ? ` · next run ${formatDateTime(Date.parse(nextRunAt), now)}` : ''
            }`}
            chevron
            onPress={() => router.push(`/leagues/${leagueId}/players/waivers`)}
            accessibilityLabel={`Waiver claims, ${pending} pending`}
            testID="open-waivers"
          />
        </Card>
      ) : null}
      <QueryStatus
        query={{ ...players, data: first }}
        loading={
          <Card flush>
            <SkeletonRows count={8} />
          </Card>
        }
      >
        {() =>
          items.length === 0 ? (
            <Card>
              <EmptyState
                icon={watchlist ? Star : Users}
                title={
                  watchlist
                    ? 'Your watchlist is empty'
                    : tagShown
                      ? `No players tagged ${tagShown.label}`
                      : 'No players match'
                }
                body={
                  watchlist
                    ? 'Star a player on their card to keep an eye on them here.'
                    : tagShown
                      ? 'Tag a player on his card; only you see your tags.'
                      : 'Try another name or view.'
                }
                testID="players-empty"
              >
                {filtered ? (
                  <Button label="Clear filters" variant="secondary" onPress={clear} />
                ) : null}
              </EmptyState>
            </Card>
          ) : null
        }
      </QueryStatus>
      <ViewSheet
        visible={viewOpen}
        view={scope}
        onClose={() => setViewOpen(false)}
        onPick={(next) => setViewSettings({ ...viewSettings, view: next })}
      />
      <SortSheet
        visible={sortOpen}
        sort={sort}
        onClose={() => setSortOpen(false)}
        onPick={(next) => setSortChoice({ sort: next })}
      />
    </Screen>
  );
}

interface TwoWayOption {
  icon?: LucideIcon;
  text?: string;
  /** Spoken name of the option. */
  label: string;
  testID: string;
}

/**
 * A two-state pill toggle beside a section title (Sleeper's trend switch):
 * the chosen half is a filled accent circle or pill.
 */
function TwoWay({
  label,
  value,
  onChange,
  on,
  off,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
  on: TwoWayOption;
  off: TwoWayOption;
}) {
  const { colors, radii, spacing } = useTheme();
  return (
    <View
      role="radiogroup"
      accessibilityLabel={label}
      style={{
        flexDirection: 'row',
        padding: 2,
        borderRadius: radii.pill,
        borderWidth: 1.5,
        borderColor: colors.accent,
        backgroundColor: tint(colors.accent, colors.background),
      }}
    >
      {[
        { opt: on, v: true },
        { opt: off, v: false },
      ].map(({ opt, v }) => {
        const selected = value === v;
        const fg = selected ? colors.onAccent : colors.accentBright;
        const Icon = opt.icon;
        return (
          <Pressable
            key={opt.testID}
            role="radio"
            accessibilityLabel={opt.label}
            accessibilityState={{ checked: selected }}
            aria-checked={selected}
            onPress={() => onChange(v)}
            hitSlop={4}
            testID={opt.testID}
            style={{
              minWidth: 28,
              height: 26,
              paddingHorizontal: opt.text ? spacing.sm : 0,
              borderRadius: radii.pill,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: selected ? colors.accent : 'transparent',
            }}
          >
            {Icon ? (
              <Icon size={15} color={fg} strokeWidth={2.5} aria-hidden />
            ) : (
              <AppText weight="bold" color={fg} style={{ fontSize: 12, lineHeight: 16 }}>
                {opt.text}
              </AppText>
            )}
          </Pressable>
        );
      })}
    </View>
  );
}

/**
 * A 32px filter pill (hit slop to 44): selected is an accent outline over a
 * 16% accent fill with bright text; unselected is a raised fill with muted
 * text. An icon-only chip is round.
 */
function FilterChip({
  label,
  icon: Icon,
  iconFilled,
  selected,
  onPress,
  trailingIcon: Trailing,
  accessibilityLabel,
  testID,
}: {
  label?: string;
  icon?: LucideIcon;
  iconFilled?: boolean;
  selected?: boolean;
  onPress: () => void;
  trailingIcon?: LucideIcon;
  accessibilityLabel?: string;
  testID?: string;
}) {
  const { colors, spacing, radii } = useTheme();
  const fg = selected ? colors.accentBright : colors.textMuted;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel}
      accessibilityState={{ selected: Boolean(selected) }}
      aria-pressed={Boolean(selected)}
      onPress={onPress}
      testID={testID}
      hitSlop={6}
      style={(state: PressState) => [
        {
          minHeight: 32,
          minWidth: 32,
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 4,
          paddingHorizontal: label ? spacing.md : spacing.sm,
          borderRadius: radii.pill,
          borderWidth: 1.5,
          borderColor: selected ? colors.accent : colors.surfaceRaised,
          backgroundColor: selected ? tint(colors.accent, colors.background) : colors.surfaceRaised,
        },
        ...pressFeedback(colors, state, { hoverFill: !selected }),
      ]}
    >
      {Icon ? (
        <Icon size={15} color={fg} fill={iconFilled ? fg : 'none'} strokeWidth={2.2} aria-hidden />
      ) : null}
      {label ? (
        <AppText variant="meta" weight="semibold" color={fg} numberOfLines={1}>
          {label}
        </AppText>
      ) : null}
      {Trailing ? <Trailing size={14} color={fg} aria-hidden /> : null}
    </Pressable>
  );
}
