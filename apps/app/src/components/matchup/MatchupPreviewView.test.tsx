import type { Schemas } from '@myfantasyprem/api-client';
import { render, screen, within } from '@testing-library/react-native';

import { MatchupPreviewView } from './MatchupPreviewView';

type Slot = Schemas['PreviewSlot'];

/** `DEF 2` is a defender; slots number from `first`. */
const slots = (names: string[], first: number): Slot[] =>
  names.map((name, i) => ({
    slot: first + i,
    player: {
      id: `plr_${name.replace(' ', '')}`,
      displayName: name,
      club: { code: 'ARS', name: 'Arsenal' },
      position: name.split(' ')[0] as Schemas['PlayerRef']['position'],
      availability: 'available',
    } as Schemas['PlayerRef'],
    fixtures: [],
  }));
const xi = (def: number, mid: number, fwd: number, tag: string) => [
  `GKP ${tag}1`,
  ...Array.from({ length: def }, (_, i) => `DEF ${tag}${i + 1}`),
  ...Array.from({ length: mid }, (_, i) => `MID ${tag}${i + 1}`),
  ...Array.from({ length: fwd }, (_, i) => `FWD ${tag}${i + 1}`),
];
const side = (
  team: string,
  starters: string[],
  bench: string[],
  formation: string,
  shuffled = false,
) => {
  const order = (s: Slot[]) => (shuffled ? s.reverse() : s);
  return {
    kind: 'team',
    team: { id: `tm_${team}`, name: team },
    formation,
    starters: order(slots(starters, 1)),
    bench: order(slots(bench, 12)),
  } as unknown as Schemas['PreviewSide'];
};

const names = (row: ReturnType<typeof screen.getByTestId>) =>
  within(row)
    .queryAllByTestId(/^preview-name-/)
    .map((n) => n.props.children as string);

it('pairs a 4-4-2 with a 3-5-2 in order, a split badge where the positions differ', async () => {
  const bench = ['GKP B', 'DEF B', 'MID B', 'FWD B'];
  const preview = {
    id: 'mu_1',
    gameweek: 9,
    home: side('Home', xi(4, 4, 2, 'h'), bench, '4-4-2'),
    // The away side arrives out of slot order: the view sorts it.
    away: side('Away', xi(3, 5, 2, 'a'), bench, '3-5-2', true),
  } as unknown as Schemas['MatchupPreview'];
  await render(<MatchupPreviewView preview={preview} />);

  const rows = screen.getAllByTestId('preview-starter-row');
  expect(rows).toHaveLength(11);
  expect(names(rows[0])).toEqual(['GKP h1', 'GKP a1']);
  expect(names(rows[3])).toEqual(['DEF h3', 'DEF a3']);
  // The home side's fourth defender meets the away side's first midfielder.
  expect(names(rows[4])).toEqual(['DEF h4', 'MID a1']);
  expect(within(rows[4]).getByTestId('pair-badge-split')).toHaveProp(
    'accessibilityLabel',
    'Positions DEF and MID',
  );
  expect(names(rows[8])).toEqual(['MID h4', 'MID a5']);
  expect(names(rows[10])).toEqual(['FWD h2', 'FWD a2']);
  expect(screen.getAllByTestId('pair-badge-split')).toHaveLength(1);

  // The benches in bench order, whatever order the array came in.
  const benchRows = screen.getAllByTestId('preview-bench-row');
  expect(benchRows.map(names)).toEqual(bench.map((b) => [b, b]));
});
