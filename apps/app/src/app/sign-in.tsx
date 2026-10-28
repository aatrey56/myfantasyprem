import * as AppleAuthentication from 'expo-apple-authentication';
import Constants from 'expo-constants';
import { Redirect, useLocalSearchParams, type Href } from 'expo-router';
import { useEffect, useState, useSyncExternalStore } from 'react';
import { Platform, View } from 'react-native';

import {
  useAppleSignIn,
  useGoogleSignIn,
  useStartEmailSignIn,
  useVerifyEmailCode,
  useVerifyEmailSignIn,
} from '@/api/account-hooks';
import { shouldRetry } from '@/api/query-client';
import { useAuth } from '@/auth/AuthProvider';
import { pendingNext } from '@/auth/pending-next';
import { isSignInCancelled, mockSignInCodeHint } from '@/auth/providers';
import { AppText } from '@/components/AppText';
import { BrandMark } from '@/components/BrandMark';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { CodeInput } from '@/components/CodeInput';
import { Mail } from '@/components/icons';
import { Screen } from '@/components/Screen';
import { TextField } from '@/components/TextField';
import { APP_NAME } from '@/constants/app';
import { describeError, problemErrors } from '@/lib/errors';
import { linkNextPath, safeNextPath } from '@/lib/navigation';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

/** The magic-link token taken out of the URL (memory only, never stored). */
const linkToken = (() => {
  let value: string | null = null;
  const listeners = new Set<() => void>();
  return {
    get: () => value,
    set(next: string | null) {
      value = next;
      listeners.forEach((l) => l());
    },
    subscribe(l: () => void) {
      listeners.add(l);
      return () => {
        listeners.delete(l);
      };
    },
  };
})();

/** On the web, rewrite the current history entry without `?token=`. */
function stripTokenFromHistory() {
  if (Platform.OS !== 'web' || typeof window === 'undefined') return;
  const url = new URL(window.location.href);
  if (!url.searchParams.has('token')) return;
  url.searchParams.delete('token');
  window.history.replaceState(window.history.state, '', url.pathname + url.search + url.hash);
}

/**
 * Takes a magic-link token out of the URL: keeps it in memory and reopens
 * sign-in without it (a replace, so the URL with the token leaves the
 * history too).
 */
function TokenHandoff({ token, next }: { token: string; next?: string }) {
  useEffect(() => {
    linkToken.set(token);
    stripTokenFromHistory();
  }, [token]);
  return <Redirect href={{ pathname: '/sign-in', params: next ? { next } : {} }} />;
}

/**
 * Sign in with Apple, Google or an emailed link and code. Opening the link
 * lands here with `?token=`; the token leaves the URL at once and a tap on
 * "Continue" finishes the sign-in. Or the code from the same email goes
 * into the "Check your inbox" boxes. `next` is where to go afterwards (a
 * path inside the app, such as the invite being joined): the emailed link
 * carries it, and session storage keeps it for a link that comes back to
 * this tab without it.
 */
export default function SignInScreen() {
  const { status } = useAuth();
  const params = useLocalSearchParams<{ next?: string; token?: string }>();
  const requested = safeNextPath(params.next);
  const [storedNext] = useState(() => pendingNext.get());
  const next = requested ?? storedNext ?? '/leagues';
  const { colors, spacing, radii, layout } = useTheme();
  const nativeApple = useNativeAppleButton();
  const apple = useAppleSignIn();
  const google = useGoogleSignIn();
  const startEmail = useStartEmailSignIn();
  const verify = useVerifyEmailSignIn();
  const verifyCode = useVerifyEmailCode();
  const [email, setEmail] = useState('');
  const [emailError, setEmailError] = useState<string | null>(null);
  const [sentTo, setSentTo] = useState<string | null>(null);
  const [code, setCode] = useState('');
  const [codeError, setCodeError] = useState<string | null>(null);
  // A new key clears the boxes and puts the cursor back in the first.
  const [codeKey, setCodeKey] = useState(0);
  // Signed in: the destination has been reached.
  useEffect(() => {
    if (status === 'signedIn') pendingNext.clear();
  }, [status]);
  // A magic link opened in the app: keep the token in memory only and take
  // it out of the URL (and the browser history) at once, so it is not left
  // in history, screenshots or a Referer. Signing in waits for a tap: mail
  // scanners and link previews open links too.
  const pendingToken = useSyncExternalStore(linkToken.subscribe, linkToken.get);

  if (status === 'signedIn') return <Redirect href={next as Href} />;
  if (params.token) return <TokenHandoff token={params.token} next={params.next} />;

  const busy =
    apple.isPending ||
    google.isPending ||
    startEmail.isPending ||
    verify.isPending ||
    verifyCode.isPending;
  // A closed Apple or Google sheet is not an error worth showing.
  const failure = [apple.error, google.error, verify.error].find((e) => e && !isSignInCancelled(e));

  // The link works once: drop it after a sign-in or a refusal (used,
  // expired), but keep it after a failure worth retrying (offline, 429,
  // 5xx) so "Continue" can try again.
  const finishLink = (token: string) =>
    verify.mutate(token, {
      onSuccess: () => linkToken.set(null),
      onError: (e) => {
        if (!shouldRetry(0, e)) linkToken.set(null);
      },
    });

  const sendLink = () => {
    const value = email.trim();
    if (!EMAIL.test(value)) {
      setEmailError('Enter a valid email address.');
      return;
    }
    setEmailError(null);
    // Kept for a link that comes back to this tab without its next.
    if (requested) pendingNext.set(requested);
    else pendingNext.clear();
    const linkNext = linkNextPath(requested);
    startEmail.mutate(linkNext ? { email: value, next: linkNext } : { email: value }, {
      onSuccess: () => {
        setSentTo(value);
        setCode('');
        setCodeError(null);
        setCodeKey((k) => k + 1);
      },
      onError: (e) => setEmailError(problemErrors(e)[0] ?? describeError(e)),
    });
  };

  const submitCode = (value: string) => {
    if (!sentTo || value.length !== 6 || verifyCode.isPending) return;
    setCodeError(null);
    verifyCode.mutate(
      { email: sentTo, code: value },
      {
        onError: (e) => {
          setCodeError(describeError(e));
          // A refused code (wrong, used, expired) is cleared for the next
          // try; after a failure worth retrying it stays for "Sign in".
          if (!shouldRetry(0, e)) {
            setCode('');
            setCodeKey((k) => k + 1);
          }
        },
      },
    );
  };

  const codeHint = mockSignInCodeHint();

  return (
    <Screen title="Sign in" hideHeader maxWidth={440}>
      <View style={{ alignItems: 'center', gap: spacing.md, paddingTop: spacing.xl }}>
        <BrandMark size={56} />
        <AppText variant="title" accessibilityRole="header" style={{ textAlign: 'center' }}>
          {`Sign in to ${APP_NAME}`}
        </AppText>
        <AppText muted style={{ textAlign: 'center' }}>
          Draft a squad, set your XI each gameweek and play your friends head to head.
        </AppText>
      </View>

      {pendingToken ? (
        <Card testID="magic-link-confirm">
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
            <Mail size={20} color={colors.accent} aria-hidden />
            <AppText variant="heading">Finish signing in</AppText>
          </View>
          <AppText muted>
            You opened a sign-in link. Continue to sign in on this device. If you did not ask for
            it, close this page.
          </AppText>
          <Button
            label="Continue"
            onPress={() => finishLink(pendingToken)}
            loading={verify.isPending}
            disabled={busy}
            testID="magic-link-continue"
          />
          {verify.error ? (
            <AppText variant="meta" color={colors.loss} role="alert" testID="sign-in-error">
              {describeError(verify.error)}
            </AppText>
          ) : null}
        </Card>
      ) : sentTo ? (
        <Card testID="email-sent">
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
            <Mail size={20} color={colors.accent} aria-hidden />
            <AppText variant="heading">Check your inbox</AppText>
          </View>
          <AppText muted>
            {`We sent a sign-in link and a 6-digit code to ${sentTo}. Open the link, or enter the code here. Each works once and expires in 15 minutes.`}
          </AppText>
          <CodeInput
            key={codeKey}
            autoFocus
            onChange={(value) => {
              setCode(value);
              if (codeError) setCodeError(null);
            }}
            onComplete={submitCode}
            error={codeError}
            disabled={verifyCode.isPending}
            testID="sign-in-code"
          />
          {codeHint ? (
            <AppText variant="meta" subtle style={{ textAlign: 'center' }}>
              {codeHint}
            </AppText>
          ) : null}
          <Button
            label="Sign in"
            onPress={() => submitCode(code)}
            loading={verifyCode.isPending}
            disabled={busy || code.length !== 6}
            testID="sign-in-code-submit"
          />
          <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}>
            <Button
              label="Resend link"
              variant="secondary"
              onPress={sendLink}
              loading={startEmail.isPending}
            />
            <Button
              label="Use a different email"
              variant="ghost"
              onPress={() => {
                setSentTo(null);
                setCodeError(null);
              }}
            />
          </View>
        </Card>
      ) : (
        <Card style={{ gap: spacing.md }}>
          {appleSignInOff ? null : nativeApple ? (
            // Apple's own button (HIG): system artwork, white on the dark theme.
            <AppleAuthentication.AppleAuthenticationButton
              buttonType={AppleAuthentication.AppleAuthenticationButtonType.CONTINUE}
              buttonStyle={AppleAuthentication.AppleAuthenticationButtonStyle.WHITE}
              cornerRadius={radii.md}
              style={{ height: layout.touch, width: '100%', opacity: busy ? 0.5 : 1 }}
              onPress={() => {
                if (!busy) apple.mutate();
              }}
              testID="sign-in-apple"
            />
          ) : (
            <Button
              label="Continue with Apple"
              variant="secondary"
              onPress={() => apple.mutate()}
              loading={apple.isPending}
              disabled={busy}
              testID="sign-in-apple"
            />
          )}
          <Button
            label="Continue with Google"
            variant="secondary"
            onPress={() => google.mutate()}
            loading={google.isPending}
            disabled={busy}
            testID="sign-in-google"
          />
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }} aria-hidden>
            <View style={{ flex: 1, height: 1, backgroundColor: colors.border }} />
            <AppText variant="meta" subtle>
              or
            </AppText>
            <View style={{ flex: 1, height: 1, backgroundColor: colors.border }} />
          </View>
          <TextField
            label="Email"
            value={email}
            onChangeText={setEmail}
            placeholder="you@example.com"
            keyboardType="email-address"
            autoCapitalize="none"
            autoComplete="email"
            inputMode="email"
            onSubmitEditing={sendLink}
            returnKeyType="send"
            error={emailError}
            testID="sign-in-email"
          />
          <Button
            label="Email me a sign-in link"
            icon={Mail}
            onPress={sendLink}
            loading={startEmail.isPending || verify.isPending}
            disabled={busy}
            testID="sign-in-email-submit"
          />
          {failure ? (
            <AppText variant="meta" color={colors.loss} role="alert" testID="sign-in-error">
              {describeError(failure)}
            </AppText>
          ) : null}
        </Card>
      )}
    </Screen>
  );
}

/**
 * A native build made with APPLE_SIGN_IN=false (a free personal team cannot
 * sign the capability) has no Apple button at all.
 */
const appleSignInOff =
  Platform.OS !== 'web' && Constants.expoConfig?.ios?.usesAppleSignIn === false;

/** True on iOS 13+ devices, where Apple's native button must be used (HIG). */
function useNativeAppleButton(): boolean {
  const [available, setAvailable] = useState(false);
  useEffect(() => {
    if (Platform.OS !== 'ios') return;
    let alive = true;
    void AppleAuthentication.isAvailableAsync().then(
      (ok) => alive && setAvailable(ok),
      () => undefined,
    );
    return () => {
      alive = false;
    };
  }, []);
  return available;
}
