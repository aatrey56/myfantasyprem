import type { Schemas } from '@myfantasyprem/api-client';
import { useRouter } from 'expo-router';
import { useState } from 'react';
import { View } from 'react-native';

import { useCreateLeague } from '@/api/account-hooks';
import { useAuth } from '@/auth/AuthProvider';
import { AppText } from '@/components/AppText';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { CircleCheck } from '@/components/icons';
import { InviteCard } from '@/components/InviteCard';
import {
  DEFAULT_SETTINGS,
  LeagueSettingsForm,
  settingsInput,
  type EditableSettings,
} from '@/components/LeagueSettingsForm';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { TextField } from '@/components/TextField';
import { describeError, fieldErrors, problemErrors } from '@/lib/errors';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Create a league: name, your team, size and rules; then share the invite. */
export default function CreateLeagueScreen() {
  const router = useRouter();
  const { user } = useAuth();
  const { colors, spacing } = useTheme();
  const create = useCreateLeague();
  const [name, setName] = useState('');
  const [teamName, setTeamName] = useState(user ? `${user.displayName.split(' ')[0]} FC` : '');
  const [settings, setSettings] = useState<EditableSettings>(DEFAULT_SETTINGS);
  const [localError, setLocalError] = useState<{ name?: string; teamName?: string }>({});
  const [created, setCreated] = useState<Schemas['League'] | null>(null);

  const submit = () => {
    const errors: { name?: string; teamName?: string } = {};
    if (name.trim().length < 3 || name.trim().length > 40)
      errors.name = 'League name must be 3 to 40 characters.';
    if (teamName.trim().length < 1 || teamName.trim().length > 32)
      errors.teamName = 'Team name must be 1 to 32 characters.';
    setLocalError(errors);
    if (errors.name || errors.teamName) return;
    create.mutate(
      {
        name: name.trim(),
        teamName: teamName.trim(),
        teamCount: settings.teamCount,
        settings: settingsInput(settings),
      },
      { onSuccess: (envelope) => setCreated(envelope.data) },
    );
  };

  if (created) {
    const id = created.league.id;
    return (
      <Screen
        title="League created"
        eyebrow="Step 2 of 2 · invite"
        subtitle={`Share the code so ${created.league.teamCount - 1} more managers can join ${created.league.name}.`}
        back={{ label: 'Leagues', href: '/leagues' }}
        maxWidth={640}
      >
        <View
          testID="league-created"
          style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}
        >
          <CircleCheck size={20} color={colors.win} aria-hidden />
          <AppText weight="semibold">{created.league.name}</AppText>
        </View>
        <InviteCard leagueId={id} leagueName={created.league.name} autoCreate />
        <AppText variant="meta" muted>
          When every team has a manager, start the draft from the league&apos;s settings, or
          schedule it for later.
        </AppText>
        <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}>
          <Button
            label="Go to league"
            onPress={() => router.replace(`/leagues/${id}`)}
            testID="go-to-league"
          />
          <Button
            label="League settings"
            variant="secondary"
            onPress={() => router.replace(`/leagues/${id}/settings`)}
          />
        </View>
      </Screen>
    );
  }

  const serverErrors = problemErrors(create.error);
  return (
    <Screen
      title="Create a league"
      eyebrow="Step 1 of 2 · settings"
      back={{ label: 'Leagues', href: '/leagues' }}
      maxWidth={640}
    >
      <Card style={{ gap: spacing.lg }}>
        <TextField
          label="League name"
          value={name}
          onChangeText={setName}
          placeholder="Friday Night Fantasy"
          maxLength={40}
          error={localError.name ?? fieldErrors(create.error, '/name')}
          testID="league-name-input"
          autoFocus
        />
        <TextField
          label="Your team name"
          value={teamName}
          onChangeText={setTeamName}
          maxLength={32}
          hint="You can rename it later."
          error={localError.teamName ?? fieldErrors(create.error, '/teamName')}
          testID="team-name-input"
        />
      </Card>

      <SectionHeader title="Settings" />
      <LeagueSettingsForm value={settings} onChange={setSettings} />

      {create.isError ? (
        <Card testID="create-error">
          <AppText color={colors.loss} weight="semibold">
            {describeError(create.error)}
          </AppText>
          {serverErrors.map((m) => (
            <AppText key={m} variant="meta" muted>
              {`• ${m}`}
            </AppText>
          ))}
        </Card>
      ) : null}

      <Button
        label="Create league"
        onPress={submit}
        loading={create.isPending}
        testID="create-league-submit"
        fullWidth
      />
    </Screen>
  );
}
