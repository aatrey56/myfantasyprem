import type { Schemas } from '@myfantasyprem/api-client';
import { useRouter } from 'expo-router';
import { useRef, useState, type ReactNode } from 'react';
import { Animated, Platform, Pressable, ScrollView, View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest, ClubShirt, ClubTag, PlayerPhoto, positionLabel } from '../Badges';
import { useBrandedAssets } from '../branded/BrandedAssets';
import { Button, IconButton } from '../Button';
import { Pill } from '../Chip';
import { ArrowLeftRight, ChevronDown, CircleAlert, Star, UserMinus, UserPlus, X } from '../icons';
import { availabilityText } from '../PlayerRow';
import { pressFeedback, type PressState } from '../pressable';
import { QueryStatus } from '../QueryStatus';
import { SectionHeader } from '../SectionHeader';
import { SegmentedControl } from '../SegmentedControl';
import { Sheet, useSheetClose } from '../Sheet';
import { Skeleton } from '../Skeleton';
import { useToast } from '../Toast';
import { BlockPanel, BlockToggle, newsTime, NewsLine, TagPicker } from './CardExtras';
import {
  chanceFromNews,
  fixtureResult,
  heatCell,
  heatColors,
  heatGroups,
  logRows,
  splitName,
  type FutureFixture,
  type HeatColumn,
  type HeatGroup,
  type HeatQuality,
  type LogRow,
} from './heat';
import { SwipePager, useNestedScroll } from './SwipePager';
import { useLeagueContext, useLeaguePlayer, useLeaguePlayers, useTeam } from '@/api/hooks';
import { useAddFreeAgent, useCreateClaim } from '@/api/moves-hooks';
import { useToggleWatch, useWatchedIds } from '@/api/watchlist-hooks';
import { useLayout } from '@/hooks/useLayout';
import {
  CAREER_COLUMNS,
  careerCell,
  careerRows,
  careerTotals,
  DASH,
  type CareerColumn,
  type PastSeason,
} from '@/lib/career';
import { describeError, moveErrorText } from '@/lib/errors';
import { FixtureRank, useDifficultyRank } from '../projections/Difficulty';
import { fixtureA11y, fixtureLabel } from '@/lib/fixtures';
import { difficultyA11y } from '@/lib/projections';
import { formText } from '@/lib/form';
import {
  MISSING,
  detailSections,
  eventChips,
  opponentLabel,
  type EventChip,
  type StatCell,
} from '@/lib/gamelog';
import { initialsOf } from '@/lib/identity';
import { seasonStatTotals, statLabel } from '@/lib/matchup';
import { formatDateTime, formatKickoff } from '@/lib/time';
import { dropCandidates, waiversDueLabel } from '@/lib/waivers';
import { positionColorKey, tint, useTheme } from '@/theme';

type Detail = Schemas['PlayerDetail'];

/** What the card offers for a player, from my point of view. */
export function playerAction(
  d: Pick<Detail, 'rosterStatus' | 'owner'>,
  myTeamId: string | undefined,
): 'add' | 'claim' | 'trade' | 'mine' | 'none' {
  if (!myTeamId) return 'none';
  if (d.rosterStatus === 'free_agent') return 'add';
  if (d.rosterStatus === 'waivers') return 'claim';
  if (d.rosterStatus === 'rostered' && d.owner) return d.owner.id === myTeamId ? 'mine' : 'trade';
  return 'none';
}

interface SheetProps {
  leagueId: string;
  playerId: string | null;
  /** Open straight on the drop step (the list's Add / Claim buttons). */
  startWithDrop?: boolean;
  onClose: () => void;
}

/** The floating close button's size (SLEEPER-OBSERVED: 56px, bottom right). */
const FLOAT = 56;

/** The card fills the phone, up to the status bar. */
const FULL_HEIGHT = ['100%'] as const;

/**
 * The player card as a full-height bottom sheet (phone) or a wide centred
 * dialog (768px up). The card scrolls inside; on phones a pull down closes
 * it. One close button: on phones a round one floats over the bottom-right
 * corner, in thumb reach (SLEEPER-OBSERVED); wide, it sits in the hero.
 */
export function PlayerSheet({ leagueId, playerId, startWithDrop, onClose }: SheetProps) {
  const { spacing } = useTheme();
  const { isWide } = useLayout();
  return (
    <Sheet
      visible={playerId !== null}
      onClose={onClose}
      title="Player"
      testID="player-card"
      wideWidth={880}
      hideHeader
      snapPoints={FULL_HEIGHT}
      contentStyle={{ gap: 0, paddingBottom: isWide ? spacing.lg : FLOAT + spacing.lg * 2 }}
      overlay={playerId && !isWide ? <FloatingClose /> : null}
    >
      {playerId ? (
        <PlayerCard
          key={`${playerId}-${startWithDrop ? 'drop' : 'card'}`}
          leagueId={leagueId}
          playerId={playerId}
          startWithDrop={startWithDrop}
          onDone={onClose}
          onClose={isWide ? onClose : undefined}
        />
      ) : null}
    </Sheet>
  );
}

/**
 * A translucent round close button over the sheet's bottom-right corner
 * (phones). The sheet's only close, so it has the `sheet-close` test id the
 * sheet focuses on open; it slides the sheet down (useSheetClose).
 */
export function FloatingClose({ onPress }: { onPress?: () => void }) {
  const { colors, spacing, radii } = useTheme();
  const close = useSheetClose(onPress);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel="Close"
      // Web tooltip.
      {...({ title: 'Close' } as object)}
      onPress={close}
      testID="sheet-close"
      style={(s: PressState) => [
        {
          position: 'absolute',
          right: spacing.lg,
          bottom: spacing.lg,
          width: FLOAT,
          height: FLOAT,
          borderRadius: radii.pill,
          // The raised surface at 85%: the card shows through.
          backgroundColor: `${colors.surfaceRaised}D9`,
          borderWidth: 1,
          borderColor: colors.border,
          alignItems: 'center',
          justifyContent: 'center',
        },
        ...pressFeedback(colors, s),
      ]}
    >
      <X size={26} color={colors.text} strokeWidth={2.25} aria-hidden />
    </Pressable>
  );
}

interface CardProps {
  leagueId: string;
  playerId: string;
  startWithDrop?: boolean;
  /** After a successful add or claim, or when leaving for the trade builder. */
  onDone?: () => void;
  /** In a wide sheet: a close button in the card's hero (or above the card without one). */
  onClose?: () => void;
}

type Tab = 'overview' | 'log' | 'fixtures';
const TABS: readonly Tab[] = ['overview', 'log', 'fixtures'];

/**
 * Status banner and hero (shirt, name, bio stats, owner, the action), then
 * Summary (stat strip, next fixture, a strip of gameweek cards, season
 * totals), the game log (a heat-map table; a tap on a gameweek opens every
 * stat) and upcoming fixtures (doubles and blanks). Sign adds a free agent or
 * claims a player on waivers, choosing whom to drop.
 */
export function PlayerCard({ leagueId, playerId, startWithDrop, onDone, onClose }: CardProps) {
  const { spacing } = useTheme();
  const query = useLeaguePlayer(leagueId, playerId);
  const home = useLeagueContext(leagueId);
  const myTeamId = home.data?.data.viewer.team?.id;
  const [tab, setTab] = useState<Tab>('overview');
  const [dropping, setDropping] = useState(Boolean(startWithDrop));
  const [replacing, setReplacing] = useState(false);
  // Loading, an error and the drop and replace steps have no hero: the close goes above them.
  const shown = query.data ? playerAction(query.data.data, myTeamId) : null;
  const heroless =
    shown === null ||
    (Boolean(myTeamId) &&
      ((dropping && (shown === 'add' || shown === 'claim')) || (replacing && shown === 'mine')));
  const card = (
    <QueryStatus
      query={query}
      loading={
        <View style={{ gap: spacing.md }} testID="loading">
          <Skeleton height={140} />
          <Skeleton height={40} />
          <Skeleton height={160} />
        </View>
      }
    >
      {({ data }) => {
        const action = playerAction(data, myTeamId);
        if (replacing && action === 'mine' && myTeamId) {
          return (
            <ReplaceStep
              leagueId={leagueId}
              player={data}
              onBack={() => setReplacing(false)}
              onDone={onDone}
            />
          );
        }
        if (dropping && (action === 'add' || action === 'claim') && myTeamId) {
          return (
            <DropStep
              leagueId={leagueId}
              player={data}
              myTeamId={myTeamId}
              kind={action}
              onBack={() => setDropping(false)}
              onDone={onDone}
            />
          );
        }
        return (
          <View style={{ gap: spacing.lg }}>
            <Hero
              d={data}
              onClose={onClose}
              star={
                action === 'none' ? <WatchButton leagueId={leagueId} player={data.player} /> : null
              }
              actions={
                action === 'none' ? null : (
                  <Actions
                    leagueId={leagueId}
                    d={data}
                    action={action}
                    onDrop={() => setDropping(true)}
                    onReplace={() => setReplacing(true)}
                    onDone={onDone}
                  />
                )
              }
            />
            <NewsLine player={data.player} />
            <BlockPanel leagueId={leagueId} player={data.player} mine={action === 'mine'} />
            {myTeamId ? <TagPicker leagueId={leagueId} player={data.player} /> : null}
            <SegmentedControl
              accessibilityLabel="Player sections"
              value={tab}
              onChange={setTab}
              segments={[
                { value: 'overview', label: 'Summary', testID: 'card-tab-overview' },
                { value: 'log', label: 'Game log', testID: 'card-tab-log' },
                { value: 'fixtures', label: 'Fixtures', testID: 'card-tab-fixtures' },
              ]}
            />
            <SwipePager pages={TABS} value={tab} onChange={setTab} testID="card-pager">
              {tab === 'overview' ? (
                <Summary d={data} current={currentGameweekOf(home.data?.data.currentGameweek)} />
              ) : tab === 'fixtures' ? (
                <Fixtures d={data} />
              ) : (
                <GameLog d={data} />
              )}
            </SwipePager>
          </View>
        );
      }}
    </QueryStatus>
  );
  if (!onClose || !heroless) return card;
  return (
    <View style={{ gap: spacing.sm }}>
      <View style={{ alignItems: 'flex-end' }}>
        <IconButton icon={X} label="Close" onPress={onClose} testID="sheet-close" />
      </View>
      {card}
    </View>
  );
}

/** Where the player stands in this league. */
function ownershipText(d: Detail): string {
  if (d.rosterStatus === 'rostered' && d.owner) return `Owned by ${d.owner.name}`;
  if (d.rosterStatus === 'waivers') {
    return `On waivers${d.waiverClearsAt ? ` until ${formatDateTime(Date.parse(d.waiverClearsAt))}` : ''}`;
  }
  if (d.rosterStatus === 'free_agent') return 'Free agent';
  if (d.rosterStatus === 'locked') return waiversDueLabel(d.waiverClearsAt);
  return 'Status unknown';
}

/** Tiny tracked capitals: stat labels and section eyebrows. */
function Caps({ children, color }: { children: string; color?: string }) {
  return (
    <AppText
      variant="eyebrow"
      color={color}
      muted={!color}
      numberOfLines={1}
      style={{ fontSize: 11, lineHeight: 14, letterSpacing: 0.8 }}
    >
      {children}
    </AppText>
  );
}

/** Points per gameweek played (any minutes), or null before the first. */
function perGameweek(d: Pick<Detail, 'gameweeks' | 'totalPoints'>): number | null {
  const played = d.gameweeks.filter((g) => g.minutes > 0).length;
  return played ? d.totalPoints / played : null;
}

/** Injury or doubt across the top: amber for doubtful, red otherwise; with the chance and news. */
export function StatusBanner({
  player,
}: {
  player: Pick<Detail['player'], 'availability' | 'news' | 'newsAt'>;
}) {
  const { colors, spacing } = useTheme();
  const status = availabilityText(player.availability);
  if (!status) return null;
  const amber = player.availability === 'doubtful';
  const bg = amber ? colors.accent : colors.danger;
  const fg = amber ? colors.onAccent : colors.onDanger;
  const chance = chanceFromNews(player.news);
  const news = player.news?.trim();
  const at = newsTime(player.newsAt);
  return (
    <View
      testID="card-status"
      accessible
      accessibilityLabel={`${status}${chance === null ? '' : `, ${chance}% chance of playing`}${
        news ? `: ${news}` : ''
      }${news && at ? `, ${at}` : ''}`}
      style={{
        backgroundColor: bg,
        paddingVertical: spacing.sm,
        paddingHorizontal: spacing.lg,
        alignItems: 'center',
        gap: spacing.xxs,
      }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
        <CircleAlert size={15} color={fg} strokeWidth={2.5} aria-hidden />
        <AppText
          weight="bold"
          color={fg}
          style={{ fontSize: 13, lineHeight: 16, letterSpacing: 1 }}
        >
          {`${status.toUpperCase()}${chance === null ? '' : ` · ${chance}%`}`}
        </AppText>
      </View>
      {news ? (
        <AppText
          variant="meta"
          color={fg}
          style={{ textAlign: 'center' }}
          testID="card-status-news"
        >
          {news}
          {at ? (
            <AppText variant="meta" color={fg} testID="card-status-time">{` · ${at}`}</AppText>
          ) : null}
        </AppText>
      ) : null}
    </View>
  );
}

/**
 * The card's header, compact: photo and position·club beside the name and
 * stats; the actions row only when there are actions (otherwise the star
 * sits at the end of the stats).
 */
function Hero({
  d,
  actions,
  star,
  onClose,
}: {
  d: Detail;
  actions: ReactNode;
  star?: ReactNode;
  onClose?: () => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const { isWide } = useLayout();
  const { first, last } = splitName(d.player.displayName);
  const branded = useBrandedAssets();
  const pos = positionLabel(d.player.position);
  const club = d.player.club.code.toUpperCase();
  const owner = d.rosterStatus === 'rostered' && d.owner ? `→ ${d.owner.name}` : ownershipText(d);
  const latest = d.gameweeks.reduce<Detail['gameweeks'][number] | undefined>(
    (a, g) => (!a || g.gameweek > a.gameweek ? g : a),
    undefined,
  );
  const bio = [
    { label: 'Pts', value: String(d.totalPoints) },
    { label: 'Form', value: formText(d.form) },
    { label: 'Mins', value: String(d.minutes) },
    latest
      ? { label: `GW ${latest.gameweek}`, value: String(latest.points) }
      : { label: 'GW', value: MISSING },
  ];
  return (
    <View
      testID="card-hero"
      style={{
        backgroundColor: colors.surface,
        borderRadius: radii.lg,
        borderWidth: 1,
        borderColor: colors.border,
        overflow: 'hidden',
      }}
    >
      <StatusBanner player={d.player} />
      <View style={{ padding: spacing.lg, gap: spacing.lg }}>
        <View style={{ flexDirection: 'row', gap: spacing.lg }}>
          <View style={{ width: 88, alignItems: 'center', gap: spacing.sm }}>
            <View
              style={{
                width: 88,
                height: 88,
                borderRadius: radii.lg,
                backgroundColor: colors.surfaceSunken,
                alignItems: 'center',
                justifyContent: 'center',
                overflow: 'hidden',
              }}
            >
              <PlayerPhoto
                playerId={d.player.id}
                code={d.player.club.code}
                size={88}
                fallbackSize={64}
                placeholder={colors.surfaceSunken}
              />
              {branded ? null : (
                <View
                  aria-hidden
                  style={{
                    position: 'absolute',
                    right: spacing.xs,
                    bottom: spacing.xs,
                    minWidth: 26,
                    height: 20,
                    paddingHorizontal: spacing.xs,
                    borderRadius: radii.pill,
                    borderWidth: 1,
                    borderColor: colors.border,
                    backgroundColor: colors.surface,
                    alignItems: 'center',
                    justifyContent: 'center',
                  }}
                >
                  <AppText weight="bold" style={{ fontSize: 10, lineHeight: 12 }}>
                    {initialsOf(d.player.displayName)}
                  </AppText>
                </View>
              )}
            </View>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
              <ClubCrest code={d.player.club.code} size={12} />
              <AppText
                weight="bold"
                numberOfLines={1}
                color={colors.textMuted}
                style={{ fontSize: 12, lineHeight: 16, letterSpacing: 0.4 }}
                testID="card-meta"
                accessibilityLabel={`${pos}, ${d.player.club.name}`}
              >
                <AppText weight="bold" color={colors[positionColorKey(d.player.position)]}>
                  {pos}
                </AppText>
                {` • ${club}`}
              </AppText>
            </View>
          </View>
          <View style={{ flex: 1, minWidth: 0, gap: spacing.xs }}>
            <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
              <AppText
                variant="meta"
                muted
                numberOfLines={1}
                style={{ flex: 1 }}
                testID="card-owner"
                accessibilityLabel={ownershipText(d)}
              >
                {owner}
              </AppText>
              {onClose ? (
                <IconButton icon={X} label="Close" onPress={onClose} testID="sheet-close" />
              ) : null}
            </View>
            <View
              testID="card-name"
              accessible
              accessibilityRole="header"
              accessibilityLabel={d.player.displayName}
            >
              {first ? (
                <AppText
                  weight="semibold"
                  muted
                  numberOfLines={1}
                  style={{
                    fontSize: 13,
                    lineHeight: 16,
                    letterSpacing: 0.8,
                    textTransform: 'uppercase',
                  }}
                >
                  {/* The trailing space keeps "First Last" as one phrase in the text. */}
                  {`${first} `}
                </AppText>
              ) : null}
              <AppText
                weight="bold"
                numberOfLines={1}
                style={{ fontSize: 28, lineHeight: 34, textTransform: 'uppercase' }}
              >
                {last}
              </AppText>
            </View>
            <View
              testID="card-tiles"
              style={{
                flexDirection: 'row',
                flexWrap: 'wrap',
                columnGap: spacing.lg,
                rowGap: spacing.sm,
                marginTop: spacing.xs,
              }}
            >
              {bio.map((t) => (
                <View key={t.label} accessible accessibilityLabel={`${t.label}: ${t.value}`}>
                  <Caps>{t.label}</Caps>
                  <AppText numeric weight="semibold" style={{ fontSize: 20, lineHeight: 26 }}>
                    {t.value}
                  </AppText>
                </View>
              ))}
              {star ? (
                <View
                  testID="card-star-inline"
                  style={{ marginLeft: 'auto', justifyContent: 'center' }}
                >
                  {star}
                </View>
              ) : null}
            </View>
            {isWide && actions ? <View style={{ marginTop: spacing.sm }}>{actions}</View> : null}
          </View>
        </View>
        {isWide || !actions ? null : actions}
      </View>
    </View>
  );
}

/**
 * The card's actions (SLEEPER-OBSERVED): one wide pill (Sign, Propose
 * trade, or DROP for my own player) and a round star that adds the player
 * to my watchlist.
 */
function Actions({
  leagueId,
  d,
  action,
  onDrop,
  onReplace,
  onDone,
}: {
  leagueId: string;
  d: Detail;
  action: ReturnType<typeof playerAction>;
  onDrop: () => void;
  onReplace: () => void;
  onDone?: () => void;
}) {
  const { radii, spacing } = useTheme();
  const router = useRouter();
  const name = d.player.displayName;
  const pill = { flex: 1, borderRadius: radii.pill };
  let main: ReactNode = null;
  switch (action) {
    case 'add':
    case 'claim':
      // One verb for both: free agents are added at once, waiver players claimed.
      main = (
        <Button
          label="Sign"
          icon={UserPlus}
          onPress={onDrop}
          style={pill}
          accessibilityLabel={action === 'add' ? `Sign ${name}` : `Sign ${name}, a waiver claim`}
          testID={action === 'add' ? 'card-add' : 'card-claim'}
        />
      );
      break;
    case 'trade':
      main = (
        <Button
          label="Propose trade"
          icon={ArrowLeftRight}
          variant="secondary"
          style={pill}
          accessibilityLabel={`Propose a trade to ${d.owner!.name}`}
          onPress={() => {
            onDone?.();
            router.push(
              `/leagues/${leagueId}/players/trades/new?teamId=${d.owner!.id}&playerId=${d.player.id}`,
            );
          }}
          testID="card-trade"
        />
      );
      break;
    case 'mine':
      main = (
        <>
          <Button
            label="Drop"
            icon={UserMinus}
            variant="secondary"
            style={pill}
            accessibilityLabel={`Drop ${name}`}
            onPress={onReplace}
            testID="card-drop"
          />
          {d.owner ? <BlockToggle leagueId={leagueId} player={d.player} team={d.owner} /> : null}
        </>
      );
      break;
  }
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
      {main ?? <View style={{ flex: 1 }} />}
      <WatchButton leagueId={leagueId} player={d.player} />
    </View>
  );
}

/** The round star: on (filled, accent) while the player is on my watchlist. */
function WatchButton({ leagueId, player }: { leagueId: string; player: Detail['player'] }) {
  const { colors } = useTheme();
  const toast = useToast();
  const watched = useWatchedIds(leagueId).has(player.id);
  const toggle = useToggleWatch(leagueId);
  const label = watched
    ? `Remove ${player.displayName} from your watchlist`
    : `Add ${player.displayName} to your watchlist`;
  const fg = watched ? colors.accentBright : colors.textMuted;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={label}
      accessibilityState={{ selected: watched }}
      aria-pressed={watched}
      // Web tooltip.
      {...({ title: watched ? 'On your watchlist' : 'Add to watchlist' } as object)}
      onPress={() =>
        toggle.mutate(
          { player, watch: !watched },
          {
            onSuccess: () =>
              toast({
                message: watched
                  ? `Removed ${player.displayName} from your watchlist`
                  : `Added ${player.displayName} to your watchlist`,
                tone: 'success',
                // The star tapped lightly already; a rollback taps its own error.
                haptic: false,
              }),
            onError: (e) => toast({ message: describeError(e), tone: 'error', haptic: false }),
          },
        )
      }
      hitSlop={4}
      testID="card-watch"
      style={(s: PressState) => [
        {
          width: 44,
          height: 44,
          borderRadius: 22,
          borderWidth: 1.5,
          borderColor: watched ? colors.accent : colors.border,
          backgroundColor: watched ? tint(colors.accent, colors.surface) : colors.surfaceControl,
          alignItems: 'center',
          justifyContent: 'center',
        },
        ...pressFeedback(colors, s),
      ]}
    >
      <Star size={20} color={fg} fill={watched ? fg : 'none'} strokeWidth={2.2} aria-hidden />
    </Pressable>
  );
}

/** Summary: stat strip, next fixture, gameweek cards, then season totals by points line. */
function Summary({ d, current }: { d: Detail; current?: number }) {
  const { spacing } = useTheme();
  return (
    <View style={{ gap: spacing.xl }}>
      <StatStrip d={d} />
      <NextFixture d={d} />
      <GameweekStrip d={d} current={current} />
      <View style={{ gap: spacing.sm }}>
        <Caps>Season</Caps>
        <SeasonStats d={d} />
      </View>
      <View style={{ gap: spacing.sm }}>
        <Caps>Previous seasons</Caps>
        <CareerTable seasons={d.previousSeasons ?? []} />
      </View>
    </View>
  );
}

const sumStat = (d: Pick<Detail, 'gameweeks'>, key: keyof Schemas['PlayerGameStats']) =>
  d.gameweeks.reduce((s, g) => s + (g.stats[key] ?? 0), 0);

/**
 * Three groups of big numbers with tiny labels: rank (in position and in the
 * pre-draft list), points per gameweek (season and latest) and returns
 * (goals, assists, and clean sheets or saves where they score).
 */
export function StatStrip({
  d,
}: {
  d: Pick<Detail, 'player' | 'gameweeks' | 'totalPoints' | 'positionRank' | 'draftRank' | 'form'>;
}) {
  const { spacing } = useTheme();
  const pos = d.player.position;
  const avg = perGameweek(d);
  const groups: { label: string; items: { value: string; sub: string }[] }[] = [
    {
      label: 'Rank',
      items: [
        ...(d.positionRank === null
          ? []
          : [{ value: `#${d.positionRank}`, sub: positionLabel(pos) }]),
        { value: `#${d.draftRank}`, sub: 'Draft' },
      ],
    },
    {
      label: 'Pts / GW',
      items: [
        { value: avg === null ? MISSING : avg.toFixed(1), sub: 'Season' },
        { value: formText(d.form), sub: 'Form' },
      ],
    },
    {
      label: 'Returns',
      items: [
        { value: String(sumStat(d, 'goalsScored')), sub: 'G' },
        { value: String(sumStat(d, 'assists')), sub: 'A' },
        ...(pos === 'FWD' ? [] : [{ value: String(sumStat(d, 'cleanSheets')), sub: 'CS' }]),
        ...(pos === 'GKP' ? [{ value: String(sumStat(d, 'saves')), sub: 'SV' }] : []),
      ],
    },
  ];
  return (
    <View testID="card-strip" style={{ flexDirection: 'row' }}>
      {/* Three equal thirds, each centred in its own. */}
      {groups.map((g) => (
        <View
          key={g.label}
          testID={`card-strip-${g.label}`}
          style={{ flex: 1, flexBasis: 0, minWidth: 0, alignItems: 'center', gap: spacing.xs }}
        >
          <Caps>{g.label}</Caps>
          <View style={{ flexDirection: 'row', justifyContent: 'center', gap: spacing.sm }}>
            {g.items.map((i) => (
              <View
                key={i.sub}
                accessible
                accessibilityLabel={`${g.label}, ${i.sub}: ${i.value}`}
                style={{ alignItems: 'center' }}
              >
                <AppText numeric weight="semibold" style={{ fontSize: 20, lineHeight: 26 }}>
                  {i.value}
                </AppText>
                <Caps>{i.sub}</Caps>
              </View>
            ))}
          </View>
        </View>
      ))}
    </View>
  );
}

/** The next gameweek's fixtures in an outlined card with the GW chip notched into its top edge. */
export function NextFixture({ d }: { d: Pick<Detail, 'upcoming' | 'player'> }) {
  const { colors, spacing, radii } = useTheme();
  const rankOf = useDifficultyRank();
  const dated = scheduledFixtures(d.upcoming);
  if (dated.length === 0) return null;
  const gw = Math.min(...dated.map((f) => f.gameweek));
  const fx = dated.filter((f) => f.gameweek === gw).sort(byKickoff);
  const club = d.player.club.code.toUpperCase();
  return (
    <View style={{ gap: spacing.md }}>
      <Caps>Next fixture</Caps>
      <View
        testID="card-next"
        style={{
          marginTop: spacing.xs,
          borderWidth: 1,
          borderColor: colors.border,
          borderRadius: radii.lg,
          paddingTop: spacing.lg,
          paddingBottom: spacing.md,
          paddingHorizontal: spacing.lg,
          gap: spacing.sm,
        }}
      >
        <View
          testID="card-next-gw"
          style={{
            position: 'absolute',
            top: -11,
            left: spacing.md,
            height: 22,
            paddingHorizontal: spacing.sm,
            borderRadius: radii.pill,
            borderWidth: 1,
            borderColor: colors.border,
            backgroundColor: colors.surfaceRaised,
            justifyContent: 'center',
          }}
        >
          <AppText
            weight="bold"
            color={colors.accent}
            style={{ fontSize: 11, lineHeight: 14, letterSpacing: 0.8 }}
          >
            {`GW ${gw}`}
          </AppText>
        </View>
        {fx.map((f) => {
          const when = kickoffText(f);
          return (
            <View
              key={f.fixtureId}
              accessible
              accessibilityLabel={[
                `Gameweek ${gw}: ${fixtureA11y(f)}`,
                when,
                difficultyA11yOf(rankOf(d.player.position, f.opponent.code)),
              ]
                .filter(Boolean)
                .join(', ')}
              style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}
            >
              <ClubShirt code={d.player.club.code} size={22} />
              <AppText weight="semibold">{club}</AppText>
              <AppText muted>{f.isHome ? 'v' : '@'}</AppText>
              <ClubShirt code={f.opponent.code} size={22} />
              {/* The code, as on his side: a phone has no room for the name
                  beside the rank chip and the kickoff. */}
              <AppText weight="semibold">{f.opponent.code.toUpperCase()}</AppText>
              <FixtureRank position={d.player.position} opponent={f.opponent.code} />
              <View style={{ flex: 1 }} />
              <AppText variant="meta" muted numeric>
                {when}
              </AppText>
            </View>
          );
        })}
        {fx.length > 1 ? <Pill label="Double" tone="accent" /> : null}
      </View>
    </View>
  );
}

type Upcoming = Detail['upcoming'][number];

/** Fixtures in a gameweek (a moved one without a new gameweek has none yet). */
export const scheduledFixtures = (fx: readonly Upcoming[]) =>
  fx.filter((f): f is Upcoming & { gameweek: number } => f.gameweek !== null);

/** By kickoff; one without a time (TBC) last. */
const byKickoff = (a: Upcoming, b: Upcoming) =>
  (a.kickoffAt ? Date.parse(a.kickoffAt) : Infinity) -
  (b.kickoffAt ? Date.parse(b.kickoffAt) : Infinity);

/** The kickoff, `Moved` for a postponed fixture, `TBC` with no time yet. */
export function kickoffText(f: Pick<Upcoming, 'kickoffAt' | 'status'>): string {
  if (f.status === 'postponed') return 'Moved';
  return f.kickoffAt ? formatKickoff(Date.parse(f.kickoffAt)) : 'TBC';
}

const difficultyA11yOf = (rank: number | null) => (rank === null ? '' : difficultyA11y(rank));

/** Text colour of a fixture in a row or card: by result once played, muted before. */
function useFixtureColor() {
  const { colors } = useTheme();
  return (f: { homeScore?: number | null; awayScore?: number | null; isHome: boolean }) => {
    const r = fixtureResult(f);
    return r === 'win' ? colors.win : r === 'loss' ? colors.loss : colors.textMuted;
  };
}

/**
 * The season's current gameweek: the latest whose deadline has passed (the
 * league's current one until its deadline is the one before it).
 */
export function currentGameweekOf(
  gw: Pick<Schemas['Gameweek'], 'number' | 'deadlineAt'> | undefined,
  now = Date.now(),
): number | undefined {
  if (!gw) return undefined;
  return Date.parse(gw.deadlineAt) <= now ? gw.number : gw.number - 1;
}

/**
 * A horizontal strip of gameweek cards, one per gameweek from the first up
 * to and including the current one (fixture by result, points), newest
 * last and scrolled to it; never a future gameweek. Without a current
 * gameweek, up to the last one with a game under way or played.
 */
export function GameweekStrip({ d, current }: { d: Pick<Detail, 'gameweeks'>; current?: number }) {
  const { colors, spacing, radii } = useTheme();
  const toneOf = useFixtureColor();
  const scroller = useRef<ScrollView>(null);
  const rows = logRows(d.gameweeks, []);
  const shown =
    current === undefined
      ? rows.slice(0, rows.map((r) => r.kind).lastIndexOf('played') + 1)
      : rows.filter((r) => r.gameweek <= current);
  if (shown.length === 0) return null;
  return (
    <View style={{ gap: spacing.sm }}>
      <Caps>By gameweek</Caps>
      <ScrollView
        ref={scroller}
        horizontal
        onContentSizeChange={() => scroller.current?.scrollToEnd({ animated: false })}
        testID="card-gw-strip"
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={{ gap: spacing.sm }}
      >
        {shown.map((r) => {
          const fixtures: { key: string; label: string; color: string }[] =
            r.kind === 'played'
              ? r.gw.fixtures.map((f) => ({
                  key: f.fixtureId,
                  label: fixtureLabel(f),
                  color: toneOf(f),
                }))
              : r.kind === 'future'
                ? r.fixtures.map((f) => ({
                    key: f.fixtureId,
                    label: fixtureLabel(f),
                    color: colors.textMuted,
                  }))
                : [];
          const points = r.kind === 'played' ? String(r.gw.points) : MISSING;
          return (
            <View
              key={r.gameweek}
              testID={`gw-card-${r.gameweek}`}
              accessible
              accessibilityLabel={`Gameweek ${r.gameweek}: ${
                fixtures.length ? fixtures.map((f) => f.label).join(', ') : 'no fixture'
              }${r.kind === 'played' ? `, ${points} points` : ''}`}
              style={{
                width: 104,
                padding: spacing.sm,
                gap: spacing.xs,
                borderRadius: radii.md,
                borderWidth: 1,
                borderColor: colors.border,
                backgroundColor: colors.surface,
              }}
            >
              <AppText weight="bold" style={{ fontSize: 12, lineHeight: 16 }}>
                {`GW ${r.gameweek}`}
              </AppText>
              <View style={{ minHeight: 18 }}>
                {fixtures.length ? (
                  fixtures.map((f) => (
                    <AppText key={f.key} variant="meta" weight="semibold" color={f.color}>
                      {f.label}
                    </AppText>
                  ))
                ) : (
                  <AppText variant="meta" subtle>
                    Blank
                  </AppText>
                )}
              </View>
              <View
                style={{
                  flexDirection: 'row',
                  alignItems: 'baseline',
                  justifyContent: 'space-between',
                  paddingTop: spacing.xs,
                  borderTopWidth: 1,
                  borderTopColor: colors.border,
                }}
              >
                <Caps>Pts</Caps>
                <AppText
                  numeric
                  weight="semibold"
                  color={r.kind === 'played' ? colors.text : colors.textMuted}
                  style={{ fontSize: 18, lineHeight: 24 }}
                >
                  {points}
                </AppText>
              </View>
            </View>
          );
        })}
      </ScrollView>
    </View>
  );
}

/** Yellow and red cards as small coloured cards; a dimmed 0 for none. */
export function CardsGlyphs({ cell }: { cell: Extract<StatCell, { kind: 'cards' }> }) {
  const { colors } = useTheme();
  const { yellow, red } = cell.cards;
  if (yellow === null && red === null) {
    return (
      <AppText variant="meta" numeric subtle>
        {MISSING}
      </AppText>
    );
  }
  const glyphs = [
    ...Array<string>(yellow ?? 0).fill(colors.warning),
    ...Array<string>(red ?? 0).fill(colors.loss),
  ];
  if (glyphs.length === 0) {
    return (
      <AppText variant="meta" numeric subtle accessibilityLabel="No cards">
        0
      </AppText>
    );
  }
  return (
    <View
      accessibilityLabel={cell.text}
      accessibilityRole="image"
      testID="log-cards"
      style={{ flexDirection: 'row', gap: 3, justifyContent: 'flex-end' }}
    >
      {glyphs.map((color, i) => (
        <View
          key={i}
          testID={color === colors.loss ? 'card-red' : 'card-yellow'}
          style={{ width: 9, height: 12, borderRadius: 2, backgroundColor: color }}
        />
      ))}
    </View>
  );
}

function StatValue({ cell, align = 'right' }: { cell: StatCell; align?: 'right' | 'left' }) {
  const { colors } = useTheme();
  if (cell.kind === 'cards') return <CardsGlyphs cell={cell} />;
  return (
    <AppText
      variant="meta"
      numeric
      color={cell.zero || cell.text === MISSING ? colors.textSubtle : colors.text}
      style={{ textAlign: align }}
    >
      {cell.text}
    </AppText>
  );
}

const GW_W = 40;
const OPP_W = 72;
const TILE_H = 28;
const TILE_GAP = 4;
const GROUP_GAP = 12;
/** Decimals and four-letter labels get a wider tile. */
const tileWidth = (c: HeatColumn) => (c.format || c.label.length > 3 ? 46 : 38);
const groupWidth = (g: HeatGroup) =>
  g.columns.reduce((w, c) => w + tileWidth(c), 0) + TILE_GAP * (g.columns.length - 1);

/** One heat-map cell: a small rounded tile tinted by quality. */
function HeatTile({
  text,
  quality,
  width,
  testID,
}: {
  text: string;
  quality: HeatQuality;
  width: number;
  testID?: string;
}) {
  const { colors } = useTheme();
  const { bg, fg } = heatColors(quality, colors);
  return (
    <View
      testID={testID}
      style={{
        width,
        height: TILE_H,
        borderRadius: 6,
        backgroundColor: bg,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <AppText
        numeric
        weight="semibold"
        numberOfLines={1}
        color={quality === 'neutral' && Number(text) === 0 ? colors.textMuted : fg}
        style={{ fontSize: 12, lineHeight: 16 }}
      >
        {text}
      </AppText>
    </View>
  );
}

type GameLogData = Pick<Detail, 'gameweeks' | 'player'> & { upcoming?: Detail['upcoming'] };

/**
 * The game log as a heat map: one row per gameweek, oldest first, then the
 * next few still to play (dashes on dark tiles); blank gameweeks greyed.
 * GW and opponent stay pinned on the left while the stat groups scroll
 * sideways. Rows start collapsed; a tap on a played row opens every stat
 * (and what scored for the position) beneath it, one row at a time.
 */
export function GameLog({ d }: { d: GameLogData }) {
  const { colors, spacing, radii } = useTheme();
  const [open, setOpen] = useState<number | null>(null);
  const [viewport, setViewport] = useState(0);
  const [scrollX] = useState(() => new Animated.Value(0));
  // The table's own sideways scroll wins over the card's tab swipe.
  const nested = useNestedScroll();
  const position = d.player.position;
  const groups = heatGroups(position);
  const rows = logRows(d.gameweeks, d.upcoming ?? []);
  if (rows.length === 0) return <AppText muted>No games yet.</AppText>;
  const pad = spacing.sm;
  // Pinned cells ride along with the scroll, so they stay put on screen. They
  // are opaque and reach back over the table's left padding (and a gap's
  // width past their right edge), so nothing scrolled shows left of or
  // under them.
  const pinned = pinnedStyle(colors.surface, colors.border, pad, scrollX);
  const onScroll = Animated.event([{ nativeEvent: { contentOffset: { x: scrollX } } }], {
    useNativeDriver: Platform.OS !== 'web',
  });
  return (
    <View
      testID="card-log"
      style={{
        borderRadius: radii.lg,
        borderWidth: 1,
        borderColor: colors.border,
        backgroundColor: colors.surface,
        overflow: 'hidden',
      }}
    >
      <Animated.ScrollView
        horizontal
        testID="card-log-scroll"
        {...nested}
        onScroll={onScroll}
        scrollEventThrottle={16}
        onLayout={(e) => setViewport(e.nativeEvent.layout.width)}
        contentContainerStyle={{ paddingVertical: spacing.md, paddingHorizontal: pad }}
      >
        <View style={{ gap: TILE_GAP }}>
          <View style={{ flexDirection: 'row' }} aria-hidden>
            <Animated.View
              testID="log-pinned-head"
              style={[pinned, { width: GW_W + OPP_W + pad + GROUP_GAP, alignSelf: 'stretch' }]}
            />
            {groups.map((g) => (
              <View
                key={g.id}
                testID={`log-group-${g.id}`}
                style={{
                  width: groupWidth(g),
                  marginLeft: GROUP_GAP,
                  alignItems: 'center',
                  paddingBottom: 2,
                  borderBottomWidth: 1,
                  borderBottomColor: colors.border,
                }}
              >
                {/* Tracked capitals end in a letter space: pad the start to match, so the label centres. */}
                <View style={{ paddingLeft: 0.8 }}>
                  <Caps color={colors.text}>{g.label}</Caps>
                </View>
              </View>
            ))}
          </View>
          <View style={{ flexDirection: 'row', alignItems: 'center' }}>
            <Animated.View style={pinned}>
              <HeadCell label="GW" width={GW_W} />
              <HeadCell label="OPP" title="Opponent" width={OPP_W} />
            </Animated.View>
            {groups.map((g) => (
              <View
                key={g.id}
                style={{ flexDirection: 'row', gap: TILE_GAP, marginLeft: GROUP_GAP }}
              >
                {g.columns.map((c) => (
                  <HeadCell
                    key={c.key}
                    label={c.label}
                    title={c.title}
                    width={tileWidth(c)}
                    center
                    testID={`log-head-${c.key}`}
                  />
                ))}
              </View>
            ))}
          </View>
          {rows.map((r) =>
            r.kind === 'played' ? (
              <PlayedRow
                key={r.gameweek}
                g={r.gw}
                position={position}
                groups={groups}
                pinned={pinned}
                open={open === r.gameweek}
                onToggle={() => setOpen(open === r.gameweek ? null : r.gameweek)}
                detailWidth={viewport ? viewport - pad * 2 : undefined}
                scrollX={scrollX}
              />
            ) : (
              <UnplayedRow key={r.gameweek} row={r} groups={groups} pinned={pinned} />
            ),
          )}
        </View>
      </Animated.ScrollView>
    </View>
  );
}

type Pinned = Record<string, unknown>;

/**
 * The pinned GW and OPP block's style: the card's background, from the
 * table's left edge (over its padding) to a divider one group gap past OPP.
 */
export function pinnedStyle(bg: string, divider: string, pad: number, scrollX: Animated.Value) {
  return {
    flexDirection: 'row' as const,
    alignItems: 'center' as const,
    zIndex: 1,
    elevation: 1,
    backgroundColor: bg,
    marginLeft: -pad,
    paddingLeft: pad,
    marginRight: -GROUP_GAP,
    paddingRight: GROUP_GAP - 1,
    borderRightWidth: 1,
    borderRightColor: divider,
    transform: [{ translateX: scrollX }],
  };
}

function HeadCell({
  label,
  title,
  width,
  center,
  testID,
}: {
  label: string;
  title?: string;
  width: number;
  center?: boolean;
  testID?: string;
}) {
  return (
    <View
      testID={testID}
      aria-label={title}
      // Web tooltip with the full stat name.
      {...(title ? ({ title } as object) : null)}
      style={{ width, alignItems: center ? 'center' : 'flex-start' }}
    >
      <Caps>{label}</Caps>
    </View>
  );
}

function PlayedRow({
  g,
  position,
  groups,
  pinned,
  open,
  onToggle,
  detailWidth,
  scrollX,
}: {
  g: Detail['gameweeks'][number];
  position: string;
  groups: HeatGroup[];
  pinned: Pinned;
  open: boolean;
  onToggle: () => void;
  detailWidth: number | undefined;
  scrollX: Animated.Value;
}) {
  const { colors, spacing, radii } = useTheme();
  const toneOf = useFixtureColor();
  const chips = eventChips(position, g.stats);
  return (
    <View>
      <Pressable
        accessibilityRole="button"
        accessibilityState={{ expanded: open }}
        aria-expanded={open}
        accessibilityLabel={`Gameweek ${g.gameweek}, ${opponentLabel(g.fixtures)}, ${g.points} points`}
        onPress={onToggle}
        testID={`log-row-${g.gameweek}`}
        style={(s: PressState) => [
          { flexDirection: 'row', alignItems: 'center', borderRadius: radii.sm },
          ...pressFeedback(colors, s, { hoverFill: true }),
        ]}
      >
        <Animated.View style={[pinned, { alignSelf: 'stretch' }]}>
          <View
            style={{ width: GW_W, flexDirection: 'row', alignItems: 'center', gap: spacing.xxs }}
          >
            <AppText variant="meta" weight="semibold" numeric>
              {String(g.gameweek)}
            </AppText>
            <View style={{ transform: [{ rotate: open ? '180deg' : '0deg' }] }}>
              <ChevronDown size={12} color={colors.textMuted} aria-hidden />
            </View>
          </View>
          <View style={{ width: OPP_W }}>
            {g.fixtures.map((f) => (
              <AppText
                key={f.fixtureId}
                variant="meta"
                weight="bold"
                numberOfLines={1}
                color={toneOf(f)}
              >
                {fixtureLabel(f)}
              </AppText>
            ))}
          </View>
        </Animated.View>
        {groups.map((gr) => (
          <View key={gr.id} style={{ flexDirection: 'row', gap: TILE_GAP, marginLeft: GROUP_GAP }}>
            {gr.columns.map((c) => {
              const cell = heatCell(g, c, position);
              return (
                <HeatTile
                  key={c.key}
                  testID={`log-${c.key}`}
                  text={cell.text}
                  quality={cell.quality}
                  width={tileWidth(c)}
                />
              );
            })}
          </View>
        ))}
      </Pressable>
      {open ? (
        <Animated.View
          testID={`log-detail-${g.gameweek}`}
          style={{
            width: detailWidth,
            transform: [{ translateX: scrollX }],
            paddingTop: spacing.sm,
            paddingBottom: spacing.md,
            gap: spacing.md,
          }}
        >
          {chips.length ? (
            <View
              testID={`log-chips-${g.gameweek}`}
              style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}
            >
              {chips.map((c) => (
                <EventChipView key={c.text} chip={c} />
              ))}
            </View>
          ) : null}
          {detailSections(g).map((section) => (
            <View
              key={section.group}
              testID={`log-detail-${section.group}`}
              style={{
                backgroundColor: colors.surfaceRaised,
                borderRadius: radii.lg,
                padding: spacing.md,
                gap: spacing.sm,
              }}
            >
              <AppText variant="eyebrow" muted accessibilityRole="header">
                {section.title}
              </AppText>
              <View style={{ flexDirection: 'row', flexWrap: 'wrap', rowGap: spacing.xs }}>
                {section.stats.map((st) => (
                  <View
                    key={st.key}
                    accessibilityLabel={`${st.label}: ${st.text}`}
                    style={{
                      width: '50%',
                      flexDirection: 'row',
                      justifyContent: 'space-between',
                      gap: spacing.sm,
                      paddingRight: spacing.lg,
                    }}
                  >
                    <AppText variant="meta" muted numberOfLines={1} style={{ flexShrink: 1 }}>
                      {st.label}
                    </AppText>
                    <StatValue cell={st} />
                  </View>
                ))}
              </View>
            </View>
          ))}
        </Animated.View>
      ) : null}
    </View>
  );
}

/** A blank gameweek (greyed, `Blank`) or one still to play: dashes on dark tiles. */
function UnplayedRow({
  row,
  groups,
  pinned,
}: {
  row: Exclude<LogRow, { kind: 'played' }>;
  groups: HeatGroup[];
  pinned: Pinned;
}) {
  const { colors } = useTheme();
  const fixtures: FutureFixture[] = row.kind === 'future' ? row.fixtures : [];
  const blank = row.kind === 'blank';
  return (
    <View
      testID={blank ? `log-blank-${row.gameweek}` : `log-next-${row.gameweek}`}
      accessible
      accessibilityLabel={`Gameweek ${row.gameweek}, ${
        blank ? 'no fixture' : `${fixtures.map((f) => fixtureA11y(f)).join(', ')}, not played yet`
      }`}
      style={{ flexDirection: 'row', alignItems: 'center' }}
    >
      <Animated.View style={[pinned, { alignSelf: 'stretch' }]}>
        <View style={{ width: GW_W }}>
          <AppText variant="meta" numeric color={blank ? colors.textSubtle : colors.textMuted}>
            {String(row.gameweek)}
          </AppText>
        </View>
        <View style={{ width: OPP_W }}>
          {blank ? (
            <AppText variant="meta" weight="semibold" subtle>
              Blank
            </AppText>
          ) : (
            fixtures.map((f) => (
              <AppText key={f.fixtureId} variant="meta" weight="semibold" muted numberOfLines={1}>
                {fixtureLabel(f)}
              </AppText>
            ))
          )}
        </View>
      </Animated.View>
      {groups.map((gr) => (
        <View key={gr.id} style={{ flexDirection: 'row', gap: TILE_GAP, marginLeft: GROUP_GAP }}>
          {gr.columns.map((c) => (
            <HeatTile key={c.key} text={MISSING} quality="none" width={tileWidth(c)} />
          ))}
        </View>
      ))}
    </View>
  );
}

function EventChipView({ chip }: { chip: EventChip }) {
  const { colors, spacing, radii } = useTheme();
  const fg =
    chip.tone === 'yellow' ? colors.warning : chip.tone === 'red' ? colors.loss : colors.text;
  return (
    <View
      testID={`chip-${chip.tone}`}
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.xs,
        paddingHorizontal: spacing.sm,
        paddingVertical: 2,
        borderRadius: radii.pill,
        backgroundColor: chip.tone === 'neutral' ? colors.surfaceRaised : tint(fg, colors.surface),
      }}
    >
      {chip.tone === 'neutral' ? null : (
        <View
          aria-hidden
          style={{ width: 7, height: 10, borderRadius: 1.5, backgroundColor: fg }}
        />
      )}
      <AppText variant="meta" color={fg}>
        {chip.text}
      </AppText>
    </View>
  );
}

/**
 * Season totals by points line: what happened (middle column) and the points
 * it earned (right, headed "Pts"). Minutes read as appearances, since the
 * points come from playing at all, with the minutes as secondary text.
 */
export function SeasonStats({
  d,
}: {
  d: Pick<Detail, 'gameweeks'> & Partial<Pick<Detail, 'starts'>>;
}) {
  const { colors, spacing, radii } = useTheme();
  const totals = seasonStatTotals(d.gameweeks);
  // Games started, beside the minutes; left out when the source sent none.
  const starts =
    d.starts === null || d.starts === undefined
      ? null
      : `${d.starts} ${d.starts === 1 ? 'start' : 'starts'}`;
  if (totals.length === 0) return <AppText muted>No minutes yet this season.</AppText>;
  const cells = (label: ReactNode, value: ReactNode, points: ReactNode) => (
    <>
      <View style={{ flex: 1, flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm }}>
        {label}
      </View>
      <View style={{ width: 48, alignItems: 'flex-end' }}>{value}</View>
      <View style={{ width: 40, alignItems: 'flex-end' }}>{points}</View>
    </>
  );
  const rowStyle = {
    flexDirection: 'row',
    alignItems: 'center',
    gap: spacing.sm,
    paddingHorizontal: spacing.md,
    paddingVertical: spacing.sm,
  } as const;
  return (
    <View
      testID="card-season"
      style={{ borderRadius: radii.md, borderWidth: 1, borderColor: colors.border }}
    >
      <View testID="card-season-head" style={[rowStyle, { paddingBottom: spacing.xs }]} aria-hidden>
        {cells(
          null,
          null,
          <AppText variant="eyebrow" subtle>
            Pts
          </AppText>,
        )}
      </View>
      {totals.map((t) => {
        const apps = t.stat === 'minutes';
        // Bonus is its own points: the count would repeat them.
        const pointsOnly = t.stat === 'bonus';
        const label = apps ? 'Appearances' : statLabel(t.stat);
        const value = apps ? t.fixtures : t.value;
        return (
          <View
            key={t.stat}
            testID={`season-row-${t.stat}`}
            accessible
            accessibilityLabel={
              pointsOnly
                ? `${label}: ${t.points} points`
                : `${label}: ${value}${apps ? `, ${t.value} minutes${starts ? `, ${starts}` : ''}` : ''}, ${t.points} points`
            }
            style={[rowStyle, { borderTopWidth: 1, borderTopColor: colors.border }]}
          >
            {cells(
              <>
                <AppText variant="meta" style={{ flexShrink: 1 }}>
                  {label}
                </AppText>
                {apps ? (
                  <AppText variant="meta" subtle numeric>
                    {starts ? `${t.value} min · ${starts}` : `${t.value} min`}
                  </AppText>
                ) : null}
              </>,
              pointsOnly ? null : (
                <AppText variant="meta" muted numeric style={{ textAlign: 'right' }}>
                  {String(value)}
                </AppText>
              ),
              <AppText variant="meta" numeric weight="semibold" style={{ textAlign: 'right' }}>
                {t.points > 0 ? `+${t.points}` : String(t.points)}
              </AppText>,
            )}
          </View>
        );
      })}
    </View>
  );
}

const SEASON_W = 64;
const CAREER_W = 44;

/**
 * Previous Premier League seasons (FPL history_past), newest first, then
 * the career row. Like the game log, the season column stays pinned while
 * the stats scroll sideways. "—" is a stat FPL did not track that season;
 * the career row adds only the seasons that tracked it (lib/career.ts).
 */
export function CareerTable({ seasons }: { seasons: readonly PastSeason[] }) {
  const { colors, spacing, radii } = useTheme();
  const [scrollX] = useState(() => new Animated.Value(0));
  const nested = useNestedScroll();
  if (seasons.length === 0) {
    return (
      <AppText muted testID="career-empty">
        No previous Premier League seasons.
      </AppText>
    );
  }
  const rows = careerRows(seasons);
  const totals = careerTotals(seasons);
  const pad = spacing.sm;
  const pinned = pinnedStyle(colors.surface, colors.border, pad, scrollX);
  const onScroll = Animated.event([{ nativeEvent: { contentOffset: { x: scrollX } } }], {
    useNativeDriver: Platform.OS !== 'web',
  });
  const line = (
    testID: string,
    label: string,
    value: (c: CareerColumn) => string,
    opts: { head?: boolean; total?: boolean } = {},
  ) => (
    <View
      key={testID}
      testID={testID}
      accessible={!opts.head}
      aria-hidden={opts.head}
      accessibilityLabel={
        opts.head
          ? undefined
          : `${label}: ${CAREER_COLUMNS.map((c) => `${c.title} ${value(c)}`).join(', ')}`
      }
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        paddingVertical: spacing.xs,
        ...(opts.total ? { borderTopWidth: 1, borderTopColor: colors.border } : null),
      }}
    >
      <Animated.View style={[pinned, { alignSelf: 'stretch' }]}>
        <View style={{ width: SEASON_W }}>
          {opts.head ? (
            <Caps>Season</Caps>
          ) : (
            <AppText variant="meta" weight="semibold" numeric>
              {label}
            </AppText>
          )}
        </View>
      </Animated.View>
      <View style={{ flexDirection: 'row', gap: TILE_GAP, marginLeft: GROUP_GAP }}>
        {CAREER_COLUMNS.map((c) =>
          opts.head ? (
            <HeadCell
              key={c.key}
              label={c.label}
              title={c.title}
              width={CAREER_W}
              center
              testID={`career-head-${c.key}`}
            />
          ) : (
            <View key={c.key} style={{ width: CAREER_W, alignItems: 'center' }}>
              <AppText
                variant="meta"
                numeric
                muted={value(c) === DASH}
                weight={opts.total || c.key === 'points' ? 'semibold' : undefined}
                testID={`${testID}-${c.key}`}
              >
                {value(c)}
              </AppText>
            </View>
          ),
        )}
      </View>
    </View>
  );
  return (
    <View style={{ gap: spacing.xs }}>
      <View
        testID="card-career"
        style={{
          borderRadius: radii.lg,
          borderWidth: 1,
          borderColor: colors.border,
          backgroundColor: colors.surface,
          overflow: 'hidden',
        }}
      >
        <Animated.ScrollView
          horizontal
          testID="card-career-scroll"
          {...nested}
          onScroll={onScroll}
          scrollEventThrottle={16}
          contentContainerStyle={{ paddingVertical: spacing.sm, paddingHorizontal: pad }}
        >
          <View>
            {line('career-head', '', () => '', { head: true })}
            {rows.map((s) =>
              line(`career-row-${s.season}`, s.season, (c) => careerCell(s[c.key], c)),
            )}
            {line('career-row-total', 'Career', (c) => careerCell(totals[c.key], c), {
              total: true,
            })}
          </View>
        </Animated.ScrollView>
      </View>
      <AppText variant="meta" subtle testID="career-note">
        A: FPL assists, which FPL awards more often than Opta. —: not tracked that season.
      </AppText>
    </View>
  );
}

/**
 * Every remaining fixture of the player's club this season, by gameweek
 * from the next to the last: two in a double, `Blank` in a blank. A
 * postponed fixture reads `Moved`; one without a time `TBC`; fixtures
 * without a gameweek yet come last, under `TBC`. Always the latest
 * fixture data (the query refetches on fixture updates).
 */
export function Fixtures({ d }: { d: Pick<Detail, 'upcoming' | 'player'> }) {
  const { colors, spacing, radii } = useTheme();
  if (d.upcoming.length === 0) return <AppText muted>No fixtures scheduled.</AppText>;
  const dated = scheduledFixtures(d.upcoming);
  const unscheduled = d.upcoming.filter((f) => f.gameweek === null);
  const nums = dated.map((f) => f.gameweek);
  const first = nums.length ? Math.min(...nums) : 0;
  const last = nums.length ? Math.max(...nums) : -1;
  const weeks: { key: string; label: string; fx: Upcoming[] }[] = Array.from(
    { length: last - first + 1 },
    (_, i) => first + i,
  ).map((gw) => ({
    key: String(gw),
    label: `GW ${gw}`,
    fx: dated.filter((f) => f.gameweek === gw).sort(byKickoff),
  }));
  if (unscheduled.length) weeks.push({ key: 'tbc', label: 'TBC', fx: unscheduled });
  return (
    <View
      testID="card-upcoming"
      style={{ borderRadius: radii.md, borderWidth: 1, borderColor: colors.border }}
    >
      {weeks.map((w, i) => (
        <View
          key={w.key}
          testID={`card-gw-${w.key}`}
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            gap: spacing.sm,
            paddingHorizontal: spacing.md,
            paddingVertical: spacing.sm,
            borderTopWidth: i > 0 ? 1 : 0,
            borderTopColor: colors.border,
          }}
        >
          <AppText variant="meta" muted numeric style={{ width: 48 }}>
            {w.label}
          </AppText>
          <View style={{ flex: 1, flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}>
            {w.fx.length === 0 ? (
              <AppText variant="meta" subtle>
                — Blank
              </AppText>
            ) : (
              w.fx.map((f) => (
                <View
                  key={f.fixtureId}
                  testID={`card-fixture-${f.fixtureId}`}
                  style={{
                    flexDirection: 'row',
                    alignItems: 'center',
                    gap: 4,
                    paddingHorizontal: spacing.sm,
                    paddingVertical: 2,
                    borderRadius: radii.sm,
                    backgroundColor: colors.surfaceRaised,
                  }}
                >
                  <AppText variant="meta">{`${fixtureLabel(f)} · ${kickoffText(f)}`}</AppText>
                  <FixtureRank position={d.player.position} opponent={f.opponent.code} />
                </View>
              ))
            )}
            {w.key !== 'tbc' && w.fx.length > 1 ? <Pill label="Double" tone="accent" /> : null}
          </View>
        </View>
      ))}
    </View>
  );
}

/** Pick the same-position player to drop, then add or claim. */
function DropStep({
  leagueId,
  player,
  myTeamId,
  kind,
  onBack,
  onDone,
}: {
  leagueId: string;
  player: Detail;
  myTeamId: string;
  kind: 'add' | 'claim';
  onBack: () => void;
  onDone?: () => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const toast = useToast();
  const router = useRouter();
  const team = useTeam(leagueId, myTeamId);
  const add = useAddFreeAgent(leagueId);
  const claim = useCreateClaim(leagueId);
  const [drop, setDrop] = useState<string | null>(null);
  const mutation = kind === 'add' ? add : claim;
  const submit = () => {
    if (!drop) return;
    const body = { addPlayerId: player.player.id, dropPlayerId: drop };
    const dropped = team.data?.data.roster.find((r) => r.player.id === drop)?.player.displayName;
    const name = player.player.displayName;
    // Optimistic: the squad and lists show the move at once, so the card
    // closes now; the toast confirms (or the rollback explains) later.
    if (kind === 'add') {
      add.mutateAsync(body).then(
        () =>
          toast({
            message: `Added ${name}, dropped ${dropped ?? 'a player'}`,
            tone: 'success',
            haptic: false,
          }),
        (e: unknown) => toast({ message: moveErrorText(e), tone: 'error', haptic: false }),
      );
    } else {
      claim.mutateAsync(body).then(
        (envelope) =>
          toast({
            message: `Claim placed for ${name} (your #${envelope.data.order})`,
            tone: 'success',
            haptic: false,
          }),
        (e: unknown) => toast({ message: moveErrorText(e), tone: 'error', haptic: false }),
      );
      router.push(`/leagues/${leagueId}/players/waivers`);
    }
    onDone?.();
  };
  return (
    <View style={{ gap: spacing.md }} testID="drop-step">
      <AppText variant="heading">
        {kind === 'add' ? `Add ${player.player.displayName}` : `Claim ${player.player.displayName}`}
      </AppText>
      <AppText variant="meta" muted>
        {`Squads stay 2 GK, 5 DEF, 5 MID, 3 FWD, so drop a ${positionLabel(player.player.position)}.${
          kind === 'claim' ? ' Claims run in waiver priority order at the next waiver run.' : ''
        }`}
      </AppText>
      <SectionHeader title="Drop" />
      <QueryStatus query={team}>
        {({ data }) => (
          <View
            accessibilityRole="radiogroup"
            accessibilityLabel="Player to drop"
            style={{ borderRadius: radii.md, borderWidth: 1, borderColor: colors.border }}
          >
            {dropCandidates(data.roster, player.player.position).map((r, i) => {
              const selected = drop === r.player.id;
              return (
                <Pressable
                  key={r.player.id}
                  accessibilityRole="radio"
                  accessibilityState={{ checked: selected }}
                  accessibilityLabel={`${r.player.displayName}, ${r.totalPoints} points this season`}
                  onPress={() => setDrop(r.player.id)}
                  testID={`drop-${r.player.id}`}
                  style={(s: PressState) => [
                    {
                      minHeight: 52,
                      flexDirection: 'row',
                      alignItems: 'center',
                      gap: spacing.md,
                      paddingHorizontal: spacing.md,
                      borderTopWidth: i > 0 ? 1 : 0,
                      borderTopColor: colors.border,
                      backgroundColor: selected
                        ? tint(colors.accent, colors.surface)
                        : 'transparent',
                    },
                    ...pressFeedback(colors, s, { hoverFill: !selected }),
                  ]}
                >
                  <View
                    aria-hidden
                    style={{
                      width: 20,
                      height: 20,
                      borderRadius: 10,
                      borderWidth: 2,
                      borderColor: selected ? colors.accent : colors.textMuted,
                      alignItems: 'center',
                      justifyContent: 'center',
                    }}
                  >
                    {selected ? (
                      <View
                        style={{
                          width: 10,
                          height: 10,
                          borderRadius: 5,
                          backgroundColor: colors.accent,
                        }}
                      />
                    ) : null}
                  </View>
                  <View style={{ flex: 1, minWidth: 0 }}>
                    <AppText weight="semibold" numberOfLines={1}>
                      {r.player.displayName}
                    </AppText>
                    <ClubTag code={r.player.club.code} name={r.player.club.name} />
                  </View>
                  <AppText numeric muted>{`${r.totalPoints} pts`}</AppText>
                </Pressable>
              );
            })}
          </View>
        )}
      </QueryStatus>
      {mutation.isError ? (
        <AppText color={colors.loss} role="alert" testID="drop-error">
          {describeError(mutation.error)}
        </AppText>
      ) : null}
      <View style={{ flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' }}>
        <Button label="Back" variant="secondary" onPress={onBack} />
        <Button
          label={kind === 'add' ? 'Add and drop' : 'Place claim'}
          onPress={submit}
          disabled={!drop}
          loading={mutation.isPending}
          testID="drop-confirm"
          style={{ flex: 1 }}
        />
      </View>
    </View>
  );
}

/**
 * DROP on my own player: squads stay 2/5/5/3, so dropping him means picking
 * an available player of his position to take his place. The same moves as
 * Sign: a free agent joins at once (Add and drop), a player on waivers is a
 * claim for the next run.
 */
function ReplaceStep({
  leagueId,
  player,
  onBack,
  onDone,
}: {
  leagueId: string;
  player: Detail;
  onBack: () => void;
  onDone?: () => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const toast = useToast();
  const router = useRouter();
  const options = useLeaguePlayers(leagueId, {
    status: 'available',
    position: player.player.position,
    sort: 'form',
    limit: 20,
  });
  const add = useAddFreeAgent(leagueId);
  const claim = useCreateClaim(leagueId);
  const [pick, setPick] = useState<Schemas['PlayerListItem'] | null>(null);
  const isClaim = pick?.rosterStatus === 'waivers';
  const mutation = isClaim ? claim : add;
  const name = player.player.displayName;
  const submit = () => {
    if (!pick) return;
    const body = { addPlayerId: pick.player.id, dropPlayerId: player.player.id };
    const picked = pick.player.displayName;
    if (isClaim) {
      claim.mutateAsync(body).then(
        (envelope) =>
          toast({
            message: `Claim placed for ${picked} (your #${envelope.data.order})`,
            tone: 'success',
            haptic: false,
          }),
        (e: unknown) => toast({ message: moveErrorText(e), tone: 'error', haptic: false }),
      );
      router.push(`/leagues/${leagueId}/players/waivers`);
    } else {
      add.mutateAsync(body).then(
        () =>
          toast({ message: `Added ${picked}, dropped ${name}`, tone: 'success', haptic: false }),
        (e: unknown) => toast({ message: moveErrorText(e), tone: 'error', haptic: false }),
      );
    }
    onDone?.();
  };
  const items = options.data?.pages.flatMap((p) => p.data) ?? [];
  return (
    <View style={{ gap: spacing.md }} testID="replace-step">
      <AppText variant="heading">{`Drop ${name}`}</AppText>
      <AppText variant="meta" muted>
        {`Squads stay 2 GK, 5 DEF, 5 MID, 3 FWD, so pick a ${positionLabel(
          player.player.position,
        )} to replace them. Free agents join at once; a player on waivers is a claim for the next run.`}
      </AppText>
      <SectionHeader title="Replace with" />
      <QueryStatus query={{ ...options, data: options.data?.pages[0] }}>
        {() =>
          items.length === 0 ? (
            <AppText muted testID="replace-empty">
              {`No ${positionLabel(player.player.position)} is available right now.`}
            </AppText>
          ) : (
            <View
              accessibilityRole="radiogroup"
              accessibilityLabel="Player to add"
              style={{ borderRadius: radii.md, borderWidth: 1, borderColor: colors.border }}
            >
              {items.map((r, i) => {
                const selected = pick?.player.id === r.player.id;
                const waivers = r.rosterStatus === 'waivers';
                return (
                  <Pressable
                    key={r.player.id}
                    accessibilityRole="radio"
                    accessibilityState={{ checked: selected }}
                    accessibilityLabel={`${r.player.displayName}${waivers ? ', on waivers' : ', free agent'}, form ${formText(r.form)}`}
                    onPress={() => setPick(r)}
                    testID={`replace-${r.player.id}`}
                    style={(s: PressState) => [
                      {
                        minHeight: 52,
                        flexDirection: 'row',
                        alignItems: 'center',
                        gap: spacing.md,
                        paddingHorizontal: spacing.md,
                        borderTopWidth: i > 0 ? 1 : 0,
                        borderTopColor: colors.border,
                        backgroundColor: selected
                          ? tint(colors.accent, colors.surface)
                          : 'transparent',
                      },
                      ...pressFeedback(colors, s, { hoverFill: !selected }),
                    ]}
                  >
                    <View
                      aria-hidden
                      style={{
                        width: 20,
                        height: 20,
                        borderRadius: 10,
                        borderWidth: 2,
                        borderColor: selected ? colors.accent : colors.textMuted,
                        alignItems: 'center',
                        justifyContent: 'center',
                      }}
                    >
                      {selected ? (
                        <View
                          style={{
                            width: 10,
                            height: 10,
                            borderRadius: 5,
                            backgroundColor: colors.accent,
                          }}
                        />
                      ) : null}
                    </View>
                    <View style={{ flex: 1, minWidth: 0 }}>
                      <AppText weight="semibold" numberOfLines={1}>
                        {r.player.displayName}
                      </AppText>
                      <ClubTag code={r.player.club.code} name={r.player.club.name} />
                    </View>
                    {waivers ? <Pill label="Waivers" tone="warning" /> : null}
                    <AppText numeric muted>{`${formText(r.form)} form`}</AppText>
                  </Pressable>
                );
              })}
            </View>
          )
        }
      </QueryStatus>
      {mutation.isError ? (
        <AppText color={colors.loss} role="alert" testID="replace-error">
          {describeError(mutation.error)}
        </AppText>
      ) : null}
      <View style={{ flexDirection: 'row', gap: spacing.sm, flexWrap: 'wrap' }}>
        <Button label="Back" variant="secondary" onPress={onBack} />
        <Button
          label={isClaim ? 'Place claim' : 'Add and drop'}
          onPress={submit}
          disabled={!pick}
          loading={mutation.isPending}
          testID="replace-confirm"
          style={{ flex: 1 }}
        />
      </View>
    </View>
  );
}
