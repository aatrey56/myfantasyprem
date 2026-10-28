import type { Schemas } from '@myfantasyprem/api-client';

import { formatDateTime } from './time';
import { positionLabel } from '@/components/Badges';

/**
 * Moves one claim up (-1) or down (+1) in the preference order and returns
 * the new id order for `PUT /waivers/claim-order`. Moving past either end
 * leaves the order as it is.
 */
export function moveClaim(ids: readonly string[], id: string, delta: -1 | 1): string[] {
  const from = ids.indexOf(id);
  const to = from + delta;
  if (from < 0 || to < 0 || to >= ids.length) return [...ids];
  const next = [...ids];
  next[from] = ids[to]!;
  next[to] = id;
  return next;
}

/** Pending claims in the order the server will try them. */
export function sortClaims(claims: readonly Schemas['WaiverClaim'][]): Schemas['WaiverClaim'][] {
  return [...claims].sort((a, b) => a.order - b.order || a.createdAt.localeCompare(b.createdAt));
}

/** Applies a new id order to the claims (optimistic reorder), renumbering `order`. */
export function reorderClaims(
  claims: readonly Schemas['WaiverClaim'][],
  ids: readonly string[],
): Schemas['WaiverClaim'][] {
  const byId = new Map(claims.map((c) => [c.id, c]));
  return ids
    .map((id) => byId.get(id))
    .filter((c): c is Schemas['WaiverClaim'] => Boolean(c))
    .map((c, i) => ({ ...c, order: i + 1 }));
}

/**
 * Who can be dropped to add `position`: my squad's players at that
 * position (squads stay 2/5/5/3), weakest season first.
 */
export function dropCandidates(
  roster: readonly Schemas['RosterEntry'][],
  position: string,
): Schemas['RosterEntry'][] {
  return roster
    .filter((r) => r.player.position === position)
    .sort(
      (a, b) =>
        a.totalPoints - b.totalPoints || a.player.displayName.localeCompare(b.player.displayName),
    );
}

/** The player a move brings in, with their status in the league when known. */
export interface MoveTarget {
  player: Pick<Schemas['PlayerRef'], 'id' | 'displayName' | 'position'>;
  rosterStatus?: string;
}

/**
 * FPL Draft's rules for adding a player and dropping one of mine, checked
 * before the move is shown (the server stays the authority): the drop is
 * on my squad, the arrival is not, both play the same position (squads
 * stay 2/5/5/3), a free agent is added straight away and a player on
 * waivers only through a claim, once. The first broken rule, or null.
 */
export function rosterMoveProblem(
  kind: 'add' | 'claim',
  roster: readonly Pick<Schemas['RosterEntry'], 'player'>[],
  add: MoveTarget,
  dropId: string,
  pendingClaims: readonly Pick<Schemas['WaiverClaim'], 'add' | 'drop'>[] = [],
): string | null {
  const name = add.player.displayName;
  const drop = roster.find((r) => r.player.id === dropId);
  if (!drop) return 'The player to drop is not on your team.';
  if (roster.some((r) => r.player.id === add.player.id)) return `${name} is already on your team.`;
  if (drop.player.position !== add.player.position) {
    const pos = positionLabel(add.player.position);
    return `${name} is a ${pos}; drop a ${pos}.`;
  }
  if (add.rosterStatus === 'rostered') return `${name} is on a team.`;
  if (kind === 'add' && add.rosterStatus === 'waivers') {
    return `${name} is on waivers: put in a claim.`;
  }
  if (kind === 'claim' && add.rosterStatus === 'free_agent') {
    return `${name} is a free agent: add him straight away.`;
  }
  if (
    kind === 'claim' &&
    pendingClaims.some((c) => c.add.id === add.player.id && c.drop.id === dropId)
  ) {
    return 'You already have this claim.';
  }
  return null;
}

/** Why a claim failed, in words. Unknown reasons render neutrally. */
export function claimFailureText(reason: string | null | undefined): string {
  switch (reason) {
    case 'player_in_unavailable':
      return 'Another team with higher priority got him first.';
    case 'player_out_unavailable':
      return 'The player to drop had already left your squad.';
    case 'roster_locked':
      return 'A player in the claim was tied up in a trade or locked after a recent move.';
    default:
      return 'The claim did not go through.';
  }
}

/** The waiver order rules, in words (ADR 0008 rule 12). */
export const WAIVER_ORDERS: Record<
  Schemas['WaiverOrderRule'],
  { label: string; short: string; help: string }
> = {
  reverse_standings: {
    label: 'Reverse standings',
    short: 'reverse standings',
    help: 'Each waiver run goes in reverse order of the table: the bottom team picks first. Ties: fewer points scored picks first, then the team that joined first. Before the first result, the reverse of the draft order. A team that wins a claim drops to the back for the rest of that run.',
  },
  rolling: {
    label: 'Rolling',
    short: 'rolling',
    help: 'The order starts as the reverse of the draft order. A team that wins a claim drops to the back and stays there for later runs.',
  },
};

/** The rule's label and explanation; unknown rules render neutrally. */
export function waiverOrderText(order: string | null | undefined): {
  label: string;
  short: string;
  help: string;
} {
  return (
    WAIVER_ORDERS[order as Schemas['WaiverOrderRule']] ?? {
      label: 'Waiver priority',
      short: 'priority',
      help: 'Claims are processed in priority order.',
    }
  );
}

/**
 * When a change of the waiver order rule takes effect: the next waiver
 * run, or before the season starts.
 */
export function waiverOrderEffect(
  state: Pick<Schemas['WaiverState'], 'nextRunGameweek' | 'nextRunAt'> | undefined,
  status: Schemas['LeagueStatus'],
  now = Date.now(),
  timeZone?: string,
): string {
  if (status === 'pre_draft' || status === 'drafting') {
    return 'Applies from the first waiver run of the season.';
  }
  if (!state?.nextRunGameweek || !state.nextRunAt) {
    return 'There are no waiver runs left this season.';
  }
  return `A change applies from the GW ${state.nextRunGameweek} waiver run, ${formatDateTime(
    Date.parse(state.nextRunAt),
    now,
    timeZone,
  )}.`;
}

/** The label shown instead of Waivers/Sign while the gameweek is locked. */
export function waiversDueLabel(until: string | null | undefined, now = Date.now()): string {
  return until ? `Waivers due ${formatDateTime(Date.parse(until), now)}` : 'Locked until waivers';
}

/** The league's transaction window in words (Status banner). Unknown phases say nothing. */
export function transactionPhaseLabel(
  p: Pick<Schemas['TransactionPhase'], 'phase' | 'until'> | undefined,
  now = Date.now(),
): string | null {
  if (!p) return null;
  const at = p.until ? formatDateTime(Date.parse(p.until), now) : null;
  switch (p.phase) {
    case 'locked':
      return waiversDueLabel(p.until, now);
    case 'waivers':
      return at ? `Waivers due ${at}` : 'Waivers';
    case 'free_agency':
      return at ? `Free agency until ${at}` : 'Free agency';
    default:
      return null;
  }
}
