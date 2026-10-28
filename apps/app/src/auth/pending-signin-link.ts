/**
 * The code of a sign-in link being opened (docs/adr/0013), kept in memory
 * only: /signin-link/CODE moves it here and leaves the URL and history at
 * once, before anything is sent, so a failure or a scanner never leaves a
 * live code behind.
 */
let value: string | null = null;
const listeners = new Set<() => void>();

export const pendingSignInLink = {
  get: () => value,
  set(next: string | null) {
    value = next;
    listeners.forEach((l) => l());
  },
  subscribe(l: () => void) {
    listeners.add(l);
    return () => {
      listeners.delete(l);
    };
  },
};
