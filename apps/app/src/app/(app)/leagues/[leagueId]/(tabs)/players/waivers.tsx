import { useLocalSearchParams } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { useFreshness, useNow } from '@/api/freshness';
import { useLeagueContext } from '@/api/hooks';
import {
  isOptimisticClaim,
  useCancelClaim,
  useReorderClaims,
  useWaiverClaims,
  useWaiverState,
} from '@/api/moves-hooks';
import { AppText } from '@/components/AppText';
import { Avatar, PositionBadge } from '@/components/Badges';
import { IconButton } from '@/components/Button';
import { Card } from '@/components/Card';
import { Pill } from '@/components/Chip';
import { EmptyState } from '@/components/EmptyState';
import { ArrowDown, ArrowUp, Clock, ListOrdered, Trash } from '@/components/icons';
import { ListRow } from '@/components/ListRow';
import { PlayerName } from '@/components/players/PlayerCardContext';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { ConfirmDialog } from '@/components/Sheet';
import { useToast } from '@/components/Toast';
import { useLayout } from '@/hooks/useLayout';
import { describeError } from '@/lib/errors';
import { originBack } from '@/lib/navigation';
import { formatDateTime, formatRelative } from '@/lib/time';
import { claimFailureText, moveClaim, sortClaims, waiverOrderText } from '@/lib/waivers';
import { tint, useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * Waivers: when the next run is, my pending claims in my order (move up,
 * move down, cancel), the league's waiver priority, and my decided claims.
 */
export default function WaiversScreen() {
  const { leagueId, from } = useLocalSearchParams<{ leagueId: string; from?: string }>();
  const { spacing, colors } = useTheme();
  const { isWide } = useLayout();
  const toast = useToast();
  const now = useNow(30_000);
  const home = useLeagueContext(leagueId);
  const state = useWaiverState(leagueId);
  const pending = useWaiverClaims(leagueId, 'pending');
  const processed = useWaiverClaims(leagueId, 'processed');
  const reorder = useReorderClaims(leagueId);
  const cancel = useCancelClaim(leagueId);
  const [cancelling, setCancelling] = useState<{ id: string; name: string } | null>(null);
  const freshness = useFreshness(pending);
  const myTeamId = home.data?.data.viewer.team?.id;
  // FPL runs the waivers while the league syncs: nothing here can change.

  const move = (ids: string[], id: string, delta: -1 | 1) =>
    // Shown at once; moves queue one after another; a refusal rolls back.
    reorder.mutate(moveClaim(ids, id, delta), {
      onError: (e) => toast({ message: describeError(e), tone: 'error', haptic: false }),
    });

  const claimsCard = (
    <View style={{ gap: spacing.xs, flex: isWide ? 3 : undefined, minWidth: 0 }}>
      <SectionHeader title="My claims" />
      <QueryStatus query={pending}>
        {({ data }) => {
          const claims = sortClaims(data);
          const ids = claims.map((c) => c.id);
          // A claim still being placed has no id the server knows yet.
          const settling = ids.some(isOptimisticClaim);
          if (claims.length === 0) {
            return (
              <Card>
                <EmptyState
                  icon={ListOrdered}
                  title="No pending claims"
                  body="Claim a player on waivers from the Players tab. Your claims stay private until they run."
                  testID="claims-empty"
                />
              </Card>
            );
          }
          return (
            <Card flush testID="claims">
              {claims.map((c, i) => (
                <View
                  key={c.id}
                  testID={`claim-${c.id}`}
                  accessibilityLabel={`Claim ${i + 1}: add ${c.add.displayName}, drop ${c.drop.displayName}`}
                  style={{
                    flexDirection: 'row',
                    alignItems: 'center',
                    gap: spacing.sm,
                    paddingLeft: spacing.lg,
                    paddingRight: spacing.xs,
                    paddingVertical: spacing.sm,
                    borderTopWidth: i > 0 ? 1 : 0,
                    borderTopColor: colors.border,
                  }}
                >
                  <AppText numeric weight="bold" accent style={{ width: 20 }}>
                    {String(i + 1)}
                  </AppText>
                  <PositionBadge position={c.add.position} size="sm" />
                  <View style={{ flex: 1, minWidth: 0, gap: 2, alignItems: 'flex-start' }}>
                    <PlayerName
                      player={c.add}
                      label={`+ ${c.add.displayName}`}
                      weight="semibold"
                      color={colors.win}
                    />
                    <PlayerName
                      player={c.drop}
                      label={`− ${c.drop.displayName}`}
                      variant="meta"
                      color={colors.loss}
                    />
                  </View>
                  <IconButton
                    icon={ArrowUp}
                    label={`Move ${c.add.displayName} claim up`}
                    disabled={i === 0 || settling}
                    onPress={() => move(ids, c.id, -1)}
                    testID={`claim-up-${c.id}`}
                    size={18}
                  />
                  <IconButton
                    icon={ArrowDown}
                    label={`Move ${c.add.displayName} claim down`}
                    disabled={i === claims.length - 1 || settling}
                    onPress={() => move(ids, c.id, 1)}
                    testID={`claim-down-${c.id}`}
                    size={18}
                  />
                  <IconButton
                    icon={Trash}
                    label={`Cancel claim for ${c.add.displayName}`}
                    disabled={isOptimisticClaim(c.id)}
                    onPress={() => setCancelling({ id: c.id, name: c.add.displayName })}
                    testID={`claim-cancel-${c.id}`}
                    size={18}
                    color={colors.loss}
                  />
                </View>
              ))}
            </Card>
          );
        }}
      </QueryStatus>
      <SectionHeader title="Results" detail="last run" />
      <QueryStatus query={processed}>
        {({ data }) =>
          data.length === 0 ? (
            <AppText variant="meta" muted>
              No claims have run yet.
            </AppText>
          ) : (
            <Card flush testID="claim-results">
              {data.map((c, i) => (
                <ListRow
                  key={c.id}
                  divider={i > 0}
                  left={<PositionBadge position={c.add.position} size="sm" />}
                  title={
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
                      <PlayerName player={c.add} weight="semibold" />
                      <AppText muted>for</AppText>
                      <PlayerName player={c.drop} weight="semibold" />
                    </View>
                  }
                  accessibilityLabel={`${c.add.displayName} for ${c.drop.displayName}`}
                  subtitle={
                    c.status === 'won'
                      ? `Won · GW ${c.gameweek}`
                      : `${claimFailureText(c.failureReason)} · GW ${c.gameweek}`
                  }
                  right={
                    <Pill
                      label={c.status === 'won' ? 'Won' : c.status === 'lost' ? 'Lost' : 'Done'}
                      tone={c.status === 'won' ? 'win' : c.status === 'lost' ? 'loss' : 'neutral'}
                    />
                  }
                />
              ))}
            </Card>
          )
        }
      </QueryStatus>
    </View>
  );

  const priority = (
    <View style={{ gap: spacing.xs, flex: isWide ? 2 : undefined, minWidth: 0 }}>
      <SectionHeader
        title="Waiver priority"
        detail={state.data ? waiverOrderText(state.data.data.order).short : undefined}
      />
      <QueryStatus query={state}>
        {({ data }) => (
          <Card flush testID="waiver-priority">
            {data.priority.map((row, i) => {
              const mine = row.team.id === myTeamId;
              return (
                <ListRow
                  key={row.team.id}
                  divider={i > 0}
                  style={
                    mine
                      ? { backgroundColor: tint(colors.accent, colors.surface, 0.08) }
                      : undefined
                  }
                  left={
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
                      <AppText
                        numeric
                        weight="semibold"
                        muted
                        style={{ width: 20, textAlign: 'right' }}
                      >
                        {String(row.rank)}
                      </AppText>
                      <Avatar name={row.team.name} size={26} square />
                    </View>
                  }
                  title={row.team.name}
                  subtitle={mine ? 'You' : row.team.managerName}
                />
              );
            })}
          </Card>
        )}
      </QueryStatus>
      <AppText variant="meta" subtle testID="waiver-order-help">
        {state.data
          ? waiverOrderText(state.data.data.order).help
          : 'A team that wins a claim drops to the back of the order.'}
      </AppText>
    </View>
  );

  return (
    <Screen title="Waivers" back={originBack(leagueId, from, 'players')} freshness={freshness}>
      <QueryStatus query={state}>
        {({ data }) => (
          <Card
            testID="waiver-next-run"
            style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}
          >
            <Clock size={22} color={colors.accent} aria-hidden />
            <View style={{ flex: 1 }}>
              <AppText variant="eyebrow" muted>
                Next waiver run
              </AppText>
              <AppText variant="heading">
                {data.nextRunAt ? formatDateTime(Date.parse(data.nextRunAt)) : 'Not scheduled'}
              </AppText>
            </View>
            {data.nextRunAt ? (
              <Pill label={formatRelative(Date.parse(data.nextRunAt), now)} tone="accent" />
            ) : null}
          </Card>
        )}
      </QueryStatus>
      <View
        style={
          isWide
            ? { flexDirection: 'row', alignItems: 'flex-start', gap: spacing.lg }
            : { gap: spacing.lg }
        }
      >
        {claimsCard}
        {priority}
      </View>
      <ConfirmDialog
        visible={cancelling !== null}
        title="Cancel this claim?"
        message={cancelling ? `Your claim for ${cancelling.name} will be removed.` : ''}
        confirmLabel="Cancel claim"
        cancelLabel="Keep"
        destructive
        onCancel={() => setCancelling(null)}
        onConfirm={() => {
          if (!cancelling) return;
          // The claim leaves the list at once; a refusal brings it back.
          setCancelling(null);
          cancel.mutateAsync(cancelling.id).then(
            () => toast({ message: 'Claim cancelled', tone: 'success' }),
            (e: unknown) => toast({ message: describeError(e), tone: 'error', haptic: false }),
          );
        }}
        testID="cancel-claim-dialog"
      />
    </Screen>
  );
}
