import { Geist_400Regular } from '@expo-google-fonts/geist/400Regular';
import { Geist_500Medium } from '@expo-google-fonts/geist/500Medium';
import { Geist_600SemiBold } from '@expo-google-fonts/geist/600SemiBold';
import { Geist_700Bold } from '@expo-google-fonts/geist/700Bold';
import { GeistMono_500Medium } from '@expo-google-fonts/geist-mono/500Medium';
import { GeistMono_600SemiBold } from '@expo-google-fonts/geist-mono/600SemiBold';
import { useFonts } from 'expo-font';

/**
 * Geist (UI) and Geist Mono (numbers), bundled with the app so native never
 * fetches from Google. Keys must match `fontFamilies` in tokens.ts.
 */
export const fontAssets = {
  Geist_400Regular,
  Geist_500Medium,
  Geist_600SemiBold,
  Geist_700Bold,
  GeistMono_500Medium,
  GeistMono_600SemiBold,
};

/** `true` once the fonts are ready, or once loading has failed (fall back to system fonts). */
export function useAppFonts(): boolean {
  const [loaded, error] = useFonts(fontAssets);
  return loaded || error != null;
}
