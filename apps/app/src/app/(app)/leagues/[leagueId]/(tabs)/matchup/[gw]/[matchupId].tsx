import { useLocalSearchParams } from 'expo-router';

import { MatchupScreen } from '@/components/matchup/MatchupScreen';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Any matchup in the league: both lineups, bench, fixture states, live points, auto-subs. */
export default function MatchupDetailScreen() {
  const { leagueId, gw, matchupId } = useLocalSearchParams<{
    leagueId: string;
    gw: string;
    matchupId: string;
  }>();
  const n = Number(gw);
  return (
    <MatchupScreen
      leagueId={leagueId}
      gameweek={Number.isInteger(n) ? n : undefined}
      matchupId={matchupId}
    />
  );
}
