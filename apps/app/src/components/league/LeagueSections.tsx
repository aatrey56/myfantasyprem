import type { Schemas } from '@myfantasyprem/api-client';
import { useMemo, useState } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar } from '../Badges';
import { Card } from '../Card';
import { Pill } from '../Chip';
import { EmptyState } from '../EmptyState';
import { Activity, Calendar, Crown, LayoutGrid } from '../icons';
import { ListRow } from '../ListRow';
import { MatchupList } from '../matchup/MatchupList';
import { oddsById } from '../projections/WinChance';
import { QueryStatus } from '../QueryStatus';
import { SectionHeader } from '../SectionHeader';
import { GameweekSwitcher } from '../team/GameweekSwitcher';
import { useMatchups, useSchedule } from '@/api/hooks';
import { useLeagueProjections } from '@/api/projections';
import { useMembers } from '@/api/league-admin-hooks';
import { useLayout } from '@/hooks/useLayout';
import { findMyMatchup, leagueGameweek, matchupUpcoming, type AnySide } from '@/lib/matchup';
import { formatMonthYear } from '@/lib/time';
import { waiverOrderText } from '@/lib/waivers';
import { useTheme } from '@/theme';

/**
 * The League tab's opening section, as in Sleeper: a "Matchups" heading
 * with the `‹ GW 7 ›` stepper, then one compact hero card per matchup, mine
 * first. The league's current gameweek comes from the league home (live
 * scores), earlier ones from their matchups, and later ones (or the current
 * one before its deadline) as pairings from the schedule. Two cards a row on wide screens.
 */
export function LeagueMatchups({
  leagueId,
  home,
}: {
  leagueId: string;
  home: Schemas['LeagueHome'];
}) {
  const { spacing } = useTheme();
  const { isWide } = useLayout();
  const current = home.currentGameweek.number;
  const [gw, setGw] = useState(() => leagueGameweek(home));
  const myTeamId = home.viewer.team?.id;
  // Before its deadline the current gameweek is a pairing, like any later one.
  const upcoming = matchupUpcoming(home, gw);
  const past = gw < current;
  const scores = useMatchups(leagueId, past ? gw : undefined);
  const schedule = useSchedule(leagueId, upcoming);
  // Win chance before and during the gameweek (a past one shows its result).
  const projections = useLeagueProjections(leagueId, past ? undefined : gw);
  const odds = useMemo(() => oddsById(projections.data?.data), [projections.data]);
  const list = (matchups: readonly (Schemas['MatchupSummary'] | Schemas['ScheduledMatchup'])[]) =>
    matchups.length > 0 ? (
      <MatchupList
        leagueId={leagueId}
        matchups={mineFirst(matchups, myTeamId)}
        myTeamId={myTeamId}
        columns={isWide ? 2 : 1}
        odds={odds}
        testID="league-matchups"
      />
    ) : (
      <Card>
        <EmptyState icon={Calendar} title={`No matchups in GW ${gw}`} />
      </Card>
    );
  return (
    <View style={{ gap: spacing.xs }}>
      <SectionHeader
        title="Matchups"
        testID="league-matchups-header"
        right={
          <GameweekSwitcher
            gameweek={gw}
            min={home.settings.startGameweek}
            max={home.settings.endGameweek}
            onChange={setGw}
          />
        }
      />
      {gw === current && !upcoming ? (
        list(home.matchups)
      ) : past ? (
        <QueryStatus query={scores}>{({ data }) => list(data)}</QueryStatus>
      ) : (
        <QueryStatus query={schedule}>
          {({ data }) => list(data.find((w) => w.gameweek === gw)?.matchups ?? [])}
        </QueryStatus>
      )}
    </View>
  );
}

/** My matchup at the top, the rest in their order. */
function mineFirst<M extends { home: AnySide; away: AnySide }>(
  matchups: readonly M[],
  myTeamId: string | undefined,
): M[] {
  const mine = findMyMatchup(matchups, myTeamId);
  return mine ? [mine, ...matchups.filter((m) => m !== mine)] : [...matchups];
}

/**
 * Small links at the foot of the League tab: the activity feed (trades,
 * waivers and moves) and the finished draft's results.
 */
export function LeagueLinks({ leagueId }: { leagueId: string }) {
  const { colors } = useTheme();
  return (
    <Card flush testID="league-links">
      <ListRow
        href={`/leagues/${leagueId}/activity?from=league`}
        left={<Activity size={18} color={colors.textMuted} aria-hidden />}
        title="Activity"
        subtitle="Trades, waivers and squad moves"
        accessibilityLabel="Activity"
        chevron
        testID="link-activity"
      />
      <ListRow
        divider
        href={`/leagues/${leagueId}/draft`}
        left={<LayoutGrid size={18} color={colors.textMuted} aria-hidden />}
        title="Draft results"
        accessibilityLabel="Draft results"
        chevron
        testID="link-draft-results"
      />
    </Card>
  );
}

const REVIEW: Record<string, string> = {
  none: 'Trades go through when accepted',
  commissioner: 'The commissioner reviews trades',
  league_vote: 'The league votes on trades',
};

/** The league's rules at a glance, and its members. */
export function LeagueInfo({ leagueId, home }: { leagueId: string; home: Schemas['LeagueHome'] }) {
  const { spacing, colors } = useTheme();
  const members = useMembers(leagueId);
  const s = home.settings;
  const facts: [string, string][] = [
    ['Season', `${home.league.season} · GW ${s.startGameweek}–${s.endGameweek}`],
    ['Teams', `${s.teamCount}${s.teamCount % 2 === 1 ? ' (one plays the league average)' : ''}`],
    ['Scoring', 'Standard points, head to head'],
    [
      'Squad',
      `${s.roster.GKP} GK · ${s.roster.DEF} DEF · ${s.roster.MID} MID · ${s.roster.FWD} FWD`,
    ],
    ['Waivers', `${waiverOrderText(s.waivers.order).label}; runs 24 hours before each deadline`],
    [
      'Trades',
      `${REVIEW[s.trades.review] ?? 'Trade review'}${
        s.trades.deadlineGameweek ? ` · deadline GW ${s.trades.deadlineGameweek}` : ''
      }`,
    ],
  ];
  return (
    <View style={{ gap: spacing.md }} testID="league-info">
      <SectionHeader title="League" />
      <Card flush>
        {facts.map(([label, value], i) => (
          <View
            key={label}
            style={{
              gap: 2,
              paddingHorizontal: spacing.lg,
              paddingVertical: spacing.md,
              borderTopWidth: i > 0 ? 1 : 0,
              borderTopColor: colors.border,
            }}
          >
            <AppText variant="eyebrow" muted>
              {label}
            </AppText>
            <AppText>{value}</AppText>
          </View>
        ))}
      </Card>
      <SectionHeader
        title="Members"
        detail={`${home.league.memberCount} of ${home.league.teamCount}`}
      />
      <QueryStatus query={members}>
        {({ data }) => (
          <Card flush testID="members">
            {data.map((m, i) => (
              <ListRow
                key={m.user.id}
                divider={i > 0}
                href={`/leagues/${leagueId}/team/${m.team.id}`}
                left={<Avatar name={m.user.displayName} size={32} />}
                title={m.user.displayName}
                subtitle={`${m.team.name} · joined ${formatMonthYear(Date.parse(m.joinedAt))}`}
                right={
                  m.role === 'commissioner' ? (
                    <Pill label="Commissioner" tone="accent" icon={Crown} />
                  ) : undefined
                }
                chevron
              />
            ))}
          </Card>
        )}
      </QueryStatus>
    </View>
  );
}
