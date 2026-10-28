/**
 * Web: nothing. RemoteImage.web.tsx is a lazy <img>, so a long list only
 * fetches what scrolls into view, and the browser's HTTP cache keeps it.
 */
export function prefetchImages(_urls: readonly string[]): void {}
