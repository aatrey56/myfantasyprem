import * as SecureStore from 'expo-secure-store';

import {
  parsePendingRevocations,
  PENDING_REVOCATIONS_KEY,
  type PendingRevocationStore,
} from './revocation';
import { parseStoredSession, SESSION_KEY, type SessionStore } from './session';

/**
 * iOS and Android: the Keychain / Keystore, as the contract asks for the
 * refresh token. Readable only after the first unlock.
 */
export const platformSessionStore: SessionStore = {
  async load() {
    try {
      return parseStoredSession(await SecureStore.getItemAsync(SESSION_KEY));
    } catch {
      return null;
    }
  },
  async save(session) {
    await SecureStore.setItemAsync(SESSION_KEY, JSON.stringify(session), {
      keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK,
    });
  },
  async clear() {
    await SecureStore.deleteItemAsync(SESSION_KEY);
  },
};

/** Signed-out sessions not yet revoked on the server: refresh tokens, so the Keychain too. */
export const platformPendingRevocationStore: PendingRevocationStore = {
  async load() {
    try {
      return parsePendingRevocations(await SecureStore.getItemAsync(PENDING_REVOCATIONS_KEY));
    } catch {
      return [];
    }
  },
  async save(entries) {
    if (entries.length === 0) return SecureStore.deleteItemAsync(PENDING_REVOCATIONS_KEY);
    await SecureStore.setItemAsync(PENDING_REVOCATIONS_KEY, JSON.stringify(entries), {
      keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK,
    });
  },
};
