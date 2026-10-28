/**
 * Form: the mean of a player's points over the last two completed
 * gameweeks before the current one. Pure functions: the MSW mocks compute
 * every `form` with them, and the Go server mirrors them
 * (server/internal/statuspage: Form, FormGameweek). Both test suites run
 * server/internal/statuspage/testdata/form_cases.json: change a rule there
 * and on both sides.
 *
 * - The form gameweek is the season's current gameweek while it is played,
 *   the next once it has finished: with gameweek 7 live or up next, form is
 *   (GW5 + GW6) / 2.
 * - A gameweek without minutes or without a match counts as 0, and a double
 *   gameweek's points are both matches', so the divisor is always 2; only
 *   early in the season is it the number of completed gameweeks (in GW2,
 *   GW1's points). In GW1 there is no form (null).
 * - One decimal, halves up.
 */

/** How many completed gameweeks form averages. */
export const FORM_WEEKS = 2;

/** A player's points in one match of a gameweek. */
export interface MatchPoints {
  gameweek: number;
  points: number;
}

/** The gameweek form is for, from the season's current gameweek. */
export function formGameweek(current: number, currentFinished: boolean): number {
  return currentFinished ? current + 1 : Math.max(current, 1);
}

/** The form for gameweek `formGw`; null before any gameweek is completed. */
export function form(matches: readonly MatchPoints[], formGw: number): number | null {
  const from = Math.max(formGw - FORM_WEEKS, 1);
  const n = formGw - from;
  if (n <= 0) return null;
  const sum = matches
    .filter((m) => m.gameweek >= from && m.gameweek < formGw)
    .reduce((a, m) => a + m.points, 0);
  return Math.round((sum / n) * 10) / 10;
}

/** Form on screen: one decimal, a dash when there is none yet. */
export function formText(f: number | null): string {
  return f === null ? '–' : f.toFixed(1);
}

/** Best form first; no form (everyone's in GW1) sorts as 0. */
export function compareForm(a: number | null, b: number | null): number {
  return (b ?? 0) - (a ?? 0);
}
