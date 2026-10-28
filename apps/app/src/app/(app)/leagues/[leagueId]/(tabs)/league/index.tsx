import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { useFreshness } from '@/api/freshness';
import { useLeagueHome } from '@/api/hooks';
import { LeagueInfo, LeagueLinks, LeagueMatchups } from '@/components/league/LeagueSections';
import { StandingsTable } from '@/components/league/StandingsTable';
import { PreseasonPanel } from '@/components/PreseasonPanel';
import { StartSitTable } from '@/components/projections/StartSit';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { Skeleton } from '@/components/Skeleton';
import { useLeagueId } from '@/hooks/useLeagueId';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * The League tab, Sleeper's layout: no page title (the league's name is in
 * the bar above the tabs). It opens on the gameweek's matchup cards with a
 * `‹ GW ›` stepper, then the standings, start/sit accuracy, links to the
 * activity feed and the
 * draft results, and the league's rules and members.
 */
export default function LeagueHomeScreen() {
  const leagueId = useLeagueId();
  const query = useLeagueHome(leagueId);
  const freshness = useFreshness(query);
  const { spacing } = useTheme();
  const h = query.data?.data;
  const inSeason = h?.league.status === 'in_season' || h?.league.status === 'complete';

  return (
    <Screen
      title="League"
      hideHeader
      documentTitle={h ? `${h.league.name} · League` : 'League'}
      freshness={freshness}
    >
      <QueryStatus
        query={query}
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
          ) : (
            <View style={{ gap: spacing.lg }} testID="loaded">
              <LeagueMatchups leagueId={leagueId} home={data} />
              <View style={{ gap: spacing.xs }}>
                <SectionHeader
                  title="Standings"
                  detail={standingsCaption(data)}
                  testID="standings-header"
                />
                <StandingsTable
                  leagueId={leagueId}
                  rows={data.standings}
                  myTeamId={data.viewer.team?.id}
                />
              </View>
              <StartSitTable leagueId={leagueId} myTeamId={data.viewer.team?.id} />
              <LeagueLinks leagueId={leagueId} />
              <LeagueInfo leagueId={leagueId} home={data} />
            </View>
          )
        }
      </QueryStatus>
    </Screen>
  );
}

/**
 * "after GW n" for the last gameweek in the table. `played` counts matches,
 * and the league may have started after GW 1.
 */
function standingsCaption(home: Schemas['LeagueHome']): string | undefined {
  const played = home.standings[0]?.played ?? 0;
  if (played <= 0) return undefined;
  return `after GW ${home.settings.startGameweek + played - 1}`;
}
