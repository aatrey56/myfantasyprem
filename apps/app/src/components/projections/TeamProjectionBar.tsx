import { View } from 'react-native';

import { AppText } from '../AppText';
import { WinChanceBar } from './WinChance';
import { useLeagueProjections } from '@/api/projections';
import { EXPECTED_BASIS, expectedText } from '@/lib/projections';
import { useTheme } from '@/theme';

/**
 * The team card's win/expected bar (Sleeper's projection line): the team's
 * Expected and its opponent's for the gameweek, then the win chance. Hidden
 * once the gameweek is over and before there is anything to show.
 */
export function TeamProjectionBar({
  leagueId,
  teamId,
  gameweek,
}: {
  leagueId: string;
  teamId: string;
  gameweek: number;
}) {
  const { spacing } = useTheme();
  const q = useLeagueProjections(leagueId, gameweek);
  const p = q.data?.data;
  if (!p || p.gameweek !== gameweek) return null;
  const odds = p.matchups.find((m) => m.homeTeamId === teamId || m.awayTeamId === teamId);
  if (odds?.mode === 'final') return null;
  const leftIsHome = odds ? odds.homeTeamId === teamId : true;
  const opponentId = odds ? (leftIsHome ? odds.awayTeamId : odds.homeTeamId) : null;
  const expected = (id: string | null) =>
    id === null ? null : (p.teams.find((t) => t.team.id === id)?.expected ?? null);
  const mine = expected(teamId);
  const theirs = expected(opponentId);
  if (mine === null && !odds?.odds) return null;
  return (
    <View testID="team-projection" style={{ gap: 2, marginTop: spacing.xs }}>
      <AppText
        variant="meta"
        muted
        numberOfLines={1}
        testID="team-expected"
        accessibilityLabel={`Expected ${expectedText(mine)}${
          odds ? `, opponent ${expectedText(theirs)}` : ''
        }, ${EXPECTED_BASIS}, no auto-subs`}
      >
        <AppText variant="meta" weight="semibold" numeric>
          {`Exp ${expectedText(mine)}`}
        </AppText>
        {odds ? ` · Opp ${expectedText(theirs)}` : ''}
        {` · GW ${gameweek}`}
      </AppText>
      <WinChanceBar odds={odds} leftIsHome={leftIsHome} compact testID="team-win-chance" />
    </View>
  );
}
