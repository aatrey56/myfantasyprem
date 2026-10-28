import type { Schemas } from '@myfantasyprem/api-client';

import { formatClock, formatDay, formatKickoff } from './time';

type Fixture = Schemas['GameweekFixture'];

export type FixturePhase = 'upcoming' | 'live' | 'done' | 'postponed';

/** Where a fixture is. Unknown statuses (the enum is open) count as upcoming. */
export function fixturePhase(status: string): FixturePhase {
  switch (status) {
    case 'live':
      return 'live';
    case 'finished':
    case 'finished_provisional':
      return 'done';
    case 'postponed':
      return 'postponed';
    default:
      return 'upcoming';
  }
}

/**
 * A live match's clock: `HT` at half time, `67'` with a minute, `LIVE`
 * before upstream reports one (or for a period this build does not know).
 */
export function liveClock(f: Pick<Fixture, 'minute' | 'period'>): string {
  if (f.period === 'half_time') return 'HT';
  if (f.minute !== null && (f.period === 'first_half' || f.period === 'second_half')) {
    return `${f.minute}'`;
  }
  return 'LIVE';
}

/** The status column: the live clock, `FT`, `PP`, or the kickoff (`Sat 15:00`). */
export function fixtureStatusLabel(f: Fixture, now = Date.now(), timeZone?: string): string {
  switch (fixturePhase(f.status)) {
    case 'live':
      return liveClock(f);
    case 'done':
      return 'FT';
    case 'postponed':
      return 'PP';
    default:
      return f.kickoffAt ? formatKickoff(Date.parse(f.kickoffAt), now, timeZone) : 'TBC';
  }
}

/** As {@link fixtureStatusLabel}, for a row under its day's heading: the kickoff is `15:00`. */
export function fixtureTimeLabel(f: Fixture, timeZone?: string): string {
  if (fixturePhase(f.status) !== 'upcoming') return fixtureStatusLabel(f);
  return f.kickoffAt ? formatClock(Date.parse(f.kickoffAt), timeZone) : 'TBC';
}

/** A score once the match has started; a dash before. */
export function scoreText(score: number | null): string {
  return score === null ? '–' : String(score);
}

/** The winning side of a finished match; null for a draw or a match not over. */
export function winnerOf(f: Fixture): 'home' | 'away' | null {
  if (fixturePhase(f.status) !== 'done' || f.homeScore === null || f.awayScore === null) {
    return null;
  }
  if (f.homeScore === f.awayScore) return null;
  return f.homeScore > f.awayScore ? 'home' : 'away';
}

export interface FixtureDay {
  key: string;
  /** `Sat 17 Oct`, `Date to be confirmed` or `Postponed`. */
  title: string;
  fixtures: Fixture[];
}

/**
 * A gameweek's fixtures under day headings, as the scores screen lists
 * them: each calendar day (in the device's zone) in kickoff order, then
 * fixtures without a date, then postponed ones. Within a day by kickoff,
 * then the server's order.
 */
export function groupFixturesByDay(
  fixtures: readonly Fixture[],
  now = Date.now(),
  timeZone?: string,
): FixtureDay[] {
  const days = new Map<string, { at: number; fixtures: { f: Fixture; i: number }[] }>();
  const tbc: Fixture[] = [];
  const postponed: Fixture[] = [];
  fixtures.forEach((f, i) => {
    if (fixturePhase(f.status) === 'postponed') postponed.push(f);
    else if (!f.kickoffAt) tbc.push(f);
    else {
      const at = Date.parse(f.kickoffAt);
      const title = formatDay(at, now, timeZone);
      const day = days.get(title) ?? { at, fixtures: [] };
      day.at = Math.min(day.at, at);
      day.fixtures.push({ f, i });
      days.set(title, day);
    }
  });
  const out: FixtureDay[] = [...days.entries()]
    .sort(([, a], [, b]) => a.at - b.at)
    .map(([title, day]) => ({
      key: title,
      title,
      fixtures: day.fixtures
        .sort((a, b) => Date.parse(a.f.kickoffAt!) - Date.parse(b.f.kickoffAt!) || a.i - b.i)
        .map(({ f }) => f),
    }));
  if (tbc.length) out.push({ key: 'tbc', title: 'Date to be confirmed', fixtures: tbc });
  if (postponed.length) out.push({ key: 'postponed', title: 'Postponed', fixtures: postponed });
  return out;
}

/** How many of the gameweek's fixtures are live now. */
export const liveCount = (fixtures: readonly Fixture[]) =>
  fixtures.filter((f) => fixturePhase(f.status) === 'live').length;

/** "Arsenal 2, Chelsea 1, live, 67 minutes" for screen readers. */
export function fixtureA11yLabel(f: Fixture, now = Date.now()): string {
  const phase = fixturePhase(f.status);
  if (phase === 'upcoming' || f.homeScore === null || f.awayScore === null) {
    const when = phase === 'postponed' ? 'postponed' : fixtureStatusLabel(f, now);
    return `${f.home.name} v ${f.away.name}, ${when}`;
  }
  const clock = liveClock(f);
  const state =
    phase === 'live'
      ? clock === 'HT'
        ? 'live, half time'
        : clock === 'LIVE'
          ? 'live'
          : `live, ${f.minute} minutes`
      : phase === 'done'
        ? 'full time'
        : 'postponed';
  return `${f.home.name} ${f.homeScore}, ${f.away.name} ${f.awayScore}, ${state}`;
}
