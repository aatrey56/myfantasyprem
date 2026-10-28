import type { Schemas } from '@myfantasyprem/api-client';

/**
 * A rule broken by a mock action. Handlers turn it into problem+json with
 * the same slugs the contract documents (`not-your-turn`, `lineup-locked`,
 * `trade-shape`, ...).
 */
export class MockError extends Error {
  constructor(
    readonly status: number,
    readonly slug: string,
    readonly title: string,
    readonly detail: string,
    readonly errors?: Schemas['ProblemError'][],
    /** Extra problem members, such as `leagueId` on `already-member`. */
    readonly extra?: Record<string, unknown>,
  ) {
    super(detail);
    this.name = 'MockError';
  }
}

export const forbidden = (slug: string, detail: string) =>
  new MockError(403, slug, 'Forbidden', detail);
export const notFound = (what: string) =>
  new MockError(404, 'not-found', 'Not found', `${what} not found.`);
export const conflict = (
  slug: string,
  title: string,
  detail: string,
  extra?: Record<string, unknown>,
) => new MockError(409, slug, title, detail, undefined, extra);
export const invalid = (slug: string, detail: string, errors?: Schemas['ProblemError'][]) =>
  new MockError(422, slug, 'Invalid request', detail, errors);
