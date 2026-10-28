import brand from '../../brand.json';

/**
 * The product name. Working title. It lives in brand.json because
 * app.config.ts (read by the Expo CLI before any TypeScript is compiled) needs
 * it too; this constant is the one import for app code.
 */
export const APP_NAME: string = brand.name;

/** Lowercase identifier derived from the name, for slugs and URL schemes. */
export const APP_SLUG = APP_NAME.toLowerCase();

/** A data poll older than this is stale. Mirrors the server rule (3 minutes). */
export const STALE_AFTER_MS = 3 * 60 * 1000;
