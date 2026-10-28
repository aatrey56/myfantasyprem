import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { ODDS_BASIS, oddsFor, percentText } from '@/lib/projections';
import { useTheme } from '@/theme';

/**
 * Win chance as Sleeper draws it: the left side's percent, a bar split by
 * the two chances (a draw in the middle), the right side's percent, and
 * the label saying where the numbers come from. Nothing once the gameweek
 * is over (the result is on screen) or when there are no odds yet.
 */
export function WinChanceBar({
  odds,
  leftIsHome,
  compact,
  testID = 'win-chance',
}: {
  odds: Schemas['MatchupOdds'] | undefined;
  leftIsHome: boolean;
  /** Cards: a thinner bar. */
  compact?: boolean;
  testID?: string;
}) {
  const { colors, spacing, radii } = useTheme();
  if (!odds?.odds || odds.mode === 'final') return null;
  const { left, right, draw } = oddsFor(odds.odds, leftIsHome);
  const live = odds.mode === 'live';
  const label = `Win chance ${percentText(left)} to ${percentText(right)}${
    draw >= 0.005 ? `, draw ${percentText(draw)}` : ''
  }${live ? ', live' : ''}. ${ODDS_BASIS}.`;
  const height = compact ? 4 : 6;
  return (
    <View testID={testID} accessible accessibilityLabel={label} style={{ gap: 2 }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText
          numeric
          weight="bold"
          variant="meta"
          testID={`${testID}-left`}
          color={left >= right ? colors.text : colors.textMuted}
        >
          {percentText(left)}
        </AppText>
        <View
          style={{
            flex: 1,
            flexDirection: 'row',
            height,
            borderRadius: radii.pill,
            overflow: 'hidden',
            backgroundColor: colors.surfaceSunken,
            gap: 2,
          }}
        >
          <View style={{ flex: left, backgroundColor: colors.accent }} />
          {draw > 0 ? <View style={{ flex: draw, backgroundColor: colors.textSubtle }} /> : null}
          <View style={{ flex: right, backgroundColor: colors.surfaceSelected }} />
        </View>
        <AppText
          numeric
          weight="bold"
          variant="meta"
          testID={`${testID}-right`}
          color={right > left ? colors.text : colors.textMuted}
        >
          {percentText(right)}
        </AppText>
      </View>
      <AppText
        variant="meta"
        subtle
        testID={`${testID}-basis`}
        style={{ fontSize: 11, lineHeight: 14, textAlign: 'center' }}
      >
        {`Win %${draw >= 0.005 ? ` · draw ${percentText(draw)}` : ''} · ${ODDS_BASIS}`}
      </AppText>
    </View>
  );
}

/** The matchup's odds, if loaded, by id. */
export function oddsById(
  p: Schemas['LeagueProjections'] | undefined,
): ReadonlyMap<string, Schemas['MatchupOdds']> {
  return new Map((p?.matchups ?? []).map((m) => [m.matchupId, m]));
}
