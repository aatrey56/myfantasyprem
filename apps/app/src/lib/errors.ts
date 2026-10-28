import { ApiError, MalformedResponseError } from '@myfantasyprem/api-client';

/** A short, user-facing explanation of a failed request. */
export function describeError(error: unknown): string {
  if (error instanceof ApiError) {
    if (error.status === 404) return error.problem?.detail ?? 'Not found.';
    if (error.status >= 500) return 'The server had a problem. Try again in a moment.';
    return error.problem?.detail ?? error.message;
  }
  if (error instanceof MalformedResponseError) return error.message;
  if (error instanceof TypeError) return "Can't reach the server. Check your connection.";
  return error instanceof Error ? error.message : 'Something went wrong.';
}

/** The stable slug at the end of a problem's `type` URI (`lineup-locked`), if any. */
export function problemSlug(error: unknown): string | null {
  if (!(error instanceof ApiError)) return null;
  const type = error.problem?.type;
  if (!type || type === 'about:blank') return null;
  return type.split('/').pop() ?? null;
}

/** The human messages of a 422's `errors` list, in order. Empty for anything else. */
export function problemErrors(error: unknown): string[] {
  if (!(error instanceof ApiError)) return [];
  const raw: unknown = error.problem?.errors;
  if (!Array.isArray(raw)) return [];
  return raw
    .map((e) => (e && typeof e === 'object' && 'message' in e ? String(e.message) : ''))
    .filter(Boolean);
}

/** Messages for one request field (JSON pointer such as `/name`). */
export function fieldErrors(error: unknown, field: string): string | null {
  if (!(error instanceof ApiError)) return null;
  const raw: unknown = error.problem?.errors;
  if (!Array.isArray(raw)) return null;
  const hit = raw.find(
    (e) => e && typeof e === 'object' && 'field' in e && (e as { field?: string }).field === field,
  ) as { message?: string } | undefined;
  return hit?.message ?? null;
}

/** The league named by a 409 `already-member` problem, if that is what `error` is. */
export function alreadyMemberLeague(error: unknown): string | null {
  if (problemSlug(error) !== 'already-member' || !(error instanceof ApiError)) return null;
  const id = error.problem?.leagueId;
  return typeof id === 'string' ? id : null;
}

/** Ends in a stop already: `.`, `!`, `?` or `…`, perhaps inside a closing quote or bracket. */
const STOPPED = /[.!?…]['"’”)\]]*$/;

/** `text` trimmed and without its closing full stop (`?`, `!`, `…` and `...` stay). */
export function withoutFullStop(text: string): string {
  const t = text.trim();
  return t.endsWith('.') && !t.endsWith('..') ? t.slice(0, -1).trimEnd() : t;
}

/** `text` trimmed and ending in exactly one stop: a full stop is added only when it has none. */
export function asSentence(text: string): string {
  const t = text.trim();
  return !t || STOPPED.test(t) ? t : `${t}.`;
}

/**
 * Sentences for the user, from our own words and the server's `detail`:
 * each ends in one stop, joined by a space. A part an earlier one already
 * says is dropped, so the server's "Try again in a few minutes." is not
 * followed by our "Try again.". Empty parts are skipped.
 */
export function joinSentences(...parts: (string | null | undefined)[]): string {
  const out: string[] = [];
  for (const part of parts) {
    const sentence = asSentence(part ?? '');
    if (!sentence) continue;
    const gist = withoutFullStop(sentence).toLowerCase();
    if (out.some((prev) => prev.toLowerCase().includes(gist))) continue;
    out.push(sentence);
  }
  return out.join(' ');
}

/**
 * The toast after a refused move: the server's own reasons (a 422's
 * `errors`, else its problem `detail`), or the client rule that stopped it.
 */
export function moveErrorText(error: unknown): string {
  const reasons = problemErrors(error);
  return reasons.length ? joinSentences(...reasons) : describeError(error);
}
