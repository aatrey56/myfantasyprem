import { createApiClient, type Schemas } from '@myfantasyprem/api-client';

import { API_BASE_URL } from './config';
import { tokens, fromSession, isSuperseded, loadPersistedSession } from '@/auth/session';

/** How long a request may take before it fails as unreachable. */
export const REQUEST_TIMEOUT = { ms: 15_000 };

/** The body readers a caller may use; each is guarded until it settles. */
const BODY_READS = ['json', 'text', 'arrayBuffer', 'blob', 'formData'] as const;

/**
 * fetch that gives up after `ms` (REQUEST_TIMEOUT by default): an
 * unreachable server (a dropped VPN, a captive Wi-Fi) can leave a request
 * hanging for minutes, holding spinners and pull-to-refresh. It fails as a
 * TypeError, like any network error, so it gets the same "Can't reach the
 * server" message and retries; the caller's own abort still works.
 *
 * Both stay armed until the body is read, not just until headers arrive: a
 * connection that drops mid-body would otherwise leave `json()` waiting for
 * ever, past the timeout and past the caller's abort.
 */
export function timeoutFetchFor(ms?: number): typeof fetch {
  return async (input, init) => {
    const controller = new AbortController();
    const outer = init?.signal ?? (input instanceof Request ? input.signal : undefined);
    if (outer?.aborted) controller.abort(outer.reason);
    const onAbort = () => controller.abort(outer?.reason);
    outer?.addEventListener('abort', onAbort);
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, ms ?? REQUEST_TIMEOUT.ms);
    const done = () => {
      clearTimeout(timer);
      outer?.removeEventListener('abort', onAbort);
    };
    const failure = (error: unknown) => (timedOut ? new TypeError('Request timed out') : error);
    let response: Response;
    try {
      response = await globalThis.fetch(input, { ...init, signal: controller.signal });
    } catch (error) {
      done();
      throw failure(error);
    }
    if (response.body === null || response.status === 204) {
      done();
      return response;
    }
    // An abort rejects a pending read even if the stream itself never hears of it.
    const aborted = new Promise<never>((_resolve, reject) => {
      const onStop = () => {
        response.body?.cancel().catch(() => undefined);
        reject(failure(abortError(outer?.reason)));
      };
      if (controller.signal.aborted) onStop();
      else controller.signal.addEventListener('abort', onStop, { once: true });
    });
    aborted.catch(() => undefined);
    for (const name of BODY_READS) {
      const read = response[name] as (() => Promise<unknown>) | undefined;
      if (typeof read !== 'function') continue;
      Object.defineProperty(response, name, {
        configurable: true,
        value: () => Promise.race([read.call(response), aborted]).finally(done),
      });
    }
    return response;
  };
}

function abortError(reason: unknown): unknown {
  if (reason instanceof Error) return reason;
  return Object.assign(new Error('The operation was aborted.'), { name: 'AbortError' });
}

export const timeoutFetch: typeof fetch = timeoutFetchFor();

export const api = createApiClient({ baseUrl: API_BASE_URL, fetch: timeoutFetch });

/** For the refresh call itself: no auth middleware, so it cannot recurse. */
const bare = createApiClient({ baseUrl: API_BASE_URL, fetch: timeoutFetch });

/** Paths that never carry the bearer token or trigger a refresh. */
const PUBLIC_AUTH = /\/v1\/auth\/(apple|google|email|email\/verify|claim|signin-link|refresh)$/;

let refreshing: Promise<boolean> | null = null;

const REFRESH_LOCK = 'mfp.session.refresh';

type LockManager = { request<T>(name: string, cb: () => Promise<T>): Promise<T> };

/**
 * Runs `fn` while holding a lock shared by every tab of this origin (Web
 * Locks), so two tabs never rotate the same refresh token at once. Without
 * the API it just runs: native has a single writer, and in an old browser
 * the persisted-session check below still covers the common case.
 */
function withRefreshLock<T>(fn: () => Promise<T>): Promise<T> {
  const locks = (globalThis.navigator as { locks?: LockManager } | undefined)?.locks;
  return locks?.request ? locks.request(REFRESH_LOCK, fn) : fn();
}

/**
 * Rotates the refresh token once for everyone waiting (single flight). On
 * failure the session is cleared, which signs the user out.
 *
 * Another tab may already have rotated the session: the server then treats
 * this tab's refresh token as reused and revokes the session. So, under the
 * cross-tab lock, the persisted session is read first; if it holds a newer
 * pair for the same user, this tab adopts it instead of rotating.
 * `loadPersistedSession` waits for this tab's own pending save, and a pair
 * this tab has already moved past (a failed or late save) is never adopted,
 * so storage lagging behind memory cannot roll the tokens back.
 */
export function refreshSession(): Promise<boolean> {
  if (refreshing) return refreshing;
  if (!tokens.get()) return Promise.resolve(false);
  refreshing = withRefreshLock(async () => {
    try {
      const current = tokens.get();
      if (!current) return false;
      const persisted = await loadPersistedSession();
      if (
        persisted &&
        persisted.user.id === current.user.id &&
        persisted.refreshToken !== current.refreshToken &&
        !isSuperseded(persisted)
      ) {
        tokens.adopt(persisted);
        return true;
      }
      const { data, response } = await bare.POST('/v1/auth/refresh', {
        body: { refreshToken: current.refreshToken },
      });
      if (!response.ok || !data) {
        // Only sign out if nobody replaced the session meanwhile.
        if (response.status === 401 && tokens.get() === current) tokens.set(null);
        return false;
      }
      tokens.set(fromSession((data as { data: Schemas['Session'] }).data));
      return true;
    } catch {
      return false;
    }
  }).finally(() => {
    refreshing = null;
  });
  return refreshing;
}

const BEARER = /^Bearer (.+)$/;

const pending = new Map<string, Request>();

/**
 * openapi-fetch only takes a middleware's response if it is an instance of
 * the global `Response`. On native, Expo's fetch (expo/fetch) answers with
 * its own response class, so a fetched response is copied into one first.
 */
async function asResponse(
  res: Pick<Response, 'status' | 'statusText' | 'headers' | 'arrayBuffer'>,
): Promise<Response> {
  if (res instanceof Response) return res;
  const empty = [101, 204, 205, 304].includes(res.status);
  return new Response(empty ? null : await res.arrayBuffer(), {
    status: res.status,
    statusText: res.statusText,
    headers: res.headers,
  });
}

api.use({
  onRequest({ request, id }) {
    if (PUBLIC_AUTH.test(new URL(request.url).pathname)) return request;
    const session = tokens.get();
    if (session) request.headers.set('Authorization', `Bearer ${session.accessToken}`);
    // Keep an unread copy so a 401 can be retried after a refresh.
    pending.set(id, request.clone());
    return request;
  },
  // Returning nothing keeps the response as it is (see asResponse).
  async onResponse({ response, id, options }) {
    const original = pending.get(id);
    pending.delete(id);
    if (response.status !== 401 || !original || !tokens.get()) return undefined;
    const sentWith = BEARER.exec(original.headers.get('Authorization') ?? '')?.[1];
    // A 401 for a token that has since been replaced (a refresh finished
    // while this request was in flight) needs no new rotation: a second one
    // would invalidate the tokens the first just handed out.
    const rotated = sentWith !== undefined && sentWith !== tokens.get()!.accessToken;
    if (!rotated && !(await refreshSession())) return undefined;
    const session = tokens.get();
    if (!session) return undefined;
    const retry = new Request(original);
    retry.headers.set('Authorization', `Bearer ${session.accessToken}`);
    // Same fetch as the original, so a per-request timeout (the import
    // preview's) still applies to the retry.
    return asResponse(await options.fetch(retry));
  },
  onError({ id }) {
    pending.delete(id);
  },
});

/** A fresh key for a mutating POST, so a retried request is not applied twice. */
export function idempotencyKey(): string {
  const c = globalThis.crypto as Crypto | undefined;
  if (c?.randomUUID) return c.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2)}-${Math.random()
    .toString(36)
    .slice(2)}`;
}
