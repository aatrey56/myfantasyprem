import { Stack } from 'expo-router';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** The LEAGUES section: the hub, a new league, and every league pushed over them. */
export default function LeaguesLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
