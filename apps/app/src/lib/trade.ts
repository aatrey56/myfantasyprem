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
 * The word for a trade vote. The wire value `allow` (an older client's
 * "uphold") reads "Uphold" and `withdraw` reads "Withdraw veto"; unknown
 * values are shown as sent.
 */
export function voteLabel(vote: string): string {
  if (vote === 'veto') return 'Veto';
  if (vote === 'allow') return 'Uphold';
  if (vote === 'withdraw') return 'Withdraw veto';
  return vote;
}

/**
 * What the league vote on a trade in review lets me do, by the clock:
 * `veto` when I may veto, `withdraw` when I have and the review is still
 * open, null once the window has closed (the server settles the trade) or
 * when the trade does not offer me a vote.
 */
export function vetoControl(
  trade: Pick<Trade, 'status' | 'viewerActions' | 'review'>,
  now: number,
): 'veto' | 'withdraw' | null {
  const review = trade.review;
  if (trade.status !== 'in_review' || !review || !trade.viewerActions.includes('vote')) return null;
  if (review.endsAt && Date.parse(review.endsAt) <= now) return null;
  return review.myVote === 'veto' ? 'withdraw' : 'veto';
}

/** "1 of 4 veto votes": how close a trade in a league vote is to being vetoed. */
export function vetoTally(
  review: Pick<NonNullable<Trade['review']>, 'vetoVotes' | 'votesNeeded'>,
): string {
  const needed = review.votesNeeded ?? '?';
  return `${review.vetoVotes} of ${needed} veto ${review.votesNeeded === 1 ? 'vote' : 'votes'}`;
}

/** The veto thresholds a commissioner picks from: the share of the managers outside a trade that blocks it. */
export const VETO_THRESHOLDS = [
  { value: 0.25, label: 'A quarter' },
  { value: 1 / 3, label: 'A third' },
  { value: 0.5, label: 'Half' },
  { value: 2 / 3, label: 'Two thirds' },
] as const;

/**
 * The preset a threshold the server sent is (a float32 on the wire, so
 * within a millionth), or null for a custom one set some other way.
 */
export function presetVetoThreshold(threshold: number): number | null {
  return VETO_THRESHOLDS.find((t) => Math.abs(t.value - threshold) < 1e-6)?.value ?? null;
}

/** A threshold as a share: "40%", "37.5%". */
export function vetoThresholdPercent(threshold: number): string {
  return `${Math.round(threshold * 1000) / 10}%`;
}

/**
 * Veto votes that block a trade among `managers` managers (teams with one,
 * the two in the trade included): the threshold of the managers outside the
 * trade, rounded up, at least one. Mirrors the server's VotesNeeded
 * (docs/adr/0017), whose product is rounded to 6 places first.
 */
export function vetoesNeeded(threshold: number, managers: number): number {
  const outside = Math.max(0, managers - 2);
  return Math.max(1, Math.ceil(Math.round(threshold * outside * 1e6) / 1e6));
}

/**
 * What the threshold means for the settings screen. With `managers`, the
 * league's managers now (what the server counts): "With 9 managers, 3 of the
 * 7 outside a trade must veto it to block it." Without (a league not
 * created yet), for a full league of `teams`.
 */
export function vetoThresholdText(threshold: number, teams: number, managers?: number): string {
  const n = managers ?? teams;
  const outside = Math.max(0, n - 2);
  const needed = vetoesNeeded(threshold, n);
  return managers === undefined
    ? `In a full league of ${teams}, ${needed} of the ${outside} managers outside a trade must veto it to block it.`
    : `With ${managers} ${managers === 1 ? 'manager' : 'managers'}, ${needed} of the ${outside} outside a trade must veto it to block it.`;
}
