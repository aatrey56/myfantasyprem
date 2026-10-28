import { Platform } from 'react-native';

/**
 * Base URL of the Go API. Empty means same-origin (relative URLs): the web
 * build is served next to the API, so it needs no URL baked in. Native
 * builds and the web dev server set EXPO_PUBLIC_API_BASE_URL
 * (`.env.development`; app.config.ts refuses a production native build
 * without it). A native mock build without one gets a placeholder origin:
 * native has no "same origin", and the in-process mocks answer by path.
 * Read env vars in dot form only: Expo inlines `process.env.EXPO_PUBLIC_*`
 * at build time and cannot see dynamic access.
 */
export const API_BASE_URL =
  process.env.EXPO_PUBLIC_API_BASE_URL ??
  (Platform.OS !== 'web' && process.env.EXPO_PUBLIC_USE_MOCKS === 'true'
    ? 'http://mocks.invalid'
    : '');
