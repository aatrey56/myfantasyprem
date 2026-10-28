import { Stack } from 'expo-router';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** A deep link to a fixture still has the scores list under it (back, tab press). */
export const unstable_settings = { initialRouteName: 'index' };

/** The SCORES section: a gameweek's fixtures, and a fixture's box score pushed over them. */
export default function ScoresLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
