import {
  parsePendingRevocations,
  PENDING_REVOCATIONS_KEY,
  type PendingRevocationStore,
} from './revocation';
import { parseStoredSession, SESSION_KEY, type SessionStore } from './session';

/**
 * Web: localStorage, so a session survives reloads and new tabs. Scripts on
 * the page can read it, which the contract warns against for the refresh
 * token; the safe web design is an HttpOnly refresh cookie set by the
 * server, which the contract does not offer yet (reported as a gap). The
 * app has no third-party scripts, and the access token lives one hour.
 */
export const platformSessionStore: SessionStore = {
  async load() {
    try {
      return parseStoredSession(globalThis.localStorage?.getItem(SESSION_KEY) ?? null);
    } catch {
      return null;
    }
  },
  async save(session) {
    try {
      globalThis.localStorage?.setItem(SESSION_KEY, JSON.stringify(session));
    } catch {
      // Private mode or storage full: the session lasts until the tab closes.
    }
  },
  async clear() {
    try {
      globalThis.localStorage?.removeItem(SESSION_KEY);
    } catch {
      // Nothing stored.
    }
  },
  /** Another tab saved or cleared the session (`storage` fires only in other tabs). */
  subscribe(listener) {
    const target = globalThis as unknown as {
      addEventListener?: (type: 'storage', l: (e: StorageEvent) => void) => void;
      removeEventListener?: (type: 'storage', l: (e: StorageEvent) => void) => void;
    };
    const onStorage = (e: StorageEvent) => {
      if (e.key !== SESSION_KEY && e.key !== null) return;
      listener(parseStoredSession(e.key === null ? null : e.newValue));
    };
    target.addEventListener?.('storage', onStorage);
    return () => target.removeEventListener?.('storage', onStorage);
  },
};

/** Signed-out sessions not yet revoked on the server (localStorage, like the session). */
export const platformPendingRevocationStore: PendingRevocationStore = {
  async load() {
    try {
      return parsePendingRevocations(
        globalThis.localStorage?.getItem(PENDING_REVOCATIONS_KEY) ?? null,
      );
    } catch {
      return [];
    }
  },
  async save(entries) {
    try {
      if (entries.length === 0) globalThis.localStorage?.removeItem(PENDING_REVOCATIONS_KEY);
      else globalThis.localStorage?.setItem(PENDING_REVOCATIONS_KEY, JSON.stringify(entries));
    } catch {
      // Private mode or storage full: retried until the tab closes.
    }
  },
};
