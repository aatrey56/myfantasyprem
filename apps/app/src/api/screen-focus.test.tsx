import { QueryClient, QueryClientProvider, useQuery } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';
import type { ReactNode } from 'react';

import { createQueryClient, DEFAULT_STALE_MS } from './query-client';
import { PagerSceneContext, useScreenFocus } from './screen-focus';

let mockFocused = true;
jest.mock('expo-router', () => ({ useIsFocused: () => mockFocused }));

const KEY = ['league', 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy'] as const;
const env = (n: number) => ({ data: { n }, meta: {} });

let client: QueryClient;
let fetches: number;
let clock: number;

beforeEach(() => {
  mockFocused = true;
  fetches = 0;
  clock = Date.now();
  jest.spyOn(Date, 'now').mockImplementation(() => clock);
  client = createQueryClient();
});
/**
 * Date.now is pinned, so no waitFor (its timeout reads the clock). Query
 * notifications go out on a timer: wait one out.
 */
const settle = () => act(() => new Promise<void>((r) => setTimeout(r, 10)));

afterEach(() => {
  client.clear();
  jest.restoreAllMocks();
});

/**
 * The Match tab's page with the pager on Status: the neighbour, so it
 * stays mounted and subscribed while hidden (the case refetchOnMount
 * never covers). Two observers of one query, like the league home and
 * the league context on one screen.
 */
function renderMatchPage(active: { current: string }) {
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <PagerSceneContext.Provider value={{ scene: 'matchup', active: active.current }}>
        {children}
      </PagerSceneContext.Provider>
    </QueryClientProvider>
  );
  const useMatchQuery = () => {
    const { subscribed } = useScreenFocus(KEY);
    return useQuery({
      queryKey: KEY,
      queryFn: async () => {
        fetches += 1;
        return env(fetches + 1);
      },
      subscribed,
    });
  };
  return renderHook(
    () => {
      const a = useMatchQuery();
      useMatchQuery();
      return a;
    },
    { wrapper },
  );
}

async function leaveAndReturn(
  view: Awaited<ReturnType<typeof renderMatchPage>>,
  active: { current: string },
) {
  active.current = 'index';
  mockFocused = false;
  await view.rerender({});
  await settle();
  return async () => {
    active.current = 'matchup';
    mockFocused = true;
    await view.rerender({});
    await settle();
  };
}

describe('returning to a pager tab that stayed mounted', () => {
  it('refetches stale data exactly once', async () => {
    client.setQueryData(KEY, env(1));
    const active = { current: 'matchup' };
    const view = await renderMatchPage(active);
    expect(view.result.current.data).toEqual(env(1));
    expect(fetches).toBe(0);

    const back = await leaveAndReturn(view, active);
    clock += DEFAULT_STALE_MS + 1;
    await back();

    await settle();
    expect(view.result.current.data).toEqual(env(2));
    expect(fetches).toBe(1);
  });

  it('leaves fresh data alone', async () => {
    client.setQueryData(KEY, env(1));
    const active = { current: 'matchup' };
    const view = await renderMatchPage(active);

    const back = await leaveAndReturn(view, active);
    clock += DEFAULT_STALE_MS - 1_000;
    await back();

    expect(view.result.current.data).toEqual(env(1));
    expect(fetches).toBe(0);
  });

  it('does not refetch on the first render or while hidden', async () => {
    client.setQueryData(KEY, env(1));
    clock += DEFAULT_STALE_MS + 1;
    mockFocused = false;
    const active = { current: 'index' };
    const view = await renderMatchPage(active);
    await settle();
    // Stale and subscribed (the neighbour): mount refetches once, nothing more.
    expect(fetches).toBe(1);
    clock += DEFAULT_STALE_MS + 1;
    await view.rerender({});
    await settle();
    expect(fetches).toBe(1);
  });
});
