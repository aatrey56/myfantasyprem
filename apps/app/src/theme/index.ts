import { darkTheme, type Theme } from './tokens';

export * from './tokens';

/** The app is dark only: it ignores the device's light or dark setting. */
export function useTheme(): Theme {
  return darkTheme;
}
export { fontAssets, useAppFonts } from './fonts';
