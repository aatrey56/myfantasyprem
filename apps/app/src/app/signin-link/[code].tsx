import { Redirect, useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { Platform } from 'react-native';

import { pendingSignInLink } from '@/auth/pending-signin-link';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import { KeyRound } from '@/components/icons';
import { Screen } from '@/components/Screen';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** A sign-in link's code: 43 URL-safe characters. */
const CODE = /^[A-Za-z0-9_-]{43}$/;

/**
 * Opening a sign-in link (docs/adr/0013): the code moves to memory and the
 * URL with it leaves the history before anything is sent (a replace to
 * /signin-link, which asks before signing in).
 */
export default function SignInLinkHandoff() {
  const params = useLocalSearchParams<{ code: string }>();
  const router = useRouter();
  const [valid] = useState(() => {
    const code = params.code ?? '';
    if (!CODE.test(code)) return false;
    pendingSignInLink.set(code);
    if (Platform.OS === 'web' && typeof window !== 'undefined') {
      window.history.replaceState(window.history.state, '', '/signin-link');
    }
    return true;
  });
  if (valid) return <Redirect href="/signin-link" />;
  return (
    <Screen title="Sign in" back={{ label: 'Home', href: '/' }} maxWidth={560}>
      <Card testID="signin-link-problem">
        <EmptyState
          icon={KeyRound}
          title="This sign-in link doesn't work"
          body="That link doesn't look right. Check the link you were sent."
        >
          <Button label="Go home" variant="secondary" onPress={() => router.replace('/')} />
        </EmptyState>
      </Card>
    </Screen>
  );
}
