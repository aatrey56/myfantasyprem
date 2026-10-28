import type { Schemas } from '@myfantasyprem/api-client';

import { formatStat } from '@/lib/gamelog';
import { tint, type ColorTokens } from '@/theme';

type Gameweek = Schemas['PlayerGameweek'];
type Stats = Schemas['PlayerGameStats'];
type Upcoming = Schemas['UpcomingFixture'];

/**
 * The game log heat map (docs/design/SLEEPER-OBSERVED.md, Player card): each
 * cell is a tile tinted by how good the number is for fantasy points.
 * `none` is a missing value (a blank or unplayed gameweek): a dash on a dark tile.
 */
export type HeatQuality = 'good' | 'mid' | 'poor' | 'neutral' | 'none';

/** A heat-map column: the gameweek's points or minutes, or one stat. */
export type HeatKey = 'points' | 'minutes' | keyof Stats;

export interface HeatColumn {
  key: HeatKey;
  label: string;
  title: string;
  format?: 'int' | 'dec1' | 'dec2';
}

export interface HeatGroup {
  id: string;
  label: string;
  columns: readonly HeatColumn[];
}

const c = (key: HeatKey, label: string, title: string, format?: HeatColumn['format']) => ({
  key,
  label,
  title,
  format,
});

/** Column groups, left to right. Keeping (saves, pens saved) only for goalkeepers. */
export function heatGroups(position: string): HeatGroup[] {
  const groups: (HeatGroup | null)[] = [
    {
      id: 'fantasy',
      label: 'Fantasy',
      columns: [
        c('points', 'PTS', 'Points'),
        c('minutes', 'MIN', 'Minutes played'),
        c('bonus', 'BON', 'Bonus'),
        c('bps', 'BPS', 'Bonus points system'),
      ],
    },
    {
      id: 'attack',
      label: 'Attack',
      columns: [
        c('goalsScored', 'G', 'Goals'),
        c('assists', 'A', 'Assists'),
        c('expectedGoals', 'xG', 'Expected goals', 'dec2'),
        c('expectedAssists', 'xA', 'Expected assists', 'dec2'),
        c('expectedGoalInvolvements', 'xGI', 'Expected goal involvements', 'dec2'),
      ],
    },
    {
      id: 'defence',
      label: 'Defence',
      columns: [
        c('cleanSheets', 'CS', 'Clean sheet'),
        c('goalsConceded', 'GC', 'Goals conceded'),
        c('expectedGoalsConceded', 'xGC', 'Expected goals conceded', 'dec2'),
        c('defensiveContributionPoints', 'DC', 'Defensive contribution points'),
        c('cbit', 'CBIT', 'Clearances, blocks, interceptions, tackles'),
        c('recoveries', 'REC', 'Recoveries'),
        c('tackles', 'TKL', 'Tackles'),
      ],
    },
    position === 'GKP'
      ? {
          id: 'keeping',
          label: 'Keeping',
          columns: [c('saves', 'SV', 'Saves'), c('penaltiesSaved', 'PS', 'Penalties saved')],
        }
      : null,
    {
      id: 'discipline',
      label: 'Cards',
      columns: [c('yellowCards', 'YC', 'Yellow cards'), c('redCards', 'RC', 'Red cards')],
    },
    {
      id: 'ict',
      label: 'ICT',
      columns: [
        c('influence', 'I', 'Influence', 'dec1'),
        c('creativity', 'C', 'Creativity', 'dec1'),
        c('threat', 'T', 'Threat', 'dec1'),
        c('ictIndex', 'ICT', 'ICT index', 'dec1'),
      ],
    },
  ];
  return groups.filter((g): g is HeatGroup => g !== null);
}

/** Where a clean sheet and goals conceded score (as `eventChips` in lib/gamelog). */
const CLEAN_SHEET_SCORES: Record<string, boolean> = { GKP: true, DEF: true, MID: true, FWD: false };
const CONCEDED_SCORES: Record<string, boolean> = { GKP: true, DEF: true, MID: false, FWD: false };

/** At or above `good`: good; at or above `mid`: middling; otherwise neutral (or `below`). */
const scale = (v: number, good: number, mid: number, below: HeatQuality = 'neutral') =>
  v >= good ? 'good' : v >= mid ? 'mid' : below;

/**
 * How good a game-log number is for fantasy points, for its tile colour.
 * Pure. Missing values (null, NaN) are `none`; zeros of counting stats are
 * `neutral`. Conceding only hurts goalkeepers and defenders, and a clean
 * sheet only helps the positions it scores for.
 */
export function heatQuality(
  key: HeatKey,
  value: number | null | undefined,
  position: string,
): HeatQuality {
  if (value === null || value === undefined || !Number.isFinite(value)) return 'none';
  switch (key) {
    case 'points':
      return scale(value, 7, 3, 'poor');
    case 'minutes':
      return value >= 60 ? 'good' : value > 0 ? 'mid' : 'poor';
    case 'bonus':
      return scale(value, 3, 1);
    case 'bps':
      return value < 0 ? 'poor' : scale(value, 30, 15);
    case 'goalsScored':
    case 'assists':
    case 'penaltiesSaved':
    case 'defensiveContributionPoints':
      return value > 0 ? 'good' : 'neutral';
    case 'expectedGoals':
    case 'expectedAssists':
      return scale(value, 0.5, 0.2);
    case 'expectedGoalInvolvements':
      return scale(value, 0.7, 0.3);
    case 'cleanSheets':
      return value > 0 && (CLEAN_SHEET_SCORES[position] ?? true) ? 'good' : 'neutral';
    case 'goalsConceded':
      if (!(CONCEDED_SCORES[position] ?? true)) return 'neutral';
      return value >= 2 ? 'poor' : value === 1 ? 'mid' : 'neutral';
    case 'expectedGoalsConceded':
      if (!(CONCEDED_SCORES[position] ?? true)) return 'neutral';
      return value >= 2 ? 'poor' : value >= 1 ? 'mid' : 'neutral';
    case 'cbit':
      return scale(value, 10, 6);
    case 'recoveries':
      return scale(value, 8, 4);
    case 'tackles':
      return scale(value, 4, 2);
    case 'saves':
      return scale(value, 3, 1);
    case 'yellowCards':
      return value > 0 ? 'mid' : 'neutral';
    case 'redCards':
    case 'ownGoals':
    case 'penaltiesMissed':
      return value > 0 ? 'poor' : 'neutral';
    // One rule for the ICT group, scaled to each column: influence,
    // creativity and threat share a scale (a big game is 30+), and the
    // index is about a tenth of their sum.
    case 'influence':
    case 'creativity':
    case 'threat':
      return scale(value, 30, 15);
    case 'ictIndex':
      return scale(value, 6, 3);
    default:
      return 'neutral';
  }
}

/**
 * Tile fill and text for a quality, from the allowed pairs (theme/tokens
 * TINT_PAIRS and TEXT_PAIRS): a 16% tint of win, accent or loss over the
 * surface with the full colour as text; raised grey for neutral; the sunken
 * ground with muted text for a dash.
 */
export function heatColors(q: HeatQuality, colors: ColorTokens): { bg: string; fg: string } {
  switch (q) {
    case 'good':
      return { bg: tint(colors.win, colors.surface), fg: colors.win };
    case 'mid':
      return { bg: tint(colors.accent, colors.surface), fg: colors.accent };
    case 'poor':
      return { bg: tint(colors.loss, colors.surface), fg: colors.loss };
    case 'neutral':
      return { bg: colors.surfaceRaised, fg: colors.text };
    default:
      return { bg: colors.surfaceSunken, fg: colors.textMuted };
  }
}

/** No stats at all and no fixture: a blank gameweek. */
export function isBlank(g: Pick<Gameweek, 'fixtures' | 'minutes' | 'stats'>): boolean {
  return (
    g.fixtures.length === 0 && g.minutes === 0 && Object.values(g.stats).every((v) => v === null)
  );
}

/** A heat-map cell's number: points, minutes (none in a blank) or a stat. */
export function heatValue(g: Pick<Gameweek, 'points' | 'minutes' | 'stats'>, key: HeatKey) {
  if (key === 'points') return g.points;
  if (key === 'minutes') {
    const none = g.minutes === 0 && Object.values(g.stats).every((v) => v === null);
    return none ? null : g.minutes;
  }
  return g.stats[key];
}

const ICT_KEYS: readonly HeatKey[] = ['influence', 'creativity', 'threat', 'ictIndex'];

/**
 * ICT not out yet: FPL publishes it once a match's stats are final, so a
 * gameweek with minutes, an unfinished game and all four at 0 has none yet.
 */
export function ictPending(
  g: Pick<Gameweek, 'minutes' | 'stats'> & { fixtures?: Gameweek['fixtures'] },
) {
  if (g.minutes <= 0 || !g.fixtures?.some((f) => f.status !== 'finished')) return false;
  return ICT_KEYS.every((k) => (g.stats[k as keyof Stats] ?? 0) === 0);
}

/** One heat-map cell: its text and quality; ICT not out yet is a dash. */
export function heatCell(
  g: Pick<Gameweek, 'points' | 'minutes' | 'stats'> & { fixtures?: Gameweek['fixtures'] },
  col: HeatColumn,
  position: string,
): { text: string; quality: HeatQuality } {
  const v = ICT_KEYS.includes(col.key) && ictPending(g) ? null : heatValue(g, col.key);
  return { text: formatStat(v, col.format), quality: heatQuality(col.key, v, position) };
}

/** A game log row: a played gameweek, a blank one, or one still to come. */
export type LogRow =
  | { kind: 'played'; gameweek: number; gw: Gameweek }
  | { kind: 'blank'; gameweek: number }
  | { kind: 'future'; gameweek: number; fixtures: FutureFixture[] };

/** What an unplayed row shows of a fixture (upcoming or not yet kicked off). */
export type FutureFixture = Pick<Upcoming, 'fixtureId' | 'opponent' | 'isHome' | 'kickoffAt'>;

/** Every fixture still to kick off (scheduled or postponed): nothing to score yet. */
const notStarted = (g: Gameweek) =>
  g.fixtures.length > 0 &&
  g.fixtures.every((f) => f.status === 'scheduled' || f.status === 'postponed');

/**
 * Oldest first: every gameweek from the first played to the last (blanks
 * filled in; one whose games have not kicked off reads as still to come), then up to `future` gameweeks still to come (a gap there is a
 * blank too).
 */
export function logRows(
  gameweeks: readonly Gameweek[],
  upcoming: readonly Upcoming[] = [],
  future = 5,
): LogRow[] {
  const byGw = new Map(gameweeks.map((g) => [g.gameweek, g]));
  const rows: LogRow[] = [];
  const nums = [...byGw.keys()];
  const last = nums.length ? Math.max(...nums) : 0;
  if (nums.length) {
    for (let gw = Math.min(...nums); gw <= last; gw++) {
      const g = byGw.get(gw);
      if (!g || isBlank(g)) rows.push({ kind: 'blank', gameweek: gw });
      else if (notStarted(g)) rows.push({ kind: 'future', gameweek: gw, fixtures: g.fixtures });
      else rows.push({ kind: 'played', gameweek: gw, gw: g });
    }
  }
  // A fixture with no gameweek yet (moved, TBC) has no row.
  const later = upcoming.filter(
    (f): f is Upcoming & { gameweek: number } => f.gameweek !== null && f.gameweek > last,
  );
  if (later.length) {
    // Straight after the log, so a blank next gameweek shows too.
    const first = last ? last + 1 : Math.min(...later.map((f) => f.gameweek));
    for (let gw = first; gw < first + future; gw++) {
      const fixtures = later.filter((f) => f.gameweek === gw);
      if (fixtures.length === 0 && !later.some((f) => f.gameweek > gw)) break;
      rows.push(
        fixtures.length
          ? { kind: 'future', gameweek: gw, fixtures }
          : { kind: 'blank', gameweek: gw },
      );
    }
  }
  return rows;
}

/** A finished (or live) fixture's result for the player's side, from the score. */
export function fixtureResult(
  f: Pick<Schemas['PlayerFixture'], 'isHome' | 'homeScore' | 'awayScore'>,
): 'win' | 'draw' | 'loss' | null {
  if (f.homeScore == null || f.awayScore == null) return null;
  const mine = f.isHome ? f.homeScore : f.awayScore;
  const theirs = f.isHome ? f.awayScore : f.homeScore;
  return mine > theirs ? 'win' : mine < theirs ? 'loss' : 'draw';
}

/** `Bukayo Saka` → first `Bukayo`, last `Saka`; one word is all last name. */
export function splitName(displayName: string): { first: string; last: string } {
  const name = displayName.trim();
  const at = name.lastIndexOf(' ');
  return at < 0
    ? { first: '', last: name }
    : { first: name.slice(0, at), last: name.slice(at + 1) };
}

/** A chance of playing from the news (`75% chance of playing` → 75), or null. */
export function chanceFromNews(news: string | null | undefined): number | null {
  const m = news ? /(\d{1,3})\s*%/.exec(news) : null;
  if (!m) return null;
  const n = Number(m[1]);
  return n >= 0 && n <= 100 ? n : null;
}
