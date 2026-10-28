import type { Schemas } from '@myfantasyprem/api-client';

import { pairInOrder } from './h2h-pairing';

type Slot = Schemas['LineupSlot'];
let n = 0;
function slot(position: string, s: number): Slot {
  n += 1;
  return {
    slot: s,
    player: {
      id: `plr_${String(n).padStart(26, '0')}`,
      displayName: `${position} ${n}`,
      club: { code: 'NOR', name: 'Northgate' },
      position,
      availability: 'available',
    },
    points: 0,
    minutes: 0,
    autoSub: 'none',
    fixtures: [],
  } as unknown as Slot;
}
function xi(formation: string): Slot[] {
  const [d, m, f] = formation.split('-').map(Number) as [number, number, number];
  return ['GKP', ...Array(d).fill('DEF'), ...Array(m).fill('MID'), ...Array(f).fill('FWD')].map(
    (p, i) => slot(p, i + 1),
  );
}
const pos = (s: Slot | null) => s?.player.position;

describe('pairInOrder', () => {
  it('4-4-2 vs 4-5-1: 11 full rows, the mismatch lands on one MID/FWD row', () => {
    const rows = pairInOrder(xi('4-4-2'), xi('4-5-1'));
    expect(rows).toHaveLength(11);
    expect(rows.every((r) => r.left && r.right)).toBe(true);
    expect(rows.map((r) => [pos(r.left), pos(r.right)]).slice(0, 1)).toEqual([['GKP', 'GKP']]);
    expect([pos(rows[9]!.left), pos(rows[9]!.right)]).toEqual(['FWD', 'MID']);
    expect([pos(rows[10]!.left), pos(rows[10]!.right)]).toEqual(['FWD', 'FWD']);
  });

  it('3-5-2 vs 5-4-1 pairs every starter exactly once', () => {
    const a = xi('3-5-2');
    const b = xi('5-4-1');
    const rows = pairInOrder(a, b);
    expect(rows).toHaveLength(11);
    expect(rows.every((r) => r.left && r.right)).toBe(true);
    expect(rows.map((r) => r.left)).toEqual(a);
    expect(rows.map((r) => r.right)).toEqual(b);
    expect([pos(rows[4]!.left), pos(rows[4]!.right)]).toEqual(['MID', 'DEF']);
  });

  it('identical formations pair like with like', () => {
    const rows = pairInOrder(xi('4-3-3'), xi('4-3-3'));
    expect(rows.every((r) => pos(r.left) === pos(r.right))).toBe(true);
  });

  it('ignores the order slots arrive in', () => {
    const rows = pairInOrder(xi('4-4-2').reverse(), xi('4-4-2'));
    expect(rows.map((r) => r.left!.slot)).toEqual([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11]);
  });

  it('a short side leaves empty cells at the end, never in the middle', () => {
    const short = xi('4-4-2').slice(0, 9);
    const rows = pairInOrder(short, xi('4-5-1'));
    expect(rows).toHaveLength(11);
    expect(rows.slice(0, 9).every((r) => r.left && r.right)).toBe(true);
    expect(rows.slice(9).every((r) => r.left === null && r.right)).toBe(true);
    expect(pairInOrder(null, xi('4-4-2'))).toHaveLength(11);
  });

  it('a missing goalkeeper does not shift the outfield', () => {
    const noGk = xi('4-4-2').slice(1);
    const rows = pairInOrder(noGk, xi('4-4-2'));
    expect(rows[0]).toMatchObject({ left: null });
    expect(rows[1]!.left).toBe(noGk[0]);
  });
});
