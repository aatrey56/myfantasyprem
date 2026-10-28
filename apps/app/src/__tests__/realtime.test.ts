import { draftPollInterval, DRAFT_IDLE_POLL_MS, DRAFT_LIVE_POLL_MS } from '@/api/draft-hooks';
import { QueryClient } from '@tanstack/react-query';

import { queryKeys } from '@/api/hooks';
import { newsKey } from '@/api/news-hooks';
import { backoffMs, invalidationsFor, REALTIME_ENABLED } from '@/api/realtime';

const LG = 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy';

describe('realtime', () => {
  it('is off under Jest (no socket server)', () => {
    expect(REALTIME_ENABLED).toBe(false);
  });

  it('maps each event to the queries it makes stale', () => {
    expect(invalidationsFor(LG, 'draft.pick_made')).toContainEqual(queryKeys.draft(LG));
    expect(invalidationsFor(LG, 'matchups.updated')).toContainEqual(queryKeys.league(LG));
    expect(invalidationsFor(LG, 'score.corrected')).toContainEqual(queryKeys.standings(LG));
    // Live points moved: the SCORES fixture lists and box scores too.
    expect(invalidationsFor(LG, 'matchups.updated')).toContainEqual(['gameweek']);
    expect(invalidationsFor(LG, 'score.corrected')).toContainEqual(['gameweek']);
    expect(invalidationsFor(LG, 'trade.updated')).toContainEqual(queryKeys.trades(LG));
    expect(invalidationsFor(LG, 'trade.updated')).toContainEqual(['league', LG, 'transactions']);
    expect(invalidationsFor(LG, 'waivers.processed')).toContainEqual(queryKeys.waivers(LG));
    expect(invalidationsFor(LG, 'league.updated')).toContainEqual(queryKeys.members(LG));
    // The trade block: its own event, and a processed trade or waiver run clears players off it.
    const block = ['league', LG, 'trade-block'];
    expect(invalidationsFor(LG, 'trade_block.updated')).toEqual([block]);
    expect(invalidationsFor(LG, 'trade.updated')).toContainEqual(block);
    expect(invalidationsFor(LG, 'waivers.processed')).toContainEqual(block);
    expect(invalidationsFor(LG, 'lineup.locked')).toContainEqual(['league', LG, 'lineup']);
    // Open enum: an unknown event touches nothing.
    expect(invalidationsFor(LG, 'season.ended')).toEqual([]);
  });

  it.each(['trade.updated', 'waivers.processed'])(
    "%s makes every team's news and the trade block stale",
    async (type) => {
      // A processed trade or waiver run moves players between teams: their
      // news and block listings move with them, as after a local roster move.
      const qc = new QueryClient();
      const mine = newsKey(LG, 'tm_mine');
      const theirs = newsKey(LG, 'tm_theirs');
      const block = ['league', LG, 'trade-block'];
      const otherLeague = newsKey('lg_other', 'tm_mine');
      for (const key of [mine, theirs, block, otherLeague]) qc.setQueryData(key, { data: [] });
      await Promise.all(
        invalidationsFor(LG, type).map((queryKey) => qc.invalidateQueries({ queryKey })),
      );
      expect(qc.getQueryState(mine)?.isInvalidated).toBe(true);
      expect(qc.getQueryState(theirs)?.isInvalidated).toBe(true);
      expect(qc.getQueryState(block)?.isInvalidated).toBe(true);
      expect(qc.getQueryState(otherLeague)?.isInvalidated).toBe(false);
      qc.clear();
    },
  );

  it('backs off from 1 s to 30 s with jitter', () => {
    expect(backoffMs(0, () => 0)).toBe(500);
    expect(backoffMs(0, () => 1)).toBe(1000);
    expect(backoffMs(3, () => 1)).toBe(8000);
    expect(backoffMs(10, () => 1)).toBe(30000);
    expect(backoffMs(10, () => 0)).toBe(15000);
  });

  it('polls the draft less while the socket is up', () => {
    expect(draftPollInterval('in_progress')).toBe(DRAFT_LIVE_POLL_MS);
    expect(draftPollInterval('in_progress', true)).toBe(DRAFT_IDLE_POLL_MS);
    expect(draftPollInterval('complete', true)).toBe(false);
  });
});
