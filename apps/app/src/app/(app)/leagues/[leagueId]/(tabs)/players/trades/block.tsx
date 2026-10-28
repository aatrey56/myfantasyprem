import { useLocalSearchParams } from 'expo-router';

import { useFreshness } from '@/api/freshness';
import { useLeagueContext } from '@/api/hooks';
import { useTradeBlock } from '@/api/trade-block-hooks';
import { Screen } from '@/components/Screen';
import { TradeBlockTable } from '@/components/trades/TradeBlock';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** The league's whole trade block as a table, by team (SLEEPER-OBSERVED). */
export default function TradeBlockScreen() {
  const { leagueId } = useLocalSearchParams<{ leagueId: string }>();
  const home = useLeagueContext(leagueId);
  const block = useTradeBlock(leagueId);
  const freshness = useFreshness(block);
  return (
    <Screen
      title="Trade Block"
      back={{ label: 'Trades', href: `/leagues/${leagueId}/players/trades` }}
      freshness={freshness}
      maxWidth={720}
    >
      <TradeBlockTable leagueId={leagueId} myTeamId={home.data?.data.viewer.team?.id} />
    </Screen>
  );
}
