import type { Schemas } from '@myfantasyprem/api-client';

import { ProviderUnavailableError } from './errors';
import { nativeAppleCredential, nativeGoogleCredential } from './native-credentials';

export { ProviderUnavailableError, SignInCancelledError, isSignInCancelled } from './errors';

/**
 * Apple's identity token, authorization code and the raw nonce whose
 * SHA-256 was sent to Apple (the server checks it against the token). The
 * mock build returns placeholders the MSW handlers accept; iOS uses
 * expo-apple-authentication; web is not wired yet (Apple JS).
 */
export async function getAppleCredential(): Promise<Schemas['AppleSignInRequest']> {
  if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true') {
    return {
      identityToken: 'mock-apple-identity-token',
      authorizationCode: 'mock-apple-code',
      nonce: 'mock-apple-nonce',
    };
  }
  const credential = await nativeAppleCredential();
  if (!credential) throw new ProviderUnavailableError('Apple');
  return credential;
}

/**
 * Google's ID token. Mock build: a placeholder; iOS: expo-auth-session
 * (authorization code + PKCE against the iOS OAuth client, then the code is
 * exchanged for the ID token); web: Google Identity Services (not wired yet).
 */
export async function getGoogleCredential(): Promise<Schemas['GoogleSignInRequest']> {
  if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true') {
    return { idToken: 'mock-google-id-token' };
  }
  const credential = await nativeGoogleCredential();
  if (!credential) throw new ProviderUnavailableError('Google');
  return credential;
}

/**
 * The mock build sends no email: this says which codes work on the "Check
 * your inbox" screen. Null in real builds.
 */
export function mockSignInCodeHint(): string | null {
  if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true') {
    return 'Mock build: any 6-digit code signs in, except 000000.';
  }
  return null;
}
