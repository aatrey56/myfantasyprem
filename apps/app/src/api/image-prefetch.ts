import { Image } from 'expo-image';

/**
 * Native: warms expo-image's memory and disk cache, so rows that scroll
 * in paint their photos at once. Mock builds load images through the
 * in-process mocks instead (components/branded/mock-image.ts): nothing
 * to warm. Inline env check, folded at build time.
 */
export function prefetchImages(urls: readonly string[]): void {
  if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true' || urls.length === 0) return;
  void Image.prefetch([...urls], 'memory-disk').catch(() => undefined);
}
