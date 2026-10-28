import type { Schemas } from '@myfantasyprem/api-client';
import { useRouter } from 'expo-router';
import { View } from 'react-native';

import { AppText } from './AppText';
import { Avatar } from './Badges';
import { Button } from './Button';
import { Card } from './Card';
import { Pill } from './Chip';
import { LayoutGrid, Settings, UserPlus } from './icons';
import { useNow } from '@/api/freshness';
import { startExplanation } from '@/lib/draft';
import { leagueStatus } from '@/lib/leagues';
import { formatDateTime, formatRelative } from '@/lib/time';
import { useTheme } from '@/theme';

/**
 * Before the season: who has joined, when the draft starts, and the way
 * into the draft room (or, for the commissioner, the invite).
 */
export function PreseasonPanel({
  leagueId,
  home,
}: {
  leagueId: string;
  home: Schemas['LeagueHome'];
}) {
  const router = useRouter();
  const now = useNow(30_000);
  const { spacing, colors } = useTheme();
  const st = leagueStatus(home.league.status);
  const startsAt = home.settings.draft.startsAt;
  const open = home.league.teamCount - home.league.memberCount;
  const drafting = home.league.status === 'drafting';
  return (
    <Card
      testID="preseason-panel"
      style={{ gap: spacing.md, borderColor: drafting ? colors.accent : colors.border }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <Pill label={st.label} tone={st.tone} dot={drafting} />
        <AppText variant="meta" muted>
          {`${home.league.memberCount} of ${home.league.teamCount} teams`}
        </AppText>
      </View>
      <AppText variant="heading">
        {drafting
          ? 'The draft is live'
          : startsAt
            ? `Draft ${formatDateTime(Date.parse(startsAt))}`
            : 'Draft not scheduled yet'}
      </AppText>
      {!drafting && startsAt && Date.parse(startsAt) > now ? (
        <AppText variant="display" numeric accent>
          {formatRelative(Date.parse(startsAt), now)}
        </AppText>
      ) : null}
      {!drafting && open > 0 ? (
        <AppText muted testID="preseason-start-note">
          {startExplanation(home.league)}
        </AppText>
      ) : null}
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}>
        {home.standings.map((row) => (
          <View key={row.team.id} accessibilityLabel={`${row.team.name}, ${row.team.managerName}`}>
            <Avatar name={row.team.managerName} size={28} />
          </View>
        ))}
      </View>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}>
        <Button
          label={drafting ? 'Enter draft room' : 'Draft room'}
          icon={LayoutGrid}
          variant={drafting ? 'primary' : 'secondary'}
          onPress={() => router.push(`/leagues/${leagueId}/draft`)}
          testID="enter-draft-room"
        />
        {home.viewer.role === 'commissioner' && !drafting ? (
          <Button
            label="Invite managers"
            icon={UserPlus}
            onPress={() => router.push(`/leagues/${leagueId}/settings`)}
            testID="invite-managers"
          />
        ) : (
          <Button
            label="League settings"
            icon={Settings}
            variant="ghost"
            onPress={() => router.push(`/leagues/${leagueId}/settings`)}
          />
        )}
      </View>
    </Card>
  );
}
