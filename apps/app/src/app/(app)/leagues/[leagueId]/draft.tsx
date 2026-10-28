import type { Schemas } from '@myfantasyprem/api-client';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { ScrollView, View } from 'react-native';

import {
  useDraft,
  useDraftControl,
  useDraftQueue,
  useMakePick,
  useSetAutopick,
  useSetDraftQueue,
} from '@/api/draft-hooks';
import { useFreshness } from '@/api/freshness';
import { useLeagueHome } from '@/api/hooks';
import { AppText } from '@/components/AppText';
import { Button, IconButton } from '@/components/Button';
import { Card } from '@/components/Card';
import { Pill } from '@/components/Chip';
import { DraftBoard } from '@/components/draft/DraftBoard';
import { DraftClock, PickTicker } from '@/components/draft/DraftClock';
import {
  DraftPlayersControls,
  DraftPlayersList,
  useDraftPlayers,
} from '@/components/draft/DraftPlayers';
import { DraftQueue, DraftRoster, DraftTeamChips } from '@/components/draft/DraftQueue';
import { MissedClockBanner } from '@/components/draft/MissedClockBanner';
import { ChevronLeft, ListOrdered, Pause, Play, Shirt, Users } from '@/components/icons';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SegmentedControl } from '@/components/SegmentedControl';
import { Skeleton } from '@/components/Skeleton';
import { useToast } from '@/components/Toast';
import { useLayout } from '@/hooks/useLayout';
import {
  positionCounts,
  queueAdd,
  queueMove,
  queueRemove,
  missedClockNotice,
  queueWithoutDrafted,
  startBlocker,
  startExplanation,
  type QueueItem,
} from '@/lib/draft';
import { describeError, problemErrors } from '@/lib/errors';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

type Pane = 'board' | 'players' | 'queue' | 'roster';

/**
 * The live snake draft. Once it is complete the same screen is the read-only
 * results: the board and each team's picks, with no players list, queue or
 * pick actions (and no queue requests). Phone: one scrolling page. The header, ticker and
 * draft card scroll away; the Board / Players / Queue / Roster tabs stick
 * to the top with the clock (while the draft runs) and the players
 * filters, and the tab's content scrolls with the page. Desktop: the board
 * fills the left, the players, queue and roster sit in a right-hand
 * column, each scrolling in its panel. Polls the draft every few seconds
 * while the clock runs (no socket in the mocks).
 */
export default function DraftRoomScreen() {
  const { leagueId } = useLocalSearchParams<{ leagueId: string }>();
  const router = useRouter();
  const home = useLeagueHome(leagueId);
  const draft = useDraft(leagueId);
  const freshness = useFreshness(draft);
  const { isDesktop } = useLayout();
  const { spacing } = useTheme();
  // The phone room scrolls edge to edge, so the screen frame adds no gutters:
  // pad the loading and error states here instead.
  const pad = isDesktop || (draft.data && home.data) ? undefined : { padding: spacing.lg };

  return (
    <Screen
      title={draft.data?.data.status === 'complete' ? 'Draft results' : 'Draft room'}
      documentTitle={
        home.data
          ? `${draft.data?.data.status === 'complete' ? 'Draft results' : 'Draft'} · ${home.data.data.league.name}`
          : 'Draft room'
      }
      hideHeader
      scroll={false}
      bleed={!isDesktop}
      maxWidth={1440}
      freshness={freshness}
    >
      <View style={[{ flex: 1, minHeight: 0 }, pad]}>
        <QueryStatus
          query={draft}
          loading={
            <View style={{ gap: 12 }} testID="loading">
              <Skeleton height={72} radius={16} />
              <Skeleton height={320} radius={16} />
            </View>
          }
        >
          {({ data, meta }) =>
            home.data ? (
              <Room
                leagueId={leagueId}
                home={home.data.data}
                draft={data}
                offsetMs={Date.parse(meta.serverTime) - draft.dataUpdatedAt}
                wide={isDesktop}
              />
            ) : home.isError ? (
              // The room needs the league too: say so, with retry and a way out.
              <View style={{ gap: 12 }}>
                <IconButton
                  icon={ChevronLeft}
                  label="Back to league"
                  onPress={() => router.push(`/leagues/${leagueId}`)}
                  testID="draft-back"
                />
                <QueryStatus query={home}>{() => null}</QueryStatus>
              </View>
            ) : (
              <Skeleton height={72} radius={16} />
            )
          }
        </QueryStatus>
      </View>
    </Screen>
  );
}

function Room({
  leagueId,
  home,
  draft,
  offsetMs,
  wide,
}: {
  leagueId: string;
  home: Schemas['LeagueHome'];
  draft: Schemas['Draft'];
  offsetMs: number;
  wide: boolean;
}) {
  const router = useRouter();
  const toast = useToast();
  const { colors, spacing, radii } = useTheme();
  const { width } = useLayout();
  const myTeam = home.viewer.team;
  const isCommissioner = home.viewer.role === 'commissioner';
  const startBlocked = startBlocker(home.league);
  // After the draft there is no queue to read: stop asking (the last pick's
  // refresh would otherwise race the completion).
  const drafting = draft.status !== 'complete';
  const queueQuery = useDraftQueue(leagueId, Boolean(myTeam) && drafting);
  const setQueue = useSetDraftQueue(leagueId);
  const setAutopick = useSetAutopick(leagueId);
  const makePick = useMakePick(leagueId);
  const control = useDraftControl(leagueId);
  // A finished draft is read-only: the board and the teams, nothing to pick from.
  const results = draft.status === 'complete';
  const [chosenPane, setPane] = useState<Pane>(
    results ? (wide ? 'roster' : 'board') : wide ? 'players' : 'board',
  );
  // The last pick can complete the draft while the Players or Queue pane is open.
  const pane: Pane =
    results && (chosenPane === 'players' || chosenPane === 'queue')
      ? wide
        ? 'roster'
        : 'board'
      : chosenPane;
  const [teamId, setTeamId] = useState<string | null>(null);
  const resultsTeam =
    draft.order.find((o) => o.team.id === teamId)?.team ?? myTeam ?? draft.order[0]?.team ?? null;
  // Phone: where the tabs stick, and the page's scroll position.
  const page = useRef<ScrollView>(null);
  const stickAt = useRef(0);
  const scrollY = useRef(0);
  // A new tab starts at its top: if the tabs were stuck, scroll back to where
  // they stick once it has rendered.
  const restick = useRef(false);
  useEffect(() => {
    if (restick.current) page.current?.scrollTo({ y: stickAt.current, animated: false });
    restick.current = false;
  }, [pane]);
  const showsPlayers =
    !results && (wide ? pane !== 'queue' && pane !== 'roster' : pane === 'players');
  const [pickError, setPickError] = useState<string | null>(null);

  const draftedIds = useMemo(() => new Set(draft.picks.map((p) => p.player.id)), [draft.picks]);
  const queue = queueWithoutDrafted(queueQuery.data?.data.players ?? [], draft.picks);
  const queuedIds = useMemo(() => new Set(queue.map((q) => q.player.id)), [queue]);
  const players = useDraftPlayers(leagueId, draftedIds, showsPlayers);
  const autopick = queueQuery.data?.data.autopick ?? false;
  const autopickOnTimeout = queueQuery.data?.data.autopickOnTimeout ?? true;
  // A missed clock is never silent: the latest one this round, until dismissed.
  const notice = results ? null : missedClockNotice(draft, myTeam?.id ?? null, autopick);
  const [dismissed, setDismissed] = useState<string | null>(null);
  const shownNotice = notice && notice.pickId !== dismissed ? notice : null;
  const myCounts = myTeam ? positionCounts(draft.picks, myTeam.id) : null;
  const onClock =
    draft.status === 'in_progress' &&
    draft.currentPick !== null &&
    myTeam !== null &&
    draft.currentPick.team.id === myTeam.id;
  // The commissioner may pick for whoever is on the clock; the UI offers it only for their own team.
  const canPick = onClock;
  const pickingId = makePick.isPending ? (makePick.variables ?? null) : null;

  // My own missed clock also gets a toast when it happens.
  const lastNoticed = useRef(notice?.pickId ?? null);
  useEffect(() => {
    const id = notice?.pickId ?? null;
    if (id && id !== lastNoticed.current && notice?.mine) {
      toast({ message: 'Your clock ran out: we autopicked for you', tone: 'error' });
    }
    lastNoticed.current = id;
  }, [notice, toast]);

  // Tell the user when their turn starts.
  const wasOnClock = useRef(onClock);
  useEffect(() => {
    if (onClock && !wasOnClock.current) toast({ message: "You're on the clock" });
    wasOnClock.current = onClock;
  }, [onClock, toast]);

  // Board changed: refresh the queue (the server drops drafted players).
  const pickCount = draft.picks.length;
  const refetchQueue = queueQuery.refetch;
  useEffect(() => {
    if (myTeam && drafting) void refetchQueue();
  }, [pickCount, myTeam, drafting, refetchQueue]);

  const saveQueue = (next: QueueItem[]) =>
    setQueue.mutate(next, {
      onError: (e) => toast({ message: describeError(e), tone: 'error' }),
    });

  const pick = (player: Schemas['PlayerRef']) => {
    setPickError(null);
    makePick.mutate(player.id, {
      onSuccess: () => toast({ message: `You drafted ${player.displayName}`, tone: 'success' }),
      onError: (e) => {
        const msg = problemErrors(e)[0] ?? describeError(e);
        setPickError(msg);
        toast({ message: msg, tone: 'error' });
      },
    });
  };

  const toggleQueue = (item: Schemas['PlayerListItem']) => {
    if (queuedIds.has(item.player.id)) saveQueue(queueRemove(queue, item.player.id));
    else {
      saveQueue(queueAdd(queue, { player: item.player, draftRank: item.draftRank }));
      toast({ message: `${item.player.displayName} queued` });
    }
  };

  const header = (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
      <IconButton
        icon={ChevronLeft}
        label="Back to league"
        onPress={() => router.push(`/leagues/${leagueId}`)}
        testID="draft-back"
      />
      <View style={{ flex: 1, minWidth: 0 }}>
        <AppText variant="eyebrow" muted numberOfLines={1}>
          {home.league.name}
        </AppText>
        <AppText
          variant={width < 400 ? 'heading' : 'title'}
          accessibilityRole="header"
          numberOfLines={1}
        >
          {results ? 'Draft results' : 'Draft room'}
        </AppText>
      </View>
      {draft.status === 'in_progress' ? <Pill label="Live" tone="live" dot /> : null}
      {isCommissioner && (draft.status === 'in_progress' || draft.status === 'paused') ? (
        <Button
          label={draft.status === 'paused' ? 'Resume' : 'Pause'}
          icon={draft.status === 'paused' ? Play : Pause}
          variant="secondary"
          size="sm"
          loading={control.isPending}
          onPress={() =>
            control.mutate(draft.status === 'paused' ? 'resume' : 'pause', {
              onError: (e) => toast({ message: describeError(e), tone: 'error' }),
            })
          }
          testID="draft-pause"
        />
      ) : null}
      {isCommissioner && draft.status === 'scheduled' ? (
        <Button
          label="Start"
          icon={Play}
          size="sm"
          disabled={startBlocked !== null}
          loading={control.isPending}
          onPress={() =>
            control.mutate('start', {
              onError: (e) => toast({ message: describeError(e), tone: 'error' }),
            })
          }
          accessibilityHint={startBlocked ?? undefined}
          testID="draft-start"
        />
      ) : null}
    </View>
  );
  const startNote =
    isCommissioner && draft.status === 'scheduled' ? (
      <AppText variant="meta" muted testID="draft-start-reason">
        {startExplanation(home.league)}
      </AppText>
    ) : null;

  const playerRows = (
    <DraftPlayersList
      state={players}
      scroll={wide}
      queuedIds={queuedIds}
      myCounts={myCounts}
      canPick={canPick}
      pickingId={pickingId}
      onPick={pick}
      onToggleQueue={toggleQueue}
    />
  );
  const queuePane = (
    <DraftQueue
      queue={queue}
      autopick={autopick}
      autopickPending={setAutopick.isPending}
      autopickOnTimeout={autopickOnTimeout}
      onAutopick={(enabled) =>
        setAutopick.mutate(
          { enabled },
          {
            onSuccess: () => toast({ message: enabled ? 'Autopick on' : 'Autopick off' }),
            onError: (e) => toast({ message: describeError(e), tone: 'error' }),
          },
        )
      }
      onAutopickOnTimeout={(onTimeout) =>
        setAutopick.mutate(
          { onTimeout },
          {
            onSuccess: () =>
              toast({
                message: onTimeout
                  ? 'Autopick turns on if your clock runs out'
                  : 'A missed clock will not turn autopick on',
              }),
            onError: (e) => toast({ message: describeError(e), tone: 'error' }),
          },
        )
      }
      onMove={(id, delta) => saveQueue(queueMove(queue, id, delta))}
      onRemove={(id) => saveQueue(queueRemove(queue, id))}
      myCounts={myCounts}
      canPick={canPick}
      pickingId={pickingId}
      onPick={pick}
      scroll={wide}
    />
  );
  const rosterPane = results ? (
    <View style={{ flex: wide ? 1 : undefined, minHeight: 0 }}>
      <DraftTeamChips
        teams={draft.order.map((o) => o.team)}
        selectedId={resultsTeam?.id ?? null}
        onSelect={setTeamId}
      />
      <DraftRoster picks={draft.picks} myTeam={resultsTeam} scroll={wide} />
    </View>
  ) : (
    <DraftRoster picks={draft.picks} myTeam={myTeam} scroll={wide} />
  );
  const board = <DraftBoard draft={draft} myTeamId={myTeam?.id ?? null} scroll={wide} />;
  const clock = (
    <DraftClock
      draft={draft}
      myTeamId={myTeam?.id ?? null}
      offsetMs={offsetMs}
      autopick={autopick}
      compact={!wide && draft.status === 'in_progress'}
    />
  );
  const alerts = (
    <>
      {shownNotice ? (
        <MissedClockBanner
          notice={shownNotice}
          onDismiss={() => setDismissed(shownNotice.pickId)}
        />
      ) : null}
      {pickError ? (
        <Card testID="pick-error" style={{ paddingVertical: spacing.sm }}>
          <AppText variant="meta" color={colors.loss}>
            {pickError}
          </AppText>
        </Card>
      ) : null}
    </>
  );

  const panel = (child: ReactNode) => (
    <View
      style={{
        flex: 1,
        minHeight: 0,
        borderRadius: radii.lg,
        borderWidth: 1,
        borderColor: colors.border,
        backgroundColor: colors.surface,
        overflow: 'hidden',
      }}
    >
      {child}
    </View>
  );

  const sideSegments = results
    ? [{ value: 'roster' as const, label: 'Teams', icon: Shirt, testID: 'pane-roster' }]
    : [
        { value: 'players' as const, label: 'Players', icon: Users, testID: 'pane-players' },
        {
          value: 'queue' as const,
          label: 'Queue',
          icon: ListOrdered,
          badge: queue.length,
          testID: 'pane-queue',
        },
        { value: 'roster' as const, label: 'Roster', icon: Shirt, testID: 'pane-roster' },
      ];

  if (wide) {
    return (
      <View style={{ flex: 1, gap: spacing.md, minHeight: 0 }} testID="draft-room">
        {header}
        {startNote}
        {clock}
        {alerts}
        {results ? null : <PickTicker picks={draft.picks} />}
        <View style={{ flex: 1, flexDirection: 'row', gap: spacing.md, minHeight: 0 }}>
          {panel(board)}
          <View style={{ width: 440, gap: spacing.sm, minHeight: 0 }}>
            {results ? null : (
              <SegmentedControl
                accessibilityLabel="Draft room panels"
                segments={sideSegments}
                value={pane === 'board' ? 'players' : pane}
                onChange={setPane}
              />
            )}
            {panel(
              results || pane === 'roster' ? (
                rosterPane
              ) : pane === 'queue' ? (
                queuePane
              ) : (
                <View style={{ flex: 1, minHeight: 0 }}>
                  <DraftPlayersControls state={players} />
                  {playerRows}
                </View>
              ),
            )}
          </View>
        </View>
      </View>
    );
  }

  // Phone. Child 1 of the scroll view sticks to the top: it is opaque and
  // runs edge to edge, so nothing shows through it or beside it.
  const live = draft.status === 'in_progress';
  const showPane = (next: Pane) => {
    restick.current = scrollY.current > stickAt.current;
    setPane(next);
  };
  const gutter = spacing.lg;
  const card = {
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.surface,
    overflow: 'hidden' as const,
  };
  return (
    <ScrollView
      ref={page}
      testID="draft-room"
      style={{ flex: 1 }}
      stickyHeaderIndices={[1]}
      keyboardShouldPersistTaps="handled"
      scrollEventThrottle={200}
      onScroll={({ nativeEvent: e }) => {
        scrollY.current = e.contentOffset.y;
        if (
          pane === 'players' &&
          e.layoutMeasurement.height + e.contentOffset.y >= e.contentSize.height - 600
        )
          players.loadMore();
      }}
      contentContainerStyle={{ paddingBottom: spacing.lg }}
    >
      <View
        style={{
          gap: spacing.md,
          paddingTop: spacing.lg,
          paddingBottom: spacing.xs,
          paddingHorizontal: gutter,
        }}
        onLayout={(e) => (stickAt.current = e.nativeEvent.layout.height)}
      >
        {header}
        {startNote}
        {live ? null : clock}
        {alerts}
        {results ? null : <PickTicker picks={draft.picks} />}
      </View>
      <View
        testID="draft-sticky"
        style={{
          gap: spacing.sm,
          paddingTop: spacing.sm,
          paddingHorizontal: gutter,
          paddingBottom: pane === 'players' ? 0 : spacing.sm,
          backgroundColor: colors.background,
        }}
      >
        <SegmentedControl
          accessibilityLabel="Draft room panels"
          segments={[
            { value: 'board', label: 'Board', testID: 'pane-board' },
            ...sideSegments.map(({ icon: _icon, ...s }) => s),
          ]}
          value={pane}
          onChange={showPane}
        />
        {live ? clock : null}
        {pane === 'players' ? (
          <DraftPlayersControls
            state={players}
            style={[
              card,
              {
                borderBottomWidth: 1,
                borderTopLeftRadius: radii.lg,
                borderTopRightRadius: radii.lg,
              },
            ]}
          />
        ) : null}
      </View>
      <View style={{ paddingHorizontal: gutter }}>
        <View
          style={[
            card,
            pane === 'players'
              ? {
                  borderTopWidth: 0,
                  borderBottomLeftRadius: radii.lg,
                  borderBottomRightRadius: radii.lg,
                }
              : { borderRadius: radii.lg },
          ]}
        >
          {pane === 'board'
            ? board
            : pane === 'queue'
              ? queuePane
              : pane === 'roster'
                ? rosterPane
                : playerRows}
        </View>
      </View>
    </ScrollView>
  );
}
