import { useLocalSearchParams } from 'expo-router';

import { FixtureScreen } from '@/components/scores/FixtureScreen';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** One fixture's box score: every player's points, mine and my leagues' highlighted. */
export default function FixtureRoute() {
  const { fixtureId } = useLocalSearchParams<{ fixtureId: string }>();
  return <FixtureScreen fixtureId={fixtureId} />;
}
