import type { Schemas } from '@myfantasyprem/api-client';

import {
  MOVES_SECTION,
  moveByline,
  movesBanner,
  movesHref,
  wantsMovesSection,
} from './commissioner-moves';

const move = (summary: string): Schemas['CommissionerMove'] => ({
  id: 'cmv_01k7m2xq800000000000000401',
  action: 'settings_changed',
  summary,
  actor: { id: 'usr_01k7m2xq8s0000000000000001', displayName: 'Jo T.' },
  teams: [],
  details: null,
  unseen: true,
  createdAt: '2026-10-13T08:00:00Z',
});

describe('movesBanner', () => {
  it('says how many moves are new and shows the latest', () => {
    expect(
      movesBanner({ unseenCount: 2, latest: move('Vetoed the trade between A and B.') }),
    ).toEqual({
      title: 'Commissioner moves · 2 new',
      detail: 'Vetoed the trade between A and B.',
    });
  });

  it('is gone once everything is seen, and while the summary loads', () => {
    expect(movesBanner({ unseenCount: 0, latest: null })).toBeNull();
    expect(movesBanner(undefined)).toBeNull();
  });

  it('still shows a count when the latest move is missing', () => {
    expect(movesBanner({ unseenCount: 1, latest: null })).toEqual({
      title: 'Commissioner moves · 1 new',
      detail: null,
    });
  });
});

describe('the settings section', () => {
  it('opens League settings at the commissioner moves', () => {
    expect(movesHref('lg_1')).toBe(`/leagues/lg_1/settings?section=${MOVES_SECTION}`);
    expect(wantsMovesSection(MOVES_SECTION)).toBe(true);
    expect(wantsMovesSection([MOVES_SECTION, 'x'])).toBe(true);
    expect(wantsMovesSection('members')).toBe(false);
    expect(wantsMovesSection(undefined)).toBe(false);
  });

  it('names who made a move, or a former commissioner once their account is deleted', () => {
    expect(moveByline(move('x'))).toBe('By Jo T.');
    expect(moveByline({ actor: null })).toBe('By a former commissioner');
  });
});
