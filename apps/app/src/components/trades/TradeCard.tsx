import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar } from '../Badges';
import { Button } from '../Button';
import { Card } from '../Card';
import { Pill } from '../Chip';
import { ArrowDown, ArrowLeftRight, ArrowUp, Check, Clock, Shield } from '../icons';
import { PlayerName } from '../players/PlayerCardContext';
import { PlayerAvatar } from '../PlayerRow';
import { useLayout } from '@/hooks/useLayout';
import { formatAgo, formatDateTime, formatRelative } from '@/lib/time';
import { pendingOnFPL, sidesFor, tradeStatusLabel, voteLabel } from '@/lib/trade';
import type { TradeResponse } from '@/api/moves-hooks';
import { tint, useTheme } from '@/theme';

interface Props {
  trade: Schemas['Trade'];
  myTeamId?: string;
  now: number;
  /** Starts an action; the screen confirms it first. */
  onAction: (trade: Schemas['Trade'], action: TradeResponse) => void;
  busy?: boolean;
}

/** Which way a trade goes for me: an offer to me, an offer from me, or neither (others' trade). */
export function tradeDirection(
  trade: Pick<Schemas['Trade'], 'offeringTeam' | 'receivingTeam'>,
  myTeamId: string | undefined,
): 'incoming' | 'outgoing' | null {
  if (myTeamId === undefined) return null;
  if (trade.receivingTeam.id === myTeamId) return 'incoming';
  if (trade.offeringTeam.id === myTeamId) return 'outgoing';
  return null;
}

/** "2-way trade" (the API has two teams per trade; kept as a word for more later). */
export function tradeWays(players: number): string {
  return `${players}-way trade`;
}

/** One side of a trade: a caps title over a cluster of round avatars with short names. */
function Side({
  title,
  players,
  testID,
}: {
  title: string;
  players: readonly Schemas['PlayerRef'][];
  testID?: string;
}) {
  const { spacing } = useTheme();
  return (
    <View
      testID={testID}
      accessibilityLabel={`${title}: ${players.map((p) => p.displayName).join(', ')}`}
      style={{ flex: 1, minWidth: 0, gap: spacing.xs, alignItems: 'center' }}
    >
      <AppText variant="eyebrow" muted numberOfLines={1}>
        {title}
      </AppText>
      <View
        style={{
          flexDirection: 'row',
          flexWrap: 'wrap',
          justifyContent: 'center',
          gap: spacing.xs,
        }}
      >
        {players.map((p) => (
          <View key={p.id} style={{ width: 60, alignItems: 'center', gap: 2 }}>
            <PlayerAvatar player={p} size={40} />
            <PlayerName
              player={p}
              variant="meta"
              muted
              textStyle={{ fontSize: 11, lineHeight: 14, textAlign: 'center' }}
              style={{ maxWidth: 60 }}
            />
          </View>
        ))}
      </View>
    </View>
  );
}

/**
 * One trade: who gets whom (from my side when I am in it), the status,
 * the message, the review window with its countdown and vote count, and
 * the actions the server says I may take.
 */
export function TradeCard({ trade, myTeamId, now, onAction, busy }: Props) {
  const { colors, spacing, radii } = useTheme();
  const { isWide } = useLayout();
  const onFPL = pendingOnFPL(trade);
  const st = tradeStatusLabel(trade.status, onFPL);
  const involved =
    myTeamId !== undefined &&
    (trade.offeringTeam.id === myTeamId || trade.receivingTeam.id === myTeamId);
  const s = sidesFor(trade, myTeamId);
  const actions = new Set(trade.viewerActions);
  const commissioner = actions.has('review');
  const memberActions = [...actions].some((a) => a !== 'review');
  const review = trade.review;
  const reviewOpen = trade.status === 'in_review' && review && !onFPL;
  const title = involved
    ? trade.offeringTeam.id === myTeamId
      ? `You offered ${trade.receivingTeam.name}`
      : `${trade.offeringTeam.name} offered you`
    : `${trade.offeringTeam.name} and ${trade.receivingTeam.name}`;
  const direction = tradeDirection(trade, myTeamId);
  const other = involved ? s.them : trade.offeringTeam;
  const DirIcon = direction === 'outgoing' ? ArrowUp : ArrowDown;
  const dirColor = direction === 'incoming' ? colors.win : colors.textMuted;
  return (
    <Card testID={`trade-${trade.id}`} style={{ gap: spacing.md }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        {direction ? (
          <View
            testID="trade-direction"
            style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}
          >
            <View
              aria-hidden
              style={{
                width: 20,
                height: 20,
                borderRadius: 10,
                alignItems: 'center',
                justifyContent: 'center',
                backgroundColor: tint(dirColor, colors.surface, 0.24),
              }}
            >
              <DirIcon size={13} color={dirColor} strokeWidth={2.6} />
            </View>
            <AppText
              variant="eyebrow"
              color={direction === 'incoming' ? colors.win : undefined}
              muted={direction !== 'incoming'}
            >
              {direction === 'incoming' ? 'Incoming' : 'Outgoing'}
            </AppText>
          </View>
        ) : null}
        <Pill label={st.label} tone={st.tone} testID="trade-status" />
        <AppText
          variant="meta"
          muted
          testID="trade-offered-at"
          style={{ marginLeft: 'auto' }}
          numberOfLines={1}
        >
          {formatAgo(Date.parse(trade.createdAt), now)}
        </AppText>
      </View>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        <Avatar name={other.managerName} size={40} />
        <View style={{ flex: 1, minWidth: 0 }}>
          <AppText weight="bold" numberOfLines={2} testID="trade-title" style={{ fontSize: 16 }}>
            {title}
          </AppText>
          <AppText variant="meta" muted numberOfLines={1}>
            {`${involved ? other.managerName : `${trade.offeringTeam.managerName} and ${trade.receivingTeam.managerName}`} · ${tradeWays(2)}`}
          </AppText>
        </View>
      </View>
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.sm,
          paddingVertical: spacing.sm,
          paddingHorizontal: spacing.sm,
          borderRadius: radii.md,
          backgroundColor: colors.surfaceRaised,
        }}
      >
        <Side
          title={involved ? 'You get' : `${trade.receivingTeam.name} gets`}
          players={involved ? s.get : trade.offered}
          testID="trade-side-get"
        />
        <View
          aria-hidden
          style={{
            width: 32,
            height: 32,
            borderRadius: 16,
            borderWidth: 1,
            borderColor: colors.border,
            backgroundColor: colors.surface,
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <ArrowLeftRight size={16} color={colors.textMuted} />
        </View>
        <Side
          title={involved ? 'You give' : `${trade.offeringTeam.name} gets`}
          players={involved ? s.give : trade.requested}
          testID="trade-side-give"
        />
      </View>
      {trade.message ? (
        <AppText variant="meta" muted style={{ fontStyle: 'italic' }}>
          {`“${trade.message}”`}
        </AppText>
      ) : null}
      {trade.status === 'offered' ? (
        <AppText variant="meta" muted>
          {`Expires ${formatDateTime(Date.parse(trade.expiresAt))} if not answered`}
        </AppText>
      ) : null}
      {onFPL ? (
        <View
          testID="trade-pending-fpl"
          style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' }}
        >
          <Clock size={16} color={colors.accent} aria-hidden />
          <AppText variant="meta" style={{ flex: 1 }}>
            Accepted on FPL, waiting for FPL to process or veto it. The players move when FPL
            processes it.
          </AppText>
        </View>
      ) : null}
      {reviewOpen ? (
        <View
          testID="trade-review"
          style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm, flexWrap: 'wrap' }}
        >
          <Clock size={16} color={colors.accent} aria-hidden />
          <AppText variant="meta">
            {review.endsAt
              ? `Review ends ${formatRelative(Date.parse(review.endsAt), now)}`
              : 'In review'}
          </AppText>
          {review.mode === 'league_vote' ? (
            <AppText variant="meta" muted>
              {`· ${review.vetoVotes} of ${review.votesNeeded ?? '?'} veto votes · ${review.allowVotes} uphold${
                review.myVote ? ` · you voted ${voteLabel(review.myVote).toLowerCase()}` : ''
              }`}
            </AppText>
          ) : review.mode === 'commissioner' ? (
            <AppText variant="meta" muted>
              · the commissioner decides, upheld if no decision
            </AppText>
          ) : null}
        </View>
      ) : null}
      {trade.status === 'vetoed' && review?.decidedBy ? (
        <AppText variant="meta" muted>{`Vetoed by ${review.decidedBy.displayName}`}</AppText>
      ) : null}
      {trade.status === 'processed' && trade.effectiveGameweek ? (
        <AppText
          variant="meta"
          muted
        >{`Players count for their new teams from GW ${trade.effectiveGameweek}`}</AppText>
      ) : null}
      {memberActions || commissioner ? (
        <View
          style={{
            flexDirection: isWide ? 'row' : 'column',
            flexWrap: isWide ? 'wrap' : 'nowrap',
            alignItems: isWide ? 'flex-end' : 'stretch',
            gap: spacing.md,
          }}
        >
          {memberActions ? (
            <View
              testID="trade-actions"
              style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}
            >
              {actions.has('vote') ? (
                <AppText variant="eyebrow" muted style={{ width: '100%' }}>
                  League vote
                </AppText>
              ) : null}
              {actions.has('accept') ? (
                <Button
                  label="Accept"
                  onPress={() => onAction(trade, { kind: 'accept' })}
                  disabled={busy}
                  testID="trade-accept"
                />
              ) : null}
              {actions.has('decline') ? (
                <Button
                  label="Decline"
                  variant="secondary"
                  onPress={() => onAction(trade, { kind: 'decline' })}
                  disabled={busy}
                  testID="trade-decline"
                />
              ) : null}
              {actions.has('withdraw') ? (
                <Button
                  label="Withdraw"
                  variant="secondary"
                  onPress={() => onAction(trade, { kind: 'withdraw' })}
                  disabled={busy}
                  testID="trade-withdraw"
                />
              ) : null}
              {actions.has('vote') ? (
                <>
                  <Button
                    label="Veto"
                    icon={review?.myVote === 'veto' ? Check : undefined}
                    accessibilityLabel={review?.myVote === 'veto' ? 'Veto, your vote' : 'Veto'}
                    variant="destructive"
                    onPress={() => onAction(trade, { kind: 'vote', vote: 'veto' })}
                    disabled={busy || review?.myVote === 'veto'}
                    testID="trade-vote-veto"
                  />
                  <Button
                    label="Uphold"
                    icon={review?.myVote === 'allow' ? Check : undefined}
                    accessibilityLabel={review?.myVote === 'allow' ? 'Uphold, your vote' : 'Uphold'}
                    variant="secondary"
                    onPress={() => onAction(trade, { kind: 'vote', vote: 'allow' })}
                    disabled={busy || review?.myVote === 'allow'}
                    testID="trade-vote-uphold"
                  />
                </>
              ) : null}
            </View>
          ) : null}
          {commissioner ? (
            <View
              testID="commissioner-actions"
              accessibilityLabel="Commissioner actions"
              style={{
                ...(isWide ? { marginLeft: 'auto' } : { alignSelf: 'stretch' }),
                gap: spacing.sm,
                padding: spacing.sm,
                borderRadius: radii.md,
                borderWidth: 1,
                borderColor: colors.accent,
                backgroundColor: tint(colors.accent, colors.surface),
              }}
            >
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
                <Shield size={14} color={colors.accent} strokeWidth={2.4} aria-hidden />
                <AppText variant="eyebrow" accent>
                  Commissioner
                </AppText>
              </View>
              <View style={{ flexDirection: 'row', gap: spacing.sm }}>
                <Button
                  label="Veto"
                  variant="destructive"
                  onPress={() => onAction(trade, { kind: 'review', decision: 'veto' })}
                  disabled={busy}
                  accessibilityLabel="Commissioner veto"
                  style={isWide ? undefined : { flex: 1 }}
                  testID="trade-veto"
                />
                <Button
                  label="Uphold"
                  onPress={() => onAction(trade, { kind: 'review', decision: 'approve' })}
                  disabled={busy}
                  accessibilityLabel="Commissioner uphold"
                  style={isWide ? undefined : { flex: 1 }}
                  testID="trade-uphold"
                />
              </View>
            </View>
          ) : null}
        </View>
      ) : null}
    </Card>
  );
}

/** Confirmation copy for each action. */
export function confirmCopy(
  trade: Schemas['Trade'],
  action: TradeResponse,
): { title: string; message: string; confirm: string; destructive?: boolean } {
  const other = `${trade.offeringTeam.name} and ${trade.receivingTeam.name}`;
  switch (action.kind) {
    case 'accept':
      return {
        title: 'Accept this trade?',
        message:
          'The players move as soon as you accept, or after the review window if this league reviews trades.',
        confirm: 'Accept trade',
      };
    case 'decline':
      return {
        title: 'Decline this trade?',
        message: `${trade.offeringTeam.name} will see it was declined.`,
        confirm: 'Decline',
        destructive: true,
      };
    case 'withdraw':
      return {
        title: 'Withdraw your offer?',
        message: `${trade.receivingTeam.name} can no longer accept it.`,
        confirm: 'Withdraw',
        destructive: true,
      };
    case 'vote':
      return {
        title: action.vote === 'veto' ? 'Vote to veto?' : 'Vote to uphold?',
        message: `Your vote on the trade between ${other}. You can change it until the review ends.`,
        confirm: voteLabel(action.vote),
        destructive: action.vote === 'veto',
      };
    case 'review':
      return {
        title: action.decision === 'veto' ? 'Veto this trade?' : 'Uphold this trade now?',
        message:
          action.decision === 'veto'
            ? `The trade between ${other} will not happen.`
            : `The players move now instead of when the review ends.`,
        confirm: action.decision === 'veto' ? 'Veto' : 'Uphold',
        destructive: action.decision === 'veto',
      };
  }
}
