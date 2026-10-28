import { send, type Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { Switch, View } from 'react-native';

import { useAppConfig } from '@/api/assets';
import { api } from '@/api/client';
import { useFreshness } from '@/api/freshness';
import {
  useNotificationPreferences,
  useUpdateNotificationPreferences,
} from '@/api/notification-hooks';
import { AppText } from '@/components/AppText';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { Chip } from '@/components/Chip';
import { EmptyState } from '@/components/EmptyState';
import { Bell } from '@/components/icons';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { SectionHeader } from '@/components/SectionHeader';
import { Skeleton, SkeletonRows } from '@/components/Skeleton';
import { useToast } from '@/components/Toast';
import { describeError } from '@/lib/errors';
import {
  disableWebPush,
  enableWebPush,
  hasStoredSubscription,
  pushPermission,
  pushSupport,
} from '@/lib/web-push';
import { groupTypes } from '@/lib/notifications';
import { useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/** Account → Notifications: which pushes to get, per league, and turning on browser push. */
export default function NotificationSettingsScreen() {
  const prefs = useNotificationPreferences();
  const freshness = useFreshness(prefs);
  const update = useUpdateNotificationPreferences();
  const toast = useToast();
  const { spacing, colors } = useTheme();
  const [picked, setPicked] = useState<string | null>(null);

  const change = (body: Schemas['NotificationPreferencesUpdate']) =>
    update.mutate(body, { onError: (e) => toast({ message: describeError(e), tone: 'error' }) });

  return (
    <Screen
      title="Notifications"
      back={{ label: 'Account', href: '/account' }}
      freshness={freshness}
      maxWidth={640}
    >
      <PushPrompt />
      <QueryStatus
        query={prefs}
        loading={
          <Card flush>
            <SkeletonRows count={8} testID="notification-settings-loading" />
          </Card>
        }
      >
        {({ data }) => {
          if (data.leagues.length === 0) {
            return (
              <Card>
                <EmptyState
                  icon={Bell}
                  title="No leagues yet"
                  body="Join or create a league to choose its notifications."
                  testID="notification-settings-empty"
                />
              </Card>
            );
          }
          const league = data.leagues.find((l) => l.leagueId === picked) ?? data.leagues[0]!;
          const enabled = new Map(league.settings.map((s) => [s.type, s.enabled]));
          const allOn = league.settings.every((s) => s.enabled);
          const allOff = league.settings.every((s) => !s.enabled);
          return (
            <>
              {data.leagues.length > 1 ? (
                <View
                  style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }}
                  accessibilityRole="radiogroup"
                  accessibilityLabel="League"
                  testID="notification-league-picker"
                >
                  {data.leagues.map((l) => (
                    <Chip
                      key={l.leagueId}
                      label={l.leagueName}
                      selected={l.leagueId === league.leagueId}
                      onPress={() => setPicked(l.leagueId)}
                      testID={`notification-league-${l.leagueId}`}
                    />
                  ))}
                </View>
              ) : null}
              <Card style={{ gap: spacing.md }}>
                <AppText variant="meta" muted>
                  {`Settings for ${league.leagueName}. Everything also lands in your Feed.`}
                </AppText>
                <View style={{ flexDirection: 'row', gap: spacing.sm }}>
                  <Button
                    label="All on"
                    size="sm"
                    variant="secondary"
                    disabled={allOn}
                    onPress={() => change({ leagueId: league.leagueId, all: true })}
                    testID="notifications-all-on"
                  />
                  <Button
                    label="All off"
                    size="sm"
                    variant="secondary"
                    disabled={allOff}
                    onPress={() => change({ leagueId: league.leagueId, all: false })}
                    testID="notifications-all-off"
                  />
                </View>
              </Card>
              {groupTypes(data.types).map((g) => (
                <View
                  key={g.group}
                  style={{ gap: spacing.sm }}
                  testID={`notification-group-${g.group}`}
                >
                  <SectionHeader title={g.title} />
                  <Card flush>
                    {g.types.map((t, i) => {
                      const on = enabled.get(t.type) ?? true;
                      const labelId = `notification-label-${t.type}`;
                      return (
                        <View
                          key={t.type}
                          style={{
                            flexDirection: 'row',
                            alignItems: 'center',
                            gap: spacing.md,
                            paddingHorizontal: spacing.lg,
                            paddingVertical: spacing.md,
                            borderTopWidth: i > 0 ? 1 : 0,
                            borderTopColor: colors.border,
                          }}
                        >
                          <AppText style={{ flex: 1 }} nativeID={labelId}>
                            {t.label}
                          </AppText>
                          <Switch
                            value={on}
                            onValueChange={(v) =>
                              change({
                                leagueId: league.leagueId,
                                settings: [{ type: t.type, enabled: v }],
                              })
                            }
                            accessibilityLabel={t.label}
                            aria-labelledby={labelId}
                            testID={`notification-switch-${t.type}`}
                            trackColor={{ true: colors.accent, false: colors.surfaceRaised }}
                            thumbColor={colors.text}
                            {...({ activeThumbColor: colors.onAccent } as object)}
                          />
                        </View>
                      );
                    })}
                  </Card>
                </View>
              ))}
            </>
          );
        }}
      </QueryStatus>
    </Screen>
  );
}

const SUPPORT_TEXT: Record<string, string> = {
  native: 'Push notifications come with the iOS app. Until then, everything shows in your Feed.',
  unsupported: "This browser can't show push notifications. Everything still shows in your Feed.",
  insecure: 'Push notifications need the app served over HTTPS.',
  'needs-home-screen':
    'On iPhone and iPad, web push only works from the Home Screen app: tap Share, then Add to Home Screen, and open MyFantasyPrem from there (over HTTPS).',
};

/** Turning on push for this browser, with why it may not be possible here. */
function PushPrompt() {
  const config = useAppConfig(true);
  const toast = useToast();
  const { spacing } = useTheme();
  const support = pushSupport();
  const [state, setState] = useState<'off' | 'on' | 'denied' | 'busy'>(() =>
    pushPermission() === 'denied' ? 'denied' : hasStoredSubscription() ? 'on' : 'off',
  );
  const key = config.data?.data.webPushPublicKey ?? null;

  const enable = async () => {
    if (!key) return;
    setState('busy');
    try {
      const result = await enableWebPush(key, async (sub) => {
        const res = await send(
          api.POST('/v1/me/push-subscriptions', { body: { kind: 'webpush', ...sub } }),
        );
        return res.data.id;
      });
      setState(result === 'enabled' ? 'on' : 'denied');
    } catch (e) {
      setState('off');
      toast({ message: describeError(e), tone: 'error' });
    }
  };
  const disable = async () => {
    setState('busy');
    try {
      await disableWebPush(async (id) => {
        await api.DELETE('/v1/me/push-subscriptions/{subscriptionId}', {
          params: { path: { subscriptionId: id } },
        });
      });
    } finally {
      setState('off');
    }
  };

  return (
    <Card style={{ gap: spacing.sm }} testID="push-prompt">
      <AppText weight="semibold">Push notifications on this device</AppText>
      {support !== 'available' ? (
        <AppText variant="meta" muted testID="push-unavailable">
          {SUPPORT_TEXT[support]}
        </AppText>
      ) : config.isPending ? (
        <Skeleton width="60%" height={14} />
      ) : !key ? (
        <AppText variant="meta" muted testID="push-unconfigured">
          {"Push isn't set up on this server yet. Everything still shows in your Feed."}
        </AppText>
      ) : state === 'denied' ? (
        <AppText variant="meta" muted testID="push-denied">
          {
            "Notifications are blocked for this site. Allow them in your browser's site settings, then come back."
          }
        </AppText>
      ) : (
        <>
          <AppText variant="meta" muted>
            {state === 'on'
              ? 'This browser gets the notifications switched on below.'
              : 'Get the notifications switched on below even when the app is closed. On iPhone, add the app to your Home Screen first (it needs HTTPS).'}
          </AppText>
          <Button
            label={state === 'on' ? 'Turn off on this device' : 'Turn on notifications'}
            variant={state === 'on' ? 'secondary' : 'primary'}
            size="sm"
            loading={state === 'busy'}
            onPress={() => void (state === 'on' ? disable() : enable())}
            testID="push-toggle"
            style={{ alignSelf: 'flex-start' }}
          />
        </>
      )}
    </Card>
  );
}
