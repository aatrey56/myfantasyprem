import { QueryClientContext } from '@tanstack/react-query';
import { useContext, useEffect, useMemo, useRef, useState } from 'react';
import { ScrollView, View } from 'react-native';

import { Card } from '../Card';
import { EmptyState } from '../EmptyState';
import { Calendar, Swords } from '../icons';
import { QueryStatus } from '../QueryStatus';
import { Screen } from '../Screen';
import { SectionHeader } from '../SectionHeader';
import { Skeleton, SkeletonRows } from '../Skeleton';
import { MatchupProjection } from '../projections/MatchupProjection';
import { oddsById } from '../projections/WinChance';
import { GameweekSwitcher } from '../team/GameweekSwitcher';
import { MatchupCarousel } from './MatchupCarousel';
import { MatchupDetailView } from './MatchupDetailView';
import { GameweekPicker } from './GameweekPicker';
import { MatchupList } from './MatchupList';
import { MatchupPreviewView } from './MatchupPreviewView';
import { useFreshness } from '@/api/freshness';
import {
  matchupQuery,
  useLeagueContext,
  useMatchup,
  useMatchupPreview,
  useMatchups,
  useSchedule,
} from '@/api/hooks';
import { usePrefetchAdjacentGameweeks } from '@/api/prefetch';
import { useLeagueProjections, useStartSit } from '@/api/projections';
import { useLayout } from '@/hooks/useLayout';
import { leagueGameweek, matchupUpcoming, teamRecords } from '@/lib/matchup';
import { carouselOrder, neighbourPages, pageOf, seasonLines } from '@/lib/matchup-carousel';
import { useTheme } from '@/theme';

interface Props {
  leagueId: string;
  /** The gameweek to show; undefined means the current one. */
  gameweek?: number;
  /** A specific matchup; undefined means mine (or the first) in the gameweek. */
  matchupId?: string;
  /** Present on the tab's index: the gameweek stepper. */
  onChangeGameweek?: (gw: number) => void;
}

/**
 * The Matchup tab, Sleeper's layout: no page title, my matchup (or the one
 * asked for) as the hero right under the league tabs, then the starters
 * head to head with the `‹ GW 7 ›` stepper on the right of their heading,
 * and every matchup in the gameweek. Upcoming gameweeks show the pairings
 * from the schedule. Polls while scores are live, only while this screen
 * has focus (the query hooks).
 */
export function MatchupScreen({ leagueId, gameweek, matchupId, onChangeGameweek }: Props) {
  const { spacing } = useTheme();
  const { width } = useLayout();
  const home = useLeagueContext(leagueId);
  const h = home.data?.data;
  const inSeason = h?.league.status === 'in_season' || h?.league.status === 'complete';
  const current = h ? leagueGameweek(h) : undefined;
  // A gameweek from the URL outside the league's (before a mid-season
  // league started) opens its nearest one.
  const gw =
    gameweek !== undefined && h
      ? Math.min(Math.max(gameweek, h.settings.startGameweek), h.settings.endGameweek)
      : (gameweek ?? current);
  const upcoming = gw !== undefined && h !== undefined && matchupUpcoming(h, gw);
  const myTeamId = h?.viewer.team?.id;

  const list = useMatchups(leagueId, inSeason && !upcoming ? gw : undefined);
  const schedule = useSchedule(leagueId, inSeason && upcoming);
  const summaries = list.data?.data;
  // The header carousel: every matchup, mine first. A swipe (or a card in
  // the list) picks one for this gameweek; another gameweek opens on mine.
  const order = useMemo(
    () => (summaries ? carouselOrder(summaries, myTeamId) : []),
    [summaries, myTeamId],
  );
  const [picked, setPicked] = useState<{ gw: number; id: string } | null>(null);
  const pickedId =
    picked && picked.gw === gw && order.some((m) => m.id === picked.id) ? picked.id : undefined;
  const chosen = pickedId ?? matchupId ?? order[0]?.id;
  const page = pageOf(order, chosen);
  // Right after a gameweek step the list still shows the last gameweek's
  // matchups (kept while the new ones load): wait for it, rather than ask
  // for an old matchup id in the new gameweek. The last matchup stays up.
  const listSettled = matchupId !== undefined || !list.isPlaceholderData;
  const detail = useMatchup(
    leagueId,
    chosen && gw !== undefined && listSettled ? gw : NaN,
    chosen ?? '',
  );
  // A jump within the gameweek to a matchup still loading keeps the last
  // one as placeholder: show the skeleton, not its lineups under the new
  // header. A gameweek step still keeps the last matchup up.
  const otherMatchup =
    detail.isPlaceholderData && detail.data?.data.gameweek === gw && detail.data.data.id !== chosen;
  usePrefetchAdjacentGameweeks(leagueId, inSeason ? gw : undefined);
  // The matchups either side of the page, so a swipe shows them at once.
  const queryClient = useContext(QueryClientContext);
  const settledGw = listSettled && !list.isPlaceholderData ? gw : undefined;
  useEffect(() => {
    if (settledGw === undefined || !queryClient) return;
    for (const p of neighbourPages(order.length, page))
      void queryClient.prefetchQuery(matchupQuery(leagueId, settledGw, order[p].id));
  }, [queryClient, leagueId, settledGw, order, page]);
  const scrollRef = useRef<ScrollView>(null);
  const select = (id: string, toTop = false) => {
    if (gw === undefined) return;
    setPicked({ gw, id });
    if (toTop) scrollRef.current?.scrollTo({ y: 0, animated: true });
  };
  // A future gameweek: the schedule's pairings in the carousel, and the
  // open one's squads and fixtures (no scores).
  const week = upcoming ? schedule.data?.data.find((w) => w.gameweek === gw) : undefined;
  const upOrder = useMemo(
    () =>
      week
        ? carouselOrder(
            week.matchups.map((m) => ({ ...m, state: 'upcoming' as const })),
            myTeamId,
          )
        : [],
    [week, myTeamId],
  );
  const upChosen =
    (picked && picked.gw === gw && upOrder.some((m) => m.id === picked.id)
      ? picked.id
      : undefined) ??
    (matchupId && upOrder.some((m) => m.id === matchupId) ? matchupId : undefined) ??
    upOrder[0]?.id;
  const preview = useMatchupPreview(
    leagueId,
    upcoming && gw !== undefined ? gw : NaN,
    upChosen ?? '',
  );
  const otherPreview = preview.isPlaceholderData && preview.data?.data.id !== upChosen;
  const startSit = useStartSit(leagueId, inSeason);
  const lines = useMemo(
    () => seasonLines(h?.standings, startSit.data?.data),
    [h?.standings, startSit.data?.data],
  );
  const freshness = useFreshness(chosen ? detail : list);

  const records = useMemo(() => teamRecords(h?.standings), [h?.standings]);
  // Expected and win chance before and during the gameweek (ADR 0014).
  const past = gw !== undefined && current !== undefined && gw < current;
  const projections = useLeagueProjections(leagueId, inSeason && !past ? gw : undefined);
  const projected = projections.data?.data;
  const odds = useMemo(() => oddsById(projected), [projected]);
  const wide = width >= 1240;

  const stepper =
    gw !== undefined && h && onChangeGameweek ? (
      <GameweekSwitcher
        gameweek={gw}
        min={h.settings.startGameweek}
        max={h.settings.endGameweek}
        onChange={onChangeGameweek}
      />
    ) : null;
  // The header's GW badge opens the gameweek picker.
  const emblem =
    h && onChangeGameweek
      ? (g: number, live: boolean) => (
          <GameweekPicker
            gameweek={g}
            min={h.settings.startGameweek}
            max={h.settings.endGameweek}
            current={current}
            live={live}
            onChange={onChangeGameweek}
          />
        )
      : undefined;
  // The stepper sits on the starters heading once the matchup shows, else on
  // the gameweek's matchups heading (or alone while neither is on screen).
  const detailShown = Boolean(chosen && detail.data);

  let content;
  if (home.data && !inSeason) {
    content = (
      <Card>
        <EmptyState
          icon={Swords}
          title="Matchups start after the draft"
          body="Once every squad is drafted, each gameweek pairs you with another manager here."
          testID="matchup-preseason"
        />
      </Card>
    );
  } else if (upcoming && gw !== undefined) {
    content = (
      <QueryStatus query={schedule}>
        {() => {
          if (!week || week.matchups.length === 0) {
            return (
              <>
                <Card>
                  <EmptyState icon={Calendar} title={`No matchups in GW ${gw}`} />
                </Card>
                {stepper ? <View style={{ alignItems: 'flex-end' }}>{stepper}</View> : null}
              </>
            );
          }
          const upPage = pageOf(upOrder, upChosen);
          const carousel = (
            <MatchupCarousel
              leagueId={leagueId}
              order={upOrder as never}
              page={upPage}
              onPageChange={(p) => upOrder[p] && select(upOrder[p].id)}
              myTeamId={myTeamId}
              records={records}
              seasonLines={lines}
              emblem={emblem}
              preview
            />
          );
          const previewLoading = (
            <Card flush>
              <SkeletonRows count={6} />
            </Card>
          );
          // Like the scored branch: another pairing's preview held over while
          // this one loads shows the skeleton, not the wrong squads.
          const main = otherPreview ? (
            previewLoading
          ) : (
            <QueryStatus query={preview} loading={previewLoading}>
              {({ data }) => (
                <MatchupPreviewView
                  preview={data}
                  myTeamId={myTeamId}
                  stepper={stepper}
                  projection={(sides) => (
                    <MatchupProjection projections={projected} matchupId={data.id} {...sides} />
                  )}
                />
              )}
            </QueryStatus>
          );
          const others = (
            <View style={{ gap: spacing.xs }}>
              <SectionHeader title={`GW ${gw} matchups`} />
              <MatchupList
                leagueId={leagueId}
                matchups={week.matchups}
                myTeamId={myTeamId}
                selectedId={upChosen}
                records={records}
                odds={odds}
                onSelect={(id) => select(id, true)}
                testID="matchup-list"
              />
            </View>
          );
          return wide ? (
            <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: spacing.lg }}>
              <View style={{ flex: 1, minWidth: 0, gap: spacing.lg }}>
                {carousel}
                {main}
              </View>
              <View style={{ width: 300 }}>{others}</View>
            </View>
          ) : (
            <>
              {carousel}
              {main}
              {others}
            </>
          );
        }}
      </QueryStatus>
    );
  } else {
    const carousel =
      order.length > 0 && gw !== undefined ? (
        <MatchupCarousel
          leagueId={leagueId}
          order={order}
          page={page}
          onPageChange={(p) => order[p] && select(order[p].id)}
          detail={detail.data?.data}
          myTeamId={myTeamId}
          records={records}
          seasonLines={lines}
          emblem={emblem}
        />
      ) : null;
    const loading = (
      <View style={{ gap: spacing.md }} testID="loading">
        <Skeleton height={150} radius={16} />
        <Card flush>
          <SkeletonRows count={6} />
        </Card>
      </View>
    );
    const main = otherMatchup ? (
      loading
    ) : chosen ? (
      <QueryStatus query={detail} loading={loading}>
        {({ data }) => (
          <MatchupDetailView
            leagueId={leagueId}
            detail={data}
            myTeamId={myTeamId}
            records={records}
            stepper={stepper}
            projections={projected}
            hideHeader={carousel !== null}
          />
        )}
      </QueryStatus>
    ) : (
      <QueryStatus query={list}>
        {() => (
          <Card>
            <EmptyState icon={Swords} title={`No matchups in GW ${gw}`} />
          </Card>
        )}
      </QueryStatus>
    );
    const others = summaries ? (
      <View style={{ gap: spacing.xs }}>
        <SectionHeader title={`GW ${gw} matchups`} right={detailShown ? undefined : stepper} />
        <MatchupList
          leagueId={leagueId}
          matchups={summaries}
          myTeamId={myTeamId}
          selectedId={chosen}
          records={records}
          odds={odds}
          onSelect={(id) => select(id, true)}
          testID="matchup-list"
        />
      </View>
    ) : !detailShown && stepper ? (
      <View style={{ alignItems: 'flex-end' }}>{stepper}</View>
    ) : null;
    content = wide ? (
      <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: spacing.lg }}>
        <View style={{ flex: 1, minWidth: 0, gap: spacing.lg }}>
          {carousel}
          {main}
        </View>
        <View style={{ width: 300 }}>{others}</View>
      </View>
    ) : (
      <>
        {carousel}
        {main}
        {others}
      </>
    );
  }

  return (
    <Screen title="Matchup" hideHeader freshness={freshness} scrollRef={scrollRef}>
      {home.isPending ? (
        <View style={{ gap: spacing.md }} testID="loading">
          <Skeleton height={190} radius={16} />
          <Card flush>
            <SkeletonRows count={6} />
          </Card>
        </View>
      ) : home.data ? (
        content
      ) : (
        <QueryStatus query={home}>{() => null}</QueryStatus>
      )}
    </Screen>
  );
}
