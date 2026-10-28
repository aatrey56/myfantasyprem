import { ApiError } from '@myfantasyprem/api-client';
import { useRouter } from 'expo-router';
import { useSyncExternalStore } from 'react';

import { useSignInWithLink } from '@/api/account-hooks';
import { shouldRetry } from '@/api/query-client';
import { useAuth } from '@/auth/AuthProvider';
import { pendingSignInLink } from '@/auth/pending-signin-link';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import { KeyRound } from '@/components/icons';
import { Screen } from '@/components/Screen';
import { Skeleton } from '@/components/Skeleton';
import { useToast } from '@/components/Toast';
import { describeError } from '@/lib/errors';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * A sign-in link the commissioner issued for a claimed team's manager
 * (docs/adr/0013), its code already out of the URL. It works once, so it
 * is spent only on a tap (link scanners open links too), after the stored
 * session has loaded, and someone signed in is asked before being replaced.
 */
export default function SignInLinkScreen() {
  const code = useSyncExternalStore(pendingSignInLink.subscribe, pendingSignInLink.get) ?? '';
  const { status, user } = useAuth();
  const router = useRouter();
  const toast = useToast();
  const signIn = useSignInWithLink(code);

  const use = () => {
    const before = user?.id;
    signIn.mutate(undefined, {
      onSuccess: (envelope) => {
        pendingSignInLink.set(null);
        const who = envelope.data.session.user;
        toast({
          message:
            who.id === before
              ? `Already signed in as ${who.displayName}`
              : `Signed in as ${who.displayName}`,
          tone: 'success',
        });
        router.replace(`/leagues/${envelope.data.leagueId}`);
      },
      // Spent or refused: drop it; keep it for a retry worth making.
      onError: (e) => {
        if (!shouldRetry(0, e)) pendingSignInLink.set(null);
      },
    });
  };

  const back = { label: 'Home', href: '/' as const };
  if (status === 'loading' || signIn.isPending || signIn.isSuccess) {
    return (
      <Screen title="Sign in" back={back} maxWidth={560}>
        <Card testID="signin-link-loading">
          <Skeleton width="60%" height={22} />
          <Skeleton width="40%" height={14} />
        </Card>
      </Screen>
    );
  }
  if (!code || signIn.isError) {
    const body = signIn.isError
      ? signIn.error instanceof ApiError && signIn.error.status === 401
        ? 'It has expired, was already used or was replaced by a newer one. Ask your commissioner for a new sign-in link.'
        : describeError(signIn.error)
      : 'Open the sign-in link you were sent again.';
    return (
      <Screen title="Sign in" back={back} maxWidth={560}>
        <Card testID="signin-link-problem">
          <EmptyState icon={KeyRound} title="This sign-in link doesn't work" body={body}>
            {signIn.isError && code ? (
              <Button label="Try again" onPress={use} testID="signin-link-confirm" />
            ) : null}
            <Button label="Go home" variant="secondary" onPress={() => router.replace('/')} />
          </EmptyState>
        </Card>
      </Screen>
    );
  }
  const signedIn = status === 'signedIn' && user;
  return (
    <Screen title="Sign in" back={back} maxWidth={560}>
      <Card testID={signedIn ? 'signin-link-signed-in' : 'signin-link-ready'}>
        <EmptyState
          icon={KeyRound}
          title={signedIn ? 'Use this sign-in link?' : 'Sign in to your team'}
          body={
            signedIn
              ? `You're signed in as ${sentence(user.displayName)} Sign out and use this link?`
              : 'Your commissioner sent you this link. It works once.'
          }
        >
          <Button
            label={signedIn ? 'Sign out and use this link' : 'Sign in to your team'}
            onPress={use}
            testID="signin-link-confirm"
          />
          {signedIn ? (
            <Button
              label="Keep me signed in"
              variant="secondary"
              onPress={() => router.replace('/')}
            />
          ) : null}
        </EmptyState>
      </Card>
    </Screen>
  );
}

/** A name ending a sentence: "Sam P." stays as is, "Sam" becomes "Sam.". */
function sentence(name: string): string {
  return /[.!?]$/.test(name) ? name : `${name}.`;
}
