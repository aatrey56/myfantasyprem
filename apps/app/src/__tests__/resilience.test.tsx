import { ApiError, MalformedResponseError, send, type Schemas } from '@myfantasyprem/api-client';
import { onlineManager, QueryClient } from '@tanstack/react-query';
import { act, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { computeFreshness, useFreshness, type FreshnessQuery } from '@/api/freshness';
import { shouldRetry } from '@/api/query-client';
import { QueryStatus, type QueryLike } from '@/components/QueryStatus';
import { StaleBanner } from '@/components/StaleBanner';
import { AppText } from '@/components/AppText';
import { STALE_AFTER_MS } from '@/constants/app';
import { formatAsOf } from '@/lib/time';

const METRICS = {
  frame: { x: 0, y: 0, width: 375, height: 812 },
  insets: { top: 0, left: 0, right: 0, bottom: 0 },
};
const wrap = (ui: ReactNode) => <SafeAreaProvider initialMetrics={METRICS}>{ui}</SafeAreaProvider>;

type Env = { data: string; meta: Schemas['Meta'] };
const NOW = Date.parse('2026-10-17T14:00:00Z');

function envelope(over: Partial<Schemas['Meta']> = {}): Env {
  return {
    data: 'payload',
    meta: {
      dataAsOf: new Date(NOW - 12_000).toISOString(),
      serverTime: new Date(NOW).toISOString(),
      stale: false,
      // Live: stale 3 minutes after the last good poll.
      staleAt: new Date(NOW - 12_000 + STALE_AFTER_MS).toISOString(),
      ...over,
    },
  };
}

function queryLike(over: Partial<QueryLike<Env>>): QueryLike<Env> {
  return {
    data: undefined,
    error: null,
    isPending: false,
    isError: false,
    isLoadingError: false,
    isRefetchError: false,
    dataUpdatedAt: NOW,
    errorUpdatedAt: 0,
    refetch: jest.fn(),
    ...over,
  };
}

describe('QueryStatus (stale-while-revalidate)', () => {
  const show = (q: QueryLike<Env>) =>
    render(wrap(<QueryStatus query={q}>{(d) => <AppText>{d.data}</AppText>}</QueryStatus>));

  it('keeps rendering data on a refetch error, with a refresh strip', async () => {
    const q = queryLike({
      data: envelope(),
      isError: true,
      isRefetchError: true,
      error: new ApiError(503),
      errorUpdatedAt: NOW + 1,
    });
    await show(q);
    expect(screen.getByText('payload')).toBeOnTheScreen();
    expect(screen.queryByTestId('query-error')).toBeNull();
    expect(screen.getByTestId('refresh-error')).toHaveTextContent(
      `Couldn't refresh · last updated ${formatAsOf(NOW)} ·Retry`,
    );
  });

  it('shows the full error card only when there is no data', async () => {
    await show(
      queryLike({
        isError: true,
        isLoadingError: true,
        error: new ApiError(404, {
          type: 'about:blank',
          title: 'Not found',
          status: 404,
          detail: 'League not found.',
        }),
      }),
    );
    expect(screen.getByTestId('query-error')).toHaveTextContent(/League not found\./);
    expect(screen.queryByText('payload')).toBeNull();
  });

  it('explains a malformed envelope', async () => {
    await show(
      queryLike({ isError: true, isLoadingError: true, error: new MalformedResponseError(200) }),
    );
    expect(screen.getByTestId('query-error')).toHaveTextContent(/cannot read/);
  });
});

describe('malformed responses', () => {
  const response = (status = 200) => new Response(null, { status });

  it('rejects a 2xx body that is not a {data, meta} envelope', async () => {
    await expect(
      send(Promise.resolve({ data: { data: 1 }, response: response() })),
    ).rejects.toBeInstanceOf(MalformedResponseError);
    await expect(
      send(
        Promise.resolve({
          data: { data: 1, meta: { stale: false, dataAsOf: 'x' } },
          response: response(),
        }),
      ),
    ).rejects.toBeInstanceOf(MalformedResponseError); // no serverTime
  });

  it('turns an unparseable 2xx body into MalformedResponseError, not a retryable error', async () => {
    await expect(
      send(Promise.reject(new SyntaxError('Unexpected token <'))),
    ).rejects.toBeInstanceOf(MalformedResponseError);
    expect(shouldRetry(0, new MalformedResponseError(200))).toBe(false);
  });

  it('passes a valid envelope through', async () => {
    const body = envelope();
    await expect(send(Promise.resolve({ data: body, response: response() }))).resolves.toBe(body);
  });
});

describe('retry policy', () => {
  it.each([
    [400, false],
    [404, false],
    [422, false],
    [408, true],
    [429, true],
    [500, true],
    [503, true],
  ])('status %i retries: %s', (status, expected) => {
    expect(shouldRetry(0, new ApiError(status))).toBe(expected);
  });

  it('retries network errors, at most 3 times', async () => {
    expect(shouldRetry(0, new TypeError('Failed to fetch'))).toBe(true);
    expect(shouldRetry(2, new TypeError('Failed to fetch'))).toBe(true);
    expect(shouldRetry(3, new TypeError('Failed to fetch'))).toBe(false);
  });

  async function attempts(error: Error) {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: shouldRetry, retryDelay: 0 } },
    });
    const queryFn = jest.fn(() => Promise.reject(error));
    await client.fetchQuery({ queryKey: ['x'], queryFn }).catch(() => {});
    client.clear();
    return queryFn.mock.calls.length;
  }

  it('does not retry 404 or 422 through a real QueryClient', async () => {
    expect(await attempts(new ApiError(404))).toBe(1);
    expect(await attempts(new ApiError(422))).toBe(1);
  });

  it('retries 503 three times through a real QueryClient', async () => {
    expect(await attempts(new ApiError(503))).toBe(4);
  });
});

describe('freshness', () => {
  const base = (over: Partial<FreshnessQuery> = {}): FreshnessQuery => ({
    data: envelope(),
    dataUpdatedAt: NOW,
    errorUpdatedAt: 0,
    isRefetchError: false,
    ...over,
  });

  it('orders offline, then server unreachable, then upstream stale', async () => {
    const everythingWrong = base({ data: envelope({ stale: true }), isRefetchError: true });
    expect(computeFreshness(everythingWrong, NOW, false).kind).toBe('offline');
    expect(computeFreshness(everythingWrong, NOW, true).kind).toBe('serverUnreachable');
    expect(computeFreshness(base({ data: envelope({ stale: true }) }), NOW, true).kind).toBe(
      'upstreamStale',
    );
    expect(computeFreshness(base(), NOW, true).kind).toBe('fresh');
  });

  it('treats an error newer than the data as unreachable', async () => {
    expect(computeFreshness(base({ errorUpdatedAt: NOW + 5 }), NOW, true)).toEqual({
      kind: 'serverUnreachable',
      lastUpdatedAt: NOW,
    });
  });

  it('ages dataAsOf against serverTime, not the device clock', async () => {
    // Device clock 1 hour ahead of the server: data is still only 12 s old.
    const skewed = base({ dataUpdatedAt: NOW + 3_600_000 });
    expect(computeFreshness(skewed, NOW + 3_600_000, true).kind).toBe('fresh');
    // Past STALE_AFTER_MS since dataAsOf, it goes stale even without meta.stale.
    const later = NOW + 3_600_000 + STALE_AFTER_MS;
    expect(computeFreshness(skewed, later, true)).toEqual({
      kind: 'upstreamStale',
      since: NOW + 3_600_000 - 12_000,
    });
  });

  // The live report: between match days the poller is idle (last good poll
  // 19:09, next in 16 h), and the app flagged the data as delayed minutes on.
  it('keeps idle data fresh until the next scheduled poll is overdue', async () => {
    const polled = Date.parse('2026-10-17T19:09:00Z');
    const idle = base({
      dataUpdatedAt: polled + 60_000,
      data: envelope({
        dataAsOf: new Date(polled + 60_000).toISOString(),
        serverTime: new Date(polled + 60_000).toISOString(),
        staleAt: new Date(polled + 16 * 3_600_000 + STALE_AFTER_MS).toISOString(),
      }),
    });
    expect(computeFreshness(idle, polled + 10 * 60_000, true).kind).toBe('fresh');
    expect(computeFreshness(idle, polled + 4 * 3_600_000, true).kind).toBe('fresh');
    expect(computeFreshness(idle, polled + 16 * 3_600_000 + STALE_AFTER_MS + 1, true).kind).toBe(
      'upstreamStale',
    );
  });

  it('never calls league state (no staleAt) delayed by its age alone; the server flag still counts', async () => {
    const league = base({
      data: envelope({ dataAsOf: new Date(NOW).toISOString(), staleAt: undefined }),
    });
    expect(computeFreshness(league, NOW + 6 * 3_600_000, true).kind).toBe('fresh');
    const flagged = base({ data: envelope({ staleAt: undefined, stale: true }) });
    expect(computeFreshness(flagged, NOW, true).kind).toBe('upstreamStale');
  });

  function Probe({ query }: { query: FreshnessQuery }) {
    return <StaleBanner freshness={useFreshness(query)} />;
  }

  describe('banners with fake timers', () => {
    beforeEach(() => jest.useFakeTimers({ now: NOW }));
    afterEach(() => jest.useRealTimers());

    it('shows "Player data delayed" once data passes STALE_AFTER_MS, as a polite live region', async () => {
      await render(wrap(<Probe query={base()} />));
      expect(screen.queryByTestId('stale-banner')).toBeNull();
      await act(async () => jest.advanceTimersByTime(STALE_AFTER_MS));
      const banner = screen.getByTestId('stale-banner');
      expect(banner).toHaveTextContent(`Player data delayed since ${formatAsOf(NOW - 12_000)}`);
      expect(banner.props.accessibilityLiveRegion).toBe('polite');
      expect(banner.props.accessibilityRole).not.toBe('alert');
    });

    it('shows the unreachable banner after a failed refetch', async () => {
      await render(wrap(<Probe query={base({ isRefetchError: true, errorUpdatedAt: NOW + 1 })} />));
      expect(screen.getByTestId('unreachable-banner')).toHaveTextContent(
        `Can't reach the server · showing data from ${formatAsOf(NOW)}`,
      );
    });

    it('shows the offline banner while offline', async () => {
      await render(wrap(<Probe query={base()} />));
      await act(async () => onlineManager.setOnline(false));
      expect(screen.getByTestId('offline-banner')).toHaveTextContent("You're offline");
      await act(async () => onlineManager.setOnline(true));
      expect(screen.queryByTestId('offline-banner')).toBeNull();
    });
  });
});
