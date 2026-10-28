import { useLocalSearchParams } from 'expo-router';

import { LeagueTabs } from '@/components/LeagueTabs';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

export default function LeagueTabsLayout() {
  const { leagueId } = useLocalSearchParams<{ leagueId: string }>();
  return <LeagueTabs leagueId={leagueId} />;
}
