import type { Schemas } from '@myfantasyprem/api-client';
import { useEffect, useRef } from 'react';
import { View } from 'react-native';

import { AppText } from './AppText';
import { Button } from './Button';
import { Card } from './Card';
import { Copy, Link as LinkIcon, Share2 } from './icons';
import { Skeleton } from './Skeleton';
import { useToast } from './Toast';
import { useCreateInvite, useInvites } from '@/api/league-admin-hooks';
import { describeError, problemSlug } from '@/lib/errors';
import { canShare, copyText, shareLink } from '@/lib/share';
import { formatDateTime } from '@/lib/time';
import { useTheme } from '@/theme';

/** The newest invite that still works, if any. */
export function activeInvite(
  invites: readonly Schemas['Invite'][],
  now = Date.now(),
): Schemas['Invite'] | null {
  return (
    invites.find(
      (i) =>
        !i.revoked &&
        (i.expiresAt === null || Date.parse(i.expiresAt) > now) &&
        (i.maxUses === null || i.uses < i.maxUses),
    ) ?? null
  );
}

interface Props {
  leagueId: string;
  leagueName: string;
  /** Create an invite when the league has none that works (just after creating a league). */
  autoCreate?: boolean;
}

/**
 * The commissioner's invite: the code in large type, the link, and copy and
 * share buttons. Creates a fresh invite when none is usable.
 */
export function InviteCard({ leagueId, leagueName, autoCreate }: Props) {
  const { colors, spacing, radii } = useTheme();
  const toast = useToast();
  const invites = useInvites(leagueId);
  const create = useCreateInvite(leagueId);
  const invite = invites.data ? activeInvite(invites.data.data) : null;
  const attempted = useRef(false);

  useEffect(() => {
    if (autoCreate && invites.data && !invite && !attempted.current) {
      attempted.current = true;
      create.mutate({});
    }
  }, [autoCreate, invites.data, invite, create]);

  const copy = async (text: string, what: string) => {
    const ok = await copyText(text);
    toast(
      ok
        ? { message: `${what} copied`, tone: 'success' }
        : {
            message: `Couldn't copy. Select the ${what.toLowerCase()} and copy it.`,
            tone: 'error',
          },
    );
  };

  if (invites.isPending || (create.isPending && !invite)) {
    return (
      <Card testID="invite-card-loading">
        <Skeleton width="40%" height={12} />
        <Skeleton width="60%" height={32} />
      </Card>
    );
  }
  if (invites.isError) {
    return (
      <Card testID="invite-error">
        <AppText muted>{describeError(invites.error)}</AppText>
        <Button
          label="Try again"
          variant="secondary"
          onPress={() => void invites.refetch()}
          style={{ alignSelf: 'flex-start' }}
        />
      </Card>
    );
  }
  if (!invite) {
    const full = problemSlug(create.error) === 'league-full';
    return (
      <Card testID="invite-none">
        <AppText weight="semibold">
          {full ? 'Every team has a manager' : 'No active invite'}
        </AppText>
        <AppText variant="meta" muted>
          {full
            ? 'The league is full, so there is nobody left to invite.'
            : create.error
              ? describeError(create.error)
              : 'Create an invite to share a code and link.'}
        </AppText>
        {full ? null : (
          <Button
            label="Create invite"
            onPress={() => create.mutate({})}
            loading={create.isPending}
            testID="create-invite"
            style={{ alignSelf: 'flex-start' }}
          />
        )}
      </Card>
    );
  }

  return (
    <Card testID="invite-card" style={{ gap: spacing.md }}>
      <AppText variant="eyebrow" muted>
        Invite code
      </AppText>
      <View
        style={{
          alignSelf: 'flex-start',
          paddingHorizontal: spacing.lg,
          paddingVertical: spacing.sm,
          borderRadius: radii.md,
          borderWidth: 1,
          borderStyle: 'dashed',
          borderColor: colors.accent,
        }}
      >
        <AppText
          variant="display"
          numeric
          selectable
          testID="invite-code"
          accessibilityLabel={`Invite code ${invite.code.split('').join(' ')}`}
          style={{ letterSpacing: 4 }}
        >
          {invite.code}
        </AppText>
      </View>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
        <LinkIcon size={14} color={colors.textMuted} aria-hidden />
        <AppText
          variant="meta"
          muted
          selectable
          numberOfLines={1}
          style={{ flex: 1 }}
          testID="invite-url"
        >
          {invite.url}
        </AppText>
      </View>
      <AppText variant="meta" subtle>
        {[
          `${invite.uses} joined`,
          invite.maxUses ? `${invite.maxUses - invite.uses} uses left` : null,
          invite.expiresAt ? `expires ${formatDateTime(Date.parse(invite.expiresAt))}` : null,
        ]
          .filter(Boolean)
          .join(' · ')}
      </AppText>
      <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}>
        <Button
          label="Copy link"
          icon={Copy}
          onPress={() => void copy(invite.url, 'Link')}
          testID="copy-invite-link"
        />
        <Button
          label="Copy code"
          variant="secondary"
          onPress={() => void copy(invite.code, 'Code')}
          testID="copy-invite-code"
        />
        {canShare() ? (
          <Button
            label="Share"
            variant="secondary"
            icon={Share2}
            onPress={() =>
              void shareLink(
                `Join ${leagueName}`,
                `Join my league "${leagueName}". Invite code ${invite.code}.`,
                invite.url,
              )
            }
          />
        ) : null}
      </View>
    </Card>
  );
}
