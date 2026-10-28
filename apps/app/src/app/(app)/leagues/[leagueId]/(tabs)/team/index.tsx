import { useFreshness } from '@/api/freshness';
import { useLeagueHome } from '@/api/hooks';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import { Shirt } from '@/components/icons';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { TeamView } from '@/components/team/TeamView';
import { useLeagueId } from '@/hooks/useLeagueId';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Team tab: my lineup for the next gameweek. */
export default function MyTeamScreen() {
  const leagueId = useLeagueId();
  const home = useLeagueHome(leagueId);
  const freshness = useFreshness(home);
  const myTeam = home.data?.data.viewer.team;
  if (myTeam) return <TeamView leagueId={leagueId} teamId={myTeam.id} isMine />;
  return (
    <Screen title="My team" freshness={freshness}>
      <QueryStatus query={home}>
        {() => (
          <Card>
            <EmptyState
              icon={Shirt}
              title="You don't have a team here"
              body="Ask the commissioner for an invite to join this league."
            />
          </Card>
        )}
      </QueryStatus>
    </Screen>
  );
}
