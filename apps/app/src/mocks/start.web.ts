import type { ScenarioName } from './scenarios';

declare global {
  interface Window {
    /** Mock builds only: switch MSW failure scenarios from tests. */
    __mfpMocks?: { use: (name: ScenarioName) => void; reset: () => void };
  }
}

/**
 * Web: start the MSW service worker (`/mockServiceWorker.js`, copied in only
 * by the mock build and the mock dev server). Production builds compile this
 * branch away: the env check is inlined, so nothing here reaches the bundle.
 */
export async function startMocks(): Promise<void> {
  if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true') {
    const [{ setupWorker }, { handlers }, { scenarios, isScenario }] = await Promise.all([
      import('msw/browser'),
      import('./handlers'),
      import('./scenarios'),
    ]);
    const worker = setupWorker(...handlers);
    await worker.start({
      onUnhandledFrame: 'bypass',
      quiet: true,
      serviceWorker: { url: '/mockServiceWorker.js' },
    });
    const applyScenario = (name: ScenarioName) => worker.use(...scenarios[name]);
    window.__mfpMocks = { use: applyScenario, reset: () => worker.resetHandlers() };
    let initial: string | null = null;
    try {
      initial = localStorage.getItem('mfp.mockScenario');
    } catch {
      // Storage can be unavailable (private mode); run without a scenario.
    }
    if (isScenario(initial)) applyScenario(initial);
  }
}
