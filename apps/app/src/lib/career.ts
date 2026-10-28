import type { Schemas } from '@myfantasyprem/api-client';

/**
 * The player card's "Previous seasons" table: one row per finished season,
 * newest first, and a career row. A null stat was not tracked by FPL that
 * season (or not sent), so it reads "—", never 0. The career total adds
 * only the seasons where the stat was tracked; with none it is "—" too.
 */
export type PastSeason = Schemas['PlayerSeasonTotals'];
export type CareerKey = Exclude<keyof PastSeason, 'season'>;

export type CareerColumn = {
  key: CareerKey;
  label: string;
  title: string;
  /** Decimal places; 0 for counts. */
  decimals: number;
};

export const CAREER_COLUMNS: readonly CareerColumn[] = [
  { key: 'points', label: 'PTS', title: 'Points', decimals: 0 },
  { key: 'minutes', label: 'MIN', title: 'Minutes', decimals: 0 },
  { key: 'starts', label: 'ST', title: 'Starts', decimals: 0 },
  { key: 'goalsScored', label: 'G', title: 'Goals', decimals: 0 },
  { key: 'assists', label: 'A', title: 'FPL assists', decimals: 0 },
  { key: 'cleanSheets', label: 'CS', title: 'Clean sheets', decimals: 0 },
  { key: 'bonus', label: 'B', title: 'Bonus', decimals: 0 },
  { key: 'expectedGoals', label: 'xG', title: 'Expected goals', decimals: 2 },
  { key: 'expectedAssists', label: 'xA', title: 'Expected assists', decimals: 2 },
  {
    key: 'expectedGoalInvolvements',
    label: 'xGI',
    title: 'Expected goal involvements',
    decimals: 2,
  },
  { key: 'ictIndex', label: 'ICT', title: 'ICT index', decimals: 1 },
  { key: 'defensiveContribution', label: 'DC', title: 'Defensive contribution', decimals: 0 },
];

export const DASH = '—';

/** Newest first, whatever order the server sent. */
export function careerRows(seasons: readonly PastSeason[]): PastSeason[] {
  return [...seasons].sort((a, b) => b.season.localeCompare(a.season));
}

/** The career row: per stat, the sum over tracked seasons; null if none. */
export function careerTotals(seasons: readonly PastSeason[]): Record<CareerKey, number | null> {
  const out = {} as Record<CareerKey, number | null>;
  for (const c of CAREER_COLUMNS) {
    let sum: number | null = null;
    for (const s of seasons) {
      const v = s[c.key];
      if (v !== null && v !== undefined) sum = (sum ?? 0) + v;
    }
    // Decimal stats add up float noise (0.1 + 0.2): round to the shown places.
    out[c.key] = sum === null ? null : Number(sum.toFixed(c.decimals));
  }
  return out;
}

/** A cell: "—" for untracked, else the number at the column's places. */
export function careerCell(v: number | null | undefined, c: CareerColumn): string {
  if (v === null || v === undefined) return DASH;
  return c.decimals > 0 ? v.toFixed(c.decimals) : String(v);
}
