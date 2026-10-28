import type { Schemas } from '@myfantasyprem/api-client';

/**
 * Invented previous seasons for the mock players, deterministic per id, in
 * the API's shape: null where FPL did not track the stat that season (ICT
 * before 2016/17, starts and expected stats before 2022/23, defensive
 * contribution before 2024/25). About one player in six has none.
 */
type PastSeason = Schemas['PlayerSeasonTotals'];

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

export function previousSeasonsFor(playerId: string): PastSeason[] {
  const h = hash(playerId);
  if (h % 6 === 0) return [];
  const count = 2 + (h % 11); // 2 .. 12 seasons, ending 2025/26
  const out: PastSeason[] = [];
  for (let i = 0; i < count; i++) {
    const y = 2025 - i;
    const r = hash(`${playerId}:${y}`);
    const minutes = 600 + (r % 2700);
    const xg = ((r >>> 3) % 1200) / 100;
    const xa = ((r >>> 7) % 700) / 100;
    out.push({
      season: `${y}/${String((y + 1) % 100).padStart(2, '0')}`,
      points: 20 + (r % 200),
      minutes,
      goalsScored: (r >>> 5) % 15,
      assists: (r >>> 9) % 10,
      cleanSheets: (r >>> 11) % 14,
      bonus: (r >>> 13) % 20,
      starts: y >= 2022 ? Math.floor(minutes / 88) : null,
      expectedGoals: y >= 2022 ? xg : null,
      expectedAssists: y >= 2022 ? xa : null,
      expectedGoalInvolvements: y >= 2022 ? Number((xg + xa).toFixed(2)) : null,
      ictIndex: y >= 2016 ? ((r >>> 2) % 3000) / 10 : null,
      defensiveContribution: y >= 2024 ? (r >>> 4) % 220 : null,
    });
  }
  return out;
}
