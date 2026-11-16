/**
 * The sign-in page offers only what the server accepts (GET /v1/config
 * `signIn`): production has no email, Apple or Google, so invited friends
 * see how to get in instead of buttons that fail.
 */
import { fireEvent, screen } from '@testing-library/react-native';
import Constants from 'expo-constants';
import { renderRouter } from 'expo-router/testing-library';
import { getResponse, HttpResponse } from 'msw';

import { persistOptions } from '@/api/persist';
import { memorySessionStore, tokens } from '@/auth/session';
import { handlers, resetMocks } from '@/mocks/handlers';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

const mockStore = memorySessionStore();
jest.mock('@/auth/session-store', () => ({
  get platformSessionStore() {
    return mockStore;
  },
}));

type Methods = { email: boolean; apple: boolean; google: boolean; claimLink: boolean };

/** What GET /v1/config answers: methods, a failure, or held (still loading). */
let config: Methods | 'fail' | 'hold' | undefined;
let configCalls = 0;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    if (request.url.endsWith('/v1/config') && config !== undefined) {
      configCalls++;
      if (config === 'hold') return new Promise<Response>(() => undefined);
      if (config === 'fail') {
        return HttpResponse.json(
          { type: 'about:blank', title: 'Bad request', status: 400 },
          { status: 400, headers: { 'Content-Type': 'application/problem+json' } },
        );
      }
      const now = '2026-11-16T12:00:00Z';
      return HttpResponse.json({
        data: { brandedAssets: true, signIn: config },
        meta: { dataAsOf: now, serverTime: now, stale: false },
      });
    }
    const response = await getResponse(handlers, request);
    if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
    return response;
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});
beforeEach(async () => {
  await persistOptions.persister.removeClient();
  resetMocks();
  tokens.set(null);
  config = undefined;
  configCalls = 0;
});

const INVITE_ONLY =
  'This league is invite-only. Open the claim link or sign-in link your commissioner sent you.';

const methods = (on: Partial<Methods>): Methods => ({
  email: false,
  apple: false,
  google: false,
  claimLink: true,
  ...on,
});

/** Which of the three methods the page shows. */
function shown() {
  return {
    apple: screen.queryByTestId('sign-in-apple') !== null,
    google: screen.queryByTestId('sign-in-google') !== null,
    email: screen.queryByTestId('sign-in-email-submit') !== null,
  };
}

describe('sign-in methods', () => {
  const combos: Partial<Methods>[] = [
    { email: true, apple: true, google: true },
    { email: true },
    { apple: true },
    { google: true },
    { email: true, apple: true },
    { email: true, google: true },
    { apple: true, google: true },
  ];
  it.each(combos)('shows only the enabled methods: %j', async (on) => {
    config = methods(on);
    await renderRouter('./src/app', { initialUrl: '/sign-in' });
    await screen.findByText(/^Sign in to /);
    await screen.findByTestId(
      on.apple ? 'sign-in-apple' : on.google ? 'sign-in-google' : 'sign-in-email-submit',
    );
    expect(shown()).toEqual({
      apple: on.apple === true,
      google: on.google === true,
      email: on.email === true,
    });
    expect(screen.queryByTestId('sign-in-invite-only')).toBeNull();
  });

  it('with none of them, says the league is invite-only', async () => {
    config = methods({});
    await renderRouter('./src/app', { initialUrl: '/sign-in' });
    expect(await screen.findByTestId('sign-in-invite-only')).toHaveTextContent(INVITE_ONLY, {
      exact: false,
    });
    expect(shown()).toEqual({ apple: false, google: false, email: false });
    expect(screen.queryByTestId('sign-in-config-retry')).toBeNull();
  });

  it('shows no buttons while the config loads', async () => {
    config = 'hold';
    await renderRouter('./src/app', { initialUrl: '/sign-in' });
    expect(await screen.findByTestId('sign-in-loading')).toBeOnTheScreen();
    expect(shown()).toEqual({ apple: false, google: false, email: false });
    expect(screen.queryByTestId('sign-in-invite-only')).toBeNull();
  });

  it('when the config cannot be read, shows the message and retries', async () => {
    config = 'fail';
    await renderRouter('./src/app', { initialUrl: '/sign-in' });
    expect(await screen.findByTestId('sign-in-invite-only')).toHaveTextContent(INVITE_ONLY, {
      exact: false,
    });
    expect(shown()).toEqual({ apple: false, google: false, email: false });
    const before = configCalls;
    config = methods({ email: true });
    await fireEvent.press(screen.getByTestId('sign-in-config-retry'));
    expect(await screen.findByTestId('sign-in-email-submit')).toBeOnTheScreen();
    expect(configCalls).toBeGreaterThan(before);
    expect(screen.queryByTestId('sign-in-invite-only')).toBeNull();
  });

  it('the mocks offer every method', async () => {
    await renderRouter('./src/app', { initialUrl: '/sign-in' });
    await screen.findByTestId('sign-in-email-submit');
    expect(shown()).toEqual({ apple: true, google: true, email: true });
  });

  it('with every method off, still says why an expired link failed', async () => {
    config = methods({});
    await renderRouter('./src/app', { initialUrl: '/sign-in?token=expired-token-0000' });
    await fireEvent.press(await screen.findByTestId('magic-link-continue'));
    expect(await screen.findByTestId('sign-in-error')).toHaveTextContent(
      /expired or was already used/,
    );
    expect(await screen.findByTestId('sign-in-invite-only')).toHaveTextContent(INVITE_ONLY, {
      exact: false,
    });
  });
});
