import { send } from '@myfantasyprem/api-client';

import { api, REQUEST_TIMEOUT, timeoutFetch } from './client';
import { describeError } from '@/lib/errors';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

const realFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = realFetch;
  jest.useRealTimers();
});

/** A server that never answers, but honours an abort as fetch does. */
const hang = jest.fn(
  (_input: RequestInfo | URL, init?: RequestInit) =>
    new Promise<Response>((_resolve, reject) => {
      init?.signal?.addEventListener('abort', () =>
        reject(Object.assign(new Error('aborted'), { name: 'AbortError' })),
      );
    }),
);

/** Headers arrive, then the connection drops: the body never closes. */
function stalledBody(): Response {
  const body = new ReadableStream<Uint8Array>({
    start(c) {
      c.enqueue(new TextEncoder().encode('{"data":'));
    },
  });
  return new Response(body, { headers: { 'Content-Type': 'application/json' } });
}

describe('request timeout', () => {
  it('a server that never answers fails as unreachable after the timeout', async () => {
    jest.useFakeTimers();
    globalThis.fetch = hang as typeof fetch;
    const req = send(api.GET('/v1/me')).catch((e: unknown) => e);
    await jest.advanceTimersByTimeAsync(REQUEST_TIMEOUT.ms - 1);
    expect(hang).toHaveBeenCalled();
    await jest.advanceTimersByTimeAsync(1);
    const error = await req;
    expect(error).toBeInstanceOf(TypeError);
    expect(describeError(error)).toBe("Can't reach the server. Check your connection.");
  });

  it("the caller's own abort still aborts, as an abort", async () => {
    globalThis.fetch = hang as typeof fetch;
    const ctl = new AbortController();
    const req = timeoutFetch('http://api.test/v1/me', { signal: ctl.signal }).catch(
      (e: unknown) => e,
    );
    ctl.abort();
    const error = (await req) as Error;
    expect(error).not.toBeInstanceOf(TypeError);
    expect(error.name).toBe('AbortError');
  });

  it('a body that stops mid-stream still fails as unreachable after the timeout', async () => {
    jest.useFakeTimers();
    globalThis.fetch = jest.fn(async () => stalledBody()) as typeof fetch;
    const req = send(api.GET('/v1/me')).catch((e: unknown) => e);
    await jest.advanceTimersByTimeAsync(REQUEST_TIMEOUT.ms);
    const error = await req;
    expect(error).toBeInstanceOf(TypeError);
    expect(describeError(error)).toBe("Can't reach the server. Check your connection.");
  });

  it("the caller's abort still aborts while the body is being read", async () => {
    globalThis.fetch = jest.fn(async () => stalledBody()) as typeof fetch;
    const ctl = new AbortController();
    const res = await timeoutFetch('http://api.test/v1/me', { signal: ctl.signal });
    const read = res.json().catch((e: unknown) => e);
    ctl.abort();
    const error = (await read) as Error;
    expect(error).not.toBeInstanceOf(TypeError);
    expect(error.name).toBe('AbortError');
  });

  it('a quick answer passes through untouched', async () => {
    globalThis.fetch = jest.fn(async () => new Response('ok')) as typeof fetch;
    await expect((await timeoutFetch('http://api.test/x')).text()).resolves.toBe('ok');
  });
});
