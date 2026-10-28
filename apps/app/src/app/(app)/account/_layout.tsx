import { Stack } from 'expo-router';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** A deep link to the notification settings still has the account under it. */
export const unstable_settings = { initialRouteName: 'index' };

/** The ACCOUNT section: profile, and the notification settings pushed over it. */
export default function AccountLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
