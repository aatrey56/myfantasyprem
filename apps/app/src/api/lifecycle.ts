/**
 * Web: TanStack Query already listens to `visibilitychange` and the
 * browser's online/offline events, so there is nothing to wire.
 * See lifecycle.native.ts for iOS and Android.
 */
export function setupQueryLifecycle(): () => void {
  return () => {};
}
