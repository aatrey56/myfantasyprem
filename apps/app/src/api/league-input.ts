/**
 * What the user pasted on the import screen: an FPL Draft league link or a
 * bare league id, normalised to the league id. Mirrors the server's parser
 * (server/internal/httpapi/leagueurl.go); both are tested on
 * api/testdata/league-inputs.json. Accepts a draft.premierleague.com link
 * with or without www. or the scheme, a locale segment (/en/), a trailing
 * path, query or fragment; a bare id (spaces allowed); or a link inside
 * other text (a shared message).
 */
export type LeagueInput =
  | { kind: 'id'; fplLeagueId: number }
  /** A classic FPL link. Draft and classic league ids overlap, so it is refused. */
  | { kind: 'classic' }
  | { kind: 'invalid'; reason: string };

const DRAFT_HOST = 'draft.premierleague.com';
const CLASSIC_HOST = 'fantasy.premierleague.com';

/** Plain English, never a request field name. The server says the same. */
export const LEAGUE_INPUT_MESSAGES = {
  empty: 'Paste your league link or league ID.',
  invalid:
    "That doesn't look like an FPL Draft league link. Paste the link from your league page, or just the league ID.",
  /** A draft link with a `league` path segment but no id (FPL Draft's own league page). */
  missingId:
    "That page's link doesn't include your league ID. Type just your league's ID number instead.",
} as const;

const invalid = (reason: string): LeagueInput => ({ kind: 'invalid', reason });

const toId = (s: string | null | undefined) =>
  s && /^\d{1,12}$/.test(s) && Number(s) > 0 ? Number(s) : undefined;

export function parseLeagueInput(raw: string): LeagueInput {
  let text = raw.trim();
  if (!text) return invalid(LEAGUE_INPUT_MESSAGES.empty);
  if (/^\d[\d\s]*$/.test(text)) {
    const id = toId(text.replace(/\s/g, ''));
    return id === undefined
      ? invalid(LEAGUE_INPUT_MESSAGES.invalid)
      : { kind: 'id', fplLeagueId: id };
  }
  const words = text.split(/\s+/);
  if (words.length > 1) {
    const link = words.find((w) => w.toLowerCase().includes('premierleague.com'));
    if (!link) return invalid(LEAGUE_INPUT_MESSAGES.invalid);
    text = link;
  }

  let url: URL;
  try {
    url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(text) ? text : `https://${text}`);
  } catch {
    return invalid(LEAGUE_INPUT_MESSAGES.invalid);
  }
  const host = url.hostname.toLowerCase().replace(/^www\./, '');
  if (host === CLASSIC_HOST || host.endsWith(`.${CLASSIC_HOST}`)) return { kind: 'classic' };
  if (host !== DRAFT_HOST || !/^https?:$/.test(url.protocol)) {
    return invalid(LEAGUE_INPUT_MESSAGES.invalid);
  }
  const segments = url.pathname.split('/').filter(Boolean);
  const at = segments.findIndex((s) => /^leagues?$/i.test(s));
  const id = toId(at >= 0 ? segments[at + 1] : undefined) ?? toId(url.searchParams.get('league'));
  if (id !== undefined) return { kind: 'id', fplLeagueId: id };
  if (at >= 0 && at + 1 === segments.length) return invalid(LEAGUE_INPUT_MESSAGES.missingId);
  return invalid(LEAGUE_INPUT_MESSAGES.invalid);
}
