/**
 * Mock data for the Status tab: gameweek awards (from every player in the
 * mock world, through the real rules in lib/awards) and a league's status
 * page. Invented players and clubs only.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import type { InSeasonLeague } from './league';
import {
  CURRENT_GW,
  fixturesFor,
  fixturesInGameweek,
  PLAYERS,
  toPlayerRef,
  type PlayerRecord,
} from './world';
import { playerOfTheWeek, teamOfTheWeek, type AwardCandidate } from '@/lib/awards';
import { compareForm } from '@/lib/form';

type Position = Schemas['Position'];
type AwardStats = Schemas['AwardStats'];

const BPS_GOAL: Record<Position, number> = { GKP: 12, DEF: 12, MID: 18, FWD: 24 };

/**
 * A player's award numbers for one gameweek, summed over a double. The mock
 * world does not simulate BPS, so it is approximated from the same events
 * (FPL's weights, plus a little for general play from quality).
 */
export function awardStatsFor(p: PlayerRecord, gw: number): AwardStats {
  const s: AwardStats = {
    points: 0,
    bonus: 0,
    bps: 0,
    goals: 0,
    assists: 0,
    defensiveContribution: 0,
    cleanSheets: 0,
    minutes: 0,
  };
  for (const f of fixturesFor(p.id, gw)) {
    s.points += f.points;
    s.minutes += f.minutes;
    if (f.minutes > 0) s.bps += (f.minutes >= 60 ? 6 : 3) + Math.floor(p.quality * 8);
    for (const l of f.breakdown) {
      switch (l.stat) {
        case 'bonus':
          s.bonus += l.value;
          break;
        case 'goals_scored':
          s.goals += l.value;
          s.bps += l.value * BPS_GOAL[p.position];
          break;
        case 'assists':
          s.assists += l.value;
          s.bps += l.value * 9;
          break;
        case 'clean_sheets':
          s.cleanSheets += l.value;
          if (p.position === 'GKP' || p.position === 'DEF') s.bps += 12;
          break;
        case 'defensive_contribution':
          s.defensiveContribution += l.value;
          s.bps += 2;
          break;
        case 'saves':
          s.bps += l.value * 2;
          break;
        case 'goals_conceded':
          s.bps += l.points * 4;
          break;
        case 'yellow_cards':
          s.bps -= 3;
          break;
        case 'red_cards':
          s.bps -= 9;
          break;
        default:
          break;
      }
    }
  }
  return s;
}

const scoreState = (gw: number): Schemas['ScoreState'] =>
  gw < CURRENT_GW ? 'final' : gw === CURRENT_GW ? 'live' : 'upcoming';

const hasStarted = (gw: number) => Number.isInteger(gw) && gw >= 1 && gw <= CURRENT_GW;

type Candidate = AwardCandidate & { award: Schemas['AwardPlayer'] };

const awardsCache = new Map<number, Schemas['GameweekAwards']>();

/** Player and Team of the Week for a started gameweek; undefined otherwise. */
export function gameweekAwards(gw: number): Schemas['GameweekAwards'] | undefined {
  if (!hasStarted(gw)) return undefined;
  const cached = awardsCache.get(gw);
  if (cached) return cached;
  const pool: Candidate[] = PLAYERS.map((p) => {
    const stats = awardStatsFor(p, gw);
    return { id: p.id, position: p.position, stats, award: { player: toPlayerRef(p), stats } };
  });
  const awards: Schemas['GameweekAwards'] = {
    gameweek: gw,
    state: scoreState(gw),
    playerOfTheWeek: playerOfTheWeek(pool)?.award ?? null,
    teamOfTheWeek: teamOfTheWeek(pool).map((c) => c.award),
  };
  awardsCache.set(gw, awards);
  return awards;
}

/** Player of the Week for every started gameweek, oldest first. */
export function playersOfTheWeek(): Schemas['PlayerOfTheWeek'][] {
  return Array.from({ length: CURRENT_GW }, (_, i) => gameweekAwards(i + 1)!)
    .filter((a) => a.playerOfTheWeek)
    .map((a) => ({ gameweek: a.gameweek, state: a.state, award: a.playerOfTheWeek! }));
}

// ------------------------------------------------------------ status page

const UPCOMING_WEEKS = 3;
const TOP_PER_POSITION = 10;

/** Chance of playing from the news line ("50% chance of playing"); 0 when out. */
export function chanceOfPlaying(p: Schemas['PlayerRef']): number | null {
  if (p.availability === 'doubtful') {
    const m = p.news?.match(/(\d{1,3})%/);
    return m ? Number(m[1]) : null;
  }
  if (p.availability === 'available') return null;
  return 0;
}

/** Rank of every team from its results up to and including `lastGw`. */
function ranksAfter(league: InSeasonLeague, lastGw: number): Map<string, number> {
  const rows = league.TEAMS.map((t) => {
    const results = league.teamDetail(t.id)?.results.filter((r) => r.gameweek <= lastGw) ?? [];
    const lp = results.reduce((s, r) => s + (r.result === 'W' ? 3 : r.result === 'D' ? 1 : 0), 0);
    const pf = results.reduce((s, r) => s + r.pointsFor, 0);
    return { id: t.id, lp, pf };
  }).sort((a, b) => b.lp - a.lp || b.pf - a.pf);
  return new Map(rows.map((r, i) => [r.id, i + 1]));
}

function attention(league: InSeasonLeague): Schemas['StatusAttentionItem'][] {
  const me = league.myTeam.id;
  const nextGw = CURRENT_GW + 1;
  const starters = (league.teamDetail(me)?.roster ?? []).filter((e) => e.slot <= 11);
  const items: Schemas['StatusAttentionItem'][] = [];
  const blank = starters.filter((e) => fixturesInGameweek(e.player.id, nextGw).length === 0).length;
  if (blank > 0) {
    items.push({
      kind: 'starters_without_fixture',
      severity: 'critical',
      message: `${blank} starter${blank === 1 ? ' has' : 's have'} no game in GW ${nextGw}`,
      count: blank,
      gameweek: nextGw,
    });
  }
  const flagged = starters.filter((e) => e.player.availability !== 'available').length;
  if (flagged > 0) {
    items.push({
      kind: 'injured_starters',
      severity: 'warning',
      message: `${flagged} starter${flagged === 1 ? ' is' : 's are'} injured or doubtful`,
      count: flagged,
      gameweek: nextGw,
    });
  }
  const offers = league
    .listTrades('offered')
    .filter((t) => t.receivingTeam.id === me && t.viewerActions.includes('accept')).length;
  if (offers > 0) {
    items.push({
      kind: 'trade_offers',
      severity: 'warning',
      message: `${offers} trade offer${offers === 1 ? '' : 's'} waiting for you`,
      count: offers,
      gameweek: null,
    });
  }
  const claims = league.listClaims('pending').length;
  if (claims > 0) {
    items.push({
      kind: 'pending_waivers',
      severity: 'info',
      message: `${claims} waiver claim${claims === 1 ? '' : 's'} pending`,
      count: claims,
      gameweek: nextGw,
    });
  }
  return items;
}

function topAvailable(league: InSeasonLeague): Schemas['StatusAvailablePlayer'][] {
  const rows = PLAYERS.filter((p) => league.rosterStatus(p.id) !== 'rostered')
    .map((p) => league.playerListItem(p))
    .sort(
      (a, b) =>
        compareForm(a.form, b.form) ||
        b.totalPoints - a.totalPoints ||
        a.player.id.localeCompare(b.player.id),
    );
  const perPosition = new Map<string, number>();
  return rows
    .filter((r) => {
      const n = perPosition.get(r.player.position) ?? 0;
      perPosition.set(r.player.position, n + 1);
      return n < TOP_PER_POSITION;
    })
    .map((r) => ({
      player: r.player,
      rosterStatus: r.rosterStatus,
      waiverClearsAt: r.waiverClearsAt,
      form: r.form,
      upcoming: Array.from({ length: UPCOMING_WEEKS }, (_, i) => CURRENT_GW + 1 + i).map((gw) => ({
        gameweek: gw,
        fixtures: fixturesInGameweek(r.player.id, gw),
      })),
    }));
}

function injuries(league: InSeasonLeague): Schemas['StatusFlaggedPlayer'][] {
  return PLAYERS.filter((p) => p.availability !== 'available')
    .map((p) => {
      const player = toPlayerRef(p);
      return { player, owner: league.ownerOf(p.id), chanceOfPlaying: chanceOfPlaying(player) };
    })
    .sort(
      (a, b) =>
        Number(a.player.availability === 'doubtful') -
          Number(b.player.availability === 'doubtful') ||
        a.player.displayName.localeCompare(b.player.displayName),
    );
}

/** The Status page for the demo user in an in-season league. */
export function leagueStatus(league: InSeasonLeague): Schemas['LeagueStatusPage'] {
  const last = CURRENT_GW - 1;
  const me = league.myTeam.id;
  const now = ranksAfter(league, last).get(me) ?? 1;
  const before = last > 1 ? (ranksAfter(league, last - 1).get(me) ?? now) : now;
  return {
    gameweek: league.leagueGameweek,
    transactionPhase: league.transactionPhase(),
    rankChange: before - now,
    attention: attention(league),
    topAvailable: topAvailable(league),
    injuries: injuries(league),
  };
}
