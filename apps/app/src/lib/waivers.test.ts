import type { Schemas } from '@myfantasyprem/api-client';

import { formatDateTime } from './time';
import {
  claimFailureText,
  dropCandidates,
  waiversDueLabel,
  moveClaim,
  transactionPhaseLabel,
  reorderClaims,
  sortClaims,
  waiverOrderEffect,
  waiverOrderText,
} from './waivers';

const claim = (id: string, order: number, createdAt = '2026-10-10T00:00:00Z') =>
  ({ id, order, createdAt }) as Schemas['WaiverClaim'];

describe('moveClaim', () => {
  it('moves a claim up or down one place', () => {
    expect(moveClaim(['a', 'b', 'c'], 'c', -1)).toEqual(['a', 'c', 'b']);
    expect(moveClaim(['a', 'b', 'c'], 'a', 1)).toEqual(['b', 'a', 'c']);
  });
  it('leaves the order alone at either end or for an unknown id', () => {
    expect(moveClaim(['a', 'b'], 'a', -1)).toEqual(['a', 'b']);
    expect(moveClaim(['a', 'b'], 'b', 1)).toEqual(['a', 'b']);
    expect(moveClaim(['a', 'b'], 'z', 1)).toEqual(['a', 'b']);
  });
  it('never mutates its input', () => {
    const ids = ['a', 'b'];
    moveClaim(ids, 'b', -1);
    expect(ids).toEqual(['a', 'b']);
  });
});

describe('claim order', () => {
  it('sorts by preference, then age', () => {
    const sorted = sortClaims([
      claim('late', 2, '2026-10-11T00:00:00Z'),
      claim('second', 2, '2026-10-10T00:00:00Z'),
      claim('first', 1),
    ]);
    expect(sorted.map((c) => c.id)).toEqual(['first', 'second', 'late']);
  });
  it('applies a new id order and renumbers', () => {
    const next = reorderClaims([claim('a', 1), claim('b', 2), claim('c', 3)], ['c', 'a', 'b']);
    expect(next.map((c) => [c.id, c.order])).toEqual([
      ['c', 1],
      ['a', 2],
      ['b', 3],
    ]);
  });
});

describe('drops', () => {
  const entry = (name: string, position: string, totalPoints: number) =>
    ({
      player: { id: name, displayName: name, position },
      totalPoints,
    }) as Schemas['RosterEntry'];

  it('offers same-position players, weakest first', () => {
    const roster = [entry('A', 'MID', 40), entry('B', 'DEF', 10), entry('C', 'MID', 12)];
    expect(dropCandidates(roster, 'MID').map((r) => r.player.displayName)).toEqual(['C', 'A']);
    expect(dropCandidates(roster, 'GKP')).toEqual([]);
  });

  it('explains failures and tolerates unknown reasons', () => {
    expect(claimFailureText('player_in_unavailable')).toMatch(/higher priority/);
    expect(claimFailureText('something_else')).toBe('The claim did not go through.');
  });
});

describe('waiverOrderText', () => {
  it('explains both rules and renders an unknown one neutrally', () => {
    expect(waiverOrderText('reverse_standings').label).toBe('Reverse standings');
    expect(waiverOrderText('reverse_standings').help).toMatch(/bottom team picks first/);
    expect(waiverOrderText('rolling').help).toMatch(/stays there for later runs/);
    expect(waiverOrderText('fifo').label).toBe('Waiver priority');
  });
});

describe('waiverOrderEffect', () => {
  const now = Date.parse('2026-10-14T12:00:00Z');
  it('names the next waiver run in season', () => {
    expect(
      waiverOrderEffect(
        { nextRunGameweek: 8, nextRunAt: '2026-10-16T10:00:00Z' },
        'in_season',
        now,
        'Europe/London',
      ),
    ).toBe('A change applies from the GW 8 waiver run, Fri 16 Oct, 11:00.');
  });
  it('before the season and after the last run', () => {
    expect(waiverOrderEffect(undefined, 'pre_draft', now)).toMatch(/first waiver run/);
    expect(waiverOrderEffect({ nextRunGameweek: null, nextRunAt: null }, 'in_season', now)).toMatch(
      /no waiver runs left/,
    );
  });
});

describe('transaction phase labels', () => {
  // Thu 15 Oct 2026 12:00 UTC; gameweek 8's waivers run Fri 16 Oct 10:00 UTC.
  const now = Date.parse('2026-10-15T12:00:00Z');
  const run = '2026-10-16T10:00:00Z';
  const at = formatDateTime(Date.parse(run), now);

  it('says when a locked gameweek reopens', () => {
    expect(waiversDueLabel(run, now)).toBe(`Waivers due ${at}`);
    expect(waiversDueLabel(null, now)).toBe('Locked until waivers');
    expect(transactionPhaseLabel({ phase: 'locked', until: run }, now)).toBe(`Waivers due ${at}`);
  });

  it('names the other phases and ignores unknown ones', () => {
    expect(transactionPhaseLabel({ phase: 'waivers', until: run }, now)).toBe(`Waivers due ${at}`);
    expect(transactionPhaseLabel({ phase: 'free_agency', until: null }, now)).toBe('Free agency');
    expect(transactionPhaseLabel({ phase: 'later' as never, until: null }, now)).toBeNull();
    expect(transactionPhaseLabel(undefined, now)).toBeNull();
  });
});
