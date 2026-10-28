import type { Schemas } from '@myfantasyprem/api-client';

type Gameweek = Schemas['PlayerGameweek'];
type Stats = Schemas['PlayerGameStats'];
type StatKey = keyof Stats;

/** The game log's column groups (chips on desktop, sections on phones). */
export type StatGroup = 'scoring' | 'underlying' | 'defensive';

export const STAT_GROUPS: readonly { value: StatGroup; label: string }[] = [
  { value: 'scoring', label: 'Scoring' },
  { value: 'underlying', label: 'Underlying' },
  { value: 'defensive', label: 'Defensive' },
];

/** The shown dash for a missing stat. */
export const MISSING = '–';

type Format = 'int' | 'dec1' | 'dec2' | 'plus';

/** One stat: where it comes from and how it reads. `minutes` is the gameweek's. */
interface StatDef {
  key: StatKey | 'minutes' | 'cards';
  format?: Format;
}

/** A desktop table column. */
export interface StatColumn extends StatDef {
  label: string;
  title: string;
}

const col = (key: StatColumn['key'], label: string, title: string, format?: Format) => ({
  key,
  label,
  title,
  format,
});

/** Desktop table columns per group, in the approved order (DC before B). */
export const GROUP_COLUMNS: Record<StatGroup, readonly StatColumn[]> = {
  scoring: [
    col('minutes', 'MP', 'Minutes played'),
    col('goalsScored', 'G', 'Goals'),
    col('assists', 'A', 'Assists'),
    col('cleanSheets', 'CS', 'Clean sheet'),
    col('goalsConceded', 'GC', 'Goals conceded'),
    col('cards', 'Cards', 'Yellow and red cards'),
    col('defensiveContributionPoints', 'DC', 'Defensive contribution points'),
    col('bonus', 'B', 'Bonus'),
    col('bps', 'BPS', 'Bonus points system'),
  ],
  underlying: [
    col('influence', 'I', 'Influence', 'dec1'),
    col('creativity', 'C', 'Creativity', 'dec1'),
    col('threat', 'T', 'Threat', 'dec1'),
    col('ictIndex', 'ICT', 'ICT index', 'dec1'),
    col('expectedGoals', 'xG', 'Expected goals', 'dec2'),
    col('expectedAssists', 'xA', 'Expected assists', 'dec2'),
    col('expectedGoalInvolvements', 'xGI', 'Expected goal involvements', 'dec2'),
    col('expectedGoalsConceded', 'xGC', 'Expected goals conceded', 'dec2'),
  ],
  defensive: [
    col('cbit', 'CBIT', 'Clearances, blocks, interceptions, tackles'),
    col('recoveries', 'R', 'Recoveries'),
    col('tackles', 'T', 'Tackles'),
    col('defensiveContributionPoints', 'DC', 'Defensive contribution points'),
  ],
};

/** A phone row's expanded stats: every stat, by group, with full names. */
export const GROUP_DETAILS: Record<StatGroup, readonly (StatDef & { label: string })[]> = {
  scoring: [
    { key: 'minutes', label: 'Minutes' },
    { key: 'goalsScored', label: 'Goals' },
    { key: 'assists', label: 'Assists' },
    { key: 'cleanSheets', label: 'Clean sheet' },
    { key: 'goalsConceded', label: 'Goals conceded' },
    { key: 'ownGoals', label: 'Own goals' },
    { key: 'penaltiesSaved', label: 'Pens saved' },
    { key: 'penaltiesMissed', label: 'Pens missed' },
    { key: 'yellowCards', label: 'Yellow cards' },
    { key: 'redCards', label: 'Red cards' },
    { key: 'saves', label: 'Saves' },
    { key: 'defensiveContributionPoints', label: 'Def. contribution', format: 'plus' },
    { key: 'bonus', label: 'Bonus' },
    { key: 'bps', label: 'BPS' },
  ],
  underlying: [
    { key: 'influence', label: 'Influence', format: 'dec1' },
    { key: 'creativity', label: 'Creativity', format: 'dec1' },
    { key: 'threat', label: 'Threat', format: 'dec1' },
    { key: 'ictIndex', label: 'ICT index', format: 'dec1' },
    { key: 'expectedGoals', label: 'xG', format: 'dec2' },
    { key: 'expectedAssists', label: 'xA', format: 'dec2' },
    { key: 'expectedGoalInvolvements', label: 'xGI', format: 'dec2' },
    { key: 'expectedGoalsConceded', label: 'xGC', format: 'dec2' },
  ],
  defensive: [
    { key: 'cbit', label: 'CBIT' },
    { key: 'recoveries', label: 'Recoveries' },
    { key: 'tackles', label: 'Tackles' },
  ],
};

/** A stat as text: a dash when missing, fixed decimals, `+2` for DC points. */
export function formatStat(value: number | null | undefined, format: Format = 'int'): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return MISSING;
  switch (format) {
    case 'dec1':
      return value.toFixed(1);
    case 'dec2':
      return value.toFixed(2);
    case 'plus':
      return value > 0 ? `+${Math.round(value)}` : String(Math.round(value));
    default:
      return String(Math.round(value));
  }
}

/** Yellow and red cards; null when not known. */
export interface Cards {
  yellow: number | null;
  red: number | null;
}

/** One cell of a game log row. `zero` cells are dimmed. */
export type StatCell =
  | { kind: 'text'; key: string; text: string; zero: boolean }
  | { kind: 'cards'; key: string; cards: Cards; text: string };

/** Screen-reader (and plain) text for a cards cell: `1 yellow`, `None`, `–`. */
export function cardsText({ yellow, red }: Cards): string {
  if (yellow === null && red === null) return MISSING;
  const parts = [yellow ? `${yellow} yellow` : null, red ? `${red} red` : null].filter(
    (p): p is string => p !== null,
  );
  return parts.length ? parts.join(', ') : 'None';
}

/** No stats at all: a blank gameweek, or a source without them. */
const noStats = (s: Stats) => Object.values(s).every((v) => v === null);

function valueOf(gw: Pick<Gameweek, 'minutes' | 'stats'>, key: StatDef['key']): number | null {
  // A blank gameweek's 0 minutes is no game, not a benching.
  if (key === 'minutes') return gw.minutes === 0 && noStats(gw.stats) ? null : gw.minutes;
  if (key === 'cards') return null;
  return gw.stats[key];
}

function cell(gw: Pick<Gameweek, 'minutes' | 'stats'>, def: StatDef): StatCell {
  if (def.key === 'cards') {
    const cards = { yellow: gw.stats.yellowCards, red: gw.stats.redCards };
    return { kind: 'cards', key: def.key, cards, text: cardsText(cards) };
  }
  const v = valueOf(gw, def.key);
  return { kind: 'text', key: def.key, text: formatStat(v, def.format), zero: v === 0 };
}

/** A desktop row's cells for one group, in column order. */
export function rowCells(gw: Pick<Gameweek, 'minutes' | 'stats'>, group: StatGroup): StatCell[] {
  return GROUP_COLUMNS[group].map((c) => cell(gw, c));
}

/** A phone row's expanded sections: all three groups with every stat. */
export function detailSections(gw: Pick<Gameweek, 'minutes' | 'stats'>) {
  return STAT_GROUPS.map((g) => ({
    group: g.value,
    title: g.label,
    stats: GROUP_DETAILS[g.value].map((d) => ({ label: d.label, ...cell(gw, d) })),
  }));
}

/**
 * Season totals for a group's columns. A column with no known values is a
 * dash; decimals are summed and rounded like the cells.
 */
export function groupTotals(
  gameweeks: readonly Pick<Gameweek, 'minutes' | 'stats'>[],
  group: StatGroup,
): StatCell[] {
  const sum = (key: StatDef['key']) => {
    const known = gameweeks.map((g) => valueOf(g, key)).filter((v): v is number => v !== null);
    return known.length ? Math.round(known.reduce((a, b) => a + b, 0) * 100) / 100 : null;
  };
  return GROUP_COLUMNS[group].map((c) => {
    if (c.key === 'cards') {
      const cards = { yellow: sum('yellowCards'), red: sum('redCards') };
      return { kind: 'cards', key: c.key, cards, text: cardsText(cards) };
    }
    const v = sum(c.key);
    return { kind: 'text', key: c.key, text: formatStat(v, c.format), zero: v === 0 };
  });
}

/** A phone row's event chip; cards are coloured. */
export interface EventChip {
  text: string;
  tone: 'neutral' | 'yellow' | 'red';
}

/**
 * Where a summary event scores (FPL 2026-27): a clean sheet for GK, DEF and
 * MID (not FWD); goals conceded for GK and DEF. Positions not listed here
 * (an unknown one) show both.
 */
const CLEAN_SHEET_SCORES: Record<string, boolean> = { GKP: true, DEF: true, MID: true, FWD: false };
const CONCEDED_SCORES: Record<string, boolean> = { GKP: true, DEF: true, MID: false, FWD: false };

/**
 * What happened in a gameweek, as chips under its phone row: only events
 * that scored for the player's position. Two conceded and three saves are
 * where those start to count (-1 per 2, +1 per 3; saves for whoever keeps
 * goal); DC and bonus when they earned points. The expanded row lists every
 * stat.
 */
export function eventChips(position: string, s: Stats): EventChip[] {
  const out: EventChip[] = [];
  const add = (text: string, tone: EventChip['tone'] = 'neutral') => out.push({ text, tone });
  const n = (v: number | null, one: string, many: string) =>
    v ? add(v > 1 ? `${v} ${many}` : one) : undefined;
  n(s.goalsScored, 'Goal', 'goals');
  n(s.assists, 'Assist', 'assists');
  if (s.cleanSheets && (CLEAN_SHEET_SCORES[position] ?? true)) add('Clean sheet');
  if ((s.goalsConceded ?? 0) >= 2 && (CONCEDED_SCORES[position] ?? true)) {
    add(`${s.goalsConceded} conceded`);
  }
  if ((s.saves ?? 0) >= 3) add(`${s.saves} saves`);
  n(s.penaltiesSaved, 'Pen saved', 'pens saved');
  if (s.defensiveContributionPoints) add(`DC +${s.defensiveContributionPoints}`);
  if (s.bonus) add(`Bonus +${s.bonus}`);
  n(s.ownGoals, 'Own goal', 'own goals');
  n(s.penaltiesMissed, 'Pen missed', 'pens missed');
  if (s.yellowCards) add(s.yellowCards > 1 ? `${s.yellowCards} yellows` : 'Yellow', 'yellow');
  if (s.redCards) add('Red', 'red');
  return out;
}

/** 1st, 2nd, 3rd, 4th, 11th, 21st; a dash when unknown. */
export function ordinal(n: number | null | undefined): string {
  if (n === null || n === undefined) return MISSING;
  const tens = n % 100;
  const suffix = tens >= 11 && tens <= 13 ? 'th' : ({ 1: 'st', 2: 'nd', 3: 'rd' }[n % 10] ?? 'th');
  return `${n}${suffix}`;
}

/**
 * A gameweek's games as the log shows them: `AVL (H) 4-0`, home score first;
 * no score before kick-off; both games in a double; `No fixture` in a blank.
 */
export function opponentLabel(fixtures: readonly Schemas['PlayerFixture'][]): string {
  if (fixtures.length === 0) return 'No fixture';
  return fixtures
    .map((f) => {
      const score =
        f.homeScore == null || f.awayScore == null ? '' : ` ${f.homeScore}-${f.awayScore}`;
      return `${f.opponent.code.toUpperCase()} (${f.isHome ? 'H' : 'A'})${score}`;
    })
    .join(', ');
}
