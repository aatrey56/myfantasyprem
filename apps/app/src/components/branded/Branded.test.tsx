import { act, fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { StyleSheet, Text } from 'react-native';

import { ClubCrest, ClubShirt, ClubTag, PlayerPhoto } from '../Badges';
import { FormationPitch, Shirt } from '../pitch/FormationPitch';
import { PlayerRow } from '../PlayerRow';
import { LineupRow } from '../RosterRow';
import { BrandedAssetsOverride, BrandedAssetsProvider, useBrandedAssets } from './BrandedAssets';

const mockConfig = { data: undefined as { data: { brandedAssets: boolean } } | undefined };
jest.mock('@/api/assets', () => ({
  ...jest.requireActual('@/api/assets'),
  useAppConfig: () => mockConfig,
}));

const PLAYER = 'plr_01k7m2xq8v00000000000000a1';

const ROW_PLAYER = {
  id: PLAYER,
  displayName: 'C. Strand',
  club: { code: 'ARS', name: 'Arsenal' },
  position: 'MID',
  availability: 'available',
  news: null,
} as const;
const FIXTURE = {
  fixtureId: 'fx_1',
  opponent: { code: 'CHE', name: 'Chelsea' },
  isHome: true,
  kickoffAt: '2026-10-17T14:00:00Z',
  status: 'scheduled',
} as const;

const on = (children: ReactNode) => (
  <BrandedAssetsOverride value={true}>{children}</BrandedAssetsOverride>
);

// The images are decorative (hidden from screen readers): include hidden elements.
const hidden = { includeHiddenElements: true } as const;
const image = () => screen.getByTestId('branded-image', hidden);
/** expo-image hands its native view a list of sources: the first is ours. */
const srcOf = (el: { props: Record<string, unknown> }) => {
  const source = el.props.source as { uri: string } | { uri: string }[];
  return Array.isArray(source) ? source[0]!.uri : source.uri;
};
const uri = () => srcOf(image());
const load = () => fireEvent(image(), 'load', { nativeEvent: { source: { url: uri() } } });
const fail = () => fireEvent(image(), 'error', { nativeEvent: { error: 'not found' } });
const opacity = () => StyleSheet.flatten(image().props.style).opacity;

describe('branded images switched off', () => {
  it('draws the generic shirts and requests nothing', async () => {
    await render(
      <>
        <ClubShirt code="ARS" goalkeeper />
        <PlayerPhoto playerId={PLAYER} code="ARS" size={38} />
        <ClubTag code="ARS" name="Arsenal" />
        <Shirt kit="#C81E3A" club="ARS" size={34} />
      </>,
    );
    expect(screen.getAllByTestId('generic-shirt', hidden)).toHaveLength(4);
    expect(screen.queryByTestId('branded-image', hidden)).toBeNull();
  });

  it('keeps the shirt avatars in the list and roster rows', async () => {
    await render(
      <>
        <PlayerRow player={ROW_PLAYER} avatar fixtures={[FIXTURE]} />
        <LineupRow player={ROW_PLAYER} fixtures={[FIXTURE]} />
      </>,
    );
    expect(screen.getAllByTestId('generic-shirt', hidden)).toHaveLength(2);
    expect(screen.queryByTestId('branded-image', hidden)).toBeNull();
  });

  it('adds no crest where there was none', async () => {
    await render(<ClubCrest code="ARS" />);
    expect(screen.toJSON()).toBeNull();
  });
});

describe('branded images switched on', () => {
  it("loads the club's goalkeeper shirt from our API, with a placeholder until it loads", async () => {
    await render(on(<ClubShirt code="ars" goalkeeper size={20} />));
    expect(uri()).toBe('/v1/assets/clubs/ARS/shirt?gk=true');
    expect(opacity()).toBe(0);
    expect(screen.queryByTestId('generic-shirt', hidden)).toBeNull();
    await act(async () => load());
    expect(opacity()).toBe(1);
  });

  it('falls back to the generic shirt when the image fails', async () => {
    await render(on(<ClubShirt code="ARS" />));
    await act(async () => fail());
    expect(screen.queryByTestId('branded-image', hidden)).toBeNull();
    expect(screen.getByTestId('generic-shirt', hidden)).toBeTruthy();
  });

  it("shows a player's photo, and the club shirt if it is missing", async () => {
    await render(on(<PlayerPhoto playerId={PLAYER} code="ARS" size={38} />));
    expect(uri()).toBe(`/v1/assets/players/${PLAYER}/photo`);
    await act(async () => fail());
    expect(screen.getByTestId('generic-shirt', hidden)).toBeTruthy();
  });

  it('puts the crest next to the club code, falling back to the shirt glyph', async () => {
    await render(on(<ClubTag code="ARS" name="Arsenal" />));
    expect(uri()).toBe('/v1/assets/clubs/ARS/crest');
    expect(screen.getByText('ARS')).toBeTruthy();
    await act(async () => fail());
    expect(screen.getByTestId('generic-shirt', hidden)).toBeTruthy();
    expect(screen.getByText('ARS')).toBeTruthy();
  });

  it('puts photos and crests in the list and roster rows', async () => {
    const uris = async (ui: ReactNode) => {
      const { unmount } = await render(on(ui));
      const got = screen.getAllByTestId('branded-image', hidden).map(srcOf).sort();
      await unmount();
      return got;
    };
    expect(await uris(<PlayerRow player={ROW_PLAYER} avatar fixtures={[FIXTURE]} />)).toEqual([
      '/v1/assets/clubs/ARS/crest',
      '/v1/assets/clubs/CHE/crest',
      `/v1/assets/players/${PLAYER}/photo`,
    ]);
    expect(await uris(<LineupRow player={ROW_PLAYER} fixtures={[FIXTURE]} />)).toEqual([
      '/v1/assets/clubs/CHE/crest',
      `/v1/assets/players/${PLAYER}/photo`,
    ]);
  });

  it('draws real shirts on the pitch, the goalkeeper in his own', async () => {
    await render(
      on(
        <FormationPitch
          lines={[
            {
              key: 'GKP',
              players: [{ id: 'g', name: 'Raya', kit: '#fff', club: 'ARS', goalkeeper: true }],
            },
            { key: 'DEF', players: [{ id: 'd', name: 'Saliba', kit: '#fff', club: 'ARS' }] },
          ]}
        />,
      ),
    );
    const uris = screen.getAllByTestId('branded-image', hidden).map(srcOf).sort();
    expect(uris).toEqual(['/v1/assets/clubs/ARS/shirt', '/v1/assets/clubs/ARS/shirt?gk=true']);
  });

  it('keeps the generic shirt for a pitch player without a club', async () => {
    await render(on(<Shirt kit="#C81E3A" size={34} />));
    expect(screen.getByTestId('generic-shirt', hidden)).toBeTruthy();
  });
});

describe('BrandedAssetsProvider', () => {
  function Probe() {
    return <Text>{String(useBrandedAssets())}</Text>;
  }

  it('is on only when the server says so', async () => {
    for (const [config, want] of [
      [undefined, 'false'],
      [{ data: { brandedAssets: false } }, 'false'],
      [{ data: { brandedAssets: true } }, 'true'],
    ] as const) {
      mockConfig.data = config;
      const { unmount } = await render(
        <BrandedAssetsProvider>
          <Probe />
        </BrandedAssetsProvider>,
      );
      expect(screen.getByText(want)).toBeTruthy();
      await unmount();
    }
  });
});
