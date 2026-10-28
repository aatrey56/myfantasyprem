import {
  ALL_VIEW,
  DEFAULT_VIEW,
  knownView,
  parseViewSettings,
  viewButtonText,
  viewOptions,
  viewWhere,
} from './playerView';

const CLUBS = [
  { code: 'WOL', name: 'Wolves' },
  { code: 'ARS', name: 'Arsenal' },
  { code: 'CHE', name: 'Chelsea' },
  { code: 'AVL', name: 'Aston Villa' },
];

describe('viewOptions', () => {
  it('lists All players, the four positions, then clubs A–Z', () => {
    expect(viewOptions(CLUBS).map((o) => o.label)).toEqual([
      'All players',
      'Goalkeepers',
      'Defenders',
      'Midfielders',
      'Forwards',
      'Arsenal',
      'Aston Villa',
      'Chelsea',
      'Wolves',
    ]);
  });

  it('gives every option a distinct id and a single choice', () => {
    const ids = viewOptions(CLUBS).map((o) => o.id);
    expect(new Set(ids).size).toBe(ids.length);
    expect(ids.slice(0, 5)).toEqual(['all', 'pos-GKP', 'pos-DEF', 'pos-MID', 'pos-FWD']);
    expect(viewOptions(CLUBS)[5]!.choice).toEqual({ kind: 'club', code: 'ARS' });
  });
});

describe('viewButtonText', () => {
  it('names the current choice', () => {
    expect(viewButtonText(ALL_VIEW, CLUBS)).toBe('View: All players');
    expect(viewButtonText({ kind: 'position', position: 'DEF' }, CLUBS)).toBe('View: Defenders');
    expect(viewButtonText({ kind: 'club', code: 'ARS' }, CLUBS)).toBe('View: Arsenal');
    // Before the clubs load: the code.
    expect(viewButtonText({ kind: 'club', code: 'ARS' }, undefined)).toBe('View: ARS');
  });
});

describe('viewWhere', () => {
  it('sends one of position or club, never both', () => {
    expect(viewWhere(ALL_VIEW)).toEqual({});
    expect(viewWhere({ kind: 'position', position: 'MID' })).toEqual({ position: 'MID' });
    expect(viewWhere({ kind: 'club', code: 'CHE' })).toEqual({ club: 'CHE' });
  });
});

describe('stored settings', () => {
  it('reads a stored view and toggle', () => {
    expect(
      parseViewSettings(JSON.stringify({ view: { kind: 'club', code: 'ARS' }, available: false })),
    ).toEqual({ view: { kind: 'club', code: 'ARS' }, available: false });
    expect(
      parseViewSettings(JSON.stringify({ view: { kind: 'position', position: 'FWD' } })),
    ).toEqual({ view: { kind: 'position', position: 'FWD' }, available: true });
  });

  it('ignores stale or malformed values', () => {
    for (const raw of [
      null,
      '',
      'not json',
      'null',
      '42',
      JSON.stringify({ view: { kind: 'position', position: 'GK' } }),
      JSON.stringify({ view: { kind: 'club', code: '<script>' } }),
      JSON.stringify({ view: 'DEF', available: 'yes' }),
    ]) {
      expect(parseViewSettings(raw)).toEqual(DEFAULT_VIEW);
    }
  });

  it('drops a club the club list no longer has', () => {
    const gone = { kind: 'club', code: 'XYZ' } as const;
    expect(knownView(gone, CLUBS)).toEqual(ALL_VIEW);
    expect(knownView(gone, undefined)).toBe(gone);
    expect(knownView({ kind: 'club', code: 'ARS' }, CLUBS)).toEqual({ kind: 'club', code: 'ARS' });
  });
});
