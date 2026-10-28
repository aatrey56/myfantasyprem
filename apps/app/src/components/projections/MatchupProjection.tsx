import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { WinChanceBar } from './WinChance';
import { EXPECTED_BASIS, expectedText } from '@/lib/projections';
import { useTheme } from '@/theme';

/**
 * Under a matchup's header: each side's Expected points and the win
 * chance (ADR 0014). Expected sums the XI as named (no auto-subs); the
 * league average has none. Nothing when the projections have not loaded.
 */
export function MatchupProjection({
  projections,
  matchupId,
  leftTeamId,
  rightTeamId,
  leftIsHome,
}: {
  projections: Schemas['LeagueProjections'] | undefined;
  matchupId: string;
  /** Null for the league-average side. */
  leftTeamId: string | null;
  rightTeamId: string | null;
  leftIsHome: boolean;
}) {
  const { spacing } = useTheme();
  if (!projections) return null;
  const odds = projections.matchups.find((m) => m.matchupId === matchupId);
  const exp = (id: string | null) =>
    id === null ? null : (projections.teams.find((t) => t.team.id === id)?.expected ?? null);
  const left = exp(leftTeamId);
  const right = exp(rightTeamId);
  const showExpected = odds?.mode !== 'final' && (left !== null || right !== null);
  if (!showExpected && (!odds?.odds || odds.mode === 'final')) return null;
  return (
    <View testID="matchup-projection" style={{ gap: spacing.xs }}>
      {showExpected ? (
        <View
          accessible
          accessibilityLabel={`Expected ${expectedText(left)} to ${expectedText(right)}, ${EXPECTED_BASIS}, no auto-subs`}
          style={{ flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm }}
        >
          <AppText numeric weight="semibold" testID="expected-left">
            {expectedText(left)}
          </AppText>
          <AppText variant="meta" muted style={{ flex: 1, textAlign: 'center' }}>
            {`Exp · ${EXPECTED_BASIS}`}
          </AppText>
          <AppText numeric weight="semibold" testID="expected-right">
            {expectedText(right)}
          </AppText>
        </View>
      ) : null}
      <WinChanceBar odds={odds} leftIsHome={leftIsHome} />
    </View>
  );
}
