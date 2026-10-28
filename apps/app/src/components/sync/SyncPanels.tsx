import type { Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Button } from '../Button';
import { Card } from '../Card';
import { Pill } from '../Chip';
import { Copy, RefreshCw, Share2 } from '../icons';
import { ListRow } from '../ListRow';
import { QueryStatus } from '../QueryStatus';
import { SectionHeader } from '../SectionHeader';
import { ConfirmDialog } from '../Sheet';
import { SkeletonRows } from '../Skeleton';
import { TextField } from '../TextField';
import { useToast } from '../Toast';
import {
  useCheckSync,
  useCreateSeatSignInLink,
  useExplainSyncItem,
  useLeagueSync,
  useResendSeatInvite,
  useResync,
  useSeats,
} from '@/api/sync-hooks';
import { describeError } from '@/lib/errors';
import {
  resyncWarning,
  seatSubtitle,
  shadowLine,
  shadowSummary,
  syncErrorText,
  syncItemTitle,
  syncItemValues,
  syncLine,
  syncPendingText,
  verificationSummary,
} from '@/lib/fpl-import';
import { canShare, copyText, shareLink } from '@/lib/share';
import { formatAgo } from '@/lib/time';
import { useTheme } from '@/theme';

type Sync = Schemas['LeagueSync'];

/**
 * League settings for an imported league (ADR 0012): where FPL's state
 * stands, then for the commissioner the re-sync, the check against FPL and
 * the claim links.
 */
export function ImportedLeagueSections({
  leagueId,
  leagueName,
  isCommissioner,
  myTeamId = null,
}: {
  leagueId: string;
  leagueName: string;
  isCommissioner: boolean;
  /** The viewer's team, which needs no sign-in link. */
  myTeamId?: string | null;
}) {
  const sync = useLeagueSync(leagueId);
  return (
    <QueryStatus
      query={sync}
      loading={
        <Card flush>
          <SkeletonRows count={3} />
        </Card>
      }
    >
      {({ data }) => (
        <>
          <SectionHeader title="Imported from FPL Draft" />
          <SyncPanel leagueId={leagueId} sync={data} isCommissioner={isCommissioner} />
          {isCommissioner ? (
            <>
              <SectionHeader title="Claim links" />
              <ClaimLinks leagueId={leagueId} leagueName={leagueName} myTeamId={myTeamId} />
            </>
          ) : null}
        </>
      )}
    </QueryStatus>
  );
}

function SyncPanel({
  leagueId,
  sync,
  isCommissioner,
}: {
  leagueId: string;
  sync: Sync;
  isCommissioner: boolean;
}) {
  const { spacing, colors } = useTheme();
  const toast = useToast();
  const resync = useResync(leagueId);
  const check = useCheckSync(leagueId);
  const [confirming, setConfirming] = useState(false);
  const pending = syncPendingText(sync);
  const error = syncErrorText(sync);
  const v = sync.verification;
  const summary = v ? verificationSummary(v) : null;
  return (
    <Card style={{ gap: spacing.md }} testID="sync-panel">
      <AppText weight="semibold">{syncLine(sync)}</AppText>
      <AppText variant="meta" muted>
        This league is played here like any other. Nothing syncs by itself.
        {isCommissioner
          ? ' You can re-sync it from FPL whenever you want to start again from there.'
          : ''}
      </AppText>
      {pending ? (
        <AppText testID="sync-panel-pending" role="status">
          {pending}
        </AppText>
      ) : null}
      {error ? (
        <AppText color={colors.loss} role="alert">
          {error}
        </AppText>
      ) : null}
      {sync.note ? <AppText testID="sync-panel-note">{sync.note}</AppText> : null}
      {isCommissioner ? (
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}>
          <Button
            label="Re-sync from FPL"
            variant="secondary"
            icon={RefreshCw}
            disabled={sync.pending !== null}
            onPress={() => setConfirming(true)}
            testID="resync"
          />
          <Button
            label="Check against FPL"
            variant="ghost"
            disabled={sync.pending !== null}
            loading={check.isPending}
            onPress={() =>
              check.mutate(undefined, {
                onError: (e) => toast({ message: describeError(e), tone: 'error' }),
              })
            }
            testID="check-sync"
          />
        </View>
      ) : null}
      {v && summary ? (
        <View style={{ gap: spacing.sm }} testID="verification">
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
            <AppText weight="semibold">Check against FPL</AppText>
            <View testID="verification-status">
              <Pill label={summary.label} tone={summary.tone} />
            </View>
          </View>
          <AppText variant="meta" muted>
            {`Our engines recomputed the imported season and compared it with FPL: ${v.counts.ok} checks agree. Checked ${formatAgo(Date.parse(v.checkedAt))}. After moves made here the league differs from FPL by design, so this is for information only.`}
          </AppText>
          {v.pending.length > 0 ? (
            <Card flush testID="verification-pending">
              {v.pending.map((item, i) => (
                <PendingItem key={item.key} item={item} divider={i > 0} />
              ))}
            </Card>
          ) : null}
          {v.failing.length > 0 ? (
            <Card flush testID="verification-failing">
              {v.failing.map((item, i) => (
                <FailingItem key={item.key} leagueId={leagueId} item={item} divider={i > 0} />
              ))}
            </Card>
          ) : null}
        </View>
      ) : null}
      {isCommissioner && sync.shadow ? <ShadowSection shadow={sync.shadow} /> : null}
      <ConfirmDialog
        visible={confirming}
        title="Re-sync from FPL?"
        message={resyncWarning(sync)}
        confirmLabel="Re-sync from FPL"
        destructive
        loading={resync.isPending}
        onCancel={() => setConfirming(false)}
        onConfirm={() =>
          resync.mutate(undefined, {
            onSuccess: () => toast({ message: 'Re-sync started', tone: 'success' }),
            onError: (e) => toast({ message: describeError(e), tone: 'error' }),
            onSettled: () => setConfirming(false),
          })
        }
        testID="resync-confirm"
      />
    </Card>
  );
}

/**
 * The automatic read-only checks against FPL (shadow checks): run around
 * every deadline, waiver run and match until the switch-over, never
 * changing the league. Differences on finalized data are mismatches;
 * differences while FPL is still updating are listed apart.
 */
function ShadowSection({ shadow }: { shadow: Schemas['ShadowCheck'] }) {
  const { spacing } = useTheme();
  const summary = shadowSummary(shadow);
  return (
    <View style={{ gap: spacing.sm }} testID="shadow-check">
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText weight="semibold">Automatic checks</AppText>
        <View testID="shadow-status">
          <Pill label={summary.label} tone={summary.tone} />
        </View>
      </View>
      <AppText variant="meta" muted>
        {`Read-only comparisons with FPL around every deadline, waiver run and match. ${shadow.counts.ok} checks agree. ${shadowLine(shadow)}`}
      </AppText>
      {shadow.mismatches.length > 0 ? (
        <Card flush testID="shadow-mismatches">
          {shadow.mismatches.map((item, i) => (
            <ShadowItem key={item.key} item={item} divider={i > 0} />
          ))}
        </Card>
      ) : null}
      {shadow.pending.length > 0 ? (
        <Card flush testID="shadow-pending">
          {shadow.pending.map((item, i) => (
            <ShadowItem key={item.key} item={item} divider={i > 0} />
          ))}
        </Card>
      ) : null}
    </View>
  );
}

function ShadowItem({ item, divider }: { item: Schemas['SyncItem']; divider: boolean }) {
  const { spacing, colors } = useTheme();
  const mismatch = item.status === 'mismatch';
  return (
    <View
      testID="shadow-item"
      style={{
        gap: spacing.xs,
        padding: spacing.md,
        borderTopWidth: divider ? 1 : 0,
        borderTopColor: colors.border,
      }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText weight="semibold" style={{ flex: 1 }}>
          {syncItemTitle(item)}
        </AppText>
        <Pill label={mismatch ? 'Mismatch' : 'FPL updating'} tone={mismatch ? 'loss' : 'warning'} />
      </View>
      <AppText variant="meta" muted>
        {syncItemValues(item)}
      </AppText>
      {item.note ? (
        <AppText variant="meta" muted>
          {item.note}
        </AppText>
      ) : null}
    </View>
  );
}

/** A trade FPL accepted and has not processed or vetoed yet: not failing; a re-sync applies what FPL decides. */
function PendingItem({ item, divider }: { item: Schemas['SyncItem']; divider: boolean }) {
  const { spacing, colors } = useTheme();
  return (
    <View
      testID="verification-pending-item"
      style={{
        gap: spacing.xs,
        padding: spacing.md,
        borderTopWidth: divider ? 1 : 0,
        borderTopColor: colors.border,
      }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText weight="semibold" style={{ flex: 1 }}>
          {syncItemTitle(item)}
        </AppText>
        <Pill label="Pending on FPL" tone="warning" />
      </View>
      <AppText variant="meta" muted>
        {item.theirs}
      </AppText>
      <AppText variant="meta" muted>
        Imported as a pending trade. Nobody moves until FPL processes it; the next re-sync applies
        what FPL decides.
      </AppText>
      {item.note ? <AppText variant="meta">{item.note}</AppText> : null}
    </View>
  );
}

function FailingItem({
  leagueId,
  item,
  divider,
}: {
  leagueId: string;
  item: Schemas['SyncItem'];
  divider: boolean;
}) {
  const { spacing, colors } = useTheme();
  const toast = useToast();
  const explain = useExplainSyncItem(leagueId);
  const [note, setNote] = useState<string | null>(null);
  return (
    <View
      testID="verification-item"
      style={{
        gap: spacing.xs,
        padding: spacing.md,
        borderTopWidth: divider ? 1 : 0,
        borderTopColor: colors.border,
      }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText weight="semibold" style={{ flex: 1 }}>
          {syncItemTitle(item)}
        </AppText>
        <Pill
          label={
            item.explanation ? 'Explained' : item.status === 'mismatch' ? 'Mismatch' : 'Unchecked'
          }
          tone={item.explanation ? 'neutral' : item.status === 'mismatch' ? 'loss' : 'warning'}
        />
      </View>
      <AppText variant="meta" muted>
        {syncItemValues(item)}
      </AppText>
      {item.note ? (
        <AppText variant="meta" muted>
          {item.note}
        </AppText>
      ) : null}
      {item.explanation ? (
        <AppText variant="meta">{`“${item.explanation.note}” · ${item.explanation.explainedBy}`}</AppText>
      ) : note === null ? (
        <Button
          label="Explain"
          variant="ghost"
          size="sm"
          onPress={() => setNote('')}
          style={{ alignSelf: 'flex-start' }}
        />
      ) : (
        <View style={{ gap: spacing.sm }}>
          <TextField
            label="Why this is fine"
            hint="Holds while the item shows these values."
            value={note}
            onChangeText={setNote}
            maxLength={500}
          />
          <Button
            label="Save explanation"
            size="sm"
            disabled={note.trim() === ''}
            loading={explain.isPending}
            onPress={() =>
              explain.mutate(
                { key: item.key, note: note.trim() },
                { onError: (e) => toast({ message: describeError(e), tone: 'error' }) },
              )
            }
            style={{ alignSelf: 'flex-start' }}
          />
        </View>
      )}
    </View>
  );
}

/**
 * Every seat: for each unclaimed one its single-use claim link to copy,
 * share or replace; for each claimed one but mine a sign-in link on demand.
 */
function ClaimLinks({
  leagueId,
  leagueName,
  myTeamId,
}: {
  leagueId: string;
  leagueName: string;
  myTeamId: string | null;
}) {
  const { spacing } = useTheme();
  const toast = useToast();
  const seats = useSeats(leagueId);
  const resend = useResendSeatInvite(leagueId);
  const [resending, setResending] = useState<Schemas['Seat'] | null>(null);
  const signInLink = useCreateSeatSignInLink(leagueId);
  // The sign-in links issued here, by team: the code is shown only once.
  const [signInLinks, setSignInLinks] = useState<Record<string, Schemas['SeatSignInLink']>>({});
  const share = canShare();

  const copy = async (url: string, what = 'Claim link') => {
    const ok = await copyText(url);
    toast(
      ok
        ? { message: `${what} copied`, tone: 'success' }
        : { message: "Couldn't copy. Select the link and copy it.", tone: 'error' },
    );
  };

  const issueSignInLink = (seat: Schemas['Seat']) =>
    signInLink.mutate(seat.team.id, {
      onSuccess: (envelope) =>
        setSignInLinks((links) => ({ ...links, [seat.team.id]: envelope.data })),
      onError: (e) => toast({ message: describeError(e), tone: 'error' }),
    });

  return (
    <>
      <QueryStatus
        query={seats}
        loading={
          <Card flush>
            <SkeletonRows count={4} />
          </Card>
        }
      >
        {({ data }) => (
          <Card flush testID="claim-links">
            <AppText variant="meta" muted style={{ padding: spacing.md }}>
              Send each manager the link for their own team. A link works once and never expires.
            </AppText>
            {data.map((seat) => (
              <View key={seat.team.id} testID={`seat-${seat.team.id}`}>
                <ListRow divider title={seat.team.name} subtitle={seatSubtitle(seat)} />
                {seat.invite ? (
                  <View
                    style={{
                      flexDirection: 'row',
                      flexWrap: 'wrap',
                      gap: spacing.sm,
                      paddingHorizontal: spacing.lg,
                      paddingBottom: spacing.md,
                    }}
                  >
                    <AppText
                      variant="meta"
                      selectable
                      numberOfLines={1}
                      testID="claim-link-url"
                      style={{ width: '100%' }}
                    >
                      {seat.invite.url}
                    </AppText>
                    <Button
                      label="Copy link"
                      size="sm"
                      variant="secondary"
                      icon={Copy}
                      onPress={() => void copy(seat.invite!.url)}
                      accessibilityLabel={`Copy the claim link for ${seat.team.name}`}
                      testID="claim-copy"
                    />
                    {share ? (
                      <Button
                        label="Share"
                        size="sm"
                        variant="secondary"
                        icon={Share2}
                        onPress={() =>
                          void shareLink(
                            `Claim ${seat.team.name}`,
                            `Claim ${seat.team.name} in ${leagueName} on MyFantasyPrem`,
                            seat.invite!.url,
                          )
                        }
                        testID="claim-share"
                      />
                    ) : null}
                    <Button
                      label="New link"
                      size="sm"
                      variant="ghost"
                      onPress={() => setResending(seat)}
                      accessibilityLabel={`Replace the claim link for ${seat.team.name}`}
                      testID="claim-resend"
                    />
                  </View>
                ) : seat.owner && seat.team.id !== myTeamId ? (
                  <SignInLinkRow
                    seat={seat}
                    owner={seat.owner}
                    link={signInLinks[seat.team.id]}
                    issuing={signInLink.isPending && signInLink.variables === seat.team.id}
                    onIssue={() => issueSignInLink(seat)}
                    onCopy={(url) => void copy(url, 'Sign-in link')}
                    leagueName={leagueName}
                    share={share}
                  />
                ) : null}
              </View>
            ))}
          </Card>
        )}
      </QueryStatus>
      <ConfirmDialog
        visible={resending !== null}
        title={`New link for ${resending?.team.name ?? ''}?`}
        message="The current link stops working. Share the new one with the manager."
        confirmLabel="Make a new link"
        loading={resend.isPending}
        onCancel={() => setResending(null)}
        onConfirm={() =>
          resending &&
          resend.mutate(resending.team.id, {
            onSuccess: () => toast({ message: 'New claim link ready', tone: 'success' }),
            onError: (e) => toast({ message: describeError(e), tone: 'error' }),
            onSettled: () => setResending(null),
          })
        }
      />
    </>
  );
}

/**
 * A claimed team's sign-in link (docs/adr/0013): for a manager who joined
 * by claim link and has no other way back in after signing out. The link
 * shows once, after it is made.
 */
function SignInLinkRow({
  seat,
  owner,
  link,
  issuing,
  onIssue,
  onCopy,
  leagueName,
  share,
}: {
  seat: Schemas['Seat'];
  owner: Schemas['UserRef'];
  link: Schemas['SeatSignInLink'] | undefined;
  issuing: boolean;
  onIssue: () => void;
  onCopy: (url: string) => void;
  leagueName: string;
  share: boolean;
}) {
  const { spacing } = useTheme();
  return (
    <View
      testID="signin-link"
      style={{
        flexDirection: 'row',
        flexWrap: 'wrap',
        gap: spacing.sm,
        paddingHorizontal: spacing.lg,
        paddingBottom: spacing.md,
      }}
    >
      {link ? (
        <>
          <AppText variant="meta" muted style={{ width: '100%' }} testID="signin-link-help">
            Send this to {owner.displayName} to sign back in. Works once, expires in 7 days.
          </AppText>
          <AppText
            variant="meta"
            selectable
            numberOfLines={1}
            testID="signin-link-url"
            style={{ width: '100%' }}
          >
            {link.url}
          </AppText>
          <Button
            label="Copy link"
            size="sm"
            variant="secondary"
            icon={Copy}
            onPress={() => onCopy(link.url)}
            accessibilityLabel={`Copy the sign-in link for ${owner.displayName}`}
            testID="signin-link-copy"
          />
          {share ? (
            <Button
              label="Share"
              size="sm"
              variant="secondary"
              icon={Share2}
              onPress={() =>
                void shareLink(
                  `Sign in to ${leagueName}`,
                  `Sign back in to ${seat.team.name} in ${leagueName} on MyFantasyPrem`,
                  link.url,
                )
              }
              testID="signin-link-share"
            />
          ) : null}
        </>
      ) : null}
      <Button
        label="New sign-in link"
        size="sm"
        variant="ghost"
        loading={issuing}
        onPress={onIssue}
        accessibilityLabel={`New sign-in link for ${owner.displayName}`}
        testID="signin-link-issue"
      />
    </View>
  );
}
