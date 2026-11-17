import { leagues, LEAGUE_ID, PLAYERS, CURRENT_GW, pointsFor } from './data';
import { gameweekFixtures, seasonTotals } from './world';
import type { InSeasonLeague } from './league';
import { awardStatsFor, gameweekAwards, leagueStatus, playersOfTheWeek } from './status';
import { compareForAward, formationOf, TOTW_LIMITS } from '@/lib/awards';
import { gameweekEnd } from '@/lib/status';

const sunday = () => leagues.get(LEAGUE_ID) as InSeasonLeague;
const candidate = (gw: number) => (p: (typeof PLAYERS)[number]) => ({
  id: p.id,
  position: p.position,
  stats: awardStatsFor(p, gw),
});

describe('mock gameweek awards', () => {
  it.each([1, 4, 6, CURRENT_GW])('gameweek %i: a legal XI and the best player overall', (gw) => {
    const a = gameweekAwards(gw)!;
    expect(a.teamOfTheWeek).toHaveLength(11);
    const team = a.teamOfTheWeek.map((t) => ({
      id: t.player.id,
      position: t.player.position,
      stats: t.stats,
    }));
    for (const [pos, { min, max }] of Object.entries(TOTW_LIMITS)) {
      const n = team.filter((t) => t.position === pos).length;
      expect(n).toBeGreaterThanOrEqual(min);
      expect(n).toBeLessThanOrEqual(max);
    }
    expect(formationOf(team)).toMatch(/^[345]-[2-5]-[1-3]$/);
    // Nobody in the whole pool outranks the Player of the Week.
    const pool = PLAYERS.map(candidate(gw));
    const potw = pool.find((c) => c.id === a.playerOfTheWeek!.player.id)!;
    expect(pool.every((c) => c === potw || compareForAward(potw, c) < 0)).toBe(true);
  });

  it('has nothing for gameweeks that have not started', () => {
    expect(gameweekAwards(CURRENT_GW + 1)).toBeUndefined();
    expect(gameweekAwards(0)).toBeUndefined();
  });

  it('lists one Player of the Week per started gameweek, oldest first', () => {
    const list = playersOfTheWeek();
    expect(list.map((w) => w.gameweek)).toEqual(
      Array.from({ length: CURRENT_GW }, (_, i) => i + 1),
    );
    expect(list.at(-1)!.state).toBe('live');
    expect(list[0]!.state).toBe('final');
  });
});

describe('mock league status', () => {
  const s = leagueStatus(sunday());

  it('lists the latest injuries: news since the last gameweek ended, owned first', () => {
    const end = gameweekEnd(gameweekFixtures(CURRENT_GW - 1).map((f) => f.kickoffAt))!;
    const flagged = PLAYERS.filter((p) => p.availability !== 'available');
    const recent = flagged.filter((p) => Date.parse(p.newsAt as string) > end).map((p) => p.id);
    // A stale flag from before it is left out.
    expect(recent.length).toBeLessThan(flagged.length);
    expect(s.injuries.map((i) => i.player.id).sort()).toEqual([...recent].sort());
    expect(s.injuries.length).toBeGreaterThanOrEqual(5);
    expect(s.injuries.length).toBeLessThanOrEqual(20);
    // Owned players, then free ones, each by season minutes.
    const minutes = s.injuries.map((i) => seasonTotals(i.player.id).minutes);
    const split = s.injuries.findIndex((i) => i.owner === null);
    expect(split).toBeGreaterThan(0);
    expect(s.injuries.slice(split).every((i) => i.owner === null)).toBe(true);
    for (const part of [minutes.slice(0, split), minutes.slice(split)]) {
      expect(part).toEqual([...part].sort((a, b) => b - a));
    }
    expect(s.injuries.some((i) => i.player.availability === 'suspended')).toBe(true);
    for (const i of s.injuries.filter((x) => x.player.availability === 'doubtful')) {
      expect([25, 50, 75]).toContain(i.chanceOfPlaying);
    }
  });

  it('gives the ten best available per position, best first', () => {
    expect(s.topAvailable.every((p) => p.rosterStatus !== 'rostered')).toBe(true);
    for (const pos of ['GKP', 'DEF', 'MID', 'FWD']) {
      expect(s.topAvailable.filter((p) => p.player.position === pos)).toHaveLength(10);
    }
    const forms = s.topAvailable.map((p) => p.form!);
    expect(forms).toEqual([...forms].sort((a, b) => b - a));
    // Form is (GW5 + GW6) / 2 while gameweek 7 is live, a gameweek
    // without minutes counting as 0.
    for (const p of s.topAvailable) {
      const sum = [5, 6].reduce((a, gw) => a + pointsFor(p.player.id, gw), 0);
      expect(p.form).toBe(Math.round((sum / 2) * 10) / 10);
    }
    expect(s.topAvailable[0]!.upcoming.map((u) => u.gameweek)).toEqual([
      CURRENT_GW + 1,
      CURRENT_GW + 2,
      CURRENT_GW + 3,
    ]);
  });

  it('flags the demo team’s blank-gameweek starters and the waiting trade offer', () => {
    const kinds = s.attention.map((a) => a.kind);
    expect(kinds).toContain('starters_without_fixture');
    expect(kinds).toContain('trade_offers');
    expect(s.attention.find((a) => a.kind === 'starters_without_fixture')!.severity).toBe(
      'critical',
    );
  });
});
