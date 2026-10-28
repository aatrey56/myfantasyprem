import type { Schemas } from '@myfantasyprem/api-client';

import {
  ACTIVITY_FILTERS,
  describeTransaction,
  flattenFeed,
  groupFeed,
  kindFor,
  matchesFilter,
} from './activity';
import * as data from '@/mocks/data';

const txn = (id: string, kind: string, gameweek: number) =>
  ({ id, kind, gameweek, occurredAt: '2026-10-10T00:00:00Z' }) as Schemas['Transaction'];

describe('activity filters', () => {
  it('maps chips to the kind parameter', () => {
    expect(ACTIVITY_FILTERS.map((f) => f.label)).toEqual([
      'All',
      'Waivers',
      'Free agents',
      'Trades',
    ]);
    expect(kindFor('all')).toBeUndefined();
    expect(kindFor('waiver')).toBe('waiver');
    expect(kindFor('trade')).toBe('trade');
  });

  it('matches entries to a filter', () => {
    expect(matchesFilter({ kind: 'trade' }, 'all')).toBe(true);
    expect(matchesFilter({ kind: 'trade' }, 'trade')).toBe(true);
    expect(matchesFilter({ kind: 'waiver' }, 'free_agent')).toBe(false);
  });

  it('flattens pages once each, dropping entries outside the filter', () => {
    const pages = [
      { data: [txn('a', 'trade', 7), txn('b', 'waiver', 7)] },
      // A new move shifted the offset cursor: `b` repeats.
      { data: [txn('b', 'waiver', 7), txn('c', 'trade', 6)] },
    ];
    expect(flattenFeed(pages, 'all').map((t) => t.id)).toEqual(['a', 'b', 'c']);
    expect(flattenFeed(pages, 'trade').map((t) => t.id)).toEqual(['a', 'c']);
    expect(flattenFeed(undefined, 'all')).toEqual([]);
  });

  it('groups by gameweek, newest first, with the draft last', () => {
    const groups = groupFeed(
      [txn('a', 'trade', 8), txn('b', 'waiver', 8), txn('c', 'trade', 7)],
      '2026-08-01T00:00:00Z',
    );
    expect(groups.map((g) => [g.gameweek, g.entries.map((e) => e.id)])).toEqual([
      [8, ['a', 'b']],
      [7, ['c']],
      [null, ['draft']],
    ]);
    expect(groupFeed([], null)).toEqual([]);
  });

  it('puts missed-clock autopicks in the draft group, after its completion', () => {
    const groups = groupFeed(
      [txn('a', 'trade', 8), txn('x', 'draft_autopick', 8), txn('y', 'draft_autopick', 8)],
      '2026-08-01T00:00:00Z',
    );
    expect(groups.map((g) => [g.gameweek, g.entries.map((e) => e.id)])).toEqual([
      [8, ['a']],
      [null, ['draft', 'x', 'y']],
    ]);
    // While drafting there is no completion yet.
    expect(groupFeed([txn('x', 'draft_autopick', 8)], null)).toEqual([
      { gameweek: null, entries: [expect.objectContaining({ id: 'x', gameweek: null })] },
    ]);
  });

  it('describes every mock transaction in a sentence', () => {
    for (const t of data.TRANSACTIONS) {
      const text = describeTransaction(t);
      expect(text.length).toBeGreaterThan(10);
      if (t.kind === 'trade' && t.status === 'vetoed') expect(text).toMatch(/vetoed/);
      if ((t.kind === 'waiver' || t.kind === 'free_agent') && t.result.outcome === 'denied')
        expect(text).toMatch(/failed/);
    }
  });
});
