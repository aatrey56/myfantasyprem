/**
 * The mock's season sort values follow the server's rules
 * (server/internal/httpapi/players_sort.go sumLines, stats.PlayerGameweek):
 *
 * - clearances_blocks_interceptions is CBI, without tackles;
 * - defensive_contribution is the raw action count, CBIT for a defender
 *   and CBIRT (with recoveries) otherwise, never the points it earned;
 * - a player with lines but no minutes reads 0, not null.
 */
import { cbiOf, defensiveActionsOf, PLAYERS, sortValueOf, statsFor, CURRENT_GW } from './world';

describe('mock sort values follow the server rules', () => {
  // Hand-worked line: cbit 9 of which tackles 3, recoveries 4.
  const line = { cbit: 9, tackles: 3, recoveries: 4 };

  it('counts CBI without tackles: 9 - 3 = 6', () => {
    expect(cbiOf(line)).toBe(6);
  });

  it('counts defensive actions: DEF CBIT 9; MID/FWD/GKP CBIRT 9 + 4 = 13', () => {
    expect(defensiveActionsOf('DEF', line)).toBe(9);
    expect(defensiveActionsOf('MID', line)).toBe(13);
    expect(defensiveActionsOf('FWD', line)).toBe(13);
    expect(defensiveActionsOf('GKP', line)).toBe(13);
  });

  it('sums the season from those rules', () => {
    const p = PLAYERS.find((x) => x.position === 'MID' && (sortValueOf(x.id, 'minutes') ?? 0) > 0)!;
    let cbi = 0;
    let dc = 0;
    for (let gw = 1; gw <= CURRENT_GW; gw++) {
      const s = statsFor(p.id, gw);
      cbi += (s.cbit ?? 0) - (s.tackles ?? 0);
      dc += (s.cbit ?? 0) + (s.recoveries ?? 0);
    }
    expect(sortValueOf(p.id, 'clearances_blocks_interceptions')).toBe(cbi);
    expect(sortValueOf(p.id, 'defensive_contribution')).toBe(dc);
  });

  it('keeps tracked zeros at 0 for a player who has not played', () => {
    const idle = PLAYERS.find((x) => sortValueOf(x.id, 'minutes') === 0)!;
    expect(idle).toBeDefined();
    for (const sort of ['bps', 'tackles', 'defensive_contribution', 'expected_goals'] as const) {
      expect(sortValueOf(idle.id, sort)).toBe(0);
    }
  });
});
