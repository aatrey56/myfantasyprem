import { Stack } from 'expo-router';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** A deep link into the tab still has the tab's first screen under it (back, tab press). */
export const unstable_settings = { initialRouteName: 'index' };

export default function SectionLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
