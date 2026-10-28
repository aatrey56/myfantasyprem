/**
 * The auth middleware against a fake server that behaves like the real one:
 * each refresh rotates both tokens, the previous access token stops working,
 * and presenting an already-rotated refresh token is treated as reuse, which
 * revokes the whole session.
 */
import { send } from '@myfantasyprem/api-client';

import { api } from './client';
import {
  bindSessionStore,
  memorySessionStore,
  tokens,
  type SessionStore,
  type StoredSession,
} from '@/auth/session';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));

const USER = {
  id: 'usr_01k7m2xq8y0000000000000001',
  displayName: 'Sam',
  email: 'sam@example.com',
  avatarColor: '#2f6fed',
  authProviders: ['email'],
  createdAt: '2026-07-01T09:00:00Z',
} as StoredSession['user'];

const stored = (n: number): StoredSession => ({
  accessToken: `a${n}`,
  refreshToken: `r${n}`,
  expiresAt: '2099-01-01T00:00:00Z',
  refreshExpiresAt: '2099-01-01T00:00:00Z',
  user: USER,
});

const meta = { dataAsOf: '2026-10-12T00:00:00Z', serverTime: '2026-10-12T00:00:00Z', stale: false };
/** Native: Expo's fetch answers with its own class, not the global Response. */
let foreignResponses = false;
const foreign = (r: Response): Response =>
  ({
    status: r.status,
    statusText: r.statusText,
    ok: r.ok,
    headers: r.headers,
    url: r.url,
    body: r.body,
    bodyUsed: false,
    json: () => r.json(),
    text: () => r.text(),
    arrayBuffer: () => r.arrayBuffer(),
    blob: () => r.blob(),
    clone: () => foreign(r.clone()),
  }) as unknown as Response;
const json = (status: number, body: unknown) => {
  const r = new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': status >= 400 ? 'application/problem+json' : 'application/json' },
  });
  return foreignResponses ? foreign(r) : r;
};

/** The fake server's state. */
let server: { generation: number; revoked: boolean; refreshCalls: string[] };
/** Requests to /v1/me whose answer the test releases by hand. */
let held: { auth: string; release: () => void }[];
let holdNext = 0;

const realFetch = globalThis.fetch;
beforeAll(() => {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    const { pathname } = new URL(request.url);
    if (pathname === '/v1/auth/refresh') {
      const { refreshToken } = (await request.json()) as { refreshToken: string };
      server.refreshCalls.push(refreshToken);
      if (server.revoked || refreshToken !== `r${server.generation}`) {
        server.revoked = true; // reuse detection
        return json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 });
      }
      server.generation += 1;
      const s = stored(server.generation);
      return json(200, { data: { ...s, user: USER }, meta });
    }
    const auth = request.headers.get('authorization') ?? '';
    // Evaluated when the answer is released, like a slow upstream.
    const answer = () =>
      !server.revoked && auth === `Bearer a${server.generation}`
        ? json(200, { data: { ok: auth }, meta })
        : json(401, { type: 'about:blank', title: 'Unauthorized', status: 401 });
    if (holdNext > 0) {
      holdNext -= 1;
      // Decide now (the token was valid or not when the request arrived).
      const decided = answer();
      return new Promise<Response>((resolve) =>
        held.push({ auth, release: () => resolve(decided) }),
      );
    }
    return answer();
  }) as typeof fetch;
});
afterAll(() => {
  globalThis.fetch = realFetch;
});

let store: SessionStore;
beforeEach(() => {
  server = { generation: 1, revoked: false, refreshCalls: [] };
  held = [];
  holdNext = 0;
  foreignResponses = false;
  store = memorySessionStore();
  bindSessionStore(store);
  tokens.set(null); // written through to the bound store
});
afterEach(() => {
  bindSessionStore(null);
});

const me = () => send(api.GET('/v1/me'));
const flush = () => new Promise((r) => setTimeout(r, 0));

async function signIn(s: StoredSession) {
  tokens.set(s);
  await flush();
}

describe('responses from a fetch with its own Response class (Expo on native)', () => {
  beforeEach(() => {
    foreignResponses = true;
  });

  it('reads a plain answer', async () => {
    await signIn(stored(1));
    const res = await me();
    expect(res.data).toEqual({ ok: 'Bearer a1' });
  });

  it('refreshes and retries after a 401', async () => {
    await signIn({ ...stored(1), accessToken: 'a0' });
    const res = await me();
    expect(res.data).toEqual({ ok: 'Bearer a2' });
    expect(server.refreshCalls).toEqual(['r1']);
  });

  it('passes a refused request through as an error', async () => {
    await signIn({ ...stored(1), accessToken: 'a0' });
    server.revoked = true;
    await expect(me()).rejects.toMatchObject({ status: 401 });
  });
});

describe('auth refresh', () => {
  it('refreshes once and retries when the access token has expired', async () => {
    await signIn({ ...stored(1), accessToken: 'a0' }); // expired access token
    const res = await me();
    expect(res.data).toEqual({ ok: 'Bearer a2' });
    expect(server.refreshCalls).toEqual(['r1']);
  });

  it('does not rotate again for a 401 that arrives after the refresh finished', async () => {
    await signIn({ ...stored(1), accessToken: 'a0' }); // expired access token
    holdNext = 1;
    const late = me(); // sent with a0, answer held back
    await flush();
    expect(held).toHaveLength(1);
    const first = await me(); // 401 -> refresh -> a2 -> retry
    expect(first.data).toEqual({ ok: 'Bearer a2' });
    held[0]!.release(); // the late 401 for a0
    const second = await late;
    expect(second.data).toEqual({ ok: 'Bearer a2' });
    expect(server.refreshCalls).toEqual(['r1']);
    expect(tokens.get()?.accessToken).toBe('a2');
  });

  it("uses another tab's rotated session instead of reusing a stale refresh token", async () => {
    // This tab holds generation 1; another tab already rotated to 2 and saved it.
    await signIn({ ...stored(1), accessToken: 'a0' });
    server.generation = 2;
    server.refreshCalls = [];
    await store.save(stored(2));
    const res = await me();
    expect(res.data).toEqual({ ok: 'Bearer a2' });
    expect(server.revoked).toBe(false);
    expect(server.refreshCalls).toEqual([]);
    expect(tokens.get()?.refreshToken).toBe('r2');
  });

  it('does not adopt the old persisted session while a newer one is still being saved', async () => {
    // Storage still holds an earlier session for this user (r1); this tab has
    // just signed in again (r2, expired access token) and its save is slow.
    await store.save(stored(1));
    server.generation = 2;
    const slow = store;
    store = {
      ...slow,
      save: (s) => new Promise<void>((r) => setTimeout(() => void slow.save(s).then(r), 20)),
    };
    bindSessionStore(store);
    tokens.set({ ...stored(2), accessToken: 'a0' });
    const res = await me();
    expect(res.data).toEqual({ ok: 'Bearer a3' });
    expect(server.refreshCalls).toEqual(['r2']);
    expect(server.revoked).toBe(false);
    expect(tokens.get()?.refreshToken).toBe('r3');
  });

  it('does not adopt a persisted session this tab has already moved past', async () => {
    // Saving fails (private mode, quota), so storage keeps an older pair.
    await store.save(stored(1));
    server.generation = 2;
    store = { ...store, save: () => Promise.reject(new Error('quota')) };
    bindSessionStore(store);
    tokens.set({ ...stored(2), accessToken: 'a0' });
    await flush();
    const res = await me();
    expect(res.data).toEqual({ ok: 'Bearer a3' });
    expect(server.refreshCalls).toEqual(['r2']);
    expect(tokens.get()?.refreshToken).toBe('r3');
  });

  it('rotates under the cross-tab Web Lock when the browser has one', async () => {
    const names: string[] = [];
    const nav = globalThis.navigator as unknown as { locks?: unknown };
    const had = Object.getOwnPropertyDescriptor(nav, 'locks');
    Object.defineProperty(nav, 'locks', {
      configurable: true,
      value: { request: (name: string, cb: () => Promise<unknown>) => (names.push(name), cb()) },
    });
    try {
      await signIn({ ...stored(1), accessToken: 'a0' });
      const res = await me();
      expect(res.data).toEqual({ ok: 'Bearer a2' });
      expect(names).toEqual(['mfp.session.refresh']);
    } finally {
      if (had) Object.defineProperty(nav, 'locks', had);
      else delete nav.locks;
    }
  });

  it('signs out when the refresh token is refused', async () => {
    await signIn({ ...stored(1), accessToken: 'a0' });
    server.revoked = true;
    await expect(me()).rejects.toMatchObject({ status: 401 });
    expect(tokens.get()).toBeNull();
  });
});
