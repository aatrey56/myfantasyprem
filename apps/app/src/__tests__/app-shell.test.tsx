import { renderRouter, screen } from 'expo-router/testing-library';

import { APP_NAME } from '@/constants/app';

// The shell: root layout, providers, stale banner and the public routes.
describe('app shell', () => {
  it('renders the (hidden, later) import screen without a disclaimer footer', async () => {
    await renderRouter('./src/app', { initialUrl: '/import' });
    expect(await screen.findByText('Import a league')).toBeOnTheScreen();
    // The in-app disclaimer was removed at the owner's request (AGENTS.md).
    expect(screen.queryByText(/Not affiliated/)).toBeNull();
    expect(screen.queryByTestId('footer')).toBeNull();
    expect(screen.queryByTestId('stale-banner')).toBeNull();
  });

  it('sends a signed-out visitor from the root to sign in', async () => {
    await renderRouter('./src/app', { initialUrl: '/' });
    expect(await screen.findByText(`Sign in to ${APP_NAME}`)).toBeOnTheScreen();
  });

  it('sends a signed-out visitor from a league to sign in', async () => {
    await renderRouter('./src/app', { initialUrl: '/leagues' });
    expect(await screen.findByText(`Sign in to ${APP_NAME}`)).toBeOnTheScreen();
  });
});
