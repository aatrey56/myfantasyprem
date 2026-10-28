import { safeNextPath } from '@/lib/navigation';

const KEY = 'mfp.signInNext';

/**
 * Where to go after an email sign-in, kept in session storage (web) while
 * the user fetches the email: a link that comes back to this tab without
 * its `next` still lands there. The emailed link carries `next` itself for
 * another tab or browser. Native has no session storage: the link's
 * `next` is all there is.
 */
export const pendingNext = {
  get(): string | null {
    try {
      return safeNextPath(globalThis.sessionStorage?.getItem(KEY) ?? undefined);
    } catch {
      return null;
    }
  },
  set(path: string) {
    try {
      globalThis.sessionStorage?.setItem(KEY, path);
    } catch {
      // Storage blocked: the link's next still works.
    }
  },
  clear() {
    try {
      globalThis.sessionStorage?.removeItem(KEY);
    } catch {
      // Nothing to clear.
    }
  },
};
