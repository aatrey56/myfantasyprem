import type { Schemas } from '@myfantasyprem/api-client';

import { initialsOf, readableOn, userColor } from './identity';
import {
  leagueStatus,
  myMatchup,
  normaliseInviteCode,
  ordinal,
  seasonOverUnplayed,
  sortMyLeagues,
} from './leagues';
import { safeNextPath } from './navigation';
import { parseStoredSession } from '@/auth/session';

describe('invite codes', () => {
  it.each([
    ['K7QM2XRP4D', 'K7QM2XRP4D'],
    ['k7qm2xrp4d', 'K7QM2XRP4D'],
    [' K7QM-2XRP-4D ', 'K7QM2XRP4D'],
    ['https://app.example.com/join/k7qm2xrp4d', 'K7QM2XRP4D'],
    // 8-character codes from before codes grew still work.
    ['K7QM2XRP', 'K7QM2XRP'],
    ['k7qm2xrp', 'K7QM2XRP'],
    [' K7QM-2XRP ', 'K7QM2XRP'],
    ['https://app.example.com/join/derby7xq', 'DERBY7XQ'],
    ['https://app.example.com/join/DERBY7XQ?ref=1', 'DERBY7XQ'],
  ])('%j → %s', (input, code) => expect(normaliseInviteCode(input)).toBe(code));

  it.each([
    '',
    'K7QM2XR',
    'K7QM2XRPP',
    'K7QM2XRP4DX',
    'K7QM2XRP4O',
    'O1LIUXXX',
    'https://example.com/x',
  ])('rejects %j', (input) => expect(normaliseInviteCode(input)).toBeNull());
});

describe('sign-in next path', () => {
  it('keeps in-app paths', () => {
    expect(safeNextPath('/leagues/lg_1/team')).toBe('/leagues/lg_1/team');
    expect(safeNextPath(['/join/K7QM2XRP'])).toBe('/join/K7QM2XRP');
  });
  it.each([
    '//evil.example',
    '/\\evil',
    'https://evil.example',
    'javascript:alert(1)',
    '/sign-in',
    '/sign-in?next=/x',
    undefined,
  ])('drops %j', (next) => expect(safeNextPath(next)).toBeNull());
});

describe('stored session', () => {
  const valid = {
    accessToken: 'a',
    refreshToken: 'r',
    expiresAt: '2026-10-12T10:00:00Z',
    refreshExpiresAt: '2026-12-01T00:00:00Z',
    user: { id: 'usr_1' },
  };
  const now = Date.parse('2026-10-12T12:00:00Z');
  it('reads a stored session even with an expired access token (refresh will rotate it)', () => {
    expect(parseStoredSession(JSON.stringify(valid), now)?.refreshToken).toBe('r');
  });
  it('ignores junk, missing fields and an expired refresh token', () => {
    expect(parseStoredSession(null, now)).toBeNull();
    expect(parseStoredSession('{not json', now)).toBeNull();
    expect(parseStoredSession(JSON.stringify({ ...valid, user: null }), now)).toBeNull();
    expect(
      parseStoredSession(
        JSON.stringify({ ...valid, refreshExpiresAt: '2026-10-01T00:00:00Z' }),
        now,
      ),
    ).toBeNull();
  });
});

describe('league rows', () => {
  const team = (id: string, name: string): Schemas['TeamRef'] => ({ id, name, managerName: 'M' });
  const matchup: Schemas['MatchupSummary'] = {
    id: 'mu_1',
    gameweek: 7,
    state: 'live',
    home: { kind: 'team', team: team('tm_a', 'A'), points: 40 },
    away: { kind: 'average', points: 38, basis: 'floor_mean_all_teams' },
  };

  it('reads a matchup from my side, including the league average', () => {
    expect(myMatchup(matchup, 'tm_a')).toMatchObject({
      myPoints: 40,
      opponentName: 'League average',
      opponentPoints: 38,
    });
    expect(myMatchup(matchup, 'tm_z')).toBeNull();
  });

  it('orders live drafts first and labels unknown statuses neutrally', () => {
    const row = (status: string) => ({ league: { status } }) as unknown as Schemas['MyLeague'];
    expect(
      sortMyLeagues([row('pre_draft'), row('in_season'), row('drafting'), row('weird')]).map(
        (r) => r.league.status,
      ),
    ).toEqual(['drafting', 'in_season', 'pre_draft', 'weird']);
    expect(leagueStatus('weird')).toEqual({ label: 'League', tone: 'neutral' });
  });

  it('writes ordinals', () => {
    expect([1, 2, 3, 4, 11, 12, 13, 21, 22, 101].map(ordinal)).toEqual([
      '1st',
      '2nd',
      '3rd',
      '4th',
      '11th',
      '12th',
      '13th',
      '21st',
      '22nd',
      '101st',
    ]);
  });
});

describe('identity and time', () => {
  it('makes initials', () => {
    expect(initialsOf('Sam P.')).toBe('SP');
    expect(initialsOf('sunday league heroes')).toBe('SL');
    expect(initialsOf("Gaffer's Army")).toBe('GA');
    expect(initialsOf('Ö')).toBe('Ö');
    expect(initialsOf('  ')).toBe('?');
  });

  it('keeps avatar text readable', () => {
    expect(readableOn('#FFFFFF')).toBe('#0E1013');
    expect(readableOn('#0E1013')).toBe('#FFFFFF');
    expect(userColor('Sam', '#2f6fed').bg).toBe('#2f6fed');
    expect(userColor('Sam', 'red').bg).toMatch(/^#[0-9A-F]{6}$/i);
  });
});

describe('season over with nothing played', () => {
  const home = (status: string, played: number, matchups: number) =>
    ({
      league: { status },
      standings: [{ played }],
      matchups: Array.from({ length: matchups }, () => ({})),
    }) as unknown as Schemas['LeagueHome'];
  it('is a complete league with no matchups and no games played', () => {
    expect(seasonOverUnplayed(home('complete', 0, 0))).toBe(true);
  });
  it.each([
    ['complete', 38, 0],
    ['complete', 0, 2],
    ['in_season', 0, 0],
  ])('not %s with %d played and %d matchups', (status, played, matchups) =>
    expect(seasonOverUnplayed(home(status, played, matchups))).toBe(false),
  );
});
