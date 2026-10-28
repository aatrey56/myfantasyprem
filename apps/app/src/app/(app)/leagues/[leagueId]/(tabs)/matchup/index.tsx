import { useLocalSearchParams, useRouter } from 'expo-router';

import { MatchupScreen } from '@/components/matchup/MatchupScreen';
import { useLeagueId } from '@/hooks/useLeagueId';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Matchup tab: my matchup this gameweek, head to head, and the gameweek's other matchups. */
export default function MatchupTab() {
  const leagueId = useLeagueId();
  const { gw } = useLocalSearchParams<{ gw?: string }>();
  const router = useRouter();
  const n = Number(gw);
  return (
    <MatchupScreen
      leagueId={leagueId}
      gameweek={Number.isInteger(n) && n >= 1 && n <= 38 ? n : undefined}
      onChangeGameweek={(next) => router.setParams({ gw: String(next) })}
    />
  );
}
