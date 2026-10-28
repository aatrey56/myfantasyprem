/** The identity provider's SDK is not wired into this build or platform. */
export class ProviderUnavailableError extends Error {
  constructor(provider: 'Apple' | 'Google') {
    super(`Sign in with ${provider} isn't available in this build yet. Use email for now.`);
    this.name = 'ProviderUnavailableError';
  }
}

/** The user closed the Apple or Google sheet: not an error worth showing. */
export class SignInCancelledError extends Error {
  constructor() {
    super('Sign-in was cancelled.');
    this.name = 'SignInCancelledError';
  }
}

export function isSignInCancelled(error: unknown): boolean {
  return error instanceof SignInCancelledError;
}
