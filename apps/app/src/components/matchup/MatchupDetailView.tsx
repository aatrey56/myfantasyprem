import type { Schemas } from '@myfantasyprem/api-client';
import { useState, type ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar } from '../Badges';
import { Card } from '../Card';
import { PlayerRow } from '../PlayerRow';
import { MatchupProjection } from '../projections/MatchupProjection';
import { usePlayerCard } from '../players/PlayerCardContext';
import { SectionHeader } from '../SectionHeader';
import { BreakdownPoints, H2HLineups } from './H2HLineups';
import { PointsBreakdownSheet } from './PointsBreakdownSheet';
import { ScoreHeader, ToPlayRow } from './ScoreHeader';
import { useLayout } from '@/hooks/useLayout';
import { autoSubSummary, orient, type TeamRecord } from '@/lib/matchup';
import { useTheme } from '@/theme';

type Slot = Schemas['LineupSlot'];

interface Props {
  leagueId: string;
  detail: Schemas['MatchupDetail'];
  myTeamId?: string;
  records?: ReadonlyMap<string, TeamRecord>;
  /** The gameweek stepper, on the right of the starters heading. */
  stepper?: ReactNode;
  /** The gameweek's Expected and win odds (ADR 0014), under the header. */
  projections?: Schemas['LeagueProjections'];
  /** The Match tab draws the score header in its carousel: only the yet-to-play row here. */
  hideHeader?: boolean;
}

/**
 * A matchup: the score header, then both lineups side by side, or, against
 * the league average, one card with its score and how it is worked out
 * next to my lineup. A player opens their card; their points open the points
 * breakdown.
 */
export function MatchupDetailView({
  leagueId,
  detail,
  myTeamId,
  records,
  stepper,
  projections,
  hideHeader,
}: Props) {
  const { spacing } = useTheme();
  // Live polling replaces the detail: look the open player up in the
  // current one so the breakdown shows the latest points.
  const [picked, setPicked] = useState<{ slot: Slot; team: string } | null>(null);
  const { left, right } = orient(detail, myTeamId);
  const pick = (slot: Slot, team: string) => setPicked({ slot, team });
  const current = picked ? (findSlot(detail, picked.slot.player.id) ?? picked.slot) : null;

  let body;
  if (left.kind === 'team' && right.kind === 'team') {
    body = (
      <H2HLineups left={left} right={right} state={detail.state} onPick={pick} stepper={stepper} />
    );
  } else if (left.kind === 'team' && right.kind === 'average') {
    body = (
      <AverageMatchup
        team={left}
        average={right}
        state={detail.state}
        onPick={pick}
        stepper={stepper}
      />
    );
  } else {
    body = (
      <Card>
        <AppText muted>This matchup can&apos;t be shown in this version of the app.</AppText>
      </Card>
    );
  }

  return (
    <View style={{ gap: spacing.lg }} testID="matchup-detail">
      {hideHeader ? (
        <ToPlayRow left={left} right={right} />
      ) : (
        <ScoreHeader
          leagueId={leagueId}
          left={left}
          right={right}
          state={detail.state}
          gameweek={detail.gameweek}
          records={records}
        />
      )}
      <MatchupProjection
        projections={projections}
        matchupId={detail.id}
        leftTeamId={left.kind === 'team' ? left.team.id : null}
        rightTeamId={right.kind === 'team' ? right.team.id : null}
        leftIsHome={left === detail.home}
      />
      {body}
      <PointsBreakdownSheet
        slot={current}
        teamName={picked?.team}
        onClose={() => setPicked(null)}
      />
    </View>
  );
}

/** The player's slot in this version of the detail, on either side. */
function findSlot(detail: Schemas['MatchupDetail'], playerId: string): Slot | undefined {
  for (const side of [detail.home, detail.away]) {
    if (side.kind !== 'team') continue;
    const slot = [...side.starters, ...side.bench].find((s) => s.player.id === playerId);
    if (slot) return slot;
  }
  return undefined;
}

function AverageMatchup({
  team,
  average,
  state,
  onPick,
  stepper,
}: {
  team: Schemas['TeamLineup'];
  average: Schemas['AverageOpponent'];
  state: string;
  onPick: (slot: Slot, team: string) => void;
  stepper?: ReactNode;
}) {
  const { spacing, colors, radii } = useTheme();
  const { isDesktop } = useLayout();
  const openCard = usePlayerCard();
  const card = (
    <Card testID="average-opponent" style={{ gap: spacing.md, flex: isDesktop ? 1 : undefined }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        <Avatar name="League Average" size={40} square />
        <AppText variant="heading" style={{ flex: 1 }}>
          League average
        </AppText>
        <AppText variant="display" numeric testID="average-points">
          {String(average.points)}
        </AppText>
      </View>
      <AppText variant="meta" muted>
        This league has an odd number of teams, so each gameweek one team plays the league average
        instead of a manager. It scores the average of every team&apos;s points this gameweek, its
        opponent included, rounded down. Win, draw or loss count as normal.
      </AppText>
    </Card>
  );
  const rows = (zone: Slot[], bench: boolean) =>
    zone.map((s, i) => (
      <PlayerRow
        key={s.player.id}
        testID={`h2h-player-${s.player.id}`}
        player={s.player}
        fixtures={s.fixtures}
        autoSub={s.autoSub}
        divider={i > 0}
        dimmed={bench}
        // The row opens the card; the points open the breakdown.
        onOpenCard={openCard ? () => openCard(s.player.id) : undefined}
        trailing={<BreakdownPoints slot={s} onPress={() => onPick(s, team.team.name)} />}
        leading={
          bench ? (
            <AppText variant="meta" subtle numeric style={{ width: 14 }}>
              {String(i + 1)}
            </AppText>
          ) : undefined
        }
      />
    ));
  const listCard = {
    borderRadius: radii.lg,
    borderWidth: 1,
    borderColor: colors.border,
    backgroundColor: colors.surface,
    overflow: 'hidden' as const,
  };
  const subs = autoSubSummary(team, state);
  const lineup = (
    <View style={{ gap: spacing.md, flex: isDesktop ? 2 : undefined }} testID="team-lineup">
      <View style={{ gap: spacing.xs }}>
        <SectionHeader title={`${team.team.name} · starters`} right={stepper} />
        <View style={listCard}>{rows(team.starters, false)}</View>
      </View>
      {subs ? (
        <AppText variant="meta" muted testID="autosub-summary">
          {subs}
        </AppText>
      ) : null}
      <View style={{ gap: spacing.xs }}>
        <SectionHeader title="Bench" testID="bench-header" />
        <View style={[listCard, { backgroundColor: colors.background }]}>
          {rows(team.bench, true)}
        </View>
      </View>
    </View>
  );
  return isDesktop ? (
    <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: spacing.lg }}>
      {lineup}
      {card}
    </View>
  ) : (
    <View style={{ gap: spacing.lg }}>
      {card}
      {lineup}
    </View>
  );
}
