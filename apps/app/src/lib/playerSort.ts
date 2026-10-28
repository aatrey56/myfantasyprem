/**
 * The Players list sorts: the options of the sort dialog (grouped), its
 * search, the labels on the Sort chip and the row box, and the stored
 * choice. Pure; the contract's `sort` parameter. Sorting is always the
 * natural order (highest first; draft rank 1 first): the app never sends
 * the contract's `order`.
 */

export type PlayerSort =
  | 'total_points'
  | 'gameweek_points'
  | 'form'
  | 'form_rise'
  | 'form_fall'
  | 'points_per_match'
  | 'draft_rank'
  | 'name'
  | 'goals'
  | 'assists'
  | 'bonus'
  | 'bps'
  | 'clean_sheets'
  | 'creativity'
  | 'goals_conceded'
  | 'ict_index'
  | 'influence'
  | 'minutes'
  | 'own_goals'
  | 'penalties_missed'
  | 'penalties_saved'
  | 'red_cards'
  | 'saves'
  | 'threat'
  | 'yellow_cards'
  | 'starts'
  | 'expected_goals'
  | 'expected_assists'
  | 'expected_goal_involvements'
  | 'expected_goals_conceded'
  | 'clearances_blocks_interceptions'
  | 'recoveries'
  | 'tackles'
  | 'defensive_contribution';

export interface SortOption {
  value: PlayerSort;
  /** The name in the dialog. */
  label: string;
  /** The abbreviation on the chip and in the row box. */
  short: string;
  /** The long name, searched too. */
  long: string;
  /** Shown to one decimal (else a whole number). */
  decimal?: boolean;
}

export interface SortGroup {
  title: string;
  options: SortOption[];
}

const o = (
  value: PlayerSort,
  label: string,
  short: string,
  long: string,
  decimal = false,
): SortOption => ({ value, label, short, long, decimal });

export const SORT_GROUPS: SortGroup[] = [
  {
    title: 'Popular',
    options: [
      o('total_points', 'Total points', 'PTS', 'Points this season'),
      o('form', 'Form', 'FORM', 'Form', true),
      o('starts', 'Games started', 'GS', 'Starts'),
      o('goals', 'Goals', 'G', 'Goals scored'),
      o('assists', 'Assists', 'A', 'Assists'),
      o('clean_sheets', 'Clean sheets', 'CS', 'Clean sheets'),
      o('defensive_contribution', 'Defensive contribution', 'DC', 'Defensive contribution'),
    ],
  },
  {
    title: 'Points & rank',
    options: [
      o('gameweek_points', 'GW points', 'GW', 'Gameweek points'),
      o('points_per_match', 'Points per match', 'PPM', 'Points per match played', true),
      o('draft_rank', 'Draft rank', 'RANK', 'Pre-draft ranking'),
    ],
  },
  {
    title: 'Attacking',
    options: [
      o('expected_goals', 'xG', 'xG', 'Expected goals', true),
      o('expected_assists', 'xA', 'xA', 'Expected assists', true),
      o('expected_goal_involvements', 'xGI', 'xGI', 'Expected goal involvements', true),
    ],
  },
  {
    title: 'Defending',
    options: [
      o('goals_conceded', 'Goals conceded', 'GC', 'Goals conceded'),
      o('expected_goals_conceded', 'xGC', 'xGC', 'Expected goals conceded', true),
      o('saves', 'Saves', 'SV', 'Saves'),
      o('penalties_saved', 'Penalties saved', 'PS', 'Penalties saved'),
      o('clearances_blocks_interceptions', 'CBI', 'CBI', 'Clearances, blocks and interceptions'),
      o('recoveries', 'Recoveries', 'REC', 'Ball recoveries'),
      o('tackles', 'Tackles', 'TKL', 'Tackles'),
    ],
  },
  {
    title: 'Creativity & threat',
    options: [
      o('influence', 'Influence', 'INF', 'Influence', true),
      o('creativity', 'Creativity', 'CRE', 'Creativity', true),
      o('threat', 'Threat', 'THR', 'Threat', true),
      o('ict_index', 'ICT', 'ICT', 'Influence, creativity and threat index', true),
    ],
  },
  {
    title: 'Discipline & other',
    options: [
      o('minutes', 'Minutes', 'MIN', 'Minutes played'),
      o('bonus', 'Bonus', 'BON', 'Bonus points'),
      o('bps', 'BPS', 'BPS', 'Bonus points system'),
      o('yellow_cards', 'Yellow cards', 'YC', 'Yellow cards'),
      o('red_cards', 'Red cards', 'RC', 'Red cards'),
      o('own_goals', 'Own goals', 'OG', 'Own goals'),
      o('penalties_missed', 'Penalties missed', 'PM', 'Penalties missed'),
    ],
  },
];

const ALL: SortOption[] = SORT_GROUPS.flatMap((g) => g.options);

/** The option for a sort; the other sorts (trend, name) have none. */
export function sortOption(sort: PlayerSort): SortOption | undefined {
  return ALL.find((x) => x.value === sort);
}

/** The groups with only the options matching `query` (case-insensitive, label, short or long name); empty groups drop out. */
export function filterSortGroups(query: string, groups: SortGroup[] = SORT_GROUPS): SortGroup[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return groups;
  return groups
    .map((g) => ({
      ...g,
      options: g.options.filter((x) =>
        [x.label, x.short, x.long].some((t) => t.toLowerCase().includes(needle)),
      ),
    }))
    .filter((g) => g.options.length > 0);
}

/** The short label of a sort (the chip, the row box). */
export function sortShort(sort: PlayerSort): string {
  return sortOption(sort)?.short ?? 'PTS';
}

/** The Sort chip text: `Sort: xGI`. */
export function sortChipText(sort: PlayerSort): string {
  return `Sort: ${sortShort(sort)}`;
}

/** The spoken name of a sort: its label, or the long name where the label is an abbreviation (xGI, CBI). */
export function sortSpoken(sort: PlayerSort): string {
  const x = sortOption(sort);
  if (!x) return 'Total points';
  return x.label === x.short ? x.long : x.label;
}

/** The Sort chip for a screen reader: `Sort: Goals`. */
export function sortChipA11y(sort: PlayerSort): string {
  return `Sort: ${sortSpoken(sort)}`;
}

/** A row's sorted value for a screen reader: `Goals 3`, or `Goals, no value`. */
export function sortValueA11y(sort: PlayerSort, value: number | null): string {
  if (value === null) return `${sortSpoken(sort)}, no value`;
  return `${sortSpoken(sort)} ${formatSortValue(sort, value)}`;
}

/** A sorted value for a row box: a dash for none, one decimal for the fractional stats. */
export function formatSortValue(sort: PlayerSort, value: number | null): string {
  if (value === null) return '–';
  return sortOption(sort)?.decimal ? value.toFixed(1) : String(value);
}

const SORTS = new Set<string>(ALL.map((x) => x.value));

export interface SortChoice {
  sort: PlayerSort;
}

export const DEFAULT_SORT: SortChoice = { sort: 'total_points' };

/** The league's stored sort key. */
export const sortStorageKey = (leagueId: string) => `players-sort:${leagueId}`;

/** Reads a stored choice; anything unknown or malformed is the default. A stored `order` (older builds) is ignored. */
export function parseSortChoice(raw: string | null): SortChoice {
  if (!raw) return DEFAULT_SORT;
  try {
    const v = JSON.parse(raw) as Partial<SortChoice> | null;
    if (v && typeof v.sort === 'string' && SORTS.has(v.sort)) {
      return { sort: v.sort as PlayerSort };
    }
  } catch {
    // fall through
  }
  return DEFAULT_SORT;
}
