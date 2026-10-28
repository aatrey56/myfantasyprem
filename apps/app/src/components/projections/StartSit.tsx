import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Card } from '../Card';
import { SectionHeader } from '../SectionHeader';
import { useStartSit } from '@/api/projections';
import { accuracyText } from '@/lib/projections';
import { useTheme } from '@/theme';

const EXPLAIN =
  'Points scored as a share of the best legal XI from the 15 by actual points (1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD).';

/**
 * Start/sit accuracy for one team (Team page): the season figure (the mean
 * of the gameweeks') and each completed gameweek, newest first. Nothing
 * until a gameweek is completed.
 */
export function StartSitCard({ leagueId, teamId }: { leagueId: string; teamId: string }) {
  const { colors, spacing } = useTheme();
  const q = useStartSit(leagueId);
  const row = q.data?.data.find((r) => r.team.id === teamId);
  if (!row || row.gameweeks.length === 0) return null;
  const weeks = [...row.gameweeks].reverse();
  return (
    <View style={{ gap: spacing.xs }}>
      <SectionHeader title="Start/sit accuracy" testID="start-sit-header" />
      <Card testID="start-sit" style={{ gap: spacing.sm }}>
        <View style={{ flexDirection: 'row', alignItems: 'baseline', gap: spacing.sm }}>
          <AppText
            variant="display"
            numeric
            testID="start-sit-season"
            accessibilityLabel={`Season start/sit accuracy ${accuracyText(row.seasonAccuracy)}`}
          >
            {accuracyText(row.seasonAccuracy)}
          </AppText>
          <AppText variant="meta" muted>
            {`season · ${row.gameweeks.length} GW${row.gameweeks.length === 1 ? '' : 's'}`}
          </AppText>
        </View>
        <AppText variant="meta" muted>
          {EXPLAIN}
        </AppText>
        <View>
          {weeks.map((w, i) => (
            <View
              key={w.gameweek}
              testID={`start-sit-gw-${w.gameweek}`}
              accessible
              accessibilityLabel={`Gameweek ${w.gameweek}: ${w.points} of ${w.optimalPoints} possible, ${accuracyText(w.accuracy)}`}
              style={{
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.md,
                paddingVertical: spacing.xs,
                borderTopWidth: i > 0 ? 1 : 0,
                borderTopColor: colors.border,
              }}
            >
              <AppText variant="meta" muted numeric style={{ width: 48 }}>
                {`GW ${w.gameweek}`}
              </AppText>
              <AppText variant="meta" numeric style={{ flex: 1 }}>
                {`${w.points} of ${w.optimalPoints}`}
              </AppText>
              <AppText variant="meta" weight="semibold" numeric>
                {accuracyText(w.accuracy)}
              </AppText>
            </View>
          ))}
        </View>
      </Card>
    </View>
  );
}

/**
 * Every team's start/sit accuracy (League tab), best first: the season
 * figure and the last completed gameweek's.
 */
export function StartSitTable({ leagueId, myTeamId }: { leagueId: string; myTeamId?: string }) {
  const { colors, spacing } = useTheme();
  const q = useStartSit(leagueId);
  const rows = q.data?.data ?? [];
  if (!rows.some((r) => r.gameweeks.length > 0)) return null;
  const lastGw = Math.max(...rows.flatMap((r) => r.gameweeks.map((w) => w.gameweek)));
  return (
    <View style={{ gap: spacing.xs }}>
      <SectionHeader
        title="Start/sit accuracy"
        detail={`season · GW ${lastGw}`}
        testID="league-start-sit-header"
      />
      <Card flush testID="league-start-sit">
        {rows.map((r: Schemas['TeamStartSit'], i) => {
          const last = r.gameweeks.find((w) => w.gameweek === lastGw);
          const mine = r.team.id === myTeamId;
          return (
            <View
              key={r.team.id}
              testID={`start-sit-row-${r.team.id}`}
              accessible
              accessibilityLabel={`${i + 1}. ${r.team.name}: season ${accuracyText(r.seasonAccuracy)}, gameweek ${lastGw} ${accuracyText(last?.accuracy)}`}
              style={{
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.md,
                paddingHorizontal: spacing.lg,
                paddingVertical: spacing.sm,
                borderTopWidth: i > 0 ? 1 : 0,
                borderTopColor: colors.border,
              }}
            >
              <AppText variant="meta" muted numeric style={{ width: 20 }}>
                {String(i + 1)}
              </AppText>
              <AppText
                numberOfLines={1}
                weight={mine ? 'bold' : undefined}
                color={mine ? colors.accent : undefined}
                style={{ flex: 1 }}
              >
                {r.team.name}
              </AppText>
              <AppText variant="meta" muted numeric style={{ width: 44, textAlign: 'right' }}>
                {accuracyText(last?.accuracy)}
              </AppText>
              <AppText weight="semibold" numeric style={{ width: 48, textAlign: 'right' }}>
                {accuracyText(r.seasonAccuracy)}
              </AppText>
            </View>
          );
        })}
        <AppText
          variant="meta"
          muted
          style={{
            paddingHorizontal: 16,
            paddingVertical: 8,
            borderTopWidth: 1,
            borderTopColor: colors.border,
          }}
        >
          {`${EXPLAIN} Season is the mean of the gameweeks.`}
        </AppText>
      </Card>
    </View>
  );
}
