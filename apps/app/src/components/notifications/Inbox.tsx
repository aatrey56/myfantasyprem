import type { Schemas } from '@myfantasyprem/api-client';
import { type Href, useRouter } from 'expo-router';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { useInbox, useMarkRead } from '@/api/notification-hooks';
import { AppText } from '@/components/AppText';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { pressFeedback, type PressState } from '@/components/pressable';
import { SectionHeader } from '@/components/SectionHeader';
import { notificationHref } from '@/lib/notification-links';
import { formatAgo } from '@/lib/time';
import { tint, useTheme } from '@/theme';

/** How many notifications show before "Show all". */
const FIRST = 5;

/** FEED's notifications: trade, waiver and player alerts, unread first marked. */
export function Inbox() {
  const inbox = useInbox();
  const markRead = useMarkRead();
  const [all, setAll] = useState(false);
  const { spacing } = useTheme();
  const items = inbox.data?.data.items ?? [];
  const unread = inbox.data?.data.unreadCount ?? 0;
  if (items.length === 0) return null;
  const shown = all ? items : items.slice(0, FIRST);
  return (
    <View style={{ gap: spacing.sm }} testID="inbox">
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <View style={{ flex: 1 }}>
          <SectionHeader title={unread > 0 ? `Notifications · ${unread} new` : 'Notifications'} />
        </View>
        {unread > 0 ? (
          <Button
            label="Mark all read"
            size="sm"
            variant="ghost"
            onPress={() => markRead.mutate({ all: true })}
            testID="inbox-mark-all-read"
          />
        ) : null}
      </View>
      <Card flush>
        {shown.map((n, i) => (
          <InboxRow
            key={n.id}
            n={n}
            divider={i > 0}
            onOpen={() => {
              if (n.readAt === null) markRead.mutate({ ids: [n.id] });
            }}
          />
        ))}
      </Card>
      {items.length > FIRST ? (
        <Button
          label={all ? 'Show fewer' : `Show all ${items.length}`}
          size="sm"
          variant="ghost"
          onPress={() => setAll((v) => !v)}
          testID="inbox-show-all"
          style={{ alignSelf: 'flex-start' }}
        />
      ) : null}
    </View>
  );
}

function InboxRow({
  n,
  divider,
  onOpen,
}: {
  n: Schemas['Notification'];
  divider: boolean;
  onOpen: () => void;
}) {
  const { colors, spacing } = useTheme();
  const router = useRouter();
  const unread = n.readAt === null;
  return (
    <Pressable
      onPress={() => {
        onOpen();
        router.push(notificationHref(n.url) as Href);
      }}
      accessibilityRole="button"
      accessibilityLabel={`${unread ? 'Unread: ' : ''}${n.title}. ${n.body}`}
      testID={`inbox-item-${n.id}`}
      style={(state: PressState) => [
        {
          flexDirection: 'row',
          gap: spacing.md,
          paddingHorizontal: spacing.lg,
          paddingVertical: spacing.md,
          borderTopWidth: divider ? 1 : 0,
          borderTopColor: colors.border,
          backgroundColor: unread ? tint(colors.accent, colors.surface) : 'transparent',
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <View
        aria-hidden
        testID={unread ? 'inbox-unread-dot' : undefined}
        style={{
          width: 8,
          height: 8,
          marginTop: 6,
          borderRadius: 4,
          backgroundColor: unread ? colors.accent : 'transparent',
        }}
      />
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <AppText weight={unread ? 'bold' : 'semibold'} numberOfLines={2}>
          {n.title}
        </AppText>
        <AppText variant="meta" muted numberOfLines={3}>
          {n.body}
        </AppText>
        <AppText variant="meta" subtle>
          {formatAgo(Date.parse(n.createdAt))}
        </AppText>
      </View>
    </Pressable>
  );
}
