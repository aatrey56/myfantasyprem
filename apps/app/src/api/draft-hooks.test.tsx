/**
 * Rapid queue edits: each PUT replaces the whole queue, so answers that
 * arrive out of order, or a failure while a later edit is in flight, must
 * never leave the screen (or the next edit) on an older queue.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { useSetDraftQueue } from './draft-hooks';
import { queryKeys } from './hooks';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

const LEAGUE = 'lg_01k7m2xq800000000000000001';
const meta = { dataAsOf: '2026-10-12T00:00:00Z', serverTime: '2026-10-12T00:00:00Z', stale: false };
const item = (id: string) => ({ player: { id } }) as unknown as Schemas['DraftQueueItem'];
const queue = (ids: string[]) => ({ data: { players: ids.map(item), autopick: false }, meta });
const ids = (env: unknown) =>
  (env as { data: { players: { player: { id: string } }[] } } | undefined)?.data.players.map(
    (p) => p.player.id,
  );

/** The server applies each PUT on arrival; the test decides when each answer lands. */
let serverQueue: string[];
let held: { release: () => void }[];
let failNext = 0;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const { playerIds } = (await request.json()) as { playerIds: string[] };
    let response: Response;
    if (failNext > 0) {
      failNext -= 1;
      response = new Response(
        JSON.stringify({ type: 'about:blank', title: 'Unprocessable', status: 422 }),
        { status: 422, headers: { 'content-type': 'application/problem+json' } },
      );
    } else {
      serverQueue = playerIds;
      response = new Response(JSON.stringify(queue(playerIds)), {
        status: 200,
        headers: { 'content-type': 'application/json' },
      });
    }
    return new Promise<Response>((resolve) => held.push({ release: () => resolve(response) }));
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let queryClient: QueryClient;
beforeEach(() => {
  serverQueue = [];
  held = [];
  failNext = 0;
  queryClient = new QueryClient({
    defaultOptions: {
      queries: { retry: false, gcTime: Infinity },
      mutations: { gcTime: Infinity },
    },
  });
  queryClient.setQueryData(queryKeys.draftQueue(LEAGUE), queue([]));
});
afterEach(() => queryClient.clear());

const wrapper = ({ children }: { children: ReactNode }) => (
  <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
);
const cached = () => ids(queryClient.getQueryData(queryKeys.draftQueue(LEAGUE)));

/** Releases whatever answer is waiting, newest first, until nothing is in flight. */
async function settle() {
  for (let i = 0; i < 20; i++) {
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    const next = held.pop();
    if (next) {
      await act(async () => next.release());
    } else if (queryClient.isMutating() === 0) {
      return;
    }
  }
}

describe('useSetDraftQueue', () => {
  it('keeps the latest edit when answers come back out of order', async () => {
    const { result } = await renderHook(() => useSetDraftQueue(LEAGUE), { wrapper });
    await act(async () => {
      result.current.mutate([item('plr_a')]);
      result.current.mutate([item('plr_a'), item('plr_b')]);
    });
    expect(cached()).toEqual(['plr_a', 'plr_b']);
    // Answers land newest first; at no point may the older queue show.
    for (let i = 0; i < 20 && (held.length || queryClient.isMutating()); i++) {
      await act(async () => {
        await new Promise((r) => setTimeout(r, 0));
      });
      const next = held.pop();
      if (next) await act(async () => next.release());
      expect(cached()).toEqual(['plr_a', 'plr_b']);
    }
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(serverQueue).toEqual(['plr_a', 'plr_b']);
    expect(cached()).toEqual(['plr_a', 'plr_b']);
  });

  it('does not roll back a later edit when an earlier one fails', async () => {
    const { result } = await renderHook(() => useSetDraftQueue(LEAGUE), { wrapper });
    failNext = 1;
    await act(async () => {
      result.current.mutate([item('plr_a')]);
      result.current.mutate([item('plr_a'), item('plr_b')]);
    });
    // The failure lands first, while the second edit is still pending.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });
    await act(async () => held.shift()!.release());
    expect(cached()).toEqual(['plr_a', 'plr_b']);
    await settle();
    expect(serverQueue).toEqual(['plr_a', 'plr_b']);
    expect(cached()).toEqual(['plr_a', 'plr_b']);
  });
});
