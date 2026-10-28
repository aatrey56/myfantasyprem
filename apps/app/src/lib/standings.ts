import type { Schemas } from '@myfantasyprem/api-client';

type Row = Schemas['StandingRow'];

export type StandingsKey = 'rank' | 'team' | 'record' | 'pointsFor' | 'pointsAgainst' | 'streak';

/**
 * The league's own order: league points (win 3, draw 1), then points for,
 * then fewer points against, then name so the order is total and stable.
 */
export function compareStandings(a: Row, b: Row): number {
  return (
    b.leaguePoints - a.leaguePoints ||
    b.pointsFor - a.pointsFor ||
    a.pointsAgainst - b.pointsAgainst ||
    a.team.name.localeCompare(b.team.name)
  );
}

/** The current run from the newest result: `W3`, `L1`, `D2`; null before any result. */
export function streakOf(form: readonly string[]): { result: string; count: number } | null {
  const last = form.at(-1);
  if (!last) return null;
  let count = 0;
  for (let i = form.length - 1; i >= 0 && form[i] === last; i--) count++;
  return { result: last, count };
}

export function streakText(form: readonly string[]): string {
  const s = streakOf(form);
  return s ? `${s.result}${s.count}` : '–';
}

/** Wins count up, losses down, draws zero: for sorting by streak. */
function streakScore(form: readonly string[]): number {
  const s = streakOf(form);
  if (!s) return 0;
  return s.result === 'W' ? s.count : s.result === 'L' ? -s.count : 0;
}

/**
 * Sorts the table by a column. Every column falls back to the league order
 * on ties, so equal records stay in their real rank order. `desc` means
 * "best first" for every column (most points for, fewest points against,
 * longest winning run); `asc` reverses it.
 */
export function sortStandings(
  rows: readonly Row[],
  key: StandingsKey,
  dir: 'desc' | 'asc' = 'desc',
): Row[] {
  const primary = (a: Row, b: Row): number => {
    switch (key) {
      case 'rank':
        return a.rank - b.rank;
      case 'team':
        return a.team.name.localeCompare(b.team.name);
      case 'record':
        return b.leaguePoints - a.leaguePoints;
      case 'pointsFor':
        return b.pointsFor - a.pointsFor;
      case 'pointsAgainst':
        return a.pointsAgainst - b.pointsAgainst;
      case 'streak':
        return streakScore(b.form) - streakScore(a.form);
    }
  };
  const sign = dir === 'desc' ? 1 : -1;
  return [...rows].sort((a, b) => sign * (primary(a, b) || compareStandings(a, b)));
}

/** `4-1-2` (W-D-L). */
export const recordOf = (r: Row) => `${r.wins}-${r.draws}-${r.losses}`;
