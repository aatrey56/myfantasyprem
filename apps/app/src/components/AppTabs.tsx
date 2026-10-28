import { usePathname, useRouter } from 'expo-router';
import { TabList, TabSlot, TabTrigger, Tabs, type TabTriggerSlotProps } from 'expo-router/ui';
import type { ReactNode } from 'react';
import { Pressable, View, type GestureResponderEvent } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { CircleUserRound, Goal, Newspaper, Trophy, type LucideIcon } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { BottomTabBarContext } from './tab-bar-context';
import { useUnreadCount } from '@/api/notification-hooks';
import { haptics } from '@/lib/haptics';
import { useTheme } from '@/theme';

/**
 * App-wide sections, Sleeper-style: LEAGUES (the hub and every league
 * under it), SCORES (this gameweek's fixtures), FEED (moves across my
 * leagues) and ACCOUNT. `name` is the route under app/(app).
 */
export const APP_TABS: readonly { name: string; label: string; href: string; icon: LucideIcon }[] =
  [
    { name: 'leagues', label: 'Leagues', href: '/leagues', icon: Trophy },
    { name: 'scores', label: 'Scores', href: '/scores', icon: Goal },
    { name: 'feed', label: 'Feed', href: '/feed', icon: Newspaper },
    { name: 'account', label: 'Account', href: '/account', icon: CircleUserRound },
  ];

/** Full-screen routes that hide the bar: the draft room needs every pixel. */
export function hidesAppBar(pathname: string): boolean {
  return /^\/leagues\/[^/]+\/draft\/?$/.test(pathname);
}

/**
 * The signed-in app: one stack per section over a bottom bar that stays
 * visible inside a league too. Each section keeps its own history, so
 * leaving a league for SCORES and coming back returns to the same screen.
 */
export function AppTabs() {
  const pathname = usePathname();
  const shown = !hidesAppBar(pathname);
  const unread = useUnreadCount();
  return (
    <BottomTabBarContext.Provider value={shown}>
      <Tabs style={{ flex: 1, flexDirection: 'column', minHeight: 0, padding: 0, gap: 0 }}>
        <TabSlot style={{ flex: 1 }} />
        <TabList asChild>
          <AppBar shown={shown}>
            {APP_TABS.map((tab) => (
              <TabTrigger key={tab.name} name={tab.name} href={tab.href as never} asChild>
                <AppBarItem
                  name={tab.name}
                  label={tab.label}
                  icon={tab.icon}
                  root={tab.href}
                  pathname={pathname}
                  badge={tab.name === 'feed' ? unread : 0}
                />
              </TabTrigger>
            ))}
          </AppBar>
        </TabList>
      </Tabs>
    </BottomTabBarContext.Provider>
  );
}

function AppBar({ shown, children, ...rest }: { shown: boolean; children?: ReactNode }) {
  const { colors, layout } = useTheme();
  const insets = useSafeAreaInsets();
  return (
    <View
      role="navigation"
      aria-label="App sections"
      testID="app-tabs"
      style={{
        display: shown ? 'flex' : 'none',
        backgroundColor: colors.surface,
        borderTopColor: colors.border,
        borderTopWidth: 1,
        paddingBottom: insets.bottom,
        paddingLeft: insets.left,
        paddingRight: insets.right,
        alignItems: 'center',
      }}
    >
      <View
        {...rest}
        accessibilityRole="tablist"
        style={{
          flexDirection: 'row',
          height: layout.bottomTabsHeight,
          width: '100%',
          maxWidth: 560,
        }}
      >
        {children}
      </View>
    </View>
  );
}

function AppBarItem({
  name,
  label,
  icon: Icon,
  root,
  pathname,
  badge,
  isFocused,
  onPress,
  ...props
}: TabTriggerSlotProps & {
  badge: number;
  name: string;
  label: string;
  icon: LucideIcon;
  root: string;
  pathname: string;
}) {
  const { colors, radii, spacing } = useTheme();
  const router = useRouter();
  const color = isFocused ? colors.accent : colors.textMuted;
  const press = (e: GestureResponderEvent) => {
    if (isFocused) {
      // The section you are in: back to its first screen (a league → the hub).
      e.preventDefault?.();
      if (pathname !== root) router.navigate(root as never);
      return;
    }
    haptics.selection();
    onPress?.(e);
  };
  return (
    <Pressable
      {...props}
      onPress={press}
      testID={`app-tab-${name}`}
      accessibilityRole="tab"
      accessibilityLabel={badge > 0 ? `${label}, ${badge} unread` : label}
      accessibilityState={{ selected: Boolean(isFocused) }}
      aria-selected={Boolean(isFocused)}
      style={(state: PressState) => [
        { flex: 1, alignItems: 'center', justifyContent: 'center', gap: 3 },
        ...pressFeedback(colors, state, { hoverFill: !isFocused }),
        state.pressed ? { transform: [{ scale: 0.94 }] } : null,
      ]}
    >
      <View>
        <Icon size={22} color={color} strokeWidth={isFocused ? 2.4 : 2} aria-hidden />
        {badge > 0 ? (
          <View
            testID={`app-tab-${name}-badge`}
            aria-hidden
            style={{
              position: 'absolute',
              top: -4,
              left: 14,
              minWidth: 16,
              height: 16,
              paddingHorizontal: spacing.xs,
              borderRadius: radii.pill,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: colors.danger,
            }}
          >
            <AppText
              numeric
              weight="bold"
              variant="meta"
              color={colors.onDanger}
              style={{ fontSize: 10, lineHeight: 12 }}
            >
              {badge > 99 ? '99+' : String(badge)}
            </AppText>
          </View>
        ) : null}
      </View>
      <AppText
        variant="eyebrow"
        color={color}
        weight={isFocused ? 'bold' : 'semibold'}
        style={{ letterSpacing: 0.6 }}
      >
        {label}
      </AppText>
    </Pressable>
  );
}
