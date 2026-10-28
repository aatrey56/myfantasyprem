import { useLocalSearchParams, useRouter } from 'expo-router';

import { ScoresScreen } from '@/components/scores/ScoresScreen';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** SCORES: a gameweek's Premier League fixtures by day (`?gw=` picks the gameweek). */
export default function ScoresRoute() {
  const { gw } = useLocalSearchParams<{ gw?: string }>();
  const router = useRouter();
  const n = Number(gw);
  return (
    <ScoresScreen
      gameweek={Number.isInteger(n) && n >= 1 && n <= 38 ? n : undefined}
      onChangeGameweek={(next) => router.setParams({ gw: String(next) })}
    />
  );
}
