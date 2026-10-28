import { send, type Schemas } from '@myfantasyprem/api-client';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import { rollback, snapshot } from './optimistic';
import { useScreenFocus } from './screen-focus';
import { haptics } from '@/lib/haptics';

type Prefs = { data: Schemas['NotificationPreferences']; meta: Schemas['Meta'] };
type Inbox = { data: Schemas['NotificationInbox']; meta: Schemas['Meta'] };

export const notificationKeys = {
  preferences: ['me', 'notifications', 'preferences'] as const,
  inbox: ['me', 'notifications', 'inbox'] as const,
};

/** My push settings, one switch per type in each of my leagues. */
export function useNotificationPreferences() {
  const { subscribed } = useScreenFocus(notificationKeys.preferences);
  return useQuery({
    queryKey: notificationKeys.preferences,
    queryFn: () => send(api.GET('/v1/me/notifications/preferences')),
    subscribed,
  });
}

/** What the server will do with an update, applied to a league's settings. */
export function applyUpdate(
  prefs: Schemas['NotificationPreferences'],
  update: Schemas['NotificationPreferencesUpdate'],
): Schemas['NotificationPreferences'] {
  return {
    ...prefs,
    leagues: prefs.leagues.map((l) => {
      if (update.leagueId && update.leagueId !== l.leagueId) return l;
      return {
        ...l,
        settings: l.settings.map((s) => {
          let enabled = update.all ?? s.enabled;
          const change = update.settings?.find((c) => c.type === s.type);
          if (change) enabled = change.enabled;
          return { ...s, enabled };
        }),
      };
    }),
  };
}

/** Flips switches (or All on / All off) at once; flips back if the server says no. */
export function useUpdateNotificationPreferences() {
  const queryClient = useQueryClient();
  const key = notificationKeys.preferences;
  return useMutation({
    mutationFn: (body: Schemas['NotificationPreferencesUpdate']) =>
      send(api.PUT('/v1/me/notifications/preferences', { body })),
    onMutate: async (body) => {
      haptics.light();
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const before = queryClient.getQueryData<Prefs>(key);
      if (before)
        queryClient.setQueryData<Prefs>(key, { ...before, data: applyUpdate(before.data, body) });
      return { saved };
    },
    onError: (_e, _v, context) => rollback(queryClient, context?.saved),
    onSuccess: (res) => queryClient.setQueryData<Prefs>(key, res),
  });
}

/** My notification inbox (FEED), newest first, with the unread count. */
export function useInbox() {
  const { subscribed } = useScreenFocus(notificationKeys.inbox);
  return useQuery({
    queryKey: notificationKeys.inbox,
    queryFn: () => send(api.GET('/v1/me/notifications', { params: { query: { limit: 50 } } })),
    refetchInterval: 60_000,
    subscribed,
  });
}

/** The unread count for the FEED tab's badge. */
export function useUnreadCount(): number {
  const q = useQuery({
    queryKey: notificationKeys.inbox,
    queryFn: () => send(api.GET('/v1/me/notifications', { params: { query: { limit: 50 } } })),
    refetchInterval: 60_000,
  });
  return q.data?.data.unreadCount ?? 0;
}

/** Marks some (or all) notifications read, at once on screen. */
export function useMarkRead() {
  const queryClient = useQueryClient();
  const key = notificationKeys.inbox;
  return useMutation({
    mutationFn: async (body: Schemas['NotificationReadRequest']) => {
      const { response, error } = await api.POST('/v1/me/notifications/read', { body });
      if (!response.ok) await send(Promise.resolve({ response, error }));
    },
    onMutate: async (body) => {
      const saved = await snapshot(queryClient, { queryKey: key, exact: true });
      const before = queryClient.getQueryData<Inbox>(key);
      if (before) {
        const now = new Date().toISOString();
        const items = before.data.items.map((n) =>
          n.readAt === null && (body.all || body.ids?.includes(n.id)) ? { ...n, readAt: now } : n,
        );
        queryClient.setQueryData<Inbox>(key, {
          ...before,
          data: { items, unreadCount: items.filter((n) => n.readAt === null).length },
        });
      }
      return { saved };
    },
    onError: (_e, _v, context) => rollback(queryClient, context?.saved),
    onSettled: () => queryClient.invalidateQueries({ queryKey: key }),
  });
}
