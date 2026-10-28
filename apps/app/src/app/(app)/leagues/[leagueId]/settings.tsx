import type { Schemas } from '@myfantasyprem/api-client';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { useDraft, useDraftControl, useUpdateDraft } from '@/api/draft-hooks';
import { useFreshness } from '@/api/freshness';
import { useLeagueHome } from '@/api/hooks';
import { useWaiverState } from '@/api/moves-hooks';
import {
  useLeaveLeague,
  useMembers,
  useRemoveMember,
  useTransferCommissioner,
  useUpdateLeague,
} from '@/api/league-admin-hooks';
import { AppText } from '@/components/AppText';
import { Avatar } from '@/components/Badges';
import { Button, IconButton } from '@/components/Button';
import { Card } from '@/components/Card';
import { Pill } from '@/components/Chip';
import { Crown, LayoutGrid, LogOut, Play, Trash } from '@/components/icons';
import { InviteCard } from '@/components/InviteCard';
import {
  fromLeagueSettings,
  LeagueSettingsForm,
  settingsInput,
  type EditableSettings,
} from '@/components/LeagueSettingsForm';
import { ListRow } from '@/components/ListRow';
import { ImportedLeagueSections } from '@/components/sync/SyncPanels';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { SegmentedControl } from '@/components/SegmentedControl';
import { ConfirmDialog } from '@/components/Sheet';
import { SkeletonRows } from '@/components/Skeleton';
import { TextField } from '@/components/TextField';
import { useToast } from '@/components/Toast';
import { startBlocker, startExplanation } from '@/lib/draft';
import { describeError } from '@/lib/errors';
import { leagueStatus } from '@/lib/leagues';
import { formatDateTime } from '@/lib/time';
import { waiverOrderEffect } from '@/lib/waivers';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Commissioner settings, invites, members and the draft schedule. Read-only for members. */
export default function LeagueSettingsScreen() {
  const { leagueId } = useLocalSearchParams<{ leagueId: string }>();
  const home = useLeagueHome(leagueId);
  const freshness = useFreshness(home);
  return (
    <Screen
      title="League settings"
      back={{ label: 'League', href: `/leagues/${leagueId}/league` }}
      freshness={freshness}
      maxWidth={720}
    >
      <QueryStatus query={home}>
        {({ data }) => <SettingsBody leagueId={leagueId} home={data} />}
      </QueryStatus>
    </Screen>
  );
}

function SettingsBody({ leagueId, home }: { leagueId: string; home: Schemas['LeagueHome'] }) {
  const isCommissioner = home.viewer.role === 'commissioner';
  const imported = home.league.source === 'fpl_import';
  const preDraft = home.league.status === 'pre_draft';
  const { spacing } = useTheme();
  const st = leagueStatus(home.league.status);
  return (
    <View style={{ gap: spacing.lg }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        <Avatar name={home.league.name} size={44} square />
        <View style={{ flex: 1, minWidth: 0 }}>
          <AppText variant="heading" numberOfLines={1} testID="settings-league-name">
            {home.league.name}
          </AppText>
          <AppText variant="meta" muted>
            {`${home.league.memberCount} of ${home.league.teamCount} teams · ${home.league.season}`}
          </AppText>
        </View>
        <Pill label={st.label} tone={st.tone} />
      </View>

      {isCommissioner && preDraft ? (
        <>
          <SectionHeader title="Invite managers" />
          <InviteCard leagueId={leagueId} leagueName={home.league.name} />
        </>
      ) : null}

      {imported ? (
        <ImportedLeagueSections
          leagueId={leagueId}
          leagueName={home.league.name}
          isCommissioner={isCommissioner}
          myTeamId={home.viewer.team?.id ?? null}
        />
      ) : null}

      {preDraft || home.league.status === 'drafting' ? (
        <DraftSection leagueId={leagueId} home={home} isCommissioner={isCommissioner} />
      ) : null}

      <SectionHeader title="Settings" />
      {isCommissioner ? (
        <CommissionerSettings leagueId={leagueId} home={home} />
      ) : (
        <LeagueSettingsForm
          value={fromLeagueSettings(home.settings)}
          onChange={() => undefined}
          locked
          readOnly
        />
      )}

      <MembersSection leagueId={leagueId} home={home} isCommissioner={isCommissioner} />
    </View>
  );
}

function CommissionerSettings({
  leagueId,
  home,
}: {
  leagueId: string;
  home: Schemas['LeagueHome'];
}) {
  const { spacing } = useTheme();
  const toast = useToast();
  const update = useUpdateLeague(leagueId);
  const waivers = useWaiverState(leagueId);
  const initial = fromLeagueSettings(home.settings);
  const [name, setName] = useState(home.league.name);
  const [settings, setSettings] = useState<EditableSettings>(initial);
  const locked = home.league.status !== 'pre_draft';
  const nameChanged = name.trim() !== home.league.name;
  const settingsChanged = JSON.stringify(settings) !== JSON.stringify(initial);

  const save = () => {
    const body: Schemas['LeagueUpdate'] = {};
    if (nameChanged) body.name = name.trim();
    if (settingsChanged && !locked) {
      body.settings = {
        ...(settings.teamCount !== initial.teamCount ? { teamCount: settings.teamCount } : {}),
        ...settingsInput(settings),
      };
    } else if (settings.waiverOrder !== initial.waiverOrder) {
      // The only setting that changes after the draft has started.
      body.settings = { waivers: { order: settings.waiverOrder } };
    }
    update.mutate(body, {
      onSuccess: () => toast({ message: 'Settings saved', tone: 'success' }),
      onError: (e) => toast({ message: describeError(e), tone: 'error' }),
    });
  };

  return (
    <View style={{ gap: spacing.lg }}>
      <Card>
        <TextField
          label="League name"
          value={name}
          onChangeText={setName}
          maxLength={40}
          testID="settings-name"
        />
      </Card>
      <LeagueSettingsForm
        value={settings}
        onChange={setSettings}
        locked={locked}
        minTeams={home.league.memberCount}
        waiverNote={waiverOrderEffect(waivers.data?.data, home.league.status)}
      />
      <Button
        label="Save settings"
        onPress={save}
        disabled={!nameChanged && !settingsChanged}
        loading={update.isPending}
        testID="save-settings"
        style={{ alignSelf: 'flex-start' }}
      />
    </View>
  );
}

const START_PRESETS = [
  { value: 'none', label: 'Not set' },
  { value: 'tomorrow', label: 'Tomorrow' },
  { value: 'weekend', label: 'Saturday' },
] as const;

function presetTime(preset: string, now = new Date()): string | null {
  if (preset === 'none') return null;
  const d = new Date(now);
  d.setHours(20, 0, 0, 0);
  if (preset === 'tomorrow') d.setDate(d.getDate() + 1);
  else d.setDate(d.getDate() + ((6 - d.getDay() + 7) % 7 || 7));
  return d.toISOString();
}

function DraftSection({
  leagueId,
  home,
  isCommissioner,
}: {
  leagueId: string;
  home: Schemas['LeagueHome'];
  isCommissioner: boolean;
}) {
  const router = useRouter();
  const toast = useToast();
  const { spacing } = useTheme();
  const draft = useDraft(leagueId);
  const control = useDraftControl(leagueId);
  const update = useUpdateDraft(leagueId);
  const preDraft = home.league.status === 'pre_draft';
  const startsAt = draft.data?.data.startsAt ?? home.settings.draft.startsAt;
  const startBlocked = startBlocker(home.league);

  return (
    <>
      <SectionHeader title="Draft" />
      <Card style={{ gap: spacing.md }} testID="draft-section">
        <AppText>
          {home.league.status === 'drafting'
            ? 'The draft is running.'
            : startsAt
              ? `Scheduled for ${formatDateTime(Date.parse(startsAt))}`
              : 'No start time yet.'}
        </AppText>
        {isCommissioner && preDraft ? (
          <>
            <SegmentedControl
              kind="radio"
              accessibilityLabel="Draft start"
              segments={START_PRESETS}
              value={startsAt ? 'custom' : 'none'}
              onChange={(v) =>
                update.mutate(
                  { startsAt: presetTime(v) },
                  { onError: (e) => toast({ message: describeError(e), tone: 'error' }) },
                )
              }
            />
            <AppText variant="meta" muted>
              Quick start times are 8pm your time.
            </AppText>
            <AppText variant="meta" muted>
              {startExplanation(home.league)}
            </AppText>
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}>
              <Button
                label="Start draft now"
                icon={Play}
                disabled={startBlocked !== null}
                loading={control.isPending}
                onPress={() =>
                  control.mutate('start', {
                    onSuccess: () => router.push(`/leagues/${leagueId}/draft`),
                    onError: (e) => toast({ message: describeError(e), tone: 'error' }),
                  })
                }
                testID="start-draft"
              />
              <Button
                label="Randomise order"
                variant="secondary"
                loading={update.isPending}
                onPress={() =>
                  update.mutate(
                    { randomizeOrder: true },
                    {
                      onSuccess: () => toast({ message: 'Draft order shuffled', tone: 'success' }),
                      onError: (e) => toast({ message: describeError(e), tone: 'error' }),
                    },
                  )
                }
              />
            </View>
          </>
        ) : null}
        <Button
          label="Open draft room"
          variant="secondary"
          icon={LayoutGrid}
          onPress={() => router.push(`/leagues/${leagueId}/draft`)}
          style={{ alignSelf: 'flex-start' }}
          testID="open-draft-room"
        />
      </Card>
    </>
  );
}

function MembersSection({
  leagueId,
  home,
  isCommissioner,
}: {
  leagueId: string;
  home: Schemas['LeagueHome'];
  isCommissioner: boolean;
}) {
  const router = useRouter();
  const toast = useToast();
  const members = useMembers(leagueId);
  const remove = useRemoveMember(leagueId);
  const leave = useLeaveLeague(leagueId);
  const transfer = useTransferCommissioner(leagueId);
  const [transferring, setTransferring] = useState<Schemas['Member'] | null>(null);
  const [removing, setRemoving] = useState<Schemas['Member'] | null>(null);
  const [leaving, setLeaving] = useState(false);
  const { colors, spacing } = useTheme();
  const myTeamId = home.viewer.team?.id;

  return (
    <>
      <SectionHeader
        title="Managers"
        detail={members.data ? String(members.data.data.length) : undefined}
      />
      <QueryStatus
        query={members}
        loading={
          <Card flush>
            <SkeletonRows count={4} />
          </Card>
        }
      >
        {({ data }) => (
          <Card flush>
            {data.map((m, i) => (
              <ListRow
                key={m.user.id}
                divider={i > 0}
                left={<Avatar name={m.user.displayName} size={36} />}
                title={
                  // Wraps rather than truncates: the actions sit under the name, not beside it.
                  <AppText weight="semibold" testID={`member-name-${m.user.id}`}>
                    {m.team.name}
                  </AppText>
                }
                subtitle={
                  <View style={{ gap: spacing.sm }}>
                    <AppText variant="meta" muted numberOfLines={1}>
                      {`${m.user.displayName}${m.team.id === myTeamId ? ' · you' : ''}`}
                    </AppText>
                    {m.role !== 'commissioner' && isCommissioner ? (
                      <View
                        style={{
                          flexDirection: 'row',
                          flexWrap: 'wrap',
                          alignItems: 'center',
                          gap: spacing.sm,
                        }}
                      >
                        <Button
                          label="Make commissioner"
                          variant="secondary"
                          size="sm"
                          onPress={() => setTransferring(m)}
                          testID={`make-commissioner-${m.user.id}`}
                        />
                        <IconButton
                          icon={Trash}
                          label={`Remove ${m.user.displayName}`}
                          color={colors.loss}
                          onPress={() => setRemoving(m)}
                          testID={`remove-member-${m.user.id}`}
                        />
                      </View>
                    ) : null}
                  </View>
                }
                right={
                  m.role === 'commissioner' ? (
                    <Crown size={18} color={colors.accent} accessibilityLabel="Commissioner" />
                  ) : null
                }
              />
            ))}
          </Card>
        )}
      </QueryStatus>

      {!isCommissioner ? (
        <Button
          label="Leave league"
          variant="destructive"
          icon={LogOut}
          onPress={() => setLeaving(true)}
          style={{ alignSelf: 'flex-start' }}
          testID="leave-league"
        />
      ) : null}

      <ConfirmDialog
        visible={removing !== null}
        title={`Remove ${removing?.user.displayName ?? ''}?`}
        message={
          home.league.status === 'pre_draft'
            ? 'Their team is deleted and the place opens for someone else.'
            : 'Their team stays in the league without a manager.'
        }
        confirmLabel="Remove"
        destructive
        loading={remove.isPending}
        onCancel={() => setRemoving(null)}
        onConfirm={() =>
          removing &&
          remove.mutate(removing.user.id, {
            onSettled: () => setRemoving(null),
            onError: (e) => toast({ message: describeError(e), tone: 'error' }),
          })
        }
      />
      <ConfirmDialog
        visible={transferring !== null}
        title={`Make ${transferring?.user.displayName ?? ''} commissioner?`}
        message="They take over league settings and members. You become a regular manager."
        confirmLabel="Make commissioner"
        loading={transfer.isPending}
        onCancel={() => setTransferring(null)}
        onConfirm={() =>
          transferring &&
          transfer.mutate(transferring.user.id, {
            onSuccess: () =>
              toast({
                message: `${transferring.user.displayName} is now commissioner`,
                tone: 'success',
              }),
            onSettled: () => setTransferring(null),
            onError: (e) => toast({ message: describeError(e), tone: 'error' }),
          })
        }
      />
      <ConfirmDialog
        visible={leaving}
        title={`Leave ${home.league.name}?`}
        message={
          home.league.status === 'pre_draft'
            ? 'Your team is deleted. You can rejoin with an invite before the draft.'
            : 'Your team stays in the league without a manager. You cannot come back to it.'
        }
        confirmLabel="Leave league"
        destructive
        loading={leave.isPending}
        onCancel={() => setLeaving(false)}
        onConfirm={() =>
          leave.mutate(undefined, {
            onSuccess: () => {
              toast({ message: `You left ${home.league.name}` });
              router.replace('/leagues');
            },
            onError: (e) => {
              setLeaving(false);
              toast({ message: describeError(e), tone: 'error' });
            },
          })
        }
      />
    </>
  );
}
