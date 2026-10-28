import { ApiError } from '@myfantasyprem/api-client';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import {
  useAcceptInvite,
  useClaimSeatSignIn,
  useInvitePreview,
  useMyLeagues,
} from '@/api/account-hooks';
import { useAuth } from '@/auth/AuthProvider';
import { AppText } from '@/components/AppText';
import { Avatar } from '@/components/Badges';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { Pill } from '@/components/Chip';
import { EmptyState } from '@/components/EmptyState';
import { Ticket } from '@/components/icons';
import { Screen } from '@/components/Screen';
import { Skeleton } from '@/components/Skeleton';
import { TextField } from '@/components/TextField';
import { useToast } from '@/components/Toast';
import { alreadyMemberLeague, describeError, fieldErrors, problemSlug } from '@/lib/errors';
import { leagueStatus, normaliseInviteCode } from '@/lib/leagues';
import { formatDateTime } from '@/lib/time';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * An invite link: "Join The Sunday Draft?". Public, so the preview shows
 * before sign-in; joining needs an account. A seat's claim link is the
 * exception while the server allows it (`claimSignIn`, docs/adr/0013): a
 * signed-out person enters a name and is signed in with the seat.
 */
export default function JoinScreen() {
  const params = useLocalSearchParams<{ code: string }>();
  const code = normaliseInviteCode(params.code ?? '') ?? undefined;
  const router = useRouter();
  const toast = useToast();
  const { status, user } = useAuth();
  const { spacing, colors } = useTheme();
  const preview = useInvitePreview(code);
  const accept = useAcceptInvite(code ?? '');
  const claimSignIn = useClaimSeatSignIn(code ?? '');
  const [claimName, setClaimName] = useState('');
  const [claimNameError, setClaimNameError] = useState<string | null>(null);
  const mine = useMyLeagues(status === 'signedIn');
  // Opened straight from a link, the stored session loads after the first
  // render: suggest the name once the user is known, unless one was typed.
  const suggested = user ? `${user.displayName.split(' ')[0]} FC` : '';
  const [typedName, setTeamName] = useState<string | null>(null);
  const teamName = typedName ?? suggested;
  const [nameError, setNameError] = useState<string | null>(null);

  // Until the session and my leagues load, the screen can't tell whether to
  // ask for sign-in, offer to join, or open a league I'm already in.
  const settling = status === 'loading' || (status === 'signedIn' && mine.isPending);
  const back = { label: status === 'signedIn' ? 'Leagues' : 'Home', href: '/' as const };

  if (!code) {
    return (
      <Screen title="Join a league" back={back} maxWidth={560}>
        <Card>
          <EmptyState
            icon={Ticket}
            title="That code doesn't look right"
            body="Invite codes are 10 letters and numbers, like K7QM2XRP4D. Check the link you were sent."
            testID="join-invalid"
          />
        </Card>
      </Screen>
    );
  }

  // A claim link (an imported league's seat): the team keeps its FPL name.
  const claimSeat = (teamName: string) =>
    accept.mutate(undefined, {
      onSuccess: (envelope) => {
        toast({ message: `${teamName} is your team now`, tone: 'success' });
        router.replace(`/leagues/${envelope.data.league.id}/team`);
      },
      onError: (e) => {
        const leagueId = alreadyMemberLeague(e);
        if (leagueId) {
          toast({ message: "You're already in this league", tone: 'info' });
          router.replace(`/leagues/${leagueId}`);
        }
      },
    });

  // Signed out, with a claim link: a name is all it takes.
  const claimAndSignIn = (teamName: string) => {
    const name = claimName.trim();
    if (name.length < 1 || name.length > 32) {
      setClaimNameError('Your name must be 1 to 32 characters.');
      return;
    }
    setClaimNameError(null);
    claimSignIn.mutate(name, {
      onSuccess: (envelope) => {
        toast({ message: `${teamName} is your team now`, tone: 'success' });
        // A replace: the link (a credential, used up now) leaves the history.
        router.replace(`/leagues/${envelope.data.membership.league.id}/team`);
      },
    });
  };

  const join = () => {
    const name = teamName.trim();
    if (name.length < 1 || name.length > 32) {
      setNameError('Team name must be 1 to 32 characters.');
      return;
    }
    setNameError(null);
    accept.mutate(name, {
      onSuccess: (envelope) => {
        toast({ message: `You joined ${envelope.data.league.name}`, tone: 'success' });
        router.replace(`/leagues/${envelope.data.league.id}`);
      },
      onError: (e) => {
        // Already in it: the problem names the league, so open it directly.
        const leagueId = alreadyMemberLeague(e);
        if (leagueId) {
          toast({ message: "You're already in this league", tone: 'info' });
          router.replace(`/leagues/${leagueId}`);
        }
      },
    });
  };

  const slug = problemSlug(accept.error);
  const previewError = preview.error;
  const gone =
    previewError instanceof ApiError &&
    (previewError.status === 410 || previewError.status === 404);

  return (
    <Screen title="Join a league" back={back} maxWidth={560}>
      {preview.isPending || settling ? (
        <Card testID="loading">
          <Skeleton width="60%" height={22} />
          <Skeleton width="40%" height={14} />
        </Card>
      ) : preview.isError ? (
        <Card testID="invite-problem">
          <EmptyState
            icon={Ticket}
            title={gone ? 'This invite no longer works' : "Couldn't load the invite"}
            body={
              gone
                ? 'It may have expired or been revoked. Ask the commissioner for a new link.'
                : describeError(previewError)
            }
          >
            {gone ? null : (
              <Button
                label="Try again"
                variant="secondary"
                onPress={() => void preview.refetch()}
              />
            )}
          </EmptyState>
        </Card>
      ) : (
        (() => {
          const p = preview.data.data;
          const st = leagueStatus(p.status);
          const member = mine.data?.data.some((row) => row.league.id === p.leagueId) ?? false;
          return (
            <>
              <Card testID="invite-preview" style={{ gap: spacing.md }}>
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
                  <Avatar name={p.leagueName} size={52} square />
                  <View style={{ flex: 1, minWidth: 0 }}>
                    <AppText variant="eyebrow" muted>
                      {p.claimTeam ? 'Claim your team in' : "You're invited to"}
                    </AppText>
                    <AppText variant="title" testID="invite-league-name" numberOfLines={2}>
                      {p.leagueName}
                    </AppText>
                  </View>
                </View>
                <View
                  style={{
                    flexDirection: 'row',
                    flexWrap: 'wrap',
                    gap: spacing.sm,
                    alignItems: 'center',
                  }}
                >
                  <Pill label={st.label} tone={st.tone} />
                  <AppText variant="meta" muted>
                    {`${p.memberCount} of ${p.teamCount} teams · ${p.season} · run by ${p.commissionerName}`}
                  </AppText>
                </View>
                {p.claimTeam ? (
                  <View testID="claim-team" style={{ gap: 2 }}>
                    <AppText weight="semibold">{p.claimTeam.teamName}</AppText>
                    <AppText variant="meta" muted>
                      {`${p.claimTeam.fplManagerName}'s team in FPL Draft.`}
                    </AppText>
                  </View>
                ) : null}
                {p.expiresAt ? (
                  <AppText variant="meta" subtle>
                    {`Invite expires ${formatDateTime(Date.parse(p.expiresAt))}`}
                  </AppText>
                ) : null}
              </Card>

              {member ? (
                <Card testID="invite-already-member">
                  <AppText weight="semibold">You&apos;re already in this league</AppText>
                  <Button
                    label={`Open ${p.leagueName}`}
                    onPress={() => router.replace(`/leagues/${p.leagueId}`)}
                    testID="join-open-league"
                  />
                </Card>
              ) : !p.joinable ? (
                <Card testID="invite-not-joinable">
                  <AppText weight="semibold">You can&apos;t join this league</AppText>
                  <AppText variant="meta" muted>
                    Every team already has a manager.
                  </AppText>
                </Card>
              ) : status !== 'signedIn' && p.claimTeam && p.claimSignIn ? (
                <Card style={{ gap: spacing.md }} testID="claim-sign-in">
                  <AppText variant="heading" testID="claim-heading">
                    {`Claim ${p.claimTeam.teamName} in ${p.leagueName}`}
                  </AppText>
                  <TextField
                    label="Your name"
                    value={claimName}
                    onChangeText={setClaimName}
                    maxLength={32}
                    hint="Shown to the other managers in the league. No email needed."
                    error={claimNameError ?? fieldErrors(claimSignIn.error, '/displayName')}
                    testID="claim-name"
                  />
                  {claimSignIn.isError && !fieldErrors(claimSignIn.error, '/displayName') ? (
                    <AppText color={colors.loss} role="alert" testID="claim-error">
                      {describeError(claimSignIn.error)}
                    </AppText>
                  ) : null}
                  <Button
                    label={`Claim ${p.claimTeam.teamName}`}
                    onPress={() => claimAndSignIn(p.claimTeam!.teamName)}
                    loading={claimSignIn.isPending}
                    testID="claim-sign-in-button"
                  />
                  <Button
                    label="I already have an account"
                    variant="secondary"
                    onPress={() =>
                      router.push({ pathname: '/sign-in', params: { next: `/join/${code}` } })
                    }
                    testID="join-sign-in"
                  />
                </Card>
              ) : status !== 'signedIn' ? (
                <Card>
                  <AppText>
                    {p.claimTeam
                      ? `Sign in to claim ${p.claimTeam.teamName}. You come back here after.`
                      : 'Sign in to pick your team name and join.'}
                  </AppText>
                  <Button
                    label={p.claimTeam ? 'Sign in to claim' : 'Sign in to join'}
                    onPress={() =>
                      router.push({ pathname: '/sign-in', params: { next: `/join/${code}` } })
                    }
                    testID="join-sign-in"
                  />
                </Card>
              ) : p.claimTeam ? (
                <Card style={{ gap: spacing.md }}>
                  {accept.isError ? (
                    <AppText color={colors.loss} role="alert" testID="join-error">
                      {slug === 'already-member'
                        ? "You're already in this league."
                        : describeError(accept.error)}
                    </AppText>
                  ) : null}
                  <Button
                    label={`Claim ${p.claimTeam.teamName}`}
                    onPress={() => claimSeat(p.claimTeam!.teamName)}
                    loading={accept.isPending}
                    testID="claim-seat-button"
                  />
                </Card>
              ) : (
                <Card style={{ gap: spacing.md }}>
                  <TextField
                    label="Your team name"
                    value={teamName}
                    onChangeText={setTeamName}
                    maxLength={32}
                    error={nameError ?? fieldErrors(accept.error, '/teamName')}
                    testID="join-team-name"
                  />
                  {accept.isError ? (
                    <AppText color={colors.loss} role="alert" testID="join-error">
                      {slug === 'already-member'
                        ? "You're already in this league."
                        : describeError(accept.error)}
                    </AppText>
                  ) : null}
                  {slug === 'already-member' ? (
                    <Button
                      label={`Open ${p.leagueName}`}
                      variant="secondary"
                      onPress={() =>
                        router.replace(
                          `/leagues/${alreadyMemberLeague(accept.error) ?? p.leagueId}`,
                        )
                      }
                    />
                  ) : (
                    <Button
                      label={`Join ${p.leagueName}`}
                      onPress={join}
                      loading={accept.isPending}
                      testID="join-submit"
                    />
                  )}
                </Card>
              )}
            </>
          );
        })()
      )}
    </Screen>
  );
}
