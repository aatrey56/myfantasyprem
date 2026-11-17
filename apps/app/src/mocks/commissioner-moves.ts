/**
 * The mock commissioner log of one league (docs/adr/0017): the moves the
 * commissioner made, newest first, and the signed-in user's seen-up-to
 * marker. A move made by the viewer is never unseen, as on the server.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import { notFound } from './errors';
import { mockId } from './world';

type Move = Schemas['CommissionerMove'];
type TeamRef = Schemas['TeamRef'];

/** A move as made: the log adds its id and the viewer's unseen flag. */
export interface MoveInput {
  action: string;
  summary: string;
  actor: Schemas['UserRef'];
  teams?: TeamRef[];
  details?: Schemas['CommissionerMoveDetails'];
  createdAt: string;
}

export function createMovesLog(viewerId: string, idSeed: number) {
  // Oldest first, so the index is the order the server's seq gives.
  const moves: Omit<Move, 'unseen'>[] = [];
  let seenThrough = -1;
  let n = 0;

  function add(input: MoveInput) {
    moves.push({
      id: mockId('cmv', idSeed + ++n),
      action: input.action,
      summary: input.summary,
      actor: input.actor,
      teams: input.teams ?? [],
      details: input.details ?? null,
      createdAt: input.createdAt,
    });
  }

  const unseen = (i: number) => i > seenThrough && moves[i]!.actor.id !== viewerId;
  const view = (i: number): Move => ({ ...moves[i]!, unseen: unseen(i) });

  function list(offset: number, limit: number) {
    const newestFirst = moves.map((_, i) => moves.length - 1 - i);
    const slice = newestFirst.slice(offset, offset + limit).map(view);
    return { slice, next: offset + limit < moves.length ? offset + limit : null };
  }

  function summary(): Schemas['CommissionerMovesSummary'] {
    let count = 0;
    let latest: Move | null = null;
    for (let i = moves.length - 1; i >= 0; i--) {
      if (!unseen(i)) continue;
      count++;
      latest ??= view(i);
    }
    return { unseenCount: count, latest };
  }

  function markSeen(throughId?: string) {
    const to =
      throughId === undefined ? moves.length - 1 : moves.findIndex((m) => m.id === throughId);
    if (to < 0 && moves.length > 0) throw notFound('Commissioner move');
    seenThrough = Math.max(seenThrough, to);
    return summary();
  }

  return { add, list, summary, markSeen };
}

export type MovesLog = ReturnType<typeof createMovesLog>;

const REVIEW_WORDS: Record<string, string> = {
  none: 'no review',
  commissioner: 'commissioner review',
  league_vote: 'league vote',
};

/** What a settings change did, as the server words it; null when nothing a manager sees changed. */
export function settingsChange(
  before: Schemas['LeagueSettings'],
  after: Schemas['LeagueSettings'],
): { summary: string; before: Record<string, unknown>; after: Record<string, unknown> } | null {
  const from: Record<string, unknown> = {};
  const to: Record<string, unknown> = {};
  const parts: string[] = [];
  const note = (key: string, text: string, b: unknown, a: unknown) => {
    from[key] = b;
    to[key] = a;
    parts.push(text);
  };
  const b = before.trades;
  const a = after.trades;
  if (before.teamCount !== after.teamCount)
    note('teamCount', `team count to ${after.teamCount}`, before.teamCount, after.teamCount);
  if (b.review !== a.review)
    note(
      'tradeReview',
      `trade review to ${REVIEW_WORDS[a.review] ?? a.review}`,
      b.review,
      a.review,
    );
  if (b.reviewHours !== a.reviewHours)
    note(
      'reviewHours',
      `trade review period to ${a.reviewHours} hours`,
      b.reviewHours,
      a.reviewHours,
    );
  if (b.vetoThreshold !== a.vetoThreshold)
    note(
      'vetoThreshold',
      `veto threshold to ${Math.round(a.vetoThreshold * 100)}% of managers`,
      b.vetoThreshold,
      a.vetoThreshold,
    );
  if (before.waivers.order !== after.waivers.order)
    note(
      'waiverOrder',
      `waiver order to ${after.waivers.order.replaceAll('_', ' ')}`,
      before.waivers.order,
      after.waivers.order,
    );
  if (parts.length === 0) return null;
  return { summary: `Changed the ${parts.join(', ')}.`, before: from, after: to };
}
