import type { Schemas } from '@myfantasyprem/api-client';

import {
  checkTrade,
  pendingOnFPL,
  positionSummary,
  sidesFor,
  splitTrades,
  togglePlayer,
  tradeBucket,
  tradeStatusLabel,
  voteLabel,
} from './trade';

const p = (id: string, position: string) => ({ id, position: position as Schemas['Position'] });

describe('checkTrade', () => {
  it('accepts equal sides with matching positions', () => {
    expect(checkTrade([p('a', 'MID'), p('b', 'DEF')], [p('c', 'DEF'), p('d', 'MID')])).toEqual({
      ok: true,
      problems: [],
    });
  });

  it('needs a player on each side', () => {
    const r = checkTrade([], []);
    expect(r.ok).toBe(false);
    expect(r.problems).toEqual([
      'Pick at least one of your players to give.',
      'Pick at least one of their players to get.',
    ]);
  });

  it('needs the same number each way', () => {
    expect(checkTrade([p('a', 'MID')], [p('c', 'MID'), p('d', 'MID')]).problems).toEqual([
      'Both sides must move the same number of players: you give 1, you get 2.',
    ]);
  });

  it('needs matching positions, and names them', () => {
    expect(
      checkTrade([p('a', 'GKP'), p('b', 'MID')], [p('c', 'MID'), p('d', 'FWD')]).problems,
    ).toEqual([
      'Positions must match so both squads stay legal: you give 1 GK, 1 MID, you get 1 MID, 1 FWD.',
    ]);
  });

  it('caps each side at five and the message at 280 characters', () => {
    const six = ['a', 'b', 'c', 'd', 'e', 'f'].map((id) => p(id, 'MID'));
    const sixMore = ['g', 'h', 'i', 'j', 'k', 'l'].map((id) => p(id, 'MID'));
    expect(checkTrade(six, sixMore).problems).toContain(
      'A trade moves at most 5 players each way.',
    );
    expect(checkTrade([p('a', 'MID')], [p('b', 'MID')], 'x'.repeat(281)).problems).toEqual([
      'Keep the message to 280 characters.',
    ]);
  });

  it('toggles players in and out, keeping pick order', () => {
    const a = p('a', 'MID');
    const b = p('b', 'DEF');
    expect(togglePlayer(togglePlayer([], a), b)).toEqual([a, b]);
    expect(togglePlayer([a, b], a)).toEqual([b]);
  });

  it('summarises positions GK→FWD', () => {
    expect(positionSummary([p('a', 'FWD'), p('b', 'GKP'), p('c', 'FWD')])).toBe('1 GK, 2 FWD');
    expect(positionSummary([])).toBe('nobody');
  });
});

describe('trade inbox', () => {
  const team = (id: string) => ({ id, name: id, managerName: id });
  const trade = (status: string, from: string, to: string) =>
    ({
      status,
      offeringTeam: team(from),
      receivingTeam: team(to),
      offered: [{ id: 'o' }],
      requested: [{ id: 'r' }],
    }) as unknown as Schemas['Trade'];

  it('buckets offers to me, from me, reviews and history', () => {
    expect(tradeBucket(trade('offered', 'x', 'me'), 'me')).toBe('incoming');
    expect(tradeBucket(trade('offered', 'me', 'x'), 'me')).toBe('outgoing');
    expect(tradeBucket(trade('in_review', 'x', 'y'), 'me')).toBe('review');
    expect(tradeBucket(trade('in_review', 'me', 'y'), 'me')).toBe('review');
    for (const s of ['processed', 'declined', 'withdrawn', 'expired', 'vetoed', 'failed', 'new']) {
      expect(tradeBucket(trade(s, 'me', 'y'), 'me')).toBe('history');
    }
  });

  it('shows what I give and get from either side', () => {
    expect(sidesFor(trade('offered', 'x', 'me'), 'me')).toMatchObject({
      me: { id: 'me' },
      give: [{ id: 'r' }],
      get: [{ id: 'o' }],
    });
    expect(sidesFor(trade('offered', 'me', 'x'), 'me')).toMatchObject({
      give: [{ id: 'o' }],
      get: [{ id: 'r' }],
    });
  });

  it('labels statuses and tolerates unknown ones', () => {
    expect(tradeStatusLabel('vetoed')).toEqual({ label: 'Vetoed', tone: 'loss' });
    expect(tradeStatusLabel('mystery').label).toBe('Unknown');
    // A trade pending on FPL waits for FPL.
    expect(tradeStatusLabel('in_review')).toEqual({ label: 'In review', tone: 'warning' });
    expect(tradeStatusLabel('in_review', true)).toEqual({
      label: 'Pending on FPL',
      tone: 'warning',
    });
    expect(tradeStatusLabel('processed', true).label).toBe('Processed');
  });

  it('reads a trade in review with no review window as pending on FPL', () => {
    const review = {
      mode: 'commissioner',
      endsAt: null,
      vetoVotes: 0,
      allowVotes: 0,
      votesNeeded: null,
      myVote: null,
      decidedBy: null,
    } satisfies Schemas['TradeReviewState'];
    expect(pendingOnFPL({ status: 'in_review', review })).toBe(true);
    expect(
      pendingOnFPL({ status: 'in_review', review: { ...review, endsAt: '2026-10-15T12:00:00Z' } }),
    ).toBe(false);
    expect(pendingOnFPL({ status: 'processed', review })).toBe(false);
    expect(pendingOnFPL({ status: 'in_review', review: null })).toBe(false);
  });
});

describe('voteLabel', () => {
  it('reads the wire value allow as Uphold', () => {
    expect(voteLabel('veto')).toBe('Veto');
    expect(voteLabel('allow')).toBe('Uphold');
    expect(voteLabel('abstain')).toBe('abstain');
  });
});

describe('splitTrades', () => {
  const team = (id: string) => ({ id, name: id, managerName: id });
  const trade = (id: string, status: string, createdAt: string, from: string, to: string) =>
    ({
      id,
      status,
      createdAt,
      offeringTeam: team(from),
      receivingTeam: team(to),
    }) as unknown as Schemas['Trade'];

  it('puts every open trade in one Active list, newest first, and decided ones in Past', () => {
    const trades = [
      trade('t1', 'offered', '2026-10-10T09:00:00Z', 'me', 'b'),
      trade('t2', 'processed', '2026-10-12T09:00:00Z', 'a', 'me'),
      trade('t3', 'offered', '2026-10-14T09:00:00Z', 'a', 'me'),
      trade('t4', 'in_review', '2026-10-12T09:00:00Z', 'a', 'b'),
      trade('t5', 'declined', '2026-10-09T09:00:00Z', 'me', 'a'),
    ];
    const { active, past } = splitTrades(trades, 'me');
    expect(active.map((t) => t.id)).toEqual(['t3', 't4', 't1']);
    expect(past.map((t) => t.id)).toEqual(['t2', 't5']);
  });
});
