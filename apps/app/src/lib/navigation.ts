import type { Href } from 'expo-router';

/**
 * A post-sign-in destination, only if it is a path inside this app: starts
 * with one slash, no scheme or protocol-relative `//host`, not back to
 * sign-in. Anything else is ignored, so a crafted link cannot send the user
 * off-site.
 */
export function safeNextPath(next: string | string[] | undefined): string | null {
  const value = Array.isArray(next) ? next[0] : next;
  if (!value || !value.startsWith('/') || value.startsWith('//') || value.startsWith('/\\')) {
    return null;
  }
  if (/^\/sign-in(\/|\?|$)/.test(value)) return null;
  return value;
}

/**
 * `next` as the server takes it into an emailed sign-in link
 * (EmailSignInRequest.next): path segments only, no query. Null when the
 * path does not fit; the app then keeps it in session storage alone.
 */
export function linkNextPath(next: string | null): string | null {
  if (!next || next.length > 200) return null;
  return /^(\/[A-Za-z0-9_~-][A-Za-z0-9_.~-]*)+$/.test(next) ? next : null;
}

/** Screens a sub-page can be opened from, passed as `?from=` when navigating. */
export type Origin = 'status' | 'league' | 'players' | 'activity';

const ORIGINS: Record<Origin, { label: string; path: string }> = {
  status: { label: 'Status', path: '' },
  league: { label: 'League', path: '/league' },
  players: { label: 'Players', path: '/players' },
  activity: { label: 'Activity', path: '/activity' },
};

/** A known origin from a route param; anything else is ignored. */
export function parseOrigin(from: string | string[] | undefined): Origin | undefined {
  const value = Array.isArray(from) ? from[0] : from;
  return value !== undefined && Object.prototype.hasOwnProperty.call(ORIGINS, value)
    ? (value as Origin)
    : undefined;
}

/**
 * The "‹ label" back link for a sub-page: back to where it was opened from
 * (`from`), else to the tab it lives under (`home`). Trades and waivers
 * live under Players and Deadlines under Activity, but the League and
 * Status pages link to them too, so a visit from there goes back there.
 */
export function originBack(
  leagueId: string,
  from: string | string[] | undefined,
  home: Origin,
): { label: string; href: Href } {
  const o = ORIGINS[parseOrigin(from) ?? home];
  return { label: o.label, href: `/leagues/${leagueId}${o.path}` as Href };
}

/** `path` with `?from=` (or `&from=`) when an origin is set. */
export function withOrigin(path: string, from: Origin | undefined): Href {
  if (!from) return path as Href;
  return `${path}${path.includes('?') ? '&' : '?'}from=${from}` as Href;
}
