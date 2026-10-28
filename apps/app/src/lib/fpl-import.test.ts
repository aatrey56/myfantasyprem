import type { Schemas } from '@myfantasyprem/api-client';

import {
  importStatusText,
  previewFacts,
  resyncWarning,
  seatSubtitle,
  syncErrorText,
  syncItemTitle,
  syncItemValues,
  syncLine,
  syncPendingText,
  shadowLine,
  shadowSummary,
  verificationSummary,
} from './fpl-import';

const NOW = Date.parse('2026-10-14T12:00:00Z');

const verification = (
  over: Partial<Schemas['SyncVerification']> = {},
): Schemas['SyncVerification'] => ({
  status: 'clean',
  checkedAt: '2026-10-14T11:55:00Z',
  counts: { ok: 300, mismatch: 0, unverified: 0, pending: 0 },
  failing: [],
  unexplained: 0,
  pending: [],
  ...over,
});

const sync = (over: Partial<Schemas['LeagueSync']> = {}): Schemas['LeagueSync'] => ({
  fplLeagueId: 12345,
  lastSyncedAt: '2026-10-14T11:55:00Z',
  lastAttemptAt: '2026-10-14T11:55:00Z',
  pending: null,
  lastError: null,
  note: null,
  verification: verification(),
  shadow: null,
  ...over,
});

const item: Schemas['SyncItem'] = {
  key: 'points:gw5:tm_x',
  kind: 'points',
  gameweek: 5,
  team: { id: 'tm_x', name: 'Corner Flag Crew', managerName: 'Hugo B.' },
  status: 'mismatch',
  ours: '61',
  theirs: '63',
  note: null,
  explanation: null,
};

describe('import screen', () => {
  it('describes the preview', () => {
    const p = { teamCount: 12, currentGameweek: 7 } as Schemas['ImportPreview'];
    expect(previewFacts(p)).toBe('12 teams · gameweek 7');
    expect(previewFacts({ ...p, teamCount: 1, currentGameweek: 0 })).toBe(
      '1 team · before gameweek 1',
    );
  });

  it('describes the import job, never treating an unknown state as final', () => {
    const job = (status: string, done = 3, total = 10) =>
      ({ status, progress: { done, total }, error: null }) as unknown as Schemas['Import'];
    expect(importStatusText(job('running'))).toBe('Importing… 3 of 10 steps');
    expect(importStatusText(job('running', 0, 0))).toBe('Importing…');
    expect(importStatusText(job('succeeded'))).toMatch(/Opening your league/);
    expect(
      importStatusText({
        ...job('failed'),
        error: { type: 'x', title: 'Bad', status: 502, detail: 'FPL is down' },
      } as Schemas['Import']),
    ).toBe('Import failed: FPL is down. Try again.');
    const failed = (type: string, detail: string) =>
      importStatusText({
        ...job('failed'),
        error: { type, title: 'Failed', status: 503, detail },
      } as Schemas['Import']);
    // The server's detail keeps its own stop and its own "Try again".
    expect(
      failed(
        'https://myfantasyprem.example/problems/stats-not-ready',
        'Player data is still loading. Try again in a few minutes.',
      ),
    ).toBe('Import failed: Player data is still loading. Try again in a few minutes.');
    expect(failed('https://myfantasyprem.example/problems/import-failed', 'It broke.')).toBe(
      'Import failed: It broke. Try again.',
    );
    // No retry can help on a server without FPL's player data.
    expect(
      failed(
        'https://myfantasyprem.example/problems/import-unavailable',
        "This server isn't connected to live player data, so FPL Draft leagues can't be imported here.",
      ),
    ).toBe(
      "Import failed: This server isn't connected to live player data, so FPL Draft leagues can't be imported here.",
    );
    expect(importStatusText(job('paused'))).toMatch(/does not know the import state "paused"/);
  });
});

describe('sync status', () => {
  it('says when FPL was last applied, through lib/time', () => {
    expect(syncLine(sync(), NOW)).toBe('Imported from FPL · last synced 5m ago');
    expect(syncLine(sync({ lastSyncedAt: null }), NOW)).toBe('Imported from FPL · not synced yet');
  });

  it('says what is running, never treating an unknown kind as idle', () => {
    const pending = (kind: string) =>
      sync({ pending: { kind: kind as 'resync', requestedAt: '2026-10-14T11:59:00Z' } });
    expect(syncPendingText(sync())).toBeNull();
    expect(syncPendingText(pending('resync'))).toBe('Re-sync running…');
    expect(syncPendingText(pending('check'))).toBe('Checking against FPL…');
    expect(syncPendingText(pending('audit'))).toBe('Working with FPL…');
  });

  it('reports a failed re-sync or check, which changed nothing', () => {
    expect(syncErrorText(sync())).toBeNull();
    expect(syncErrorText(sync({ lastError: 'FPL timed out' }))).toBe(
      'The last re-sync or check failed: FPL timed out. Nothing changed.',
    );
    expect(
      syncErrorText(
        sync({ lastError: 'Player data is still loading. Try again in a few minutes.' }),
      ),
    ).toBe(
      'The last re-sync or check failed: Player data is still loading. Try again in a few minutes. Nothing changed.',
    );
  });

  it('summarises the verification for the commissioner', () => {
    expect(verificationSummary(verification())).toEqual({
      label: 'Matches FPL',
      tone: 'win',
    });
    expect(
      verificationSummary(
        verification({ counts: { ok: 1, mismatch: 2, unverified: 1, pending: 1 } }),
      ).label,
    ).toBe('2 mismatches');
    expect(
      verificationSummary(
        verification({ counts: { ok: 1, mismatch: 0, unverified: 4, pending: 0 } }),
      ),
    ).toEqual({ label: '4 not checked yet', tone: 'warning' });
    expect(
      verificationSummary(
        verification({ counts: { ok: 1, mismatch: 0, unverified: 0, pending: 1 } }),
      ),
    ).toEqual({ label: 'Matches FPL · 1 trade pending on FPL', tone: 'warning' });
  });

  it('names a failing item and its two values', () => {
    expect(syncItemTitle(item)).toBe('GW5 · Corner Flag Crew · Points');
    expect(syncItemTitle({ ...item, kind: 'brand_new' as never, team: null, gameweek: null })).toBe(
      'brand_new',
    );
    expect(syncItemValues(item)).toBe('Ours 61 · FPL 63');
  });
});

describe('re-sync warning', () => {
  it('says what is replaced, since when, and what is cancelled', () => {
    const text = resyncWarning(sync(), NOW, 'UTC');
    expect(text).toMatch(
      /^This replaces all moves made here since Wed 14 Oct, 11:55 with FPL's current state/,
    );
    expect(text).toMatch(/squads, waiver order, trades, waiver claims and lineups/);
    expect(text).toMatch(/team names and settings too/);
    expect(text).toMatch(/Open trades made here and pending claims are cancelled/);
    expect(text).toMatch(/Scores and standings are recomputed/);
  });

  it('still warns when the last sync time is unknown', () => {
    expect(resyncWarning(sync({ lastSyncedAt: null }), NOW)).toMatch(
      /^This replaces all moves made here with FPL's current state/,
    );
  });
});

describe('seats', () => {
  it('says who the seat is in FPL and whether it is claimed', () => {
    const seat = {
      team: item.team!,
      fplManagerName: 'Hugo B.',
      claimed: false,
      owner: null,
      invite: null,
    } as Schemas['Seat'];
    expect(seatSubtitle(seat)).toBe('FPL: Hugo B. · not claimed yet');
    expect(
      seatSubtitle({ ...seat, claimed: true, owner: { id: 'usr_x', displayName: 'Hugo' } }),
    ).toBe('FPL: Hugo B. · claimed by Hugo');
  });
});

describe('automatic checks', () => {
  it('summarises the automatic checks: mismatches, then what FPL is still updating', () => {
    const shadow = (
      counts: Schemas['SyncItemCounts'],
      reason = 'bonus',
    ): Schemas['ShadowCheck'] => ({
      checkedAt: '2026-10-14T11:40:00Z',
      reason,
      gameweek: 7,
      status: 'clean',
      counts,
      mismatches: [],
      pending: [],
    });
    expect(shadowSummary(shadow({ ok: 9, mismatch: 0, unverified: 0, pending: 0 }))).toEqual({
      label: 'Matches FPL',
      tone: 'win',
    });
    expect(shadowSummary(shadow({ ok: 9, mismatch: 1, unverified: 0, pending: 3 }))).toEqual({
      label: '1 mismatch',
      tone: 'loss',
    });
    // The server reports the check incomplete: never a green "Matches FPL".
    expect(shadowSummary(shadow({ ok: 9, mismatch: 0, unverified: 2, pending: 0 }))).toEqual({
      label: '2 not checked yet',
      tone: 'warning',
    });
    expect(shadowSummary(shadow({ ok: 9, mismatch: 0, unverified: 0, pending: 3 })).label).toBe(
      'Matches so far · 3 updating',
    );
    expect(shadowLine(shadow({ ok: 9, mismatch: 0, unverified: 0, pending: 0 }), NOW)).toBe(
      'Last checked after bonus, GW7, 20m ago.',
    );
    expect(
      shadowLine(shadow({ ok: 9, mismatch: 0, unverified: 0, pending: 0 }, 'something_new'), NOW),
    ).toBe('Last checked, GW7, 20m ago.');
    expect(syncItemTitle({ ...item, kind: 'player_points' })).toBe(
      'GW5 · Corner Flag Crew · Player points',
    );
  });
});
