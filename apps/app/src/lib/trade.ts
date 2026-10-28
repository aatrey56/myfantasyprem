import type { Schemas } from '@myfantasyprem/api-client';

import { positionLabel } from '@/components/Badges';
import type { PillTone } from '@/components/Chip';

type Player = Pick<Schemas['PlayerRef'], 'id' | 'position'>;
type Trade = Schemas['Trade'];

/** The contract's limits (TradeCreate). */
export const MAX_TRADE_PLAYERS = 5;
export const MAX_TRADE_MESSAGE = 280;

const POSITIONS = ['GKP', 'DEF', 'MID', 'FWD'];

/** `1 DEF, 2 MID` in GK→FWD order; `nobody` for an empty side. */
export function positionSummary(players: readonly Player[]): string {
  if (players.length === 0) return 'nobody';
  const counts = new Map<string, number>();
  for (const p of players) counts.set(p.position, (counts.get(p.position) ?? 0) + 1);
  const rank = (p: string) => (POSITIONS.includes(p) ? POSITIONS.indexOf(p) : POSITIONS.length);
  return [...counts.entries()]
    .sort((a, b) => rank(a[0]) - rank(b[0]) || a[0].localeCompare(b[0]))
    .map(([p, n]) => `${n} ${positionLabel(p)}`)
    .join(', ');
}

const shape = (players: readonly Player[]) =>
  players
    .map((p) => p.position)
    .sort()
    .join(',');

export interface TradeCheck {
  ok: boolean;
  /** Plain sentences, in the order a manager should fix them. */
  problems: string[];
}

/**
 * The trade builder's client-side check of FPL's trade shape (ADR 0008 rule
 * 13): both sides move 1-5 players, the same number, in the same positions,
 * so both squads stay 2/5/5/3. The server's 422 `trade-shape` stays the
 * authority.
 */
export function checkTrade(
  give: readonly Player[],
  get: readonly Player[],
  message = '',
): TradeCheck {
  const problems: string[] = [];
  if (give.length === 0) problems.push('Pick at least one of your players to give.');
  if (get.length === 0) problems.push('Pick at least one of their players to get.');
  if (give.length > MAX_TRADE_PLAYERS || get.length > MAX_TRADE_PLAYERS) {
    problems.push(`A trade moves at most ${MAX_TRADE_PLAYERS} players each way.`);
  }
  if (give.length > 0 && get.length > 0) {
    if (give.length !== get.length) {
      problems.push(
        `Both sides must move the same number of players: you give ${give.length}, you get ${get.length}.`,
      );
    } else if (shape(give) !== shape(get)) {
      problems.push(
        `Positions must match so both squads stay legal: you give ${positionSummary(give)}, you get ${positionSummary(get)}.`,
      );
    }
  }
  if (new Set(give.map((p) => p.id)).size !== give.length) problems.push('A player appears twice.');
  if (message.length > MAX_TRADE_MESSAGE) {
    problems.push(`Keep the message to ${MAX_TRADE_MESSAGE} characters.`);
  }
  return { ok: problems.length === 0, problems };
}

/**
 * FPL Draft's trade deadline: no offers once the current gameweek is past
 * the league's last trading gameweek (null: trades all season).
 */
export function tradeDeadlinePassed(deadlineGameweek: number | null, currentGameweek: number) {
  return deadlineGameweek !== null && currentGameweek > deadlineGameweek;
}

/** Adds or removes a player from one side of the builder, keeping pick order. */
export function togglePlayer<P extends Player>(side: readonly P[], player: P): P[] {
  return side.some((p) => p.id === player.id)
    ? side.filter((p) => p.id !== player.id)
    : [...side, player];
}

export type TradeBucket = 'incoming' | 'outgoing' | 'review' | 'history';

const OPEN = new Set(['offered', 'accepted', 'in_review']);

/**
 * Where a trade sits in my inbox: offers to me, offers from me, trades in
 * review (anyone's, so I can vote or wait), and everything decided.
 */
export function tradeBucket(t: Trade, myTeamId: string | undefined): TradeBucket {
  if (!OPEN.has(t.status)) return 'history';
  if (t.status === 'in_review' || t.status === 'accepted') return 'review';
  if (myTeamId && t.receivingTeam.id === myTeamId) return 'incoming';
  if (myTeamId && t.offeringTeam.id === myTeamId) return 'outgoing';
  return 'review';
}

/**
 * The Trades view's two lists: Active (anything still open: offers to me,
 * from me, and trades in review) newest first, and Past (decided), in the
 * server's order.
 */
export function splitTrades(
  trades: readonly Trade[],
  myTeamId: string | undefined,
): { active: Trade[]; past: Trade[] } {
  const active: Trade[] = [];
  const past: Trade[] = [];
  for (const t of trades) (tradeBucket(t, myTeamId) === 'history' ? past : active).push(t);
  active.sort((a, b) => b.createdAt.localeCompare(a.createdAt) || a.id.localeCompare(b.id));
  return { active, past };
}

/**
 * A trade FPL accepted and has not processed or vetoed yet, imported into a
 * league from FPL (ADR 0012): in review with no review window here. FPL
 * decides it, and the next re-sync applies what it decided.
 */
export function pendingOnFPL(t: Pick<Trade, 'status' | 'review'>): boolean {
  return t.status === 'in_review' && t.review?.endsAt === null;
}

/**
 * Label and tone for a trade status (`onFPL`: the trade is pending on FPL,
 * {@link pendingOnFPL}). Unknown values render neutrally.
 */
export function tradeStatusLabel(status: string, onFPL = false): { label: string; tone: PillTone } {
  if (onFPL && status === 'in_review') return { label: 'Pending on FPL', tone: 'warning' };
  switch (status) {
    case 'offered':
      return { label: 'Offered', tone: 'accent' };
    case 'accepted':
      return { label: 'Accepted', tone: 'win' };
    case 'in_review':
      return { label: 'In review', tone: 'warning' };
    case 'processed':
      return { label: 'Processed', tone: 'win' };
    case 'declined':
      return { label: 'Declined', tone: 'neutral' };
    case 'withdrawn':
      return { label: 'Withdrawn', tone: 'neutral' };
    case 'expired':
      return { label: 'Expired', tone: 'neutral' };
    case 'vetoed':
      return { label: 'Vetoed', tone: 'loss' };
    case 'failed':
      return { label: 'Failed', tone: 'loss' };
    default:
      return { label: 'Unknown', tone: 'neutral' };
  }
}

/**
 * From my side: what I give and what I get. For a trade between two other
 * teams, "give" is the offering team's players.
 */
export function sidesFor(t: Trade, myTeamId: string | undefined) {
  const receiving = myTeamId !== undefined && t.receivingTeam.id === myTeamId;
  return receiving
    ? { me: t.receivingTeam, them: t.offeringTeam, give: t.requested, get: t.offered }
    : { me: t.offeringTeam, them: t.receivingTeam, give: t.offered, get: t.requested };
}

/**
 * The word for a trade vote. The wire value `allow` reads "Uphold" in the
 * app; unknown values are shown as sent.
 */
export function voteLabel(vote: string): string {
  if (vote === 'veto') return 'Veto';
  if (vote === 'allow') return 'Uphold';
  return vote;
}
