import { ApiError, type Schemas } from '@myfantasyprem/api-client';
import type { Href } from 'expo-router';
import { useEffect, useMemo, useState, type ReactNode } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar } from '../Badges';
import { Button } from '../Button';
import { Card } from '../Card';
import { EmptyState } from '../EmptyState';
import {
  Activity,
  ArrowLeftRight,
  Calendar,
  ChevronLeft,
  CircleAlert,
  LayoutGrid,
  List,
  Lock,
  Newspaper,
  Shirt,
  SquarePen,
  X,
  type LucideIcon,
} from '../icons';
import { PlayerRow } from '../PlayerRow';
import { usePlayerCard } from '../players/PlayerCardContext';
import { pressFeedback, type PressState } from '../pressable';
import { PressableLink } from '../PressableLink';
import { QueryStatus } from '../QueryStatus';
import { Screen } from '../Screen';
import { Skeleton } from '../Skeleton';
import { StartSitCard } from '../projections/StartSit';
import { TeamProjectionBar } from '../projections/TeamProjectionBar';
import { useToast } from '../Toast';
import { GameweekSwitcher } from './GameweekSwitcher';
import { ListView, PitchView, scoresOf } from './LineupViews';
import { NewsSheet } from './NewsSheet';
import { SwapSheet, swapCandidates } from './SwapSheet';
import { useFreshness, useNow } from '@/api/freshness';
import { useCurrentGameweek, useLeagueHome, useTeam } from '@/api/hooks';
import { lineupLocked, useLineup, useSetLineup } from '@/api/lineup-hooks';
import { useTrades } from '@/api/moves-hooks';
import { useSessionPref } from '@/auth/SessionPrefs';
import { useLeagueProjections } from '@/api/projections';
import { useTagByPlayer } from '@/api/tag-hooks';
import { useBlockByPlayer } from '@/api/trade-block-hooks';
import { RuleError } from '@/api/optimistic';
import { usePrefetchAdjacentLineups } from '@/api/prefetch';
import { useLayout } from '@/hooks/useLayout';
import { describeError, problemErrors, problemSlug } from '@/lib/errors';
import { ordinal, recordText } from '@/lib/leagues';
import { checkSwap, fromLineup, tapPlayer, toLineupInput } from '@/lib/lineup';
import { autoSubLine, gameweekPlayed, leagueGameweek } from '@/lib/matchup';
import { flattenUnique } from '@/lib/pages';
import { EXPECTED_BASIS, expectedText, teamExpected } from '@/lib/projections';
import { haptics } from '@/lib/haptics';
import { tradeBucket } from '@/lib/trade';
import { formatDateTime, formatKickoff } from '@/lib/time';
import { useTheme } from '@/theme';

interface Props {
  leagueId: string;
  teamId: string;
  /** My team: lineups are editable before the deadline. */
  isMine: boolean;
  back?: { label: string; href: string };
}

type TeamQuery = ReturnType<typeof useTeam>;

/**
 * A team's page: the team card (record, rank, quick links), then its lineup
 * for a gameweek on the pitch (the default) or as a list, the bench in
 * auto-sub order. On my team, before the deadline, a position pill opens the
 * swap sheet (list) or a tap selects then swaps (pitch); both are checked
 * here, then by the server. Locked after the deadline, with points and
 * auto-subs once the gameweek has started.
 */
export function TeamView({ leagueId, teamId, isMine, back }: Props) {
  const home = useLeagueHome(leagueId);
  const team = useTeam(leagueId, teamId);
  const freshness = useFreshness(team);
  const { spacing } = useTheme();
  const gwStatus = useCurrentGameweek();
  const status = home.data?.data.league.status;
  const title = team.data?.data.team.name ?? (isMine ? 'My team' : 'Team');
  const inSeason = status === 'in_season' || status === 'complete';
  const card = (
    <TeamCard
      leagueId={leagueId}
      teamId={teamId}
      isMine={isMine}
      title={title}
      team={team}
      standings={home.data?.data.standings}
      canTrade={!isMine && Boolean(home.data?.data.viewer.team)}
      currentGw={
        inSeason && home.data
          ? leagueGameweek(home.data.data, gwStatus.data?.data.current.number)
          : undefined
      }
    />
  );

  return (
    <Screen
      title={title}
      documentTitle={isMine ? 'My team' : title}
      hideHeader
      freshness={freshness}
    >
      {back ? <BackLink label={back.label} href={back.href} /> : null}
      {home.isPending || team.isPending ? (
        <View style={{ gap: spacing.md }} testID="loading">
          <Skeleton height={132} radius={16} />
          <Skeleton height={360} radius={16} />
        </View>
      ) : status !== undefined && !inSeason ? (
        <>
          {card}
          <PreseasonRoster team={team} status={status} />
        </>
      ) : home.data ? (
        <>
          {card}
          <Lineup leagueId={leagueId} teamId={teamId} isMine={isMine} home={home.data.data} />
          <StartSitCard leagueId={leagueId} teamId={teamId} />
        </>
      ) : (
        <>
          {card}
          <QueryStatus query={home}>{() => null}</QueryStatus>
        </>
      )}
    </Screen>
  );
}

/** "‹ League" above the card (the page has no title bar of its own). */
function BackLink({ label, href }: { label: string; href: string }) {
  const { colors, spacing, radii } = useTheme();
  return (
    <PressableLink
      href={href as Href}
      testID="back-link"
      hitSlop={6}
      style={(state: PressState) => [
        {
          flexDirection: 'row',
          alignItems: 'center',
          alignSelf: 'flex-start',
          minHeight: 32,
          paddingRight: spacing.sm,
          borderRadius: radii.sm,
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <ChevronLeft size={18} color={colors.accent} aria-hidden />
      <AppText accent weight="semibold">
        {label}
      </AppText>
    </PressableLink>
  );
}

/**
 * A team's rank by one standings column, most first, ties sharing a rank:
 * Sleeper's `PF 7th` and `PA 6th` (1st in PA conceded the most).
 */
export function statRank(
  standings: readonly Schemas['StandingRow'][],
  teamId: string,
  key: 'pointsFor' | 'pointsAgainst',
): number | null {
  const mine = standings.find((r) => r.team.id === teamId);
  if (!mine) return null;
  return 1 + standings.filter((r) => r[key] > mine[key]).length;
}

/** The card's stat line: `3-0-3 · 6th · PF 7th · PA 6th`. */
export function teamStatLine(
  record: Schemas['Record'],
  standings: readonly Schemas['StandingRow'][] | undefined,
  teamId: string,
): string {
  const pf = standings ? statRank(standings, teamId, 'pointsFor') : null;
  const pa = standings ? statRank(standings, teamId, 'pointsAgainst') : null;
  return [
    recordText(record),
    ordinal(record.rank),
    pf !== null ? `PF ${ordinal(pf)}` : `PF ${record.pointsFor}`,
    pa !== null ? `PA ${ordinal(pa)}` : null,
  ]
    .filter(Boolean)
    .join(' · ');
}

/**
 * The team card, directly under the tabs: square initials avatar with the
 * rank on a small badge, the name in bold, `3-0-3 · 6th · PF 7th · PA 6th`,
 * the current gameweek's points on the right (Sleeper's total), then quick
 * links separated by hairlines: schedule, trades, moves, news.
 */
function TeamCard({
  leagueId,
  teamId,
  isMine,
  title,
  team,
  standings,
  canTrade,
  currentGw,
}: {
  leagueId: string;
  teamId: string;
  isMine: boolean;
  title: string;
  team: TeamQuery;
  standings?: readonly Schemas['StandingRow'][];
  canTrade: boolean;
  /** The league's current gameweek once the season runs: its points show on the right. */
  currentGw?: number;
}) {
  const { colors, spacing, fontFamilies } = useTheme();
  const [news, setNews] = useState(false);
  const detail = team.data?.data;
  const record = detail && detail.record.played > 0 ? detail.record : null;
  const league = `/leagues/${leagueId}`;
  // Under the stat line: Expected and the win chance for the gameweek (ADR 0014).
  const pointsBar: ReactNode =
    currentGw !== undefined ? (
      <TeamProjectionBar leagueId={leagueId} teamId={teamId} gameweek={currentGw} />
    ) : null;
  return (
    <Card flush testID="team-card">
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.md,
          paddingHorizontal: spacing.lg,
          paddingVertical: spacing.md,
        }}
      >
        <View>
          <Avatar name={title} size={48} square />
          {record ? (
            <View
              testID="team-rank"
              accessibilityLabel={`Rank ${ordinal(record.rank)}`}
              style={{
                position: 'absolute',
                left: -6,
                bottom: -6,
                minWidth: 22,
                height: 22,
                paddingHorizontal: 4,
                borderRadius: 11,
                borderWidth: 2,
                borderColor: colors.surface,
                backgroundColor: colors.accent,
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <AppText
                numeric
                weight="semibold"
                color={colors.onAccent}
                style={{ fontSize: 11, lineHeight: 14 }}
              >
                {String(record.rank)}
              </AppText>
            </View>
          ) : null}
        </View>
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <AppText
            accessibilityRole="header"
            numberOfLines={1}
            testID="team-name"
            style={{ fontFamily: fontFamilies.bold, fontSize: 18, lineHeight: 23 }}
          >
            {title}
          </AppText>
          {detail && !isMine ? (
            <AppText variant="meta" muted numberOfLines={1}>
              {detail.team.managerName}
            </AppText>
          ) : null}
          {record ? (
            <AppText variant="meta" muted numberOfLines={1} testID="team-record">
              {teamStatLine(record, standings, teamId)}
            </AppText>
          ) : null}
          {pointsBar}
        </View>
        {currentGw !== undefined ? (
          <CurrentPoints leagueId={leagueId} teamId={teamId} gameweek={currentGw} />
        ) : null}
      </View>
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'stretch',
          borderTopWidth: 1,
          borderTopColor: colors.border,
        }}
      >
        <QuickAction
          icon={Calendar}
          label="Schedule"
          href={`${league}/matchup`}
          testID="team-action-schedule"
        />
        <Hairline />
        {isMine ? (
          <TradesAction leagueId={leagueId} myTeamId={teamId} />
        ) : canTrade ? (
          <QuickAction
            icon={ArrowLeftRight}
            label="Trade"
            href={`${league}/players/trades/new?teamId=${teamId}`}
            testID="propose-trade"
            accessibilityLabel="Propose a trade"
          />
        ) : (
          <QuickAction
            icon={ArrowLeftRight}
            label="Trades"
            href={`${league}/players/trades`}
            testID="team-action-trades"
          />
        )}
        <Hairline />
        <QuickAction
          icon={Activity}
          label="Moves"
          href={`${league}/activity`}
          testID="team-action-moves"
        />
        <Hairline />
        <QuickAction
          icon={Newspaper}
          label="News"
          onPress={() => setNews(true)}
          testID="team-action-news"
          accessibilityLabel={isMine ? 'News for my players' : `News for ${title}`}
        />
      </View>
      <NewsSheet
        visible={news}
        onClose={() => setNews(false)}
        leagueId={leagueId}
        teamId={teamId}
        isMine={isMine}
      />
    </Card>
  );
}

/**
 * The current gameweek's points on the right of the card, like Sleeper's
 * total: live while it is played, a dash before kickoff. No win chance yet.
 */
function CurrentPoints({
  leagueId,
  teamId,
  gameweek,
}: {
  leagueId: string;
  teamId: string;
  gameweek: number;
}) {
  const lineup = useLineup(leagueId, teamId, gameweek);
  const data = lineup.data?.data;
  // Another team's lineup is private before its deadline: nothing to show.
  if (lineup.isError) return null;
  const points = data?.totalPoints;
  const live = data?.scoreState === 'live';
  return (
    <View
      testID="team-live"
      style={{ alignItems: 'flex-end' }}
      accessibilityLabel={
        points === undefined
          ? `Gameweek ${gameweek}, not started`
          : `Gameweek ${gameweek}${live ? ' live' : ''}, ${points} points`
      }
    >
      <AppText numeric weight="bold" testID="team-points" style={{ fontSize: 24, lineHeight: 30 }}>
        {points === undefined ? '–' : String(points)}
      </AppText>
      <AppText variant="meta" subtle style={{ fontSize: 11, lineHeight: 14 }}>
        {live ? 'Live' : `GW ${gameweek}`}
      </AppText>
    </View>
  );
}

type LineupViewMode = 'list' | 'pitch';

/** List or pitch as two small icon buttons (pitch is the default). */
function ViewToggle({
  value,
  onChange,
}: {
  value: 'list' | 'pitch';
  onChange: (next: 'list' | 'pitch') => void;
}) {
  const { colors, radii } = useTheme();
  const options = [
    { value: 'list' as const, icon: List, label: 'List view', testID: 'view-list' },
    { value: 'pitch' as const, icon: LayoutGrid, label: 'Pitch view', testID: 'view-pitch' },
  ];
  return (
    <View
      accessibilityRole="radiogroup"
      accessibilityLabel="Lineup view"
      style={{
        flexDirection: 'row',
        padding: 2,
        borderRadius: radii.sm,
        backgroundColor: colors.surfaceControl,
      }}
    >
      {options.map((o) => {
        const on = value === o.value;
        return (
          <Pressable
            key={o.value}
            testID={o.testID}
            accessibilityRole="radio"
            accessibilityLabel={o.label}
            accessibilityState={{ checked: on }}
            aria-checked={on}
            hitSlop={4}
            onPress={() => onChange(o.value)}
            style={(state: PressState) => [
              {
                width: 34,
                height: 30,
                borderRadius: radii.sm - 2,
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: on ? colors.surfaceSelected : 'transparent',
              },
              ...pressFeedback(colors, state, { hoverFill: !on }),
            ]}
          >
            <o.icon size={17} color={on ? colors.accent : colors.textMuted} aria-hidden />
          </Pressable>
        );
      })}
    </View>
  );
}

function Hairline() {
  const { colors, spacing } = useTheme();
  return (
    <View
      aria-hidden
      style={{ width: 1, marginVertical: spacing.md, backgroundColor: colors.border }}
    />
  );
}

/** Trades, with a red count of offers waiting for my answer. */
function TradesAction({ leagueId, myTeamId }: { leagueId: string; myTeamId: string }) {
  const trades = useTrades(leagueId);
  const incoming = flattenUnique(trades.data?.pages, (t) => t.id).filter(
    (t) => tradeBucket(t, myTeamId) === 'incoming',
  ).length;
  return (
    <QuickAction
      icon={ArrowLeftRight}
      label="Trade"
      href={`/leagues/${leagueId}/players/trades`}
      testID="team-action-trades"
      badge={incoming}
      accessibilityLabel={incoming > 0 ? `Trades, ${incoming} waiting for you` : 'Trades'}
    />
  );
}

function QuickAction({
  icon: Icon,
  label,
  href,
  onPress,
  testID,
  badge,
  accessibilityLabel,
}: {
  icon: LucideIcon;
  label: string;
  testID: string;
  badge?: number;
  accessibilityLabel?: string;
} & ({ href: string; onPress?: undefined } | { href?: undefined; onPress: () => void })) {
  const { colors, spacing } = useTheme();
  // Sleeper's one line of icon + label; below 360pt four of them do not fit: stack them.
  const { width } = useLayout();
  const stacked = width < 360;
  const style = (state: PressState) => [
    {
      flex: 1,
      minWidth: 0,
      minHeight: 44,
      flexDirection: stacked ? ('column' as const) : ('row' as const),
      alignItems: 'center' as const,
      justifyContent: 'center' as const,
      gap: stacked ? 2 : 5,
      paddingVertical: spacing.xs,
      paddingHorizontal: 2,
    },
    ...pressFeedback(colors, state),
  ];
  const content = (
    <>
      <Icon size={16} color={colors.text} aria-hidden />
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
        <AppText
          variant="meta"
          weight="semibold"
          numberOfLines={1}
          style={{ fontSize: 13, lineHeight: 17 }}
        >
          {label}
        </AppText>
        {badge ? (
          <View
            testID={`${testID}-badge`}
            style={{
              minWidth: 18,
              height: 18,
              paddingHorizontal: 4,
              borderRadius: 9,
              backgroundColor: colors.loss,
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <AppText
              numeric
              weight="semibold"
              color={colors.background}
              style={{ fontSize: 11, lineHeight: 14 }}
            >
              {String(badge)}
            </AppText>
          </View>
        ) : null}
      </View>
    </>
  );
  return href !== undefined ? (
    <PressableLink
      href={href as Href}
      testID={testID}
      accessibilityLabel={accessibilityLabel ?? label}
      style={style}
    >
      {content}
    </PressableLink>
  ) : (
    <Pressable
      testID={testID}
      accessibilityRole="button"
      accessibilityLabel={accessibilityLabel ?? label}
      onPress={onPress}
      style={style}
    >
      {content}
    </Pressable>
  );
}

function PreseasonRoster({ team, status }: { team: ReturnType<typeof useTeam>; status: string }) {
  const openCard = usePlayerCard();
  return (
    <QueryStatus query={team}>
      {({ data }) => (
        <>
          <Card>
            <EmptyState
              icon={Shirt}
              title={status === 'drafting' ? 'Draft in progress' : 'Lineups open after the draft'}
              body="Once every squad is full you set your XI and bench here each gameweek."
              testID="lineup-preseason"
            />
          </Card>
          {data.roster.length > 0 ? (
            <Card flush>
              {data.roster.map((r, i) => (
                <PlayerRow
                  key={r.player.id}
                  player={r.player}
                  divider={i > 0}
                  detail={`Pick ${r.slot}`}
                  onOpenCard={openCard ? () => openCard(r.player.id) : undefined}
                />
              ))}
            </Card>
          ) : null}
        </>
      )}
    </QueryStatus>
  );
}

function Lineup({
  leagueId,
  teamId,
  isMine,
  home,
}: {
  leagueId: string;
  teamId: string;
  isMine: boolean;
  home: Schemas['LeagueHome'];
}) {
  const { colors, spacing, fontFamilies } = useTheme();
  const toast = useToast();
  const myTags = useTagByPlayer(leagueId);
  const blocked = useBlockByPlayer(leagueId);
  const now = useNow(30_000);
  const gwStatus = useCurrentGameweek();
  const current = leagueGameweek(home, gwStatus.data?.data.current.number);
  // Mine: the next unlocked gameweek (the server default). Others: the current one.
  const [gw, setGw] = useState<number | undefined>(isMine ? undefined : current);
  const lineup = useLineup(leagueId, teamId, gw);
  const shownGw = gw ?? lineup.data?.data.gameweek;
  // Locked by the server, or by the clock since it answered (a lineup
  // fetched before the deadline): then fetch again, so the default lineup
  // moves on to the next gameweek, which stays editable until its own deadline.
  const locked = lineupLocked(lineup.data, lineup.dataUpdatedAt, now);
  const clockLocked = locked && lineup.data?.data.locked === false;
  const { refetch: refetchLineup, isFetching: lineupFetching } = lineup;
  useEffect(() => {
    if (clockLocked && !lineupFetching) void refetchLineup();
  }, [clockLocked, lineupFetching, refetchLineup]);
  const totals = lineup.data?.data.totalPoints !== undefined ? lineup.data.data : null;
  const locksAt = lineup.data ? Date.parse(lineup.data.data.locksAt) : null;
  const save = useSetLineup(leagueId, teamId, home.settings.lineup);
  // The pitch by default; the list is one tap away and a choice holds for the session.
  const [view, setView] = useSessionPref('lineupView', 'pitch');
  const [selected, setSelected] = useState<string | null>(null);
  // A lineup that stops being editable (it locked, or a refetch is standing
  // in) drops the selection, so no row stays highlighted with nothing open.
  const [wasEditable, setWasEditable] = useState(true);
  const [errors, setErrors] = useState<string[]>([]);
  const openCard = usePlayerCard();
  const onInfo = (id: string) => openCard?.(id);

  const rules = home.settings.lineup;
  const server = lineup.data ? fromLineup(lineup.data.data) : null;
  // The cache shows a swap at once (optimistic) and loses it on a refusal.
  const shown = server;
  // Saves queue one after another, so the next swap need not wait; not while
  // the last gameweek's lineup stands in for this one.
  const editable = isMine && !locked && !lineup.isPlaceholderData;
  if (editable !== wasEditable) {
    setWasEditable(editable);
    if (!editable && selected !== null) setSelected(null);
  }
  usePrefetchAdjacentLineups(
    leagueId,
    teamId,
    shownGw,
    home.settings.startGameweek,
    home.settings.endGameweek,
  );

  const scoreMap = useMemo(() => scoresOf(lineup.data?.data), [lineup.data]);
  // Expected per player for the gameweek shown (ADR 0014); the team's is
  // the sum of the XI on screen, so a swap moves it at once.
  const projections = useLeagueProjections(leagueId, shownGw);
  const projected = projections.data?.data;
  const expectedMap = useMemo(
    () =>
      projected && projected.gameweek === shownGw
        ? new Map(projected.players.map((p) => [p.playerId, p.expected]))
        : undefined,
    [projected, shownGw],
  );
  const autoSubs = totals ? teamAutoSubLine(totals) : null;

  const changeView = (next: LineupViewMode) => {
    setSelected(null);
    setView(next);
  };

  // List view: a position pill opens the swap sheet for that player; a
  // candidate in the sheet is the second tap of the same swap state machine.
  const openSwap = (id: string) => {
    if (!shown) return;
    // Only open a sheet that has something to show (an empty one would be a
    // blank layer over the screen): say why instead.
    if (!editable) {
      haptics.error();
      toast({
        message: locked ? `GW ${shownGw} is locked` : 'Lineup is still loading',
        tone: 'error',
        haptic: false,
      });
      return;
    }
    const c = swapCandidates(shown, id, rules);
    if (c.starters.length + c.bench.length === 0) {
      haptics.error();
      toast({ message: 'No legal swaps for this player', tone: 'error', haptic: false });
      return;
    }
    haptics.light();
    setErrors([]);
    setSelected(id);
  };

  const changeGw = (next: number) => {
    setSelected(null);
    setErrors([]);
    setGw(next);
  };

  const press = (id: string) => {
    if (!shown || !shownGw) return;
    const tap = tapPlayer(shown, selected, id, rules);
    if (tap.kind === 'select') {
      haptics.light();
      setSelected(tap.selected);
      setErrors([]);
      return;
    }
    setSelected(null);
    if (tap.kind === 'deselect') return;
    if (tap.kind === 'reject') {
      haptics.error();
      setErrors([tap.reason]);
      return;
    }
    haptics.light();
    setErrors([]);
    // The hook shows the swap at once; the server still has the last word,
    // and a refusal (its 422 reasons, or locked) rolls it back.
    save.mutateAsync(toLineupInput(tap.next, shownGw)).then(
      () => toast({ message: 'Lineup saved', tone: 'success', haptic: false }),
      (e: unknown) => {
        if (e instanceof RuleError) {
          setErrors(e.problems);
          // Refused against a deadline that passed since the fetch: load
          // the lineup that is open now (the next gameweek's).
          if (lineupLocked(lineup.data, lineup.dataUpdatedAt)) void lineup.refetch();
          return;
        }
        if (problemSlug(e) === 'lineup-locked') {
          toast({ message: describeError(e), tone: 'error', haptic: false });
          void lineup.refetch();
          return;
        }
        const list = problemErrors(e);
        setErrors(list.length ? list : [describeError(e)]);
      },
    );
  };

  const teamExp =
    shown && expectedMap
      ? teamExpected(
          shown.starters.map((e) => e.player.id),
          expectedMap,
        )
      : null;

  const forbidden =
    lineup.error instanceof ApiError &&
    lineup.error.status === 403 &&
    problemSlug(lineup.error) === 'lineup-private';

  // One row, as on Sleeper: "Starters ✎", the view toggle and the stepper;
  // under it the hint and when the lineup locks.
  const header = (
    <View style={{ gap: 2 }} testID="lineup-header">
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
        <View style={{ flex: 1, minWidth: 0, flexDirection: 'row', alignItems: 'center', gap: 6 }}>
          <AppText
            accessibilityRole="header"
            numberOfLines={1}
            testID="lineup-heading"
            style={{ fontFamily: fontFamilies.bold, fontSize: 18, lineHeight: 24, flexShrink: 1 }}
          >
            Starters
          </AppText>
          {editable ? <SquarePen size={16} color={colors.textMuted} aria-hidden /> : null}
        </View>
        <ViewToggle value={view} onChange={changeView} />
        {shownGw !== undefined ? (
          <GameweekSwitcher
            gameweek={shownGw}
            min={home.settings.startGameweek}
            max={home.settings.endGameweek}
            onChange={changeGw}
          />
        ) : null}
      </View>
      {lineup.data && locksAt !== null ? (
        // Sibling texts, not nested: each keeps its own test id on iOS (Maestro).
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center' }}>
          {teamExp !== null ? (
            <>
              <AppText
                variant="meta"
                weight="semibold"
                testID="lineup-expected"
                accessibilityLabel={`Expected ${expectedText(teamExp)} points, ${EXPECTED_BASIS}, no auto-subs`}
              >
                {`Exp ${expectedText(teamExp)}`}
              </AppText>
              <AppText variant="meta" muted aria-hidden>
                {' · '}
              </AppText>
            </>
          ) : null}
          {editable && view === 'list' ? (
            <>
              <AppText variant="meta" muted testID="swap-hint">
                Tap a position to change your lineup
              </AppText>
              <AppText variant="meta" muted aria-hidden>
                {' · '}
              </AppText>
            </>
          ) : null}
          {locked ? (
            <AppText
              variant="meta"
              muted
              testID="lineup-locked"
              accessibilityLabel={`Locked, the deadline passed ${formatDateTime(locksAt)}`}
            >
              {`Locked ${formatDateTime(locksAt)}`}
            </AppText>
          ) : (
            <AppText
              variant="meta"
              weight="semibold"
              color={colors.accent}
              testID="lineup-open"
              accessibilityLabel={`Lineup locks ${formatDateTime(locksAt)}`}
            >
              {`Locks ${formatKickoff(locksAt, now)}`}
            </AppText>
          )}
        </View>
      ) : null}
    </View>
  );

  if (forbidden) {
    return (
      <>
        {header}
        <Card>
          <EmptyState
            icon={Lock}
            title="Hidden until the deadline"
            body="Other managers' lineups show once the gameweek locks."
            testID="lineup-private"
          />
        </Card>
      </>
    );
  }

  return (
    <>
      {header}
      {errors.length > 0 ? (
        <Card testID="lineup-errors" style={{ borderColor: colors.loss }}>
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
            <CircleAlert size={18} color={colors.loss} aria-hidden />
            <AppText weight="semibold" style={{ flex: 1 }} role="alert">
              That lineup isn&apos;t allowed
            </AppText>
            <Button
              label="Dismiss"
              size="sm"
              variant="ghost"
              icon={X}
              onPress={() => setErrors([])}
            />
          </View>
          {errors.map((m) => (
            <AppText key={m} variant="meta" muted testID="lineup-error">
              {`• ${m}`}
            </AppText>
          ))}
        </Card>
      ) : null}
      <QueryStatus query={lineup} loading={<Skeleton height={420} radius={16} />}>
        {() =>
          shown ? (
            view === 'pitch' ? (
              <PitchView
                state={shown}
                scores={scoreMap}
                editable={editable}
                selectedId={selected}
                check={(t) => (selected ? checkSwap(shown, selected, t, rules) : { kind: 'none' })}
                onPress={press}
                onInfo={onInfo}
              />
            ) : (
              <ListView
                state={shown}
                scores={scoreMap}
                editable={editable}
                selectedId={selected}
                check={(t) => (selected ? checkSwap(shown, selected, t, rules) : { kind: 'none' })}
                onPress={press}
                onSwap={openSwap}
                onInfo={onInfo}
                expected={expectedMap}
                myTags={myTags}
                blocked={blocked}
              />
            )
          ) : null
        }
      </QueryStatus>
      {shown && view === 'list' ? (
        <SwapSheet
          state={shown}
          selectedId={editable ? selected : null}
          rules={rules}
          scores={scoreMap}
          onSwap={press}
          onClose={() => setSelected(null)}
        />
      ) : null}
      {autoSubs ? (
        <AppText variant="meta" muted testID="autosub-summary">
          {autoSubs}
        </AppText>
      ) : null}
    </>
  );
}

/**
 * The team page's auto-sub line. The lineup marks who goes off and who
 * comes on but not the pairs, so one of each reads "A → B" and several
 * read "A, C → B, D".
 */
export function teamAutoSubLine(lineup: Schemas['Lineup']): string | null {
  const isOut = (s?: string) => s === 'projected_out' || s === 'confirmed_out';
  const isIn = (s?: string) => s === 'projected_in' || s === 'confirmed_in';
  const outs = lineup.starters.filter((e) => isOut(e.autoSub)).map((e) => e.player.displayName);
  const ins = lineup.bench.filter((e) => isIn(e.autoSub)).map((e) => e.player.displayName);
  if (outs.length === 0 || ins.length === 0) return null;
  const played = gameweekPlayed(lineup.scoreState ?? '');
  if (outs.length === 1 && ins.length === 1) {
    return autoSubLine([{ out: outs[0]!, in: ins[0]! }], played);
  }
  return `Auto-subs${played ? '' : ' projected'}: ${outs.join(', ')} → ${ins.join(', ')}`;
}
