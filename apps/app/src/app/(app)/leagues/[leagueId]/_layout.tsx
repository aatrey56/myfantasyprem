import { Stack, useLocalSearchParams } from 'expo-router';

import { usePrefetchLeague } from '@/api/prefetch';
import { useLeagueRealtime } from '@/api/realtime';
import { PlayerCardProvider } from '@/components/players/PlayerCardContext';
import { DifficultyProvider } from '@/components/projections/Difficulty';
import { LeagueIdContext } from '@/hooks/useLeagueId';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * A league: the tabbed shell, plus full-screen routes over it (draft room,
 * settings). The league's realtime channel stays open while any of them is
 * mounted, and one player card sheet serves every player shown in them.
 * Fixture difficulty (ADR 0014) is loaded once for all of them.
 * Opening it loads the five top tabs' data in the background.
 */
export default function LeagueLayout() {
  const { leagueId } = useLocalSearchParams<{ leagueId: string }>();
  useLeagueRealtime(leagueId);
  usePrefetchLeague(leagueId);
  return (
    <LeagueIdContext.Provider value={leagueId}>
      <DifficultyProvider>
        <PlayerCardProvider leagueId={leagueId}>
          <Stack screenOptions={{ headerShown: false }} />
        </PlayerCardProvider>
      </DifficultyProvider>
    </LeagueIdContext.Provider>
  );
}
