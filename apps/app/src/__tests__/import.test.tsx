/**
 * The hidden "later" import screen against a scripted server: it honours
 * Retry-After, shows (and stops on) a failing status poll, looks the same
 * input up again on a second press, and does not get stuck on a status it
 * does not know.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, fireEvent, render, screen, waitFor } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import ImportLeagueScreen from '@/app/import';
import { shouldRetry } from '@/api/query-client';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({
  useRouter: () => ({ replace: jest.fn(), push: jest.fn(), navigate: jest.fn() }),
}));
jest.mock('expo-router/head', () => ({ __esModule: true, default: () => null }));
jest.mock('@/components/PressableLink', () => {
  const { Pressable } = jest.requireActual('react-native');
  return { PressableLink: Pressable };
});

const IMPORT_ID = 'imp_01k7m2xq8y0000000000000001';
const meta = () => ({
  dataAsOf: new Date().toISOString(),
  serverTime: new Date().toISOString(),
  stale: false,
});
const job = (status: string) => ({
  id: IMPORT_ID,
  fplLeagueId: 12345,
  status,
  progress: { done: 1, total: 10 },
  error: null,
  leagueId: null,
  createdAt: '2026-10-12T00:00:00Z',
  updatedAt: '2026-10-12T00:00:00Z',
});
const json = (status: number, body: unknown, headers: Record<string, string> = {}) =>
  new Response(JSON.stringify(body), {
    status,
    headers: {
      'content-type': status >= 400 ? 'application/problem+json' : 'application/json',
      ...headers,
    },
  });
const notFound = () =>
  json(404, { type: 'about:blank', title: 'Not found', status: 404, detail: 'No such import.' });

let previewAnswers: (() => Response)[];
let postAnswer: () => Response;
let statusAnswer: () => Response;
const calls: string[] = [];

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const { pathname } = new URL(request.url);
    calls.push(`${request.method} ${pathname}`);
    if (pathname === '/v1/imports/preview') return (previewAnswers.shift() ?? previewOk)();
    if (pathname === '/v1/imports') return postAnswer();
    return statusAnswer();
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

const previewOk = () =>
  json(200, {
    data: {
      fplLeagueId: 12345,
      name: 'The Sunday Draft',
      teamCount: 2,
      seats: [
        { seat: 1, teamName: 'A', managerName: 'Ann' },
        { seat: 2, teamName: 'B', managerName: 'Bo' },
      ],
      currentGameweek: 7,
      scoring: 'h2h',
      draftComplete: true,
      importable: true,
      problem: null,
      alreadyImportedLeagueId: null,
    },
    meta: meta(),
  });

let queryClient: QueryClient;
beforeEach(() => {
  calls.length = 0;
  previewAnswers = [];
  postAnswer = () => json(202, { data: job('queued'), meta: meta() }, { 'Retry-After': '5' });
  statusAnswer = () => json(200, { data: job('running'), meta: meta() });
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: shouldRetry, retryDelay: 0, gcTime: Infinity } },
  });
});
afterEach(() => {
  queryClient.clear();
  jest.useRealTimers();
});

async function open() {
  await render(
    <SafeAreaProvider
      initialMetrics={{
        frame: { x: 0, y: 0, width: 375, height: 812 },
        insets: { top: 0, left: 0, right: 0, bottom: 0 },
      }}
    >
      <QueryClientProvider client={queryClient}>
        <ImportLeagueScreen />
      </QueryClientProvider>
    </SafeAreaProvider>,
  );
  await fireEvent.changeText(screen.getByTestId('league-input'), '12345');
  await fireEvent.press(screen.getByTestId('find-button'));
}

/** Picks a seat in the preview, then presses Import. */
async function importSeat(seat = 1) {
  await fireEvent.press(await screen.findByTestId(`seat-option-${seat}`));
  await fireEvent.press(screen.getByTestId('import-button'));
}

const statusPolls = () => calls.filter((c) => c === `GET /v1/imports/${IMPORT_ID}`).length;
const advance = (ms: number) =>
  act(async () => {
    jest.advanceTimersByTime(ms);
  });

describe('import screen', () => {
  it('needs a seat before importing and sends it', async () => {
    let body: unknown;
    const prevFetch = globalThis.fetch;
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      const request = new Request(input, init);
      if (request.method === 'POST') body = await request.clone().json();
      return prevFetch(input, init);
    }) as typeof fetch;
    try {
      await open();
      expect(await screen.findByTestId('preview-facts')).toHaveTextContent('2 teams · gameweek 7');
      expect(screen.getByTestId('import-button')).toBeDisabled();
      await importSeat(2);
      await waitFor(() => expect(body).toEqual({ fplLeagueId: 12345, seat: 2 }));
    } finally {
      globalThis.fetch = prevFetch;
    }
  });

  it('explains a league that cannot be imported', async () => {
    previewAnswers = [
      () =>
        json(200, {
          data: {
            fplLeagueId: 12345,
            name: 'Classic Pals',
            teamCount: 2,
            seats: [],
            currentGameweek: 7,
            scoring: 'classic',
            draftComplete: true,
            importable: false,
            problem: {
              type: 'https://myfantasyprem.example/problems/unsupported-league',
              title: 'Unsupported league',
              status: 422,
              detail: 'Only head-to-head leagues can be imported.',
            },
            alreadyImportedLeagueId: null,
          },
          meta: meta(),
        }),
    ];
    await open();
    expect(await screen.findByTestId('preview-not-importable')).toHaveTextContent(
      'Only head-to-head leagues can be imported.',
    );
    expect(screen.getByTestId('import-button')).toBeDisabled();
  });

  it('waits Retry-After before polling the new import', async () => {
    jest.useFakeTimers();
    await open();
    await importSeat();
    await waitFor(() => expect(screen.getByTestId('import-status')).toBeOnTheScreen());
    await advance(4_000);
    expect(statusPolls()).toBe(0);
    await advance(1_500);
    expect(statusPolls()).toBe(1);
  });

  it('shows a failing status poll and stops polling a missing import', async () => {
    jest.useFakeTimers();
    statusAnswer = notFound;
    await open();
    await importSeat();
    await advance(5_500);
    await waitFor(() =>
      expect(screen.getByTestId('import-status-error')).toHaveTextContent(/No such import/),
    );
    const polls = statusPolls();
    await advance(60_000);
    expect(statusPolls()).toBe(polls);
  });

  it('looks the same input up again when Find is pressed after an error', async () => {
    previewAnswers = [notFound];
    await open();
    expect(await screen.findByTestId('preview-error')).toBeOnTheScreen();
    await fireEvent.press(screen.getByTestId('find-button'));
    expect(await screen.findByTestId('preview')).toBeOnTheScreen();
    expect(calls.filter((c) => c === 'GET /v1/imports/preview')).toHaveLength(2);
  });

  it('does not get stuck on an import status it does not know', async () => {
    postAnswer = () => json(200, { data: job('paused'), meta: meta() });
    await open();
    await importSeat();
    const status = await screen.findByTestId('import-status');
    expect(status).not.toHaveTextContent(/Importing/);
    expect(screen.getByTestId('import-button')).toBeEnabled();
  });
});
