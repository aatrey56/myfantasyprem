import { useSyncExternalStore } from 'react';
import { Platform } from 'react-native';

/** A mouse or trackpad that can hover: a desktop, not a phone or tablet. */
const QUERY = '(hover: hover) and (pointer: fine)';

function media(): MediaQueryList | null {
  if (Platform.OS !== 'web' || typeof window === 'undefined') return null;
  return typeof window.matchMedia === 'function' ? window.matchMedia(QUERY) : null;
}

function subscribe(onChange: () => void) {
  const m = media();
  m?.addEventListener?.('change', onChange);
  return () => m?.removeEventListener?.('change', onChange);
}

const snapshot = () => media()?.matches === true;

/**
 * True on web with a hovering, fine pointer, where a hardware keyboard is
 * likely (keyboard hints such as "Press Tab" make sense). Always false on
 * native and on touch-only browsers; follows a tablet gaining a trackpad.
 */
export function useFinePointer(): boolean {
  return useSyncExternalStore(subscribe, snapshot, () => false);
}
