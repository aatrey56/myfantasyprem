import type { Schemas } from '@myfantasyprem/api-client';
import { fireEvent, render, screen, within } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { ListView } from './LineupViews';
import { SwapSheet, swapCandidates } from './SwapSheet';
import { DEFAULT_RULES, type LineupState } from '@/lib/lineup';

type Entry = Schemas['LineupEntry'];

const entry = (id: string, position: Schemas['Position'], slot: number): Entry => ({
  slot,
  player: {
    id,
    displayName: id.toUpperCase(),
    club: { code: 'NOR', name: 'Northgate' },
    position,
    availability: 'available',
  },
  fixtures: [],
});

/** 1-4-4-2 with a bench of keeper, defender, midfielder, forward. */
const STATE: LineupState = {
  starters: [
    entry('gk', 'GKP', 1),
    entry('d1', 'DEF', 2),
    entry('d2', 'DEF', 3),
    entry('d3', 'DEF', 4),
    entry('d4', 'DEF', 5),
    entry('m1', 'MID', 6),
    entry('m2', 'MID', 7),
    entry('m3', 'MID', 8),
    entry('m4', 'MID', 9),
    entry('f1', 'FWD', 10),
    entry('f2', 'FWD', 11),
  ],
  bench: [
    entry('bgk', 'GKP', 12),
    entry('bd', 'DEF', 13),
    entry('bm', 'MID', 14),
    entry('bf', 'FWD', 15),
  ],
};

const ids = (es: Entry[]) => es.map((e) => e.player.id);

describe('swapCandidates', () => {
  it('offers a starting keeper only the backup keeper', () => {
    const c = swapCandidates(STATE, 'gk', DEFAULT_RULES);
    expect(ids(c.starters)).toEqual([]);
    expect(ids(c.bench)).toEqual(['bgk']);
  });

  it('offers a defender every outfield bench player that keeps the formation legal', () => {
    // 4-4-2 minus a defender: 3-5-2 and 3-4-3 are fine, so is 4-4-2.
    expect(ids(swapCandidates(STATE, 'd1', DEFAULT_RULES).bench)).toEqual(['bd', 'bm', 'bf']);
  });

  it('offers a bench player legal starters and other bench places, never the keeper slot', () => {
    const c = swapCandidates(STATE, 'bf', DEFAULT_RULES);
    // Any outfield starter can make way for a forward (3 forwards at most).
    expect(ids(c.starters)).toEqual(['d1', 'd2', 'd3', 'd4', 'm1', 'm2', 'm3', 'm4', 'f1', 'f2']);
    // The first bench place must stay the backup keeper.
    expect(ids(c.bench)).toEqual(['bd', 'bm']);
  });
});

describe('swapCandidates in every legal formation', () => {
  const POS = ['GKP', 'DEF', 'MID', 'FWD'] as const;
  type Pos = (typeof POS)[number];
  /** The rules, written out independently of lib/lineup: 1 GK, 3-5 DEF, 2-5 MID, 1-3 FWD. */
  const BOUNDS: Record<Pos, [number, number]> = {
    GKP: [1, 1],
    DEF: [3, 5],
    MID: [2, 5],
    FWD: [1, 3],
  };
  const legal = (starters: readonly Entry[], bench: readonly Entry[]) =>
    POS.every((p) => {
      const n = starters.filter((e) => e.player.position === p).length;
      return n >= BOUNDS[p][0] && n <= BOUNDS[p][1];
    }) && bench[0]?.player.position === 'GKP';

  const formations: [number, number, number][] = [];
  for (let d = 3; d <= 5; d++)
    for (let m = 2; m <= 5; m++) {
      const f = 10 - d - m;
      if (f >= 1 && f <= 3) formations.push([d, m, f]);
    }
  /** Outfield benches: every mix of three outfielders, in order. */
  const benches: Pos[][] = [];
  for (const a of POS.slice(1))
    for (const b of POS.slice(1)) for (const c of POS.slice(1)) benches.push([a, b, c]);

  it.each(formations)(
    '%i-%i-%i: offers exactly the swaps that keep the lineup legal',
    (d, m, f) => {
      for (const outfield of benches) {
        let n = 0;
        const mk = (p: Pos) => entry(`${p.toLowerCase()}${++n}`, p, n);
        const state: LineupState = {
          starters: [
            mk('GKP'),
            ...Array.from({ length: d }, () => mk('DEF')),
            ...Array.from({ length: m }, () => mk('MID')),
            ...Array.from({ length: f }, () => mk('FWD')),
          ],
          bench: [mk('GKP'), ...outfield.map(mk)],
        };
        for (const sel of [...state.starters, ...state.bench]) {
          const selId = sel.player.id;
          const c = swapCandidates(state, selId, DEFAULT_RULES);
          const offered = new Set([...ids(c.starters), ...ids(c.bench)]);
          const selOnBench = state.bench.includes(sel);
          for (const other of [...state.starters, ...state.bench]) {
            const otherOnBench = state.bench.includes(other);
            if (other === sel || (!selOnBench && !otherOnBench)) {
              expect(offered.has(other.player.id)).toBe(false);
              continue;
            }
            const swap = (e: Entry) => (e === sel ? other : e === other ? sel : e);
            const want = legal(state.starters.map(swap), state.bench.map(swap));
            if (offered.has(other.player.id) !== want) {
              throw new Error(
                `${d}-${m}-${f}, bench ${outfield.join('/')}: ${selId} with ${other.player.id} ` +
                  `should ${want ? '' : 'not '}be offered`,
              );
            }
          }
        }
      }
    },
  );

  it('never lets a formation edge slip: 3-4-3 defenders, 5-4-1 forward, 3-5-2 midfielders', () => {
    const lineup = (d: number, m: number, f: number, bench: Pos[]): LineupState => {
      let n = 0;
      const mk = (p: Pos) => entry(`${p.toLowerCase()}${++n}`, p, n);
      return {
        starters: [
          mk('GKP'),
          ...Array.from({ length: d }, () => mk('DEF')),
          ...Array.from({ length: m }, () => mk('MID')),
          ...Array.from({ length: f }, () => mk('FWD')),
        ],
        bench: [mk('GKP'), ...bench.map(mk)],
      };
    };
    const benchPositions = (s: LineupState, id: string) =>
      swapCandidates(s, id, DEFAULT_RULES).bench.map((e) => e.player.position);
    // Three defenders: one can only make way for another defender.
    const a = lineup(3, 4, 3, ['DEF', 'MID', 'FWD']);
    expect(benchPositions(a, 'def2')).toEqual(['DEF']);
    // Three forwards: no fourth forward comes on for a midfielder.
    expect(benchPositions(a, 'mid5')).toEqual(['DEF', 'MID']);
    // A lone forward: only a forward replaces him.
    const b = lineup(5, 4, 1, ['DEF', 'MID', 'FWD']);
    expect(benchPositions(b, 'fwd11')).toEqual(['FWD']);
    // Five midfielders: no sixth for a forward; two forwards may become one.
    const c = lineup(3, 5, 2, ['DEF', 'MID', 'FWD']);
    expect(benchPositions(c, 'fwd10')).toEqual(['DEF', 'FWD']);
    // The keepers only ever swap with each other.
    expect(ids(swapCandidates(c, 'gkp12', DEFAULT_RULES).starters)).toEqual(['gkp1']);
    expect(ids(swapCandidates(c, 'gkp12', DEFAULT_RULES).bench)).toEqual([]);
  });
});

const inApp = (node: ReactNode) => (
  <SafeAreaProvider
    initialMetrics={{
      frame: { x: 0, y: 0, width: 375, height: 812 },
      insets: { top: 0, left: 0, right: 0, bottom: 0 },
    }}
  >
    {node}
  </SafeAreaProvider>
);

describe('SwapSheet', () => {
  it('is closed with nothing selected', async () => {
    await render(
      inApp(
        <SwapSheet
          state={STATE}
          selectedId={null}
          rules={DEFAULT_RULES}
          scores={null}
          onSwap={jest.fn()}
          onClose={jest.fn()}
        />,
      ),
    );
    expect(screen.queryByTestId('swap-sheet')).toBeNull();
  });

  it('shows the player, the prompt and only legal candidates; a tap swaps', async () => {
    const onSwap = jest.fn();
    await render(
      inApp(
        <SwapSheet
          state={STATE}
          selectedId="gk"
          rules={DEFAULT_RULES}
          scores={null}
          onSwap={onSwap}
          onClose={jest.fn()}
        />,
      ),
    );
    const sheet = screen.getByTestId('swap-sheet');
    // Sleeper's centred bold title, with the ✕ kept for the keyboard and screen readers.
    expect(within(sheet).getByTestId('swap-title')).toHaveTextContent('Swap Player');
    expect(within(sheet).getByTestId('swap-title')).toHaveStyle({ textAlign: 'center' });
    expect(within(sheet).getByTestId('sheet-close')).toBeOnTheScreen();
    expect(within(sheet).getByTestId('swap-selected')).toHaveTextContent(/GK/);
    expect(within(sheet).getByTestId('swap-prompt')).toHaveTextContent('⇅ Choose a player to swap');
    const offered = within(sheet).getAllByTestId(/^swap-candidate-/);
    expect(offered.map((o) => o.props.testID)).toEqual(['swap-candidate-bgk']);
    await fireEvent.press(within(sheet).getByLabelText('Swap with BGK'));
    expect(onSwap).toHaveBeenCalledWith('bgk');
  });

  it('cancels without swapping', async () => {
    const onSwap = jest.fn();
    const onClose = jest.fn();
    await render(
      inApp(
        <SwapSheet
          state={STATE}
          selectedId="d1"
          rules={DEFAULT_RULES}
          scores={null}
          onSwap={onSwap}
          onClose={onClose}
        />,
      ),
    );
    await fireEvent.press(screen.getByTestId('sheet-close'));
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onSwap).not.toHaveBeenCalled();
  });
});

describe('LineupRow in the list', () => {
  const props = {
    scores: null,
    selectedId: null,
    check: () => ({ kind: 'none' }) as const,
    onPress: jest.fn(),
    onSwap: jest.fn(),
    onInfo: jest.fn(),
    editable: true,
  };
  const season = { form: 6.25, totalPoints: 41, appearances: 6, clubMatches: 7 };
  const flagged = (
    id: string,
    availability: Schemas['PlayerAvailability'],
    news: string | null,
    chanceOfPlaying: number | null,
  ): Entry => ({
    ...entry(id, 'MID', 6),
    player: { ...entry(id, 'MID', 6).player, availability, news },
    season,
    chanceOfPlaying,
  });

  it('shows the season line, the status tag and a news icon', async () => {
    const state: LineupState = {
      starters: [
        ...STATE.starters.slice(0, 5),
        flagged('m1', 'doubtful', 'Knock - 75% chance of playing', 75),
        flagged('m2', 'injured', 'Hamstring injury - Unknown return date', 0),
        flagged('m3', 'suspended', 'Suspended until 24 Oct', 0),
        { ...entry('m4', 'MID', 9), season },
        ...STATE.starters.slice(9),
      ],
      bench: STATE.bench,
    };
    await render(inApp(<ListView {...props} state={state} />));
    const row = (id: string) => within(screen.getByTestId(`lineup-row-${id}`));
    expect(row('m4').getByTestId('player-season')).toHaveTextContent(
      'Form 6.3 · 41 pts · Played 6/7',
    );
    expect(row('m4').queryByTestId('player-status')).toBeNull();
    expect(row('m4').queryByTestId('player-news-icon', { includeHiddenElements: true })).toBeNull();
    expect(row('m1').getByTestId('player-status')).toHaveTextContent('75%');
    expect(row('m2').getByTestId('player-status')).toHaveTextContent('OUT');
    expect(row('m3').getByTestId('player-status')).toHaveTextContent('SUSP');
    for (const id of ['m1', 'm2', 'm3']) {
      expect(row(id).getByTestId('player-news-icon', { includeHiddenElements: true })).toBeTruthy();
    }
    // The news is spoken on the row, whose press opens the card.
    expect(screen.getByLabelText('Player card for M2').props.accessibilityHint).toMatch(
      /news: Hamstring injury/,
    );
    // No season line without the data (a server that predates it).
    expect(row('gk').queryByTestId('player-season')).toBeNull();
  });
});

describe('ListView', () => {
  const props = {
    state: STATE,
    scores: null,
    selectedId: null,
    check: () => ({ kind: 'none' }) as const,
    onPress: jest.fn(),
    onInfo: jest.fn(),
  };

  it('opens the swap for a player from the position pill; the row opens the card', async () => {
    const onSwap = jest.fn();
    const onInfo = jest.fn();
    await render(inApp(<ListView {...props} editable onSwap={onSwap} onInfo={onInfo} />));
    // "Starters" and the hint live in the team header now; the list heads only the bench.
    expect(screen.queryByText('Starters')).toBeNull();
    expect(screen.getByText('Bench')).toBeOnTheScreen();
    await fireEvent.press(screen.getByLabelText('Swap M1'));
    expect(onSwap).toHaveBeenCalledWith('m1');
    expect(onInfo).not.toHaveBeenCalled();
    await fireEvent.press(screen.getByLabelText('Player card for M1'));
    expect(onInfo).toHaveBeenCalledWith('m1');
    // Before kickoff every row shows a dash for points.
    expect(screen.getAllByTestId('player-points').map((p) => p.props.children)).toEqual(
      Array(15).fill('–'),
    );
  });

  it('has no swap buttons or hint when the lineup is locked', async () => {
    await render(inApp(<ListView {...props} editable={false} onSwap={jest.fn()} />));
    expect(screen.queryAllByLabelText(/^Swap /)).toHaveLength(0);
    expect(screen.queryByTestId('swap-hint')).toBeNull();
  });
});

// The live "freeze": a sheet opened with nothing to show left a transparent
// Modal over the app that took every touch.
describe('SwapSheet never opens empty', () => {
  const sheet = (state: LineupState, selectedId: string) =>
    inApp(
      <SwapSheet
        state={state}
        selectedId={selectedId}
        rules={DEFAULT_RULES}
        scores={null}
        onSwap={jest.fn()}
        onClose={jest.fn()}
      />,
    );

  it('stays closed for a player with no legal swap (a keeper with no keeper on the bench)', async () => {
    const noBackupKeeper = {
      ...STATE,
      bench: STATE.bench.filter((e) => e.player.position !== 'GKP'),
    };
    expect(swapCandidates(noBackupKeeper, 'gk', DEFAULT_RULES).bench).toEqual([]);
    await render(sheet(noBackupKeeper, 'gk'));
    expect(screen.queryByTestId('swap-sheet')).toBeNull();
    expect(screen.queryByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeNull();
  });

  it('stays closed for a player no longer in the lineup (a refetch replaced it)', async () => {
    await render(sheet(STATE, 'gone'));
    expect(screen.queryByTestId('swap-sheet')).toBeNull();
    expect(screen.queryByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeNull();
  });
});
