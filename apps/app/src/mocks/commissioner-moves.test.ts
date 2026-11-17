import { createMovesLog, settingsChange } from './commissioner-moves';
import * as data from './data';
import { leagueSettings } from './league';

const ME = 'usr_me';
const JO = { id: 'usr_jo', displayName: 'Jo T.' };
const at = (h: number) => new Date(Date.UTC(2026, 9, 13, h)).toISOString();

describe('the mock commissioner log', () => {
  const log = () => {
    const l = createMovesLog(ME, 1);
    l.add({ action: 'settings_changed', summary: 'one', actor: JO, createdAt: at(1) });
    l.add({ action: 'trade_vetoed', summary: 'two', actor: JO, createdAt: at(2) });
    l.add({
      action: 'league_renamed',
      summary: 'mine',
      actor: { id: ME, displayName: 'Me' },
      createdAt: at(3),
    });
    return l;
  };

  it('lists newest first, in pages', () => {
    const l = log();
    const first = l.list(0, 2);
    expect(first.slice.map((m) => m.summary)).toEqual(['mine', 'two']);
    expect(first.next).toBe(2);
    const rest = l.list(2, 2);
    expect(rest.slice.map((m) => m.summary)).toEqual(['one']);
    expect(rest.next).toBeNull();
  });

  it('counts what someone else made after the marker, never my own moves', () => {
    const l = log();
    expect(l.summary()).toMatchObject({ unseenCount: 2, latest: { summary: 'two' } });
    expect(l.list(0, 10).slice.map((m) => m.unseen)).toEqual([false, true, true]);
    expect(() => l.markSeen('cmv_unknown')).toThrow(/not found/i);
    expect(l.summary().unseenCount).toBe(2);
  });

  it('marks seen up to a move, never backwards, and refuses an unknown id', () => {
    const l = log();
    const [mine, two, one] = l.list(0, 10).slice;
    expect(l.markSeen(two!.id)).toMatchObject({ unseenCount: 0 });
    expect(l.markSeen(one!.id)).toMatchObject({ unseenCount: 0 });
    expect(mine!.unseen).toBe(false);
    l.add({ action: 'settings_changed', summary: 'new', actor: JO, createdAt: at(4) });
    expect(l.summary()).toMatchObject({ unseenCount: 1, latest: { summary: 'new' } });
    expect(l.markSeen()).toMatchObject({ unseenCount: 0, latest: null });
  });
});

describe('settingsChange', () => {
  const before = leagueSettings({ teamCount: 10, review: 'league_vote' });
  it('words what changed like the server', () => {
    const after = structuredClone(before);
    after.trades.reviewHours = 48;
    after.trades.vetoThreshold = 0.5;
    expect(settingsChange(before, after)).toEqual({
      summary: 'Changed the trade review period to 48 hours, veto threshold to 50% of managers.',
      before: { reviewHours: 24, vetoThreshold: 1 / 3 },
      after: { reviewHours: 48, vetoThreshold: 0.5 },
    });
    expect(settingsChange(before, structuredClone(before))).toBeNull();
  });
});

describe('the mock leagues', () => {
  it("log the commissioner's moves, unseen unless the viewer made them", () => {
    const sunday = data.leagues.get(data.LEAGUE_ID) as data.InSeasonLeague;
    const office = data.leagues.get(data.OFFICE_LEAGUE_ID) as data.InSeasonLeague;
    expect(sunday.moves.summary().unseenCount).toBe(3);
    expect(sunday.moves.summary().latest?.summary).toBe(
      'Changed the trade review period to 48 hours.',
    );
    // The demo user is the Office League's commissioner: their moves are never new to them.
    expect(office.moves.list(0, 10).slice).toHaveLength(3);
    expect(office.moves.summary().unseenCount).toBe(0);
  });
});
