import { useLocalSearchParams } from 'expo-router';

import { useLeagueHome } from '@/api/hooks';
import { TeamView } from '@/components/team/TeamView';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Any team in the league: its lineup once locked; mine is editable. */
export default function TeamScreen() {
  const { leagueId, teamId } = useLocalSearchParams<{ leagueId: string; teamId: string }>();
  const home = useLeagueHome(leagueId);
  const isMine = home.data?.data.viewer.team?.id === teamId;
  return (
    <TeamView
      key={`${teamId}-${isMine}`}
      leagueId={leagueId}
      teamId={teamId}
      isMine={isMine}
      back={{ label: 'League', href: `/leagues/${leagueId}/league` }}
    />
  );
}
