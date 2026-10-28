import type { Schemas } from '@myfantasyprem/api-client';

/** The parts of an upcoming or played fixture a fixture label needs. */
export interface FixtureLike {
  opponent: Schemas['ClubRef'];
  isHome: boolean;
  /** Null while an upcoming fixture has no date (TBC). */
  kickoffAt: string | null;
  status?: Schemas['FixtureStatus'];
}

/**
 * A row's compact fixtures (RowFixture: the opponent is a club code) with
 * each opponent's club from the club list; a code the list lacks (or no
 * list yet) stands for its own name.
 */
export function expandRowFixtures(
  fixtures: readonly Schemas['RowFixture'][],
  clubs: readonly Schemas['ClubRef'][] | undefined,
): Schemas['UpcomingFixture'][] {
  const byCode = new Map(clubs?.map((c) => [c.code.toUpperCase(), c]));
  return fixtures.map((f) => ({
    ...f,
    opponent: byCode.get(f.opponent.toUpperCase()) ?? { code: f.opponent, name: f.opponent },
  }));
}

/** The compact row form of a fixture (as the server sends it): the opponent's code, no status while scheduled. */
export function toRowFixture(f: Schemas['UpcomingFixture']): Schemas['RowFixture'] {
  const { opponent, status, ...rest } = f;
  return {
    ...rest,
    opponent: opponent.code,
    ...(status && status !== 'scheduled' ? { status } : {}),
  };
}

/**
 * A player's next-gameweek fixtures as a short label: opponent codes in
 * kickoff order (`ARS, CHE` in a double gameweek), or `No game` in a blank.
 */
export function formatNextFixtures(fixtures: readonly Schemas['UpcomingFixture'][]): string {
  if (fixtures.length === 0) return 'No game';
  return fixtures.map((f) => f.opponent.code.toUpperCase()).join(', ');
}

/**
 * One fixture from the player's side: `v ABC` at home, `@ ABC` away (the
 * opponent's code).
 */
export function fixtureLabel(f: FixtureLike): string {
  return `${f.isHome ? 'v' : '@'} ${f.opponent.code.toUpperCase()}`;
}

/** Spoken form for screen readers. */
export function fixtureA11y(f: FixtureLike): string {
  return `${f.isHome ? 'home to' : 'away at'} ${f.opponent.name}`;
}
