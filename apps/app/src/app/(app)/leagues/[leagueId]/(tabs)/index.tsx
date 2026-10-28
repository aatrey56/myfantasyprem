import { useState } from 'react';
import { View } from 'react-native';

import { useFreshness } from '@/api/freshness';
import { useCurrentGameweek, useLeagueHome, useSchedule } from '@/api/hooks';
import { useWaiverState } from '@/api/moves-hooks';
import { useGameweekAwards, useLeagueStatus, usePlayersOfTheWeek } from '@/api/status-hooks';
import { AppText } from '@/components/AppText';
import { Card } from '@/components/Card';
import { usePlayerCard } from '@/components/players/PlayerCardContext';
import { PreseasonPanel } from '@/components/PreseasonPanel';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { Skeleton } from '@/components/Skeleton';
import {
  AttentionStrip,
  DeadlineCards,
  FixturesCard,
  GameweekSummaryCard,
  InjuriesCard,
  LeagueTableCard,
  PlayersOfTheWeek,
  TeamOfTheWeek,
  TopAvailable,
} from '@/components/status/StatusSections';
import { useLayout } from '@/hooks/useLayout';
import { useLeagueId } from '@/hooks/useLeagueId';
import { seasonOverUnplayed } from '@/lib/leagues';
import { defaultAwardsGameweek, gameweekSummary, leagueTable, myFixtures } from '@/lib/status';
import { transactionPhaseLabel } from '@/lib/waivers';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * Status: the league's landing tab. What needs attention, my gameweek,
 * deadlines, the best free agents, Player and Team of the Week, and on the
 * right (under the main column on phones) the table, my fixtures and every
 * flagged player.
 */
export default function StatusScreen() {
  const leagueId = useLeagueId();
  const home = useLeagueHome(leagueId);
  const h = home.data?.data;
  const inSeason = h?.league.status === 'in_season' || h?.league.status === 'complete';
  const status = useLeagueStatus(leagueId, inSeason);
  const freshness = useFreshness(inSeason ? status : home);
  const { spacing } = useTheme();

  return (
    <Screen
      title="Status"
      hideHeader
      documentTitle={h ? `${h.league.name} · Status` : 'Status'}
      freshness={freshness}
      maxWidth={1240}
    >
      <QueryStatus
        query={home}
        loading={
          <View style={{ gap: spacing.md }} testID="loading">
            <Skeleton height={110} radius={16} />
            <Skeleton height={320} radius={16} />
          </View>
        }
      >
        {({ data }) =>
          !inSeason ? (
            <PreseasonPanel leagueId={leagueId} home={data} />
          ) : seasonOverUnplayed(data) ? (
            <Card testID="season-over">
              <AppText variant="title">Season over — no gameweeks left</AppText>
              <AppText muted>
                The draft finished after the last gameweek&apos;s deadline, so this league has no
                matchups to play this season.
              </AppText>
            </Card>
          ) : (
            <QueryStatus
              query={status}
              loading={
                <View style={{ gap: spacing.md }} testID="loading">
                  <Skeleton height={44} radius={12} />
                  <Skeleton height={140} radius={16} />
                  <Skeleton height={320} radius={16} />
                </View>
              }
            >
              {({ data: page }) => <StatusBody leagueId={leagueId} home={data} page={page} />}
            </QueryStatus>
          )
        }
      </QueryStatus>
    </Screen>
  );
}

type Home = NonNullable<ReturnType<typeof useLeagueHome>['data']>['data'];
type Page = NonNullable<ReturnType<typeof useLeagueStatus>['data']>['data'];

function StatusBody({ leagueId, home, page }: { leagueId: string; home: Home; page: Page }) {
  const { spacing } = useTheme();
  const { isDesktop } = useLayout();
  const myTeamId = home.viewer.team?.id;
  const gameweeks = useCurrentGameweek();
  const waivers = useWaiverState(leagueId);
  const schedule = useSchedule(leagueId);
  const potw = usePlayersOfTheWeek();
  const weeks = potw.data?.data ?? [];
  const [picked, setPicked] = useState<number | null>(null);
  const totwGw = picked ?? defaultAwardsGameweek(weeks);
  const awards = useGameweekAwards(totwGw);
  const openCard = usePlayerCard();
  const open = (id: string) => openCard?.(id);

  const fixtures = myFixtures(home, schedule.data?.data);
  const maxGw = weeks.length > 0 ? Math.max(...weeks.map((w) => w.gameweek)) : 1;

  const phase = transactionPhaseLabel(page.transactionPhase);

  const main = (
    <View style={{ gap: spacing.xl, flex: isDesktop ? 1 : undefined, minWidth: 0 }}>
      {phase ? (
        <Card testID="transaction-phase">
          <AppText weight="semibold">{phase}</AppText>
        </Card>
      ) : null}
      <AttentionStrip items={page.attention} leagueId={leagueId} />
      <GameweekSummaryCard summary={gameweekSummary(home, page.rankChange)} />
      {gameweeks.data ? (
        <DeadlineCards
          gameweeks={gameweeks.data.data}
          waivers={waivers.data?.data}
          myTeamId={myTeamId}
        />
      ) : null}
      <TopAvailable
        players={page.topAvailable}
        canSign={Boolean(myTeamId)}
        onOpen={open}
        onSign={(id) => openCard?.(id, { drop: true })}
      />
      {weeks.length > 0 ? <PlayersOfTheWeek weeks={weeks} onOpen={open} /> : null}
      {totwGw !== null ? (
        <TeamOfTheWeek
          awards={awards.data?.data}
          gameweek={totwGw}
          maxGameweek={maxGw}
          onChange={setPicked}
          onOpen={open}
          loading={<Skeleton height={440} radius={16} />}
        />
      ) : null}
    </View>
  );
  const aside = (
    <View style={{ gap: spacing.xl, width: isDesktop ? 360 : undefined }}>
      <LeagueTableCard rows={leagueTable(home)} leagueId={leagueId} />
      <FixturesCard current={fixtures.current} next={fixtures.next} />
      <InjuriesCard players={page.injuries} onOpen={open} />
    </View>
  );
  return (
    <View
      testID="status-loaded"
      style={
        isDesktop
          ? { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.xl }
          : { gap: spacing.xl }
      }
    >
      {main}
      {aside}
    </View>
  );
}
