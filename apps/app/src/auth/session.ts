import type { Schemas } from '@myfantasyprem/api-client';

/** What the app keeps between launches: the tokens and the user they belong to. */
export interface StoredSession {
  accessToken: string;
  refreshToken: string;
  /** Access token expiry (ISO). */
  expiresAt: string;
  refreshExpiresAt: string;
  user: Schemas['User'];
}

/** Platform storage for the session: Keychain/Keystore on native, localStorage on web. */
export interface SessionStore {
  load(): Promise<StoredSession | null>;
  save(session: StoredSession): Promise<void>;
  clear(): Promise<void>;
  /**
   * Changes saved by someone else (another browser tab). Optional: native
   * storage has a single writer.
   */
  subscribe?(listener: (session: StoredSession | null) => void): () => void;
}

export const SESSION_KEY = 'mfp.session';

export function fromSession(s: Schemas['Session']): StoredSession {
  return {
    accessToken: s.accessToken,
    refreshToken: s.refreshToken,
    expiresAt: s.expiresAt,
    refreshExpiresAt: s.refreshExpiresAt,
    user: s.user,
  };
}

/** Parses a stored value; anything malformed or past its refresh expiry counts as no session. */
export function parseStoredSession(raw: string | null, now = Date.now()): StoredSession | null {
  if (!raw) return null;
  try {
    const v = JSON.parse(raw) as Partial<StoredSession>;
    if (
      typeof v.accessToken !== 'string' ||
      typeof v.refreshToken !== 'string' ||
      typeof v.refreshExpiresAt !== 'string' ||
      typeof v.user !== 'object' ||
      v.user === null ||
      typeof v.user.id !== 'string'
    ) {
      return null;
    }
    const refreshExpiry = Date.parse(v.refreshExpiresAt);
    if (Number.isFinite(refreshExpiry) && refreshExpiry <= now) return null;
    return v as StoredSession;
  } catch {
    return null;
  }
}

/** In-memory store, for tests and as a fallback when platform storage fails. */
export function memorySessionStore(initial: StoredSession | null = null): SessionStore {
  let value = initial;
  return {
    load: async () => value,
    save: async (s) => {
      value = s;
    },
    clear: async () => {
      value = null;
    },
  };
}

/**
 * The live tokens, outside React so the API client's middleware can read
 * them on every request.
 *
 * `set` is for changes made in this tab (sign-in, rotation, sign-out): it
 * writes through to the bound store, and `loadPersistedSession` waits for
 * that write, so the persisted session is never older than memory when the
 * API client compares them. `adopt` is for a session that came from storage
 * (launch, another tab): it is already persisted, so it is not written back,
 * which also keeps storage events from echoing between tabs.
 */
let current: StoredSession | null = null;
const listeners = new Set<(s: StoredSession | null) => void>();

/** The store AuthProvider persists to. */
let boundStore: SessionStore | null = null;
/** Pending writes, in order, so a later clear never lands before an earlier save. */
let persisting: Promise<void> = Promise.resolve();

/**
 * Refresh tokens this tab has held, newest last. A persisted session carrying
 * one of them is one this tab has already moved past (its save failed or is
 * late), never a newer one from another tab: refresh tokens are not reused.
 */
const seen = new Set<string>();
const SEEN_MAX = 32;

function remember(s: StoredSession | null) {
  // Signed out: nothing left to protect, and the next sign-in starts afresh.
  if (!s) return seen.clear();
  seen.delete(s.refreshToken);
  seen.add(s.refreshToken);
  if (seen.size > SEEN_MAX) seen.delete(seen.values().next().value as string);
}

function publish(s: StoredSession | null) {
  current = s;
  remember(s);
  listeners.forEach((l) => l(s));
}

export const tokens = {
  get: () => current,
  /** A change made by this tab: persisted (write-through), then announced. */
  set(s: StoredSession | null) {
    const store = boundStore;
    if (store) {
      persisting = persisting
        .then(() => (s ? store.save(s) : store.clear()))
        .catch(() => undefined);
    }
    publish(s);
  },
  /** A session read from storage (launch, another tab): announced, not re-saved. */
  adopt(s: StoredSession | null) {
    publish(s);
  },
  subscribe(listener: (s: StoredSession | null) => void) {
    listeners.add(listener);
    return () => {
      listeners.delete(listener);
    };
  },
};

export function bindSessionStore(store: SessionStore | null) {
  boundStore = store;
}

/**
 * The persisted session, or null when there is none or no store is bound.
 * Waits for this tab's pending writes first.
 */
export async function loadPersistedSession(): Promise<StoredSession | null> {
  await persisting;
  if (!boundStore) return null;
  try {
    return await boundStore.load();
  } catch {
    return null;
  }
}

/** True when `s` is a session this tab held before its current one. */
export function isSuperseded(s: StoredSession): boolean {
  return s.refreshToken !== current?.refreshToken && seen.has(s.refreshToken);
}
