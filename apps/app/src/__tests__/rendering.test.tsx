import type { Schemas } from '@myfantasyprem/api-client';
import { fireEvent, render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { MatchupDetailView } from '@/components/matchup/MatchupDetailView';
import { RosterRow } from '@/components/RosterRow';
import { formatNextFixtures, toRowFixture } from '@/lib/fixtures';
import * as data from '@/mocks/data';

jest.mock('expo-router', () => ({
  ...jest.requireActual('expo-router'),
  useRouter: () => ({ push: jest.fn(), replace: jest.fn(), navigate: jest.fn() }),
}));

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

const club = (code: string) => ({ code, name: code });
const fixture = (code: string, n: number): Schemas['UpcomingFixture'] => ({
  fixtureId: `fx_01k7m2xq80000000000000000${n}`,
  gameweek: 8,
  opponent: club(code),
  isHome: n % 2 === 0,
  kickoffAt: `2026-10-2${n}T15:00:00Z`,
});

describe('average opponent', () => {
  it('shows one card with a score and no lineup for the average side', async () => {
    const summary = data.matchupSummaries(7).find((m) => m.away.kind === 'average')!;
    const detail = data.matchupDetail(7, summary.id)!;
    await render(inApp(<MatchupDetailView leagueId={data.LEAGUE_ID} detail={detail} />));
    expect(screen.getByTestId('average-opponent')).toHaveTextContent(/League average/);
    expect(screen.getByTestId('average-points')).toHaveTextContent(
      String((detail.away as Schemas['AverageOpponent']).points),
    );
    // Only the real team has a lineup.
    expect(screen.getAllByTestId('team-lineup')).toHaveLength(1);
    expect(screen.queryByTestId('h2h-lineups')).toBeNull();
  });

  it('renders an unknown side kind neutrally (open enums)', async () => {
    const unknown = { kind: 'bye' } as unknown as Schemas['MatchupDetailSide'];
    await render(
      inApp(
        <MatchupDetailView
          leagueId={data.LEAGUE_ID}
          detail={{ id: 'mu_x', gameweek: 7, state: 'live', home: unknown, away: unknown }}
        />,
      ),
    );
    expect(screen.getAllByText('Opponent').length).toBeGreaterThan(0);
    expect(screen.getByText(/can't be shown/)).toBeOnTheScreen();
  });
});

describe('head-to-head lineups', () => {
  const h2h = () => {
    const summary = data
      .matchupSummaries(7)
      .find((m) => m.home.kind === 'team' && m.away.kind === 'team')!;
    return data.matchupDetail(7, summary.id)!;
  };

  it('heads the starters and bench plainly: no formations, counts or unused points', async () => {
    await render(inApp(<MatchupDetailView leagueId={data.LEAGUE_ID} detail={h2h()} />));
    expect(screen.getByTestId('starters-header')).toHaveTextContent(/^Starters$/i);
    expect(screen.getByTestId('bench-header')).toHaveTextContent(/^Bench$/i);
    expect(screen.queryByText(/pts unused/)).toBeNull();
    expect(screen.queryByText(/\d-\d-\d · \d-\d-\d/)).toBeNull();
  });

  it('projects auto-subs while games are left, then states them', async () => {
    const live = h2h();
    expect(live.state).toBe('live');
    const view = await render(inApp(<MatchupDetailView leagueId={data.LEAGUE_ID} detail={live} />));
    const lines = () => screen.getByTestId('autosub-summary');
    expect(lines()).toHaveTextContent(/^Auto-subs? projected: .+ → .+/);
    await view.rerender(
      inApp(<MatchupDetailView leagueId={data.LEAGUE_ID} detail={{ ...live, state: 'final' }} />),
    );
    expect(lines()).toHaveTextContent(/^Auto-subs?: .+ → .+/);
    expect(lines()).not.toHaveTextContent(/projected/);
  });
});

describe('points breakdown during live polling', () => {
  it('shows the latest points for the open player, not the ones from when it opened', async () => {
    const summary = data
      .matchupSummaries(data.CURRENT_GW)
      .find((m) => m.home.kind === 'team' && m.away.kind === 'team')!;
    const detail = data.matchupDetail(data.CURRENT_GW, summary.id)!;
    const home = detail.home as Schemas['TeamLineup'];
    const slot = home.starters[0]!;
    const { rerender } = await render(
      inApp(<MatchupDetailView leagueId={data.LEAGUE_ID} detail={detail} />),
    );
    await fireEvent.press(screen.getByTestId(`h2h-breakdown-${slot.player.id}`));
    expect(screen.getByTestId('breakdown-total')).toHaveTextContent(String(slot.points));
    // The next poll: he scores again.
    const polled: Schemas['MatchupDetail'] = {
      ...detail,
      home: {
        ...home,
        starters: home.starters.map((s) =>
          s.player.id === slot.player.id ? { ...s, points: s.points + 5 } : s,
        ),
      },
    };
    await rerender(inApp(<MatchupDetailView leagueId={data.LEAGUE_ID} detail={polled} />));
    expect(screen.getByTestId('breakdown-total')).toHaveTextContent(String(slot.points + 5));
  });
});

describe('double and blank gameweeks', () => {
  it('formats next fixtures', async () => {
    expect(formatNextFixtures([fixture('ARS', 1), fixture('CHE', 2)])).toBe('ARS, CHE');
    expect(formatNextFixtures([fixture('ars', 1)])).toBe('ARS');
    expect(formatNextFixtures([])).toBe('No game');
  });

  const entry = (nextFixtures: Schemas['UpcomingFixture'][]): Schemas['RosterEntry'] => ({
    slot: 3,
    player: {
      id: 'plr_01k7m2xq800000000000000001',
      displayName: 'R. Calder',
      club: club('NOR'),
      position: 'MID',
      availability: 'available',
    },
    totalPoints: 40,
    gameweekPoints: 6,
    acquiredVia: 'draft',
    nextFixtures: nextFixtures.map(toRowFixture),
  });

  it('shows every remaining fixture in one strip, not just three', async () => {
    const codes = ['ARS', 'CHE', 'LIV', 'MCI', 'TOT', 'EVE'];
    await render(<RosterRow entry={entry(codes.map((c, i) => fixture(c, i + 1)))} />);
    // The strip is hidden from assistive tech (the row's label says it).
    const hidden = { includeHiddenElements: true };
    expect(screen.getAllByTestId('fixture-strip-item', hidden)).toHaveLength(6);
    for (const c of codes)
      expect(screen.getByTestId('next-fixtures', hidden)).toHaveTextContent(new RegExp(c));
  });

  it('shows "No game" for a blank gameweek player', async () => {
    await render(<RosterRow entry={entry([])} />);
    expect(screen.getByTestId('next-fixtures')).toHaveTextContent('No game');
  });
});
