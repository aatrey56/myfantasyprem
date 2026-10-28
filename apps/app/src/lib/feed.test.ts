import type { Schemas } from '@myfantasyprem/api-client';

import { mergeLeagueFeeds } from './feed';

const txn = (id: string, at: string) =>
  ({ id, occurredAt: at }) as unknown as Schemas['Transaction'];
const A = { id: 'lg_a', name: 'Alpha' };
const B = { id: 'lg_b', name: 'Bravo' };

describe('mergeLeagueFeeds', () => {
  it('puts every league in one list, newest first', () => {
    const items = mergeLeagueFeeds([
      { league: A, txns: [txn('t1', '2026-10-10T10:00:00Z'), txn('t2', '2026-10-08T10:00:00Z')] },
      { league: B, txns: [txn('t3', '2026-10-09T10:00:00Z')] },
    ]);
    expect(items.map((i) => `${i.league.name}:${i.txn.id}`)).toEqual([
      'Alpha:t1',
      'Bravo:t3',
      'Alpha:t2',
    ]);
  });

  it('keeps each move once, but the same id in two leagues twice', () => {
    const items = mergeLeagueFeeds([
      { league: A, txns: [txn('t1', '2026-10-10T10:00:00Z'), txn('t1', '2026-10-10T10:00:00Z')] },
      { league: B, txns: [txn('t1', '2026-10-10T10:00:00Z')] },
    ]);
    expect(items.map((i) => i.league.id)).toEqual(['lg_a', 'lg_b']);
  });

  it('caps the list', () => {
    const many = Array.from({ length: 10 }, (_, i) =>
      txn(`t${i}`, `2026-10-${String(10 + i).padStart(2, '0')}T10:00:00Z`),
    );
    const items = mergeLeagueFeeds([{ league: A, txns: many }], 3);
    expect(items.map((i) => i.txn.id)).toEqual(['t9', 't8', 't7']);
  });

  it('is empty with no leagues', () => {
    expect(mergeLeagueFeeds([])).toEqual([]);
  });
});
