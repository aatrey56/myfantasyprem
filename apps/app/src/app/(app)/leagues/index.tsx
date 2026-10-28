import type { Schemas } from '@myfantasyprem/api-client';
import { useRouter, type Href } from 'expo-router';
import { useState, type ReactNode } from 'react';
import { View } from 'react-native';

import { useMyLeagues } from '@/api/account-hooks';
import { useFreshness, useNow } from '@/api/freshness';
import { usePressPrefetch } from '@/api/prefetch';
import { useAuth } from '@/auth/AuthProvider';
import { AppText } from '@/components/AppText';
import { Avatar } from '@/components/Badges';
import { Button, IconButton } from '@/components/Button';
import { Card } from '@/components/Card';
import { Pill } from '@/components/Chip';
import { EmptyState } from '@/components/EmptyState';
import { ChevronRight, Download, Plus, Trophy, UserPlus } from '@/components/icons';
import { JoinLeagueSheet } from '@/components/JoinLeagueSheet';
import { pressFeedback, type PressState } from '@/components/pressable';
import { PressableLink } from '@/components/PressableLink';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { Skeleton } from '@/components/Skeleton';
import {
  leagueHref,
  leagueStatus,
  myMatchup,
  ordinal,
  recordText,
  sortMyLeagues,
} from '@/lib/leagues';
import { formatDateTime, formatRelative } from '@/lib/time';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Home after sign-in: every league I'm in, plus create and join. */
export default function LeaguesHubScreen() {
  const { user } = useAuth();
  const router = useRouter();
  const leagues = useMyLeagues();
  const freshness = useFreshness(leagues);
  const { spacing, colors } = useTheme();
  const [joining, setJoining] = useState(false);

  return (
    <Screen
      title="Leagues"
      documentTitle="Your leagues"
      freshness={freshness}
      maxWidth={760}
      left={
        user ? (
          <PressableLink
            href="/account"
            accessibilityLabel="Account"
            testID="account-link"
            style={{ borderRadius: 999 }}
          >
            <Avatar name={user.displayName} color={user.avatarColor} size={40} />
          </PressableLink>
        ) : undefined
      }
      actions={
        <>
          <IconButton
            icon={UserPlus}
            label="Join a league"
            onPress={() => setJoining(true)}
            testID="join-league"
          />
          <IconButton
            icon={Plus}
            label="Create a league"
            variant="accent"
            onPress={() => router.push('/leagues/new')}
            testID="create-league"
          />
        </>
      }
    >
      <QueryStatus
        query={leagues}
        loading={
          <View style={{ gap: spacing.md }} testID="loading">
            {[0, 1, 2].map((i) => (
              <Card key={i}>
                <View style={{ flexDirection: 'row', gap: spacing.md, alignItems: 'center' }}>
                  <Skeleton width={44} height={44} radius={12} />
                  <View style={{ flex: 1, gap: spacing.xs }}>
                    <Skeleton width="50%" height={16} />
                    <Skeleton width="30%" height={12} />
                  </View>
                </View>
              </Card>
            ))}
          </View>
        }
      >
        {({ data, meta }) =>
          data.length === 0 ? (
            <Card>
              <EmptyState
                icon={Trophy}
                title="No leagues yet"
                body="Start a league and invite your friends, or join one with an invite code."
                testID="leagues-empty"
              >
                <Button
                  label="Create a league"
                  icon={Plus}
                  onPress={() => router.push('/leagues/new')}
                />
                <Button
                  label="Join with a code"
                  variant="secondary"
                  icon={UserPlus}
                  onPress={() => setJoining(true)}
                />
              </EmptyState>
            </Card>
          ) : (
            <View style={{ gap: spacing.md }} testID="league-list">
              {sortMyLeagues(data).map((row) => (
                <LeagueCard key={row.league.id} row={row} live={meta.scoreState === 'live'} />
              ))}
            </View>
          )
        }
      </QueryStatus>

      <SectionHeader title="Get playing" />
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.md }}>
        <ActionCard
          title="Create a league"
          body="2 to 20 teams, snake draft, head to head."
          icon={<Plus size={20} color={colors.accent} />}
          onPress={() => router.push('/leagues/new')}
          testID="create-league-card"
        />
        <ActionCard
          title="Join a league"
          body="Got an invite code or link? Enter it here."
          icon={<UserPlus size={20} color={colors.accent} />}
          onPress={() => setJoining(true)}
          testID="join-league-card"
        />
        <ActionCard
          title="Import a league"
          body="Bring your FPL Draft league over."
          icon={<Download size={20} color={colors.accent} />}
          onPress={() => router.push('/import')}
          testID="import-league-card"
        />
      </View>

      <JoinLeagueSheet visible={joining} onClose={() => setJoining(false)} />
    </Screen>
  );
}

function ActionCard({
  title,
  body,
  icon,
  onPress,
  testID,
}: {
  title: string;
  body: string;
  icon: ReactNode;
  onPress: () => void;
  testID: string;
}) {
  const { spacing, colors } = useTheme();
  return (
    <Card
      onPress={onPress}
      accessibilityLabel={title}
      testID={testID}
      style={{
        flexGrow: 1,
        flexBasis: 260,
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.md,
      }}
    >
      {icon}
      <View style={{ flex: 1 }}>
        <AppText weight="semibold">{title}</AppText>
        <AppText variant="meta" muted>
          {body}
        </AppText>
      </View>
      <ChevronRight size={18} color={colors.textMuted} aria-hidden />
    </Card>
  );
}

/** One league: badge, name, status, then the line that matters for its stage. */
function LeagueCard({ row, live }: { row: Schemas['MyLeague']; live: boolean }) {
  const { colors, spacing, radii } = useTheme();
  const now = useNow(30_000);
  const status = leagueStatus(row.league.status);
  const matchup = row.currentMatchup ? myMatchup(row.currentMatchup, row.team.id) : null;
  const href = leagueHref(row) as Href;
  const prefetch = usePressPrefetch();

  let detail: string;
  switch (row.league.status) {
    case 'pre_draft':
      detail = `${row.league.memberCount} of ${row.league.teamCount} teams · ${
        row.draftStartsAt
          ? `draft ${formatRelative(Date.parse(row.draftStartsAt), now)}`
          : 'draft not scheduled'
      }`;
      break;
    case 'drafting':
      detail = 'Draft in progress · tap to enter the draft room';
      break;
    case 'in_season':
      detail = row.record
        ? `${recordText(row.record)} · ${ordinal(row.record.rank)} of ${row.league.teamCount}`
        : `${row.league.teamCount} teams`;
      break;
    default:
      detail = `${row.league.teamCount} teams`;
  }

  return (
    <PressableLink
      href={href}
      accessibilityLabel={`${row.league.name}, ${status.label}, ${row.team.name}. ${detail}`}
      testID={`league-${row.league.id}`}
      onPressIn={() => prefetch.league(row.league.id, row.league.status)}
      style={(state: PressState) => [
        {
          backgroundColor: colors.surface,
          borderColor: row.league.status === 'drafting' ? colors.accent : colors.border,
          borderWidth: 1,
          borderRadius: radii.lg,
          padding: spacing.lg,
          gap: spacing.md,
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        <Avatar name={row.league.name} size={44} square />
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <AppText variant="heading" numberOfLines={1} testID="league-row-name">
            {row.league.name}
          </AppText>
          <AppText variant="meta" muted numberOfLines={1}>
            {`${row.team.name}${row.role === 'commissioner' ? ' · Commissioner' : ''}`}
          </AppText>
        </View>
        <Pill label={status.label} tone={status.tone} dot={row.league.status === 'drafting'} />
      </View>
      <AppText variant="meta" muted numberOfLines={2}>
        {detail}
      </AppText>
      {matchup ? (
        <View
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            gap: spacing.md,
            paddingTop: spacing.md,
            borderTopWidth: 1,
            borderTopColor: colors.border,
          }}
        >
          <View style={{ flex: 1, minWidth: 0 }}>
            <AppText variant="eyebrow" accent={live && matchup.state === 'live'} subtle={!live}>
              {`GW ${matchup.gameweek}${matchup.state === 'live' ? ' · Live' : ''}`}
            </AppText>
            <AppText variant="meta" muted numberOfLines={1}>
              {`vs ${matchup.opponentName}`}
            </AppText>
          </View>
          <AppText variant="heading" numeric testID="league-row-score">
            <AppText
              variant="heading"
              numeric
              color={matchup.myPoints >= matchup.opponentPoints ? colors.text : colors.textMuted}
            >
              {matchup.myPoints}
            </AppText>
            <AppText variant="heading" muted>
              {' – '}
            </AppText>
            <AppText
              variant="heading"
              numeric
              color={matchup.opponentPoints > matchup.myPoints ? colors.text : colors.textMuted}
            >
              {matchup.opponentPoints}
            </AppText>
          </AppText>
        </View>
      ) : row.league.status === 'pre_draft' && row.draftStartsAt ? (
        <AppText variant="meta" subtle>
          {`Draft: ${formatDateTime(Date.parse(row.draftStartsAt))}`}
        </AppText>
      ) : null}
    </PressableLink>
  );
}
