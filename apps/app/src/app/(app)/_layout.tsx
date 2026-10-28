import { Redirect, usePathname } from 'expo-router';
import { useState } from 'react';
import { Platform, View } from 'react-native';

import { useAuth } from '@/auth/AuthProvider';
import { AppTabs } from '@/components/AppTabs';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * The address the visitor asked for. On web it is read from the location
 * on first render: once this layout renders a Redirect instead of its
 * navigator, the router's own pathname no longer points at the child route.
 */
function useRequestedPath(): string {
  const pathname = usePathname();
  const [initial] = useState(() =>
    Platform.OS === 'web' && typeof window !== 'undefined'
      ? `${window.location.pathname}${window.location.search}`
      : null,
  );
  return initial ?? pathname;
}

/**
 * Everything behind sign-in, under the app's bottom bar (LEAGUES, SCORES,
 * FEED, ACCOUNT). Signed out, the user goes to /sign-in, which brings them
 * back here (`next`) afterwards.
 */
export default function SignedInLayout() {
  const { status } = useAuth();
  const next = useRequestedPath();
  const { colors } = useTheme();
  // After signing out (or deleting the account) there is nowhere to come back to.
  const [wasSignedIn, setWasSignedIn] = useState(false);
  if (status === 'signedIn' && !wasSignedIn) setWasSignedIn(true);
  if (status === 'loading') {
    return <View style={{ flex: 1, backgroundColor: colors.background }} />;
  }
  if (status === 'signedOut') {
    return (
      <Redirect href={wasSignedIn ? '/sign-in' : { pathname: '/sign-in', params: { next } }} />
    );
  }
  return <AppTabs />;
}
