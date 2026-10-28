import type { ReactNode } from 'react';

// Web draws every sheet with the hand-written one in Sheet.tsx: gorhom stays
// out of the web bundle.

export function SheetProvider({ children }: { children: ReactNode }) {
  return children;
}

export function NativeSheet(): null {
  return null;
}
