import { ApiError, type Schemas } from '@myfantasyprem/api-client';

import { moveErrorText, problemSlug } from './errors';

type Player = Schemas['PlayerRef'];

/** A trade offer as the builder holds it: the other team, both sides and the message. */
export interface TradeDraft {
  teamId: string;
  /** The other team's name, for the reasons ("no longer on Kev's Kings"). */
  teamName?: string;
  give: Player[];
  get: Player[];
  message: string;
}

/** A player taken out of a refused offer, and why. */
export interface RemovedPlayer {
  player: Player;
  reason: string;
}

/** A refused offer to edit: what is still valid, what was taken out, and the server's words. */
export interface RefusedDraft extends TradeDraft {
  removed: RemovedPlayer[];
  refusal: string;
}

/**
 * The last offer sent per league, kept until the server takes it, so a
 * refusal can reopen the builder with it. In memory: a reload drops it.
 */
const drafts = new Map<string, TradeDraft | RefusedDraft>();

export const saveTradeDraft = (leagueId: string, draft: TradeDraft | RefusedDraft) =>
  void drafts.set(leagueId, draft);
export const tradeDraft = (leagueId: string) => drafts.get(leagueId);
export const clearTradeDraft = (leagueId: string) => void drafts.delete(leagueId);

/** The ids a 422 names with a rule each, from its `errors`. */
function playerErrors(error: unknown): { playerId: string; code: string }[] {
  if (!(error instanceof ApiError)) return [];
  const raw: unknown = error.problem?.errors;
  if (!Array.isArray(raw)) return [];
  return raw.flatMap((e: Partial<Schemas['ProblemError']> | null) =>
    e && typeof e.playerId === 'string' && typeof e.code === 'string'
      ? [{ playerId: e.playerId, code: e.code }]
      : [],
  );
}

/**
 * What is left of `draft` after the server refused it with `error`: the
 * players the problem rules out are taken out, each with the reason its
 * slug gives. `trade-shape` names them (`player_not_on_roster`); the
 * `player-in-trade*` conflicts name one player in their detail. Anything
 * else (the deadline, a network failure) keeps every player.
 */
export function reviseDraft(draft: TradeDraft, error: unknown): RefusedDraft {
  const slug = problemSlug(error);
  const reasons = new Map<string, string>();
  const side = (id: string) => (draft.give.some((p) => p.id === id) ? 'give' : 'get');
  if (slug === 'trade-shape') {
    for (const { playerId, code } of playerErrors(error)) {
      if (code !== 'player_not_on_roster') continue;
      reasons.set(
        playerId,
        side(playerId) === 'give'
          ? 'is no longer on your team'
          : `is no longer on ${draft.teamName ?? 'their team'}`,
      );
    }
  } else if (slug === 'player-in-trade' || slug === 'player-in-trade-review') {
    const detail = error instanceof ApiError ? (error.problem?.detail ?? '') : '';
    for (const p of [...draft.give, ...draft.get]) {
      if (!detail.startsWith(`${p.displayName} `)) continue;
      reasons.set(
        p.id,
        slug === 'player-in-trade'
          ? 'is in a trade that takes effect next gameweek'
          : 'is in a trade waiting to be processed',
      );
    }
  }
  const removed = [...draft.give, ...draft.get].flatMap((player) => {
    const reason = reasons.get(player.id);
    return reason ? [{ player, reason }] : [];
  });
  return {
    ...draft,
    give: draft.give.filter((p) => !reasons.has(p.id)),
    get: draft.get.filter((p) => !reasons.has(p.id)),
    removed,
    refusal: moveErrorText(error),
  };
}

export const isRefused = (d: TradeDraft | RefusedDraft | undefined): d is RefusedDraft =>
  d !== undefined && 'removed' in d;
