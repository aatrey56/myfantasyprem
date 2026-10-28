import type { Schemas } from '@myfantasyprem/api-client';

/**
 * Web: Sign in with Apple JS and Google Identity Services are not wired
 * yet, so both providers report unavailable (the mock build never gets
 * here).
 */
export async function nativeAppleCredential(): Promise<Schemas['AppleSignInRequest'] | null> {
  return null;
}

export async function nativeGoogleCredential(): Promise<Schemas['GoogleSignInRequest'] | null> {
  return null;
}
