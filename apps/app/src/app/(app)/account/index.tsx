import { useState } from 'react';
import { View } from 'react-native';

import { useDeleteAccount, useMe, useSignOut, useUpdateMe } from '@/api/account-hooks';
import { useFreshness } from '@/api/freshness';
import { AppText } from '@/components/AppText';
import { AppUpdatesRow } from '@/components/AppUpdatesRow';
import { Avatar } from '@/components/Badges';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { Pill } from '@/components/Chip';
import { Bell, LogOut, Trash } from '@/components/icons';
import { ListRow } from '@/components/ListRow';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { ConfirmDialog } from '@/components/Sheet';
import { Skeleton } from '@/components/Skeleton';
import { TextField } from '@/components/TextField';
import { useToast } from '@/components/Toast';
import { describeError, fieldErrors } from '@/lib/errors';
import { formatMonthYear } from '@/lib/time';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

const PROVIDER_NAMES: Record<string, string> = {
  apple: 'Apple',
  google: 'Google',
  email: 'Email link',
};

/** Profile, sign out and account deletion (App Store 5.1.1(v)). */
export default function AccountScreen() {
  const me = useMe();
  const freshness = useFreshness(me);
  const update = useUpdateMe();
  const signOut = useSignOut();
  const deleteAccount = useDeleteAccount();
  const toast = useToast();
  const { spacing, colors } = useTheme();
  const [name, setName] = useState<string | null>(null);
  const [confirming, setConfirming] = useState(false);
  // An account from a claim link has no way back in after signing out.
  const [signingOut, setSigningOut] = useState(false);

  return (
    <Screen title="Account" freshness={freshness} maxWidth={640}>
      <QueryStatus
        query={me}
        loading={
          <Card>
            <Skeleton width={64} height={64} radius={32} />
            <Skeleton width="40%" height={18} />
          </Card>
        }
      >
        {({ data: user }) => {
          const draft = name ?? user.displayName;
          const dirty = draft.trim() !== user.displayName;
          return (
            <>
              <Card style={{ gap: spacing.lg }}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.lg }}>
                  <Avatar name={user.displayName} color={user.avatarColor} size={64} />
                  <View style={{ flex: 1, minWidth: 0 }}>
                    <AppText variant="heading" testID="account-name" numberOfLines={1}>
                      {user.displayName}
                    </AppText>
                    <AppText muted numberOfLines={1}>
                      {user.email ?? 'No email shared'}
                    </AppText>
                    <AppText variant="meta" subtle>
                      {`Member since ${formatMonthYear(Date.parse(user.createdAt))}`}
                    </AppText>
                  </View>
                </View>
                <TextField
                  label="Display name"
                  value={draft}
                  onChangeText={setName}
                  maxLength={32}
                  hint="Shown to the other managers in your leagues."
                  error={fieldErrors(update.error, '/displayName')}
                  testID="account-display-name"
                />
                <Button
                  label="Save"
                  disabled={!dirty || draft.trim().length === 0}
                  loading={update.isPending}
                  onPress={() =>
                    update.mutate(
                      { displayName: draft.trim() },
                      {
                        onSuccess: () => {
                          setName(null);
                          toast({ message: 'Profile saved', tone: 'success' });
                        },
                        onError: (e) => toast({ message: describeError(e), tone: 'error' }),
                      },
                    )
                  }
                  style={{ alignSelf: 'flex-start' }}
                />
              </Card>

              <SectionHeader title="Sign-in methods" />
              <Card flush>
                {user.authProviders.length === 0 ? (
                  <ListRow
                    title="None yet"
                    subtitle="You signed in with your team's claim link. Adding an email isn't available yet, so signing out ends your access to this account."
                    testID="no-sign-in-method"
                  />
                ) : null}
                {user.authProviders.map((p, i) => (
                  <ListRow
                    key={p}
                    divider={i > 0}
                    title={PROVIDER_NAMES[p] ?? p}
                    right={<Pill label="Connected" tone="win" />}
                  />
                ))}
              </Card>
            </>
          );
        }}
      </QueryStatus>

      <SectionHeader title="Settings" />
      <Card flush>
        <ListRow
          title="Notifications"
          subtitle="Trades, waivers, your players and your opponent's"
          left={<Bell size={20} color={colors.textMuted} aria-hidden />}
          href="/account/notifications"
          chevron
          testID="account-notifications"
        />
        <AppUpdatesRow divider />
      </Card>

      <Button
        label="Sign out"
        variant="secondary"
        icon={LogOut}
        onPress={() =>
          me.data?.data.authProviders.length === 0 ? setSigningOut(true) : signOut.mutate()
        }
        loading={signOut.isPending}
        testID="sign-out"
      />

      <SectionHeader title="Danger zone" />
      <Card>
        <AppText weight="semibold">Delete account</AppText>
        <AppText muted variant="meta">
          Deletes your profile and signs you out everywhere. In leagues that have drafted, your team
          stays for the schedule as an open seat that the next person to join takes over. This
          cannot be undone.
        </AppText>
        <Button
          label="Delete account"
          variant="destructive"
          icon={Trash}
          onPress={() => setConfirming(true)}
          testID="delete-account"
          style={{ alignSelf: 'flex-start' }}
        />
      </Card>

      <ConfirmDialog
        visible={signingOut}
        title="Sign out for good?"
        message="This account has no email or other sign-in yet, so you can't sign back in to it. Your team stays claimed; the commissioner can remove you and share a new link."
        confirmLabel="Sign out"
        destructive
        loading={signOut.isPending}
        testID="sign-out-dialog"
        onCancel={() => setSigningOut(false)}
        onConfirm={() => signOut.mutate()}
      />

      <ConfirmDialog
        visible={confirming}
        title="Delete your account?"
        message="Your profile and sign-ins are deleted now. You leave every league; leagues you run pass to the longest-standing member."
        confirmLabel="Delete account"
        destructive
        loading={deleteAccount.isPending}
        testID="delete-account-dialog"
        onCancel={() => setConfirming(false)}
        onConfirm={() =>
          deleteAccount.mutate(undefined, {
            onSuccess: () => toast({ message: 'Your account was deleted.' }),
            onError: (e) => {
              setConfirming(false);
              toast({ message: describeError(e), tone: 'error' });
            },
          })
        }
      />
    </Screen>
  );
}
