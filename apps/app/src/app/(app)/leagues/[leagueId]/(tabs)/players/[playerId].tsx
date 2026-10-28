import { useLocalSearchParams } from 'expo-router';

import { useFreshness } from '@/api/freshness';
import { useLeaguePlayer } from '@/api/hooks';
import { Card } from '@/components/Card';
import { PlayerCard } from '@/components/players/PlayerCard';
import { Screen } from '@/components/Screen';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** A player's card as a page, for links: the same card the Players tab opens in a sheet. */
export default function PlayerScreen() {
  const { leagueId, playerId, from } = useLocalSearchParams<{
    leagueId: string;
    playerId: string;
    from?: string;
  }>();
  const query = useLeaguePlayer(leagueId, playerId);
  const freshness = useFreshness(query);
  return (
    <Screen
      title={query.data?.data.player.displayName ?? 'Player'}
      back={
        from === 'feed'
          ? { label: 'Feed', href: '/feed' }
          : { label: 'Players', href: `/leagues/${leagueId}/players` }
      }
      freshness={freshness}
      maxWidth={640}
    >
      <Card>
        <PlayerCard leagueId={leagueId} playerId={playerId} />
      </Card>
    </Screen>
  );
}
