import type { Schemas } from '@myfantasyprem/api-client';
import { fireEvent, render, screen, within } from '@testing-library/react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { confirmCopy, TradeCard, tradeDirection } from './TradeCard';
import * as data from '@/mocks/data';

const office = () =>
  data.leagues.get(data.OFFICE_LEAGUE_ID) as Extract<
    data.MockLeague,
    { listTrades: (status?: string | null) => Schemas['Trade'][] }
  >;

const inApp = (node: React.ReactNode) => (
  <SafeAreaProvider
    initialMetrics={{
      frame: { x: 0, y: 0, width: 375, height: 812 },
      insets: { top: 0, left: 0, right: 0, bottom: 0 },
    }}
  >
    {node}
  </SafeAreaProvider>
);

beforeEach(() => data.resetMockLeagues());

/** The Office League's trade in a league vote; the demo user is its commissioner. */
const inReview = () => office().listTrades('in_review')[0]!;

describe('TradeCard review', () => {
  it('labels member votes "Veto" and "Uphold", never "Vote …" or "Allow"', async () => {
    const trade = inReview();
    const onAction = jest.fn();
    await render(inApp(<TradeCard trade={trade} now={Date.now()} onAction={onAction} />));
    const veto = screen.getByTestId('trade-vote-veto');
    const uphold = screen.getByTestId('trade-vote-uphold');
    expect(veto).toHaveTextContent('Veto', { exact: true });
    expect(uphold).toHaveTextContent('Uphold', { exact: true });
    expect(screen.queryByText(/vote veto|vote uphold|allow/i)).toBeNull();
    expect(screen.getByTestId('trade-review')).toHaveTextContent(/1 uphold/);
    await fireEvent.press(uphold);
    expect(onAction).toHaveBeenCalledWith(trade, { kind: 'vote', vote: 'allow' });
  });

  it('marks my vote without changing the label', async () => {
    const trade = inReview();
    const voted = { ...trade, review: { ...trade.review!, myVote: 'allow' as const } };
    await render(inApp(<TradeCard trade={voted} now={Date.now()} onAction={jest.fn()} />));
    expect(screen.getByTestId('trade-vote-uphold')).toHaveTextContent('Uphold', { exact: true });
    expect(screen.getByLabelText('Uphold, your vote')).toBeDisabled();
    expect(screen.getByTestId('trade-review')).toHaveTextContent(/you voted uphold/);
  });

  it('sets the commissioner actions apart, labelled "Veto" and "Uphold"', async () => {
    const trade = inReview();
    const onAction = jest.fn();
    await render(inApp(<TradeCard trade={trade} now={Date.now()} onAction={onAction} />));
    const panel = screen.getByTestId('commissioner-actions');
    expect(panel).toHaveTextContent(/^Commissioner/);
    expect(within(panel).getByTestId('trade-veto')).toHaveTextContent('Veto', { exact: true });
    const uphold = within(panel).getByTestId('trade-uphold');
    expect(uphold).toHaveTextContent('Uphold', { exact: true });
    // Member votes live outside the commissioner panel.
    expect(within(panel).queryByTestId('trade-vote-veto')).toBeNull();
    await fireEvent.press(uphold);
    expect(onAction).toHaveBeenCalledWith(trade, { kind: 'review', decision: 'approve' });
  });

  it('hides the commissioner panel from a member', async () => {
    const trade = inReview();
    const member = { ...trade, viewerActions: ['vote'] as Schemas['TradeAction'][] };
    await render(inApp(<TradeCard trade={member} now={Date.now()} onAction={jest.fn()} />));
    expect(screen.queryByTestId('commissioner-actions')).toBeNull();
    expect(screen.getByTestId('trade-vote-uphold')).toBeOnTheScreen();
  });

  it('confirms with "Uphold" copy', () => {
    const trade = inReview();
    expect(confirmCopy(trade, { kind: 'vote', vote: 'allow' })).toMatchObject({
      title: 'Vote to uphold?',
      confirm: 'Uphold',
    });
    expect(confirmCopy(trade, { kind: 'review', decision: 'approve' })).toMatchObject({
      title: 'Uphold this trade now?',
      confirm: 'Uphold',
    });
    expect(confirmCopy(trade, { kind: 'review', decision: 'veto' }).confirm).toBe('Veto');
  });
});

describe('TradeCard times', () => {
  it('says how long ago the offer was made and how long the review has left', async () => {
    const trade = inReview();
    const now = Date.parse('2026-10-13T08:00:00Z');
    const timed = {
      ...trade,
      createdAt: new Date(now - 2 * 3600_000 - 10 * 60_000).toISOString(),
      review: { ...trade.review!, endsAt: new Date(now + 45 * 60_000 + 30_000).toISOString() },
    };
    await render(inApp(<TradeCard trade={timed} now={now} onAction={jest.fn()} />));
    expect(screen.getByTestId('trade-offered-at')).toHaveTextContent('2h ago', { exact: true });
    // The screen refreshes every 30 s: minutes, never m:ss.
    expect(screen.getByTestId('trade-review')).toHaveTextContent(/^Review ends in 45m/);
  });
});

describe('TradeCard pending on FPL', () => {
  const pendingLeague = () =>
    data.leagues.get(data.PENDING_LEAGUE_ID) as Extract<
      data.MockLeague,
      { listTrades: (status?: string | null) => Schemas['Trade'][] }
    >;

  it('shows a trade FPL accepted as pending there, with both sides and no actions', async () => {
    const trade = pendingLeague()
      .listTrades('in_review')
      .find((t) => t.review?.endsAt === null)!;
    const myTeamId = pendingLeague().myTeam.id;
    await render(
      inApp(<TradeCard trade={trade} myTeamId={myTeamId} now={Date.now()} onAction={jest.fn()} />),
    );
    expect(within(screen.getByTestId('trade-status')).getByText('Pending on FPL')).toBeTruthy();
    expect(screen.getByTestId('trade-pending-fpl')).toBeTruthy();
    expect(screen.queryByTestId('trade-review')).toBeNull();
    expect(trade.offered).toHaveLength(3);
    expect(trade.requested).toHaveLength(3);
    for (const p of [...trade.offered, ...trade.requested])
      expect(screen.getByText(p.displayName)).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});

describe('TradeCard direction', () => {
  const teams = {
    offeringTeam: { id: 'tm_a', name: 'A', managerName: 'Ann' },
    receivingTeam: { id: 'tm_b', name: 'B', managerName: 'Bo' },
  };

  it('is incoming for the receiving team, outgoing for the offering team, else none', () => {
    expect(tradeDirection(teams, 'tm_b')).toBe('incoming');
    expect(tradeDirection(teams, 'tm_a')).toBe('outgoing');
    expect(tradeDirection(teams, 'tm_c')).toBeNull();
    expect(tradeDirection(teams, undefined)).toBeNull();
  });

  it('tags an offer to me as incoming and shows both sides by name', async () => {
    const trade = office().listTrades('offered')[0]!;
    await render(
      inApp(
        <TradeCard
          trade={trade}
          myTeamId={trade.receivingTeam.id}
          now={Date.now()}
          onAction={jest.fn()}
        />,
      ),
    );
    expect(screen.getByTestId('trade-direction')).toHaveTextContent(/incoming/i);
    expect(screen.getByTestId('trade-title')).toHaveTextContent(
      `${trade.offeringTeam.name} offered you`,
    );
    for (const p of trade.offered)
      expect(within(screen.getByTestId('trade-side-get')).getByText(p.displayName)).toBeTruthy();
    for (const p of trade.requested)
      expect(within(screen.getByTestId('trade-side-give')).getByText(p.displayName)).toBeTruthy();
  });
});
