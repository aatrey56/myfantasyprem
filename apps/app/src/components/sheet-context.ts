import { createContext, useContext } from 'react';

/** Closes the sheet around it: slides a bottom sheet down first. */
export const SheetCloseContext = createContext<(() => void) | null>(null);

/** The close of the sheet around the caller (its animated close), else `fallback`. */
export function useSheetClose(fallback?: () => void): () => void {
  const close = useContext(SheetCloseContext);
  return close ?? fallback ?? (() => undefined);
}
