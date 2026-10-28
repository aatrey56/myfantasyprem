import { ApiError, type Schemas } from '@myfantasyprem/api-client';

import { RuleError } from '@/api/optimistic';

import {
  clearTradeDraft,
  isRefused,
  reviseDraft,
  saveTradeDraft,
  tradeDraft,
  type TradeDraft,
} from './trade-draft';

const p = (id: string, displayName: string) =>
  ({ id, displayName, position: 'MID' }) as Schemas['PlayerRef'];

const draft: TradeDraft = {
  teamId: 'tm_b',
  teamName: 'Kev’s Kings',
  give: [p('plr_1', 'Saka'), p('plr_2', 'Rice')],
  get: [p('plr_3', 'Palmer'), p('plr_4', 'Gordon')],
  message: 'Fair?',
};

const problem = (status: number, slug: string, detail: string, errors?: unknown[]) =>
  new ApiError(status, {
    type: `https://myfantasyprem.example/problems/${slug}`,
    title: 'Refused',
    status,
    detail,
    ...(errors ? { errors } : {}),
  } as never);

describe('reviseDraft', () => {
  it('takes out the players a trade-shape refusal names off a roster, with why', () => {
    const r = reviseDraft(
      draft,
      problem(422, 'trade-shape', 'That trade is not allowed.', [
        { code: 'player_not_on_roster', message: 'Not on your team.', playerId: 'plr_2' },
        { code: 'player_not_on_roster', message: 'Not on Kev’s Kings.', playerId: 'plr_3' },
        { code: 'position_mismatch', message: 'Both sides must trade the same number.' },
      ]),
    );
    expect(r.give.map((x) => x.id)).toEqual(['plr_1']);
    expect(r.get.map((x) => x.id)).toEqual(['plr_4']);
    expect(r.removed).toEqual([
      { player: draft.give[1], reason: 'is no longer on your team' },
      { player: draft.get[0], reason: 'is no longer on Kev’s Kings' },
    ]);
    expect(r.message).toBe('Fair?');
    expect(r.teamId).toBe('tm_b');
    expect(r.refusal).toContain('Not on your team.');
  });

  it('takes out the player a pending-trade conflict names', () => {
    const r = reviseDraft(
      draft,
      problem(409, 'player-in-trade-review', 'Gordon is in a trade waiting to be processed.'),
    );
    expect(r.removed).toEqual([
      { player: draft.get[1], reason: 'is in a trade waiting to be processed' },
    ]);
    expect(r.get.map((x) => x.id)).toEqual(['plr_3']);
  });

  it('keeps every player when the refusal names none (deadline, rule, network)', () => {
    for (const e of [
      problem(409, 'trade-deadline', "This league's trade deadline has passed."),
      new RuleError(['The trade deadline has passed.']),
      new TypeError('Failed to fetch'),
    ]) {
      const r = reviseDraft(draft, e);
      expect(r.removed).toEqual([]);
      expect(r.give).toEqual(draft.give);
      expect(r.get).toEqual(draft.get);
    }
  });
});

describe('the draft store', () => {
  it('keeps one draft per league until cleared', () => {
    saveTradeDraft('lg_a', draft);
    saveTradeDraft('lg_b', reviseDraft(draft, new TypeError('x')));
    expect(tradeDraft('lg_a')).toBe(draft);
    expect(isRefused(tradeDraft('lg_a'))).toBe(false);
    expect(isRefused(tradeDraft('lg_b'))).toBe(true);
    clearTradeDraft('lg_a');
    expect(tradeDraft('lg_a')).toBeUndefined();
    expect(tradeDraft('lg_b')).toBeDefined();
  });
});
