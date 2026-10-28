import { createApiClient, type Schemas } from '@myfantasyprem/api-client';

import type { StoredSession } from './session';
import { API_BASE_URL } from '@/api/config';

/**
 * Sign-out revokes the session on the server, but the user is signed out
 * locally at once. A logout that fails (429, 5xx, offline) is retried with
 * backoff, honouring Retry-After, for REVOKE_FOR_MS; what still has not
 * gone through stays here, in platform storage, and is retried on the
 * next start. Dropping it instead would leave the session, and any copy of
 * its refresh token, live on the server.
 */

/** A signed-out session the server has not yet confirmed revoking. */
export interface PendingRevocation {
  accessToken: string;
  refreshToken: string;
  /** Past this (ISO) the session is dead anyway and the entry is dropped. */
  refreshExpiresAt: string;
}

/** Platform storage for the pending revocations (next to the session). */
export interface PendingRevocationStore {
  load(): Promise<PendingRevocation[]>;
  save(entries: PendingRevocation[]): Promise<void>;
}

export const PENDING_REVOCATIONS_KEY = 'mfp.pendingRevocations';

/** How long one run keeps retrying before leaving the rest for the next start. */
export const REVOKE_FOR_MS = 2 * 60_000;
const BACKOFF_FIRST_MS = 1_000;
const BACKOFF_MAX_MS = 30_000;
/** Bounds the store: signing out more often than this without a network is not a real case. */
const MAX_PENDING = 10;

/** In-memory store, for tests and as a fallback. */
export function memoryPendingRevocationStore(
  initial: PendingRevocation[] = [],
): PendingRevocationStore & { entries: () => PendingRevocation[] } {
  let value = [...initial];
  return {
    load: async () => [...value],
    save: async (entries) => {
      value = [...entries];
    },
    entries: () => [...value],
  };
}

/** Parses a stored value; malformed or expired entries are dropped. */
export function parsePendingRevocations(raw: string | null, now = Date.now()): PendingRevocation[] {
  if (!raw) return [];
  try {
    const v: unknown = JSON.parse(raw);
    return Array.isArray(v) ? v.filter((e) => isLive(e, now)) : [];
  } catch {
    return [];
  }
}

function isLive(e: unknown, now: number): e is PendingRevocation {
  const p = e as Partial<PendingRevocation> | null;
  if (
    typeof p?.accessToken !== 'string' ||
    typeof p.refreshToken !== 'string' ||
    typeof p.refreshExpiresAt !== 'string'
  ) {
    return false;
  }
  const expiry = Date.parse(p.refreshExpiresAt);
  return !Number.isFinite(expiry) || expiry > now;
}

/** No bearer middleware or refresh: these tokens are no longer the app's session. */
const bare = createApiClient({ baseUrl: API_BASE_URL });

let store: PendingRevocationStore | null = null;
/** Bumped on every bind, so a run from an earlier binding stops. */
let generation = 0;
/** The pending entries, loaded from the store at bind. */
let entries: PendingRevocation[] = [];
let loaded: Promise<void> = Promise.resolve();
let persisting: Promise<void> = Promise.resolve();
let draining: Promise<void> | null = null;
let again = false;

/** AuthProvider binds the platform store at start; null unbinds. */
export function bindPendingRevocationStore(next: PendingRevocationStore | null) {
  store = next;
  generation += 1;
  entries = [];
  draining = null;
  const gen = generation;
  loaded = (next ? next.load().catch(() => []) : Promise.resolve([])).then((list) => {
    if (gen !== generation) return;
    const now = Date.now();
    entries = [...list.filter((e) => isLive(e, now)), ...entries];
  });
}

function persist() {
  const s = store;
  const snapshot = [...entries];
  if (!s) return persisting;
  persisting = persisting.then(() => s.save(snapshot)).catch(() => undefined);
  return persisting;
}

/**
 * Records `session` for revocation and persists it. Call it before the
 * local sign-out, so a crash in between never loses the refresh token.
 */
export async function queueRevocation(session: StoredSession): Promise<void> {
  await loaded;
  if (entries.some((e) => e.refreshToken === session.refreshToken)) return;
  entries = [
    ...entries,
    {
      accessToken: session.accessToken,
      refreshToken: session.refreshToken,
      refreshExpiresAt: session.refreshExpiresAt,
    },
  ].slice(-MAX_PENDING);
  await persist();
}

/**
 * Revokes every pending session, retrying for REVOKE_FOR_MS; entries that
 * still fail stay stored for the next start. One run at a time: a call
 * during a run makes it go round again.
 */
export function drainPendingRevocations(): Promise<void> {
  if (draining) {
    again = true;
    return draining;
  }
  const gen = generation;
  const run = (async () => {
    await loaded;
    do {
      again = false;
      const deadline = Date.now() + REVOKE_FOR_MS;
      for (const entry of [...entries]) {
        if (gen !== generation) return;
        const revoked = await revoke(entry, deadline, gen);
        if (gen !== generation) return;
        if (revoked) {
          entries = entries.filter((e) => e.refreshToken !== revoked.refreshToken);
          await persist();
        }
      }
    } while (again && gen === generation);
  })().finally(() => {
    if (draining === run) draining = null;
  });
  draining = run;
  return run;
}

type Attempt =
  | { kind: 'done' }
  | { kind: 'rotated'; next: PendingRevocation }
  | { kind: 'retry'; afterMs?: number };

/**
 * Tries until revoked or out of time. Returns the entry as last stored
 * (rotated, maybe) once the session is gone, or null.
 */
async function revoke(
  entry: PendingRevocation,
  deadline: number,
  gen: number,
): Promise<PendingRevocation | null> {
  let current = entry;
  for (let failures = 0; ;) {
    const attempt = await tryRevoke(current);
    if (gen !== generation) return null;
    if (attempt.kind === 'done') return current;
    if (attempt.kind === 'rotated') {
      // The old refresh token is spent: keep the new pair, so a later run
      // presents a token the server still knows.
      const old = current.refreshToken;
      current = attempt.next;
      entries = entries.map((e) => (e.refreshToken === old ? current : e));
      await persist();
      continue;
    }
    const wait = attempt.afterMs ?? Math.min(BACKOFF_FIRST_MS * 2 ** failures, BACKOFF_MAX_MS);
    failures += 1;
    if (Date.now() + wait > deadline) return null;
    await new Promise((resolve) => setTimeout(resolve, wait));
  }
}

/**
 * One logout. A 401 means the access token has expired or the session is
 * already gone: the refresh token tells which (rotating it yields a fresh
 * access token to revoke with).
 */
async function tryRevoke(entry: PendingRevocation): Promise<Attempt> {
  let response: Response;
  try {
    ({ response } = await bare.POST('/v1/auth/logout', {
      headers: { Authorization: `Bearer ${entry.accessToken}` },
    }));
  } catch {
    return { kind: 'retry' };
  }
  if (response.ok) return { kind: 'done' };
  if (response.status !== 401) return transient(response);
  try {
    const { data, response: refreshed } = await bare.POST('/v1/auth/refresh', {
      body: { refreshToken: entry.refreshToken },
    });
    if (refreshed.ok && data) {
      const s = (data as { data: Schemas['Session'] }).data;
      return {
        kind: 'rotated',
        next: {
          accessToken: s.accessToken,
          refreshToken: s.refreshToken,
          refreshExpiresAt: s.refreshExpiresAt,
        },
      };
    }
    return transient(refreshed);
  } catch {
    return { kind: 'retry' };
  }
}

/** 429 and 5xx are worth retrying; any other refusal means nothing is left to revoke. */
function transient(response: Response): Attempt {
  if (response.status === 429) return { kind: 'retry', afterMs: retryAfterMs(response) };
  if (response.status >= 500) return { kind: 'retry' };
  return { kind: 'done' };
}

/** Retry-After in seconds or as an HTTP date; undefined when absent or unreadable. */
function retryAfterMs(response: Response): number | undefined {
  const v = response.headers.get('Retry-After')?.trim();
  if (!v) return undefined;
  if (/^\d+$/.test(v)) return Number(v) * 1000;
  const at = Date.parse(v);
  return Number.isFinite(at) ? Math.max(0, at - Date.now()) : undefined;
}
