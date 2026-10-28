import type { Schemas } from '@myfantasyprem/api-client';
import * as AppleAuthentication from 'expo-apple-authentication';
import { AuthRequest, ResponseType, exchangeCodeAsync } from 'expo-auth-session';
import * as Crypto from 'expo-crypto';
import { Platform } from 'react-native';

import { SignInCancelledError } from './errors';

/**
 * Native (iOS) identity-provider SDKs. Each returns null when the provider
 * is not available on this device or build, and throws
 * SignInCancelledError when the user closes the sheet. Web has its own
 * module (`native-credentials.web.ts`).
 */

/** Sign in with Apple: the system sheet, with a hashed nonce the server can check. */
export async function nativeAppleCredential(): Promise<Schemas['AppleSignInRequest'] | null> {
  if (Platform.OS !== 'ios' || !(await AppleAuthentication.isAvailableAsync())) return null;
  const nonce = Crypto.randomUUID();
  const hashed = await Crypto.digestStringAsync(Crypto.CryptoDigestAlgorithm.SHA256, nonce);
  try {
    const credential = await AppleAuthentication.signInAsync({
      requestedScopes: [
        AppleAuthentication.AppleAuthenticationScope.FULL_NAME,
        AppleAuthentication.AppleAuthenticationScope.EMAIL,
      ],
      nonce: hashed,
    });
    if (!credential.identityToken || !credential.authorizationCode) {
      throw new Error('Apple did not return a sign-in token. Try again.');
    }
    return {
      identityToken: credential.identityToken,
      authorizationCode: credential.authorizationCode,
      nonce,
      // Apple sends the name only on the first sign-in; the server keeps it.
      givenName: credential.fullName?.givenName ?? undefined,
      familyName: credential.fullName?.familyName ?? undefined,
    };
  } catch (e) {
    if ((e as { code?: string }).code === 'ERR_REQUEST_CANCELED') throw new SignInCancelledError();
    throw e;
  }
}

const GOOGLE = {
  authorizationEndpoint: 'https://accounts.google.com/o/oauth2/v2/auth',
  tokenEndpoint: 'https://oauth2.googleapis.com/token',
};

/**
 * Google on iOS through the system browser sheet (ASWebAuthenticationSession):
 * authorization code + PKCE against the iOS OAuth client, redirecting to the
 * reversed client id (registered as a URL scheme by app.config.ts), then the
 * code is exchanged for an ID token. Needs EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID.
 */
export async function nativeGoogleCredential(): Promise<Schemas['GoogleSignInRequest'] | null> {
  const clientId = process.env.EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID;
  if (Platform.OS !== 'ios' || !clientId) return null;
  const redirectUri = `${clientId.split('.').reverse().join('.')}:/oauthredirect`;
  const request = new AuthRequest({
    clientId,
    redirectUri,
    responseType: ResponseType.Code,
    scopes: ['openid', 'email', 'profile'],
    usePKCE: true,
  });
  const result = await request.promptAsync(GOOGLE);
  if (result.type === 'cancel' || result.type === 'dismiss') throw new SignInCancelledError();
  if (result.type !== 'success') {
    throw new Error(
      result.type === 'error'
        ? (result.error?.message ?? 'Google sign-in failed.')
        : 'Google sign-in failed.',
    );
  }
  const tokens = await exchangeCodeAsync(
    {
      clientId,
      redirectUri,
      code: result.params.code!,
      extraParams: { code_verifier: request.codeVerifier ?? '' },
    },
    GOOGLE,
  );
  if (!tokens.idToken) throw new Error('Google did not return an ID token. Try again.');
  return { idToken: tokens.idToken };
}
