import { Stack } from 'expo-router';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

export default function SectionLayout() {
  return <Stack screenOptions={{ headerShown: false }} />;
}
