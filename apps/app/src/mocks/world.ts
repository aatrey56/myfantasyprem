/**
 * The global tier of the mock data: clubs, players, gameweeks, fixtures and
 * every player's simulated stats and points. Shared by every mock league,
 * as the real stats feed is shared by every league (docs/adr/0008).
 *
 * Everything here is invented: clubs, players and managers are fictional,
 * so nothing can be mistaken for real FPL data or branding.
 *
 * Deterministic: the same numbers every run (only kickoff times and
 * `dataAsOf` move with the clock). Gameweeks 1 to 6 are final, gameweek 7 is
 * live, gameweeks 8 and up have no scores yet. Gameweek 8 has a double
 * (two clubs play twice) and a blank (two clubs do not play).
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { toRowFixture } from '@/lib/fixtures';
import { form, FORM_WEEKS, formGameweek } from '@/lib/form';
import type { PlayerSort } from '@/lib/playerSort';

type ClubRef = Schemas['ClubRef'];
type Position = Schemas['Position'];
type PlayerRef = Schemas['PlayerRef'];
type PlayerFixture = Schemas['PlayerFixture'];
type PointsLine = Schemas['PointsLine'];
type PlayerGameStats = Schemas['PlayerGameStats'];
type Gameweek = Schemas['Gameweek'];
type UpcomingFixture = Schemas['UpcomingFixture'];
type GameweekFixture = Schemas['GameweekFixture'];

export const SEASON = '2026-27';
export const CURRENT_GW = 7;
/** The next gameweek has a double and a blank. */
export const DGW_GW = CURRENT_GW + 1;
export const LAST_GW = 38;

export const HOUR = 3_600_000;
export const NOW = Date.now();

// ---------------------------------------------------------------------- ids

const CROCKFORD = '0123456789abcdefghjkmnpqrstvwxyz';

/**
 * A deterministic id in the public format: prefix, `_`, and a 26-character
 * lowercase ULID-shaped string (fixed time part, `n` as the random part).
 */
export function mockId(prefix: string, n: number): string {
  let tail = '';
  let x = n;
  for (let i = 0; i < 16; i++) {
    tail = CROCKFORD[x % 32] + tail;
    x = Math.floor(x / 32);
  }
  return `${prefix}_01k7m2xq80${tail}`;
}

// ---------------------------------------------------------------- randomness

export function mulberry32(seed: number) {
  let a = seed >>> 0;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

export const rand = mulberry32(20261009);
export const pick = <T>(items: readonly T[], r: () => number = rand): T =>
  items[Math.floor(r() * items.length)] as T;

export function shuffle<T>(items: readonly T[], r: () => number): T[] {
  const out = [...items];
  for (let i = out.length - 1; i > 0; i--) {
    const j = Math.floor(r() * (i + 1));
    [out[i], out[j]] = [out[j] as T, out[i] as T];
  }
  return out;
}

// -------------------------------------------------------------------- clubs

export const CLUBS: readonly ClubRef[] = [
  { code: 'ASH', name: 'Ashford' },
  { code: 'BRK', name: 'Brookvale' },
  { code: 'CLF', name: 'Cliffton' },
  { code: 'DAL', name: 'Dalehurst' },
  { code: 'EAS', name: 'Eastmoor' },
  { code: 'FEN', name: 'Fenwick' },
  { code: 'GRA', name: 'Granton' },
  { code: 'HAR', name: 'Harlow Town' },
  { code: 'IRO', name: 'Ironbridge' },
  { code: 'KEL', name: 'Kelham' },
  { code: 'LAK', name: 'Lakeside' },
  { code: 'MER', name: 'Merrow' },
  { code: 'NOR', name: 'Northgate' },
  { code: 'OAK', name: 'Oakley' },
  { code: 'PRI', name: 'Priory Park' },
  { code: 'QUA', name: 'Quayside' },
  { code: 'RIV', name: 'Riverton' },
  { code: 'STO', name: 'Stonebridge' },
  { code: 'TAL', name: 'Talbot' },
  { code: 'WES', name: 'Westbrook' },
];

// ------------------------------------------------------------------ players

const SURNAMES = [
  'Calder',
  'Marlow',
  'Okafor',
  'Brandt',
  'Vasquez',
  'Lindqvist',
  'Hale',
  'Moreau',
  'Dunmore',
  'Ketteridge',
  'Abara',
  'Santoro',
  'Whitlock',
  'Duarte',
  'Penhallow',
  'Ivanek',
  'Rourke',
  'Tanaka',
  'Fairbairn',
  'Osei',
  'Mbeki',
  'Castellan',
  'Thorne',
  'Verhoeven',
  'Almeida',
  'Kowalczyk',
  'Draper',
  'Nakamura',
  'Ellery',
  'Sorensen',
  'Bellamy',
  'Achterberg',
  'Quinlan',
  'Faulkner',
  'Rinaldi',
  'Okonkwo',
  'Harrow',
  'Delacroix',
  'Pemberton',
  'Szabo',
  'Lamont',
  'Adeyemi',
  'Voss',
  'Gallagher',
  'Mercer',
  'Ibarra',
  'Tolliver',
  'Brightwell',
  'Nkemelu',
  'Haverford',
  'Ostrander',
  'Mazur',
  'Cavendish',
  'Dlamini',
  'Rask',
  'Yilmaz',
  'Prentice',
  'Aldous',
  'Bernardi',
  'Kovacs',
  'Sutcliffe',
  'Mwangi',
  'Ferreira',
  'Holloway',
  'Zielinski',
  'Lockhart',
  'Abernethy',
  'Teixeira',
  'Greaves',
  'Nilsen',
  'Pascoe',
  'Rademaker',
  'Onuoha',
  'Whitcombe',
  'Delgado',
  'Strand',
  'Kimura',
  'Eastwood',
  'Baptiste',
  'Hargreave',
  'Novak',
  'Ashdown',
  'Carvalho',
  'Pellegrini',
  'Goodwin',
  'Ajayi',
  'Lorimer',
  'Fenn',
  'Sandoval',
  'Ridley',
  'Bakker',
  'Oyelaran',
  'Treadwell',
  'Mallory',
  'Vukovic',
  'Hartigan',
  'Seward',
  'Amadi',
  'Corrigan',
  'Lefebvre',
  'Dunstan',
  'Okoro',
  'Pryce',
  'Abbott',
  'Wetherby',
  'Gustavsen',
  'Tennant',
  'Madrigal',
  'Fitzroy',
  'Ncube',
  'Barrow',
  'Kerrigan',
  'Solheim',
  'Ainsley',
  'Brennan',
  'Chowdhury',
  'Dacre',
  'Eriksen',
  'Foley',
  'Garrity',
  'Heron',
  'Ingram',
  'Jessop',
  'Kendrick',
  'Lyle',
  'Maddox',
  'Nolan',
  'Orchard',
  'Pickett',
  'Quigley',
  'Rowntree',
  'Slater',
  'Tipton',
  'Upton',
  'Vance',
  'Wren',
  'Yardley',
  'Zane',
  'Ackroyd',
  'Bevan',
  'Crispin',
  'Dobbs',
  'Ellwood',
  'Frasier',
  'Gadd',
  'Hobbs',
  'Ilsley',
  'Jarrett',
  'Kemp',
  'Larkin',
  'Moss',
  'Nuttall',
  'Oldfield',
  'Parr',
  'Rudd',
  'Stott',
  'Tarrant',
  'Underhill',
  'Voke',
  'Wragg',
  'Yates',
  'Zouch',
  'Alder',
  'Birch',
  'Colby',
  'Dane',
  'Elston',
  'Fowler',
  'Gant',
  'Hurst',
  'Ives',
  'Judd',
  'Knott',
  'Lowe',
  'Mace',
  'Nash',
  'Orr',
  'Platt',
  'Rook',
  'Sharp',
  'Tull',
  'Vale',
  'Ware',
  'Yeo',
  'Zeal',
];

const INITIALS = 'ABCDEFGHJKLMNOPRSTW';

const POSITION_COUNTS: Record<Position, number> = { GKP: 28, DEF: 70, MID: 70, FWD: 42 };

export interface PlayerRecord extends PlayerRef {
  /** 0 to 1. Drives draft order, minutes and output. */
  quality: number;
  /** Chance of starting a given match. */
  startRate: number;
}

function buildPlayers(): PlayerRecord[] {
  const r = mulberry32(7);
  const names = shuffle(SURNAMES, r);
  const out: PlayerRecord[] = [];
  let seq = 214000;
  let n = 0;
  let injuredSoFar = 0;
  for (const position of ['GKP', 'DEF', 'MID', 'FWD'] as const) {
    for (let i = 0; i < POSITION_COUNTS[position]; i++) {
      const quality = r();
      const roll = r();
      const injured = roll < 0.03;
      const doubtful = !injured && roll < 0.07;
      seq += 1 + Math.floor(r() * 90);
      // Not drawn from the generator, so every other number stays put.
      if (injured) injuredSoFar += 1;
      const suspended = injured && injuredSoFar % 2 === 0;
      const chance = [75, 50, 25][Math.floor(quality * 97) % 3];
      out.push({
        id: mockId('plr', seq),
        displayName: `${INITIALS[n % INITIALS.length]}. ${names[n % names.length]}`,
        club: CLUBS[Math.floor(r() * CLUBS.length)] as ClubRef,
        position,
        availability: suspended
          ? 'suspended'
          : injured
            ? 'injured'
            : doubtful
              ? 'doubtful'
              : 'available',
        news: suspended
          ? 'Suspended for one match'
          : injured
            ? 'Hamstring injury, out for several weeks'
            : doubtful
              ? `${chance}% chance of playing`
              : null,
        // News times spread over the last three days, from the id (not the generator).
        newsAt: injured || doubtful ? new Date(NOW - (1 + (seq % 70)) * HOUR).toISOString() : null,
        quality,
        startRate: injured ? 0.1 : 0.35 + quality * 0.55,
      });
      n++;
    }
  }
  return out;
}

export const PLAYERS: readonly PlayerRecord[] = buildPlayers();
const playerById = new Map(PLAYERS.map((p) => [p.id, p]));
export const getPlayerRecord = (id: string) => playerById.get(id);

export function toPlayerRef(p: PlayerRecord): PlayerRef {
  return {
    id: p.id,
    displayName: p.displayName,
    club: p.club,
    position: p.position,
    availability: p.availability,
    news: p.news,
    newsAt: p.newsAt ?? null,
  };
}

// ------------------------------------------------------------- gameweeks

export function gameweek(n: number): Gameweek {
  // Gameweek 7 is in progress: its deadline passed a day ago, one week apart.
  const deadlineAt = NOW - 24 * HOUR + (n - CURRENT_GW) * 7 * 24 * HOUR;
  return {
    number: n,
    name: `Gameweek ${n}`,
    deadlineAt: new Date(deadlineAt).toISOString(),
    waiverDeadlineAt: new Date(deadlineAt - 24 * HOUR).toISOString(),
    tradeDeadlineAt: new Date(deadlineAt - 48 * HOUR).toISOString(),
    finished: n < CURRENT_GW,
  };
}

// ---------------------------------------------------------- club fixtures

interface ClubFixture {
  id: string;
  /** Numeric seed for the match simulation. */
  seq: number;
  gw: number;
  home: ClubRef;
  away: ClubRef;
  status: Schemas['FixtureStatus'];
  /** Minutes played so far, 90 when finished, 0 when not started. */
  elapsed: number;
  kickoffAt: string;
  homeScore: number | null;
  awayScore: number | null;
}

/**
 * Gameweek 7 kickoff offsets in hours: four done, three live (one second
 * half, one at half time, one first half), three to come.
 */
const GW7_KICKOFF_OFFSETS = [-30, -29, -28, -27, -1.25, -1, -0.75, 4, 28, 29];
const GW7_ELAPSED = [90, 90, 90, 90, 71, 45, 38, 0, 0, 0];

/** A gameweek with one fixture postponed (no date), and one with no fixtures at all. */
export const POSTPONED_GW = CURRENT_GW + 4;
export const BLANK_GW = CURRENT_GW + 5;

function buildClubFixtures(): ClubFixture[] {
  const out: ClubFixture[] = [];
  let n = 1;
  for (let gw = 1; gw <= CURRENT_GW; gw++) {
    const r = mulberry32(1000 + gw);
    const order = shuffle(CLUBS, r);
    for (let k = 0; k < 10; k++) {
      const home = order[k * 2] as ClubRef;
      const away = order[k * 2 + 1] as ClubRef;
      const live = gw === CURRENT_GW;
      const elapsed = live ? (GW7_ELAPSED[k] as number) : 90;
      const kickoff = live
        ? NOW + (GW7_KICKOFF_OFFSETS[k] as number) * HOUR
        : NOW - (CURRENT_GW - gw) * 7 * 24 * HOUR - 48 * HOUR + k * 3 * HOUR;
      // In the live gameweek a finished match stays provisional until bonus is added.
      const status: Schemas['FixtureStatus'] =
        elapsed >= 90
          ? live
            ? 'finished_provisional'
            : 'finished'
          : elapsed > 0
            ? 'live'
            : 'scheduled';
      const scale = elapsed / 90;
      const goals = () => {
        let g = 0;
        for (let i = 0; i < 4; i++) if (r() < 0.2 * scale + 0.12 * scale) g++;
        return g;
      };
      out.push({
        id: mockId('fx', n),
        seq: n++,
        gw,
        home,
        away,
        status,
        elapsed,
        kickoffAt: new Date(kickoff).toISOString(),
        homeScore: status === 'scheduled' ? null : goals(),
        awayScore: status === 'scheduled' ? null : goals(),
      });
    }
  }
  return out;
}

const CLUB_FIXTURES = buildClubFixtures();

export const isOver = (status: Schemas['FixtureStatus']) =>
  status === 'finished' || status === 'finished_provisional';

/**
 * Upcoming club fixtures for one future gameweek. In {@link DGW_GW} the last
 * pairing is dropped (a blank for both clubs) and the first and third clubs
 * in the draw meet again midweek (a double for both).
 */
function upcomingClubFixtures(gw: number) {
  if (gw === BLANK_GW) return [];
  const r = mulberry32(2000 + gw);
  const order = shuffle(CLUBS, r);
  const firstKickoff = Date.parse(gameweek(gw).deadlineAt) + 1.5 * HOUR;
  const pairs: [ClubRef, ClubRef, number][] = [];
  for (let k = 0; k < 10; k++) {
    pairs.push([
      order[k * 2] as ClubRef,
      order[k * 2 + 1] as ClubRef,
      firstKickoff + k * 2.5 * HOUR,
    ]);
  }
  if (gw === DGW_GW) {
    pairs.pop();
    pairs.push([order[2] as ClubRef, order[0] as ClubRef, firstKickoff + 72 * HOUR]);
  }
  return pairs.map(([home, away, kickoff], k) => {
    // In POSTPONED_GW the last pairing is called off: no date yet.
    const postponed = gw === POSTPONED_GW && k === pairs.length - 1;
    return {
      id: mockId('fx', gw * 100 + k),
      home,
      away,
      kickoffAt: postponed ? null : new Date(kickoff).toISOString(),
      status: (postponed ? 'postponed' : 'scheduled') as Schemas['FixtureStatus'],
    };
  });
}

/**
 * A live fixture's minute and period, as the server derives them: the
 * minute stands at 45 over half time, which is half time 50 minutes after
 * kickoff.
 */
function matchClock(f: ClubFixture): Pick<GameweekFixture, 'minute' | 'period'> {
  if (f.status !== 'live' || f.elapsed <= 0) return { minute: null, period: null };
  const sinceKickoff = NOW - Date.parse(f.kickoffAt);
  const period =
    f.elapsed > 45
      ? 'second_half'
      : f.elapsed === 45 && sinceKickoff > 50 * 60_000
        ? 'half_time'
        : 'first_half';
  return { minute: f.elapsed, period };
}

/**
 * Every Premier League fixture of one gameweek with its score, by kickoff
 * then id (`GET /v1/gameweeks/{gw}/fixtures`). The same fixtures, ids and
 * scores as the players' gameweeks: in {@link CURRENT_GW} four are over
 * (provisional), three live and three to come; earlier gameweeks are
 * finished; later ones are scheduled with null scores.
 */
export function gameweekFixtures(gw: number): GameweekFixture[] {
  const fixtures: GameweekFixture[] =
    gw <= CURRENT_GW
      ? CLUB_FIXTURES.filter((f) => f.gw === gw).map((f) => ({
          fixtureId: f.id,
          gameweek: gw,
          kickoffAt: f.kickoffAt,
          status: f.status,
          home: f.home,
          away: f.away,
          homeScore: f.homeScore,
          awayScore: f.awayScore,
          ...matchClock(f),
        }))
      : upcomingClubFixtures(gw).map((f) => ({
          fixtureId: f.id,
          gameweek: gw,
          kickoffAt: f.kickoffAt,
          status: f.status,
          home: f.home,
          away: f.away,
          homeScore: null,
          awayScore: null,
          minute: null,
          period: null,
        }));
  // By kickoff (none last), then id.
  return fixtures.sort(
    (a, b) =>
      Number(a.kickoffAt === null) - Number(b.kickoffAt === null) ||
      (a.kickoffAt ?? '').localeCompare(b.kickoffAt ?? '') ||
      a.fixtureId.localeCompare(b.fixtureId),
  );
}

/** Any fixture of the season by id. */
export function findFixture(fixtureId: string): GameweekFixture | undefined {
  for (let gw = 1; gw <= LAST_GW; gw++) {
    const f = gameweekFixtures(gw).find((x) => x.fixtureId === fixtureId);
    if (f) return f;
  }
  return undefined;
}

/** Clubs with two fixtures in {@link DGW_GW}, and clubs with none. */
export const DGW_CLUBS: readonly string[] = (() => {
  const order = shuffle(CLUBS, mulberry32(2000 + DGW_GW));
  return [order[0]!.code, order[2]!.code];
})();
export const BLANK_CLUBS: readonly string[] = (() => {
  const order = shuffle(CLUBS, mulberry32(2000 + DGW_GW));
  return [order[18]!.code, order[19]!.code];
})();

// ------------------------------------------------------- player gameweeks

const GOAL_POINTS: Record<Position, number> = { GKP: 10, DEF: 6, MID: 5, FWD: 4 };

function line(stat: PointsLine['stat'], value: number, points: number): PointsLine {
  return { stat, value, points };
}

/** One player's fixture entry before bonus is assigned. */
interface Draft {
  player: PlayerRecord;
  fixture: ClubFixture;
  isHome: boolean;
  minutes: number;
  goals: number;
  assists: number;
  conceded: number;
  lines: PointsLine[];
}

function weightedPick<T>(
  items: readonly T[],
  weight: (t: T) => number,
  r: () => number,
): T | undefined {
  const total = items.reduce((s, t) => s + weight(t), 0);
  if (total <= 0) return undefined;
  let x = r() * total;
  for (const item of items) {
    x -= weight(item);
    if (x <= 0) return item;
  }
  return items[items.length - 1];
}

function simulateFixture(fixture: ClubFixture): Draft[] {
  const r = mulberry32(fixture.seq * 7919);
  const drafts: Draft[] = [];
  const sides = [
    {
      club: fixture.home,
      isHome: true,
      scored: fixture.homeScore ?? 0,
      conceded: fixture.awayScore ?? 0,
    },
    {
      club: fixture.away,
      isHome: false,
      scored: fixture.awayScore ?? 0,
      conceded: fixture.homeScore ?? 0,
    },
  ];
  if (fixture.status === 'scheduled') {
    for (const side of sides) {
      for (const player of PLAYERS.filter((p) => p.club.code === side.club.code)) {
        drafts.push({
          player,
          fixture,
          isHome: side.isHome,
          minutes: 0,
          goals: 0,
          assists: 0,
          conceded: 0,
          lines: [],
        });
      }
    }
    return drafts;
  }
  for (const side of sides) {
    const squad = PLAYERS.filter((p) => p.club.code === side.club.code);
    const played = squad
      .filter((p) => r() < p.startRate)
      .map((player) => {
        const starts = r() < 0.9;
        const minutes = Math.min(
          fixture.elapsed,
          starts ? (r() < 0.7 ? 90 : 55 + Math.floor(r() * 30)) : 8 + Math.floor(r() * 25),
        );
        return { player, minutes };
      });
    const goalWeight = (x: { player: PlayerRecord }) =>
      ({ GKP: 0, DEF: 1, MID: 3, FWD: 5 })[x.player.position] * (0.5 + x.player.quality);
    const goals = new Map<string, number>();
    const assists = new Map<string, number>();
    for (let g = 0; g < side.scored; g++) {
      const scorer = weightedPick(played, goalWeight, r);
      if (!scorer) break;
      goals.set(scorer.player.id, (goals.get(scorer.player.id) ?? 0) + 1);
      if (r() < 0.7) {
        const helper = weightedPick(
          played.filter((x) => x.player.id !== scorer.player.id),
          goalWeight,
          r,
        );
        if (helper) assists.set(helper.player.id, (assists.get(helper.player.id) ?? 0) + 1);
      }
    }
    for (const { player, minutes } of played) {
      drafts.push({
        player,
        fixture,
        isHome: side.isHome,
        minutes,
        goals: goals.get(player.id) ?? 0,
        assists: assists.get(player.id) ?? 0,
        conceded: side.conceded,
        lines: [],
      });
    }
    for (const player of squad.filter((p) => !played.some((x) => x.player.id === p.id))) {
      drafts.push({
        player,
        fixture,
        isHome: side.isHome,
        minutes: 0,
        goals: 0,
        assists: 0,
        conceded: 0,
        lines: [],
      });
    }
  }
  // Points per FPL scoring: see docs/research/RESEARCH.md section 7.
  for (const d of drafts) {
    if (d.minutes === 0) continue;
    const pos = d.player.position;
    d.lines.push(line('minutes', d.minutes, d.minutes >= 60 ? 2 : 1));
    if (d.goals) d.lines.push(line('goals_scored', d.goals, d.goals * GOAL_POINTS[pos]));
    if (d.assists) d.lines.push(line('assists', d.assists, d.assists * 3));
    if (d.minutes >= 60 && d.conceded === 0 && pos !== 'FWD') {
      d.lines.push(line('clean_sheets', 1, pos === 'MID' ? 1 : 4));
    }
    if ((pos === 'GKP' || pos === 'DEF') && Math.floor(d.conceded / 2) > 0) {
      d.lines.push(line('goals_conceded', d.conceded, -Math.floor(d.conceded / 2)));
    }
    if (pos === 'GKP') {
      const saves = Math.floor(r() * 7);
      if (Math.floor(saves / 3) > 0) d.lines.push(line('saves', saves, Math.floor(saves / 3)));
    }
    if (pos !== 'GKP' && d.minutes >= 60 && r() < (pos === 'DEF' ? 0.28 : 0.1)) {
      d.lines.push(line('defensive_contribution', pos === 'DEF' ? 10 : 12, 2));
    }
    if (r() < 0.09) d.lines.push(line('yellow_cards', 1, -1));
    else if (r() < 0.01) d.lines.push(line('red_cards', 1, -3));
  }
  // Bonus 3/2/1 to the best three, only once the match is over.
  if (isOver(fixture.status)) {
    const ranked = drafts
      .filter((d) => d.minutes > 0)
      .map((d) => ({ d, pts: d.lines.reduce((s, l) => s + l.points, 0) + d.player.quality }))
      .sort((a, b) => b.pts - a.pts)
      .slice(0, 3);
    ranked.forEach(({ d }, i) => d.lines.push(line('bonus', 3 - i, 3 - i)));
  }
  return drafts;
}

/** playerId:gw -> fixtures. A club with no fixture in a gameweek has no entry. */
const playerFixtures = new Map<string, PlayerFixture[]>();
for (const fixture of CLUB_FIXTURES) {
  for (const d of simulateFixture(fixture)) {
    const opponent = d.isHome ? fixture.away : fixture.home;
    const entry: PlayerFixture = {
      fixtureId: fixture.id,
      opponent,
      isHome: d.isHome,
      kickoffAt: fixture.kickoffAt,
      status: fixture.status,
      homeScore: fixture.homeScore,
      awayScore: fixture.awayScore,
      minutes: d.minutes,
      points: d.lines.reduce((s, l) => s + l.points, 0),
      // Bonus is provisional until FPL adds it after the last match of the day.
      bonusProvisional: fixture.gw === CURRENT_GW && d.lines.some((l) => l.stat === 'bonus'),
      breakdown: d.lines,
    };
    const key = `${d.player.id}:${fixture.gw}`;
    playerFixtures.set(key, [...(playerFixtures.get(key) ?? []), entry]);
  }
}

export const fixturesFor = (playerId: string, gw: number): PlayerFixture[] =>
  playerFixtures.get(`${playerId}:${gw}`) ?? [];
export const pointsFor = (playerId: string, gw: number) =>
  fixturesFor(playerId, gw).reduce((s, f) => s + f.points, 0);
export const minutesFor = (playerId: string, gw: number) =>
  fixturesFor(playerId, gw).reduce((s, f) => s + f.minutes, 0);

// ------------------------------------------------------ per-game stats

const STAT_KEYS = [
  'goalsScored',
  'assists',
  'cleanSheets',
  'goalsConceded',
  'ownGoals',
  'penaltiesSaved',
  'penaltiesMissed',
  'yellowCards',
  'redCards',
  'saves',
  'defensiveContributionPoints',
  'bonus',
  'bps',
  'influence',
  'creativity',
  'threat',
  'ictIndex',
  'expectedGoals',
  'expectedAssists',
  'expectedGoalInvolvements',
  'expectedGoalsConceded',
  'cbit',
  'recoveries',
  'tackles',
] as const satisfies readonly (keyof PlayerGameStats)[];

/** Every stat null: a blank gameweek, or a match not started. */
export const NO_STATS = Object.fromEntries(STAT_KEYS.map((k) => [k, null])) as PlayerGameStats;

/** FNV-1a, to seed the stats of one player in one fixture. */
function hash(text: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 0x01000193);
  return h >>> 0;
}

const round = (x: number, dp: number) => Math.round(x * 10 ** dp) / 10 ** dp;

/**
 * Invented stats for one player in one fixture, consistent with its points
 * breakdown (goals, cards, DC and bonus match the lines). Separate random
 * stream, so the points simulation above is unchanged.
 */
function fixtureStats(p: PlayerRecord, f: PlayerFixture): PlayerGameStats {
  if (f.status === 'scheduled') return NO_STATS;
  const lineOf = (stat: string) => f.breakdown.find((l) => l.stat === stat);
  const count = (stat: string) => lineOf(stat)?.value ?? 0;
  const zero = Object.fromEntries(STAT_KEYS.map((k) => [k, 0])) as PlayerGameStats;
  if (f.minutes === 0) return zero;
  const r = mulberry32(hash(`${p.id}:${f.fixtureId}`));
  const pos = p.position;
  const share = f.minutes / 90;
  const against = (f.isHome ? f.awayScore : f.homeScore) ?? 0;
  const conceded = f.minutes >= 60 ? against : Math.floor(against * share);
  const goals = count('goals_scored');
  const assists = count('assists');
  const yellows = count('yellow_cards');
  const reds = count('red_cards');
  const bonus = count('bonus');
  const dc = lineOf('defensive_contribution')?.points ?? 0;
  const cs = f.minutes >= 60 && against === 0 ? 1 : 0;
  const saves = pos === 'GKP' ? (lineOf('saves')?.value ?? Math.floor(r() * 3)) : 0;
  const attack = { GKP: 0, DEF: 0.25, MID: 0.6, FWD: 1 }[pos] * (0.4 + p.quality);
  const xg = round(goals * 0.3 + r() * 0.35 * attack * share, 2);
  const xa = round(assists * 0.25 + r() * 0.25 * attack * share, 2);
  const influence = round(
    4 * share + goals * 20 + assists * 12 + saves * 4 + (pos !== 'FWD' ? cs * 8 : 0) + r() * 18,
    1,
  );
  const creativity = round(
    assists * 10 + r() * { GKP: 3, DEF: 28, MID: 42, FWD: 22 }[pos] * share,
    1,
  );
  const threat = Math.round(goals * 14 + r() * { GKP: 0, DEF: 12, MID: 30, FWD: 45 }[pos] * share);
  const cbit =
    pos === 'DEF'
      ? dc
        ? 10 + Math.floor(r() * 5)
        : 2 + Math.floor(r() * 8)
      : pos === 'MID'
        ? Math.floor(r() * (dc ? 9 : 6)) + (dc ? 4 : 0)
        : pos === 'GKP'
          ? 2 + Math.floor(r() * 5)
          : Math.floor(r() * 4) + (dc ? 3 : 0);
  // Midfielders and forwards earn DC from CBIRT (with recoveries) reaching 12.
  const recoveries = Math.max(
    1 + Math.floor(r() * 6),
    (pos === 'MID' || pos === 'FWD') && dc ? 12 - cbit : 0,
  );
  const tackles = Math.min(cbit, Math.floor(r() * 4));
  const bpsPerGoal = { GKP: 12, DEF: 12, MID: 18, FWD: 24 }[pos];
  const bps =
    (f.minutes >= 60 ? 6 : 3) +
    goals * bpsPerGoal +
    assists * 9 +
    (pos === 'GKP' || pos === 'DEF' ? cs * 12 - 4 * Math.floor(conceded / 2) : 0) +
    saves * 2 -
    yellows * 3 -
    reds * 9 +
    bonus * 5 +
    Math.floor(r() * 14) -
    2;
  return {
    ...zero,
    goalsScored: goals,
    assists,
    cleanSheets: cs,
    goalsConceded: conceded,
    yellowCards: yellows,
    redCards: reds,
    saves,
    defensiveContributionPoints: dc,
    bonus,
    bps,
    influence,
    creativity,
    threat,
    ictIndex: round((influence + creativity + threat) / 10, 1),
    expectedGoals: xg,
    expectedAssists: xa,
    expectedGoalInvolvements: round(xg + xa, 2),
    expectedGoalsConceded:
      pos === 'FWD' ? round(r() * 1.5 * share, 2) : round(against * 0.6 + r() * 0.9 * share, 2),
    cbit,
    recoveries,
    tackles,
  };
}

/** One player's line in one fixture, for the box score; null before he plays. */
export interface BoxScoreLine {
  player: PlayerRecord;
  entry: PlayerFixture | null;
  stats: PlayerGameStats;
}

/**
 * Every player of the fixture's two clubs with his line in it (the box
 * score, `GET /v1/fixtures/{fixtureId}`), home club first.
 */
export function fixtureLines(f: GameweekFixture): { home: BoxScoreLine[]; away: BoxScoreLine[] } {
  const side = (code: string) =>
    PLAYERS.filter((p) => p.club.code === code).map((player) => {
      const entry = fixturesFor(player.id, f.gameweek).find((x) => x.fixtureId === f.fixtureId);
      return entry
        ? { player, entry, stats: fixtureStats(player, entry) }
        : { player, entry: null, stats: NO_STATS };
    });
  return { home: side(f.home.code), away: side(f.away.code) };
}

/**
 * One player's stats for a gameweek, summed over its fixtures. A stat stays
 * null only when every fixture has it null (a blank or unplayed gameweek).
 */
export function statsFor(playerId: string, gw: number): PlayerGameStats {
  const p = playerById.get(playerId);
  const per = p ? fixturesFor(playerId, gw).map((f) => fixtureStats(p, f)) : [];
  const out = { ...NO_STATS };
  for (const k of STAT_KEYS) {
    const known = per.map((s) => s[k]).filter((v): v is number => v !== null);
    if (known.length)
      out[k] = round(
        known.reduce((a, b) => a + b, 0),
        2,
      );
  }
  return out;
}

/** Rank by season points among players of the same position, 1 = most. */
export function positionRankOf(playerId: string): number | null {
  const p = playerById.get(playerId);
  if (!p) return null;
  const points = (id: string) => seasonTotals(id).points;
  const mine = points(playerId);
  return PLAYERS.filter((o) => o.position === p.position && points(o.id) > mine).length + 1;
}

export function seasonTotals(playerId: string) {
  let points = 0;
  let minutes = 0;
  for (let gw = 1; gw <= CURRENT_GW; gw++) {
    points += pointsFor(playerId, gw);
    minutes += minutesFor(playerId, gw);
  }
  return { points, minutes };
}

export const gameweekPoints = pointsFor;

/** Matches a player played in this season (minutes in them). */
export function appearancesOf(playerId: string): number {
  let n = 0;
  for (let gw = 1; gw <= CURRENT_GW; gw++) {
    n += fixturesFor(playerId, gw).filter((f) => f.minutes > 0).length;
  }
  return n;
}

/** Matches the player's club has finished this season. */
export function clubMatchesOf(playerId: string): number {
  const code = playerById.get(playerId)?.club.code;
  return CLUB_FIXTURES.filter(
    (f) => isOver(f.status) && (f.home.code === code || f.away.code === code),
  ).length;
}

/**
 * A player's club fixtures in one future gameweek, in kickoff order: two in a
 * double gameweek, none in a blank.
 */
export function fixturesInGameweek(playerId: string, gw: number): UpcomingFixture[] {
  const p = playerById.get(playerId);
  if (!p) return [];
  return upcomingClubFixtures(gw)
    .filter((f): f is typeof f & { kickoffAt: string } => f.kickoffAt !== null)
    .filter((f) => f.home.code === p.club.code || f.away.code === p.club.code)
    .sort((a, b) => a.kickoffAt.localeCompare(b.kickoffAt))
    .map((f) => {
      const isHome = f.home.code === p.club.code;
      return {
        fixtureId: f.id,
        gameweek: gw,
        opponent: isHome ? f.away : f.home,
        isHome,
        kickoffAt: f.kickoffAt,
      };
    });
}

/**
 * Upcoming fixtures for the player sheet: every remaining gameweek's, as the
 * server sends all of the club's fixtures left this season.
 */
export function upcomingFor(playerId: string, from = CURRENT_GW + 1, gameweeks = 38 - from + 1) {
  return Array.from({ length: Math.max(0, gameweeks) }, (_, i) =>
    fixturesInGameweek(playerId, from + i),
  ).flat();
}

/** A list or roster row's remaining fixtures, compact as the server sends them (RowFixture). */
export function rowFixturesFor(playerId: string): Schemas['RowFixture'][] {
  return upcomingFor(playerId).map(toRowFixture);
}

// ------------------------------------------------------- rank and form

/** Pre-draft ranking across all positions, 1 = best (autopick uses it). */
const DRAFT_RANK = new Map(
  [...PLAYERS].sort((a, b) => b.quality - a.quality).map((p, i) => [p.id, i + 1]),
);
export const draftRankOf = (playerId: string) => DRAFT_RANK.get(playerId) ?? PLAYERS.length;

/** The gameweek form is for: the current one, live in the mocks (lib/form.ts). */
export const FORM_GW = formGameweek(CURRENT_GW, gameweek(CURRENT_GW).finished);

/** Mean points over the two completed gameweeks before FORM_GW (lib/form.ts). */
export function formOf(playerId: string): number | null {
  const matches = Array.from({ length: CURRENT_GW }, (_, i) => ({
    gameweek: i + 1,
    points: pointsFor(playerId, i + 1),
  }));
  return form(matches, FORM_GW);
}

/** Form minus the same mean for the two gameweeks before; null until there are any. */
export function formChangeOf(playerId: string): number | null {
  const matches = Array.from({ length: CURRENT_GW }, (_, i) => ({
    gameweek: i + 1,
    points: pointsFor(playerId, i + 1),
  }));
  const now = form(matches, FORM_GW);
  const before = form(matches, FORM_GW - FORM_WEEKS);
  if (now === null || before === null) return null;
  return Math.round((now - before) * 10) / 10;
}

/** Season totals for a list row's stat strip (PlayerSeasonStats). */
export function seasonStatsOf(playerId: string): Schemas['PlayerSeasonStats'] {
  const out: Schemas['PlayerSeasonStats'] = {
    goals: 0,
    assists: 0,
    cleanSheets: 0,
    saves: 0,
    defensiveContributionPoints: 0,
    expectedGoalInvolvements: null,
  };
  for (let gw = 1; gw <= CURRENT_GW; gw++) {
    const s = statsFor(playerId, gw);
    out.goals += s.goalsScored ?? 0;
    out.assists += s.assists ?? 0;
    out.cleanSheets += s.cleanSheets ?? 0;
    out.saves += s.saves ?? 0;
    out.defensiveContributionPoints += s.defensiveContributionPoints ?? 0;
    if (s.expectedGoalInvolvements !== null) {
      out.expectedGoalInvolvements =
        (out.expectedGoalInvolvements ?? 0) + s.expectedGoalInvolvements;
    }
  }
  if (out.expectedGoalInvolvements !== null) {
    out.expectedGoalInvolvements = Math.round(out.expectedGoalInvolvements * 100) / 100;
  }
  return out;
}

/** Clubs by name, for `GET /v1/clubs`. */
export const CLUBS_BY_NAME: readonly ClubRef[] = [...CLUBS].sort((a, b) =>
  a.name.localeCompare(b.name),
);

/** The `PlayerGameStats` key a season sum of each stat sort adds up. */
const SORT_STAT: Partial<Record<PlayerSort, keyof PlayerGameStats>> = {
  goals: 'goalsScored',
  assists: 'assists',
  bonus: 'bonus',
  bps: 'bps',
  clean_sheets: 'cleanSheets',
  creativity: 'creativity',
  goals_conceded: 'goalsConceded',
  ict_index: 'ictIndex',
  influence: 'influence',
  own_goals: 'ownGoals',
  penalties_missed: 'penaltiesMissed',
  penalties_saved: 'penaltiesSaved',
  red_cards: 'redCards',
  saves: 'saves',
  threat: 'threat',
  yellow_cards: 'yellowCards',
  expected_goals: 'expectedGoals',
  expected_assists: 'expectedAssists',
  expected_goal_involvements: 'expectedGoalInvolvements',
  expected_goals_conceded: 'expectedGoalsConceded',
  recoveries: 'recoveries',
  tackles: 'tackles',
};

/** CBI as the server sums it: clearances, blocks and interceptions, without tackles. */
export function cbiOf(s: Pick<PlayerGameStats, 'cbit' | 'tackles'>): number {
  return (s.cbit ?? 0) - (s.tackles ?? 0);
}

/**
 * Defensive contribution as upstream counts it (the actions, not the
 * points): CBIT for a defender, CBIRT (with recoveries) otherwise.
 */
export function defensiveActionsOf(
  pos: Schemas['Position'],
  s: Pick<PlayerGameStats, 'cbit' | 'recoveries'>,
): number {
  return (s.cbit ?? 0) + (pos === 'DEF' ? 0 : (s.recoveries ?? 0));
}

/** Stats the source tracks per match: null for a player who never played. */
const NEEDS_MINUTES = new Set<PlayerSort>([
  'bps',
  'creativity',
  'ict_index',
  'influence',
  'threat',
  'expected_goals',
  'expected_assists',
  'expected_goal_involvements',
  'expected_goals_conceded',
  'clearances_blocks_interceptions',
  'recoveries',
  'tackles',
  'defensive_contribution',
]);

/**
 * `PlayerListItem.sortValue` for a sort: the season value of that stat, or
 * null when untracked (and for `name`). Counts are whole numbers, ICT and
 * expected stats two decimals, form and points per match one.
 */
/** Season games started: the fixtures he started (over 45 minutes in this world). */
export function startsOf(playerId: string): number {
  let n = 0;
  for (let gw = 1; gw <= CURRENT_GW; gw++) {
    n += fixturesFor(playerId, gw).filter((f) => f.minutes > 45).length;
  }
  return n;
}

export function sortValueOf(playerId: string, sort: PlayerSort): number | null {
  const totals = seasonTotals(playerId);
  switch (sort) {
    case 'name':
      return null;
    case 'total_points':
      return totals.points;
    case 'gameweek_points':
      return pointsFor(playerId, CURRENT_GW);
    case 'form': {
      const f = formOf(playerId);
      return f === null ? null : round(f, 1);
    }
    case 'form_rise':
    case 'form_fall':
      return formChangeOf(playerId);
    case 'points_per_match': {
      const n = appearancesOf(playerId);
      return n === 0 ? null : round(totals.points / n, 1);
    }
    case 'draft_rank':
      return draftRankOf(playerId);
    case 'minutes':
      return totals.minutes;
    case 'starts':
      return startsOf(playerId);
    case 'clearances_blocks_interceptions':
    case 'defensive_contribution': {
      if (CURRENT_GW < 1) return null;
      const pos = PLAYERS.find((p) => p.id === playerId)?.position ?? 'MID';
      let n = 0;
      for (let gw = 1; gw <= CURRENT_GW; gw++) {
        const s = statsFor(playerId, gw);
        n += sort === 'defensive_contribution' ? defensiveActionsOf(pos, s) : cbiOf(s);
      }
      return n;
    }
  }
  const key = SORT_STAT[sort];
  // Like the server: a tracked stat reads 0 for a player with lines but no
  // minutes; null only before any gameweek has lines.
  if (!key || (NEEDS_MINUTES.has(sort) && CURRENT_GW < 1)) return null;
  let sum = 0;
  for (let gw = 1; gw <= CURRENT_GW; gw++) sum += statsFor(playerId, gw)[key] ?? 0;
  return round(sum, 2);
}
