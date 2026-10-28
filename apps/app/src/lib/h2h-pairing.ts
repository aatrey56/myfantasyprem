import type { Schemas } from '@myfantasyprem/api-client';

import { POSITION_ORDER } from '@/lib/matchup';

/** Any lineup slot: a scored matchup's or a preview's. */
export type PairableSlot = Pick<Schemas['LineupSlot'], 'slot'> & {
  player: Pick<Schemas['PlayerRef'], 'position'>;
};

/** One head-to-head starter row; a side is only null when its XI is short (data gap). */
export interface PairedRow<S extends PairableSlot = Schemas['LineupSlot']> {
  left: S | null;
  right: S | null;
}

const rank = (p: string) => {
  const i = (POSITION_ORDER as readonly string[]).indexOf(p);
  return i < 0 ? POSITION_ORDER.length : i;
};

/**
 * Pairs two XIs "in order": the goalkeepers share the first row, then each
 * side's outfield starters (DEF→MID→FWD, slot order within a position) pair
 * row by row, whatever the formations. Two full XIs give 11 rows with no
 * empty side; a side with fewer players leaves its cells empty at the end.
 */
export function pairInOrder<S extends PairableSlot>(
  left: readonly S[] | null,
  right: readonly S[] | null,
): PairedRow<S>[] {
  const split = (slots: readonly S[] | null) => {
    const sorted = [...(slots ?? [])].sort(
      (a, b) => rank(a.player.position) - rank(b.player.position) || a.slot - b.slot,
    );
    return {
      gk: sorted.filter((s) => s.player.position === 'GKP'),
      outfield: sorted.filter((s) => s.player.position !== 'GKP'),
    };
  };
  const l = split(left);
  const r = split(right);
  const rows: PairedRow<S>[] = [];
  for (const [a, b] of [
    [l.gk, r.gk],
    [l.outfield, r.outfield],
  ] as const) {
    for (let i = 0; i < Math.max(a.length, b.length); i++) {
      rows.push({ left: a[i] ?? null, right: b[i] ?? null });
    }
  }
  return rows;
}
