import { useRouter, type Href } from 'expo-router';
import TopTabs from 'expo-router/js-top-tabs';
import { CommonActions } from 'expo-router/react-navigation';
import { useEffect, useRef, useState, type ReactNode } from 'react';
import {
  Animated,
  Platform,
  Pressable,
  ScrollView,
  View,
  useWindowDimensions,
  type GestureResponderEvent,
  type LayoutRectangle,
} from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { Avatar } from './Badges';
import { BrandMark } from './BrandMark';
import { IconButton } from './Button';
import { Pill } from './Chip';
import { ChevronDown, ChevronLeft, LayoutGrid, Settings, type LucideIcon } from './icons';
import { LeagueSwitcherSheet } from './LeagueSwitcherSheet';
import { PagerScene } from './PagerScene';
import { pressFeedback, type PressState } from './pressable';
import { useReducedMotion } from './Skeleton';
import { PressableLink } from './PressableLink';
import { TopBarContext } from './tab-bar-context';
import { useLeagueShell } from '@/api/hooks';
import { useTrades } from '@/api/moves-hooks';
import { usePrefetchLeagueNeighbours } from '@/api/prefetch';
import { PagerSceneContext } from '@/api/screen-focus';
import { APP_NAME } from '@/constants/app';
import { useFinePointer } from '@/hooks/useFinePointer';
import { useLayout } from '@/hooks/useLayout';
import { haptics } from '@/lib/haptics';
import { LEAGUE_TABS, leagueTabHref, type LeagueTab } from '@/lib/league-tabs';
import { flattenUnique } from '@/lib/pages';
import { tradeBucket } from '@/lib/trade';
import { SyncObserver } from './sync/SyncObserver';
import { useTheme } from '@/theme';

type NavMode = 'phone' | 'wide';

/** What the pager hands its tab bar (react-native-tab-view through TopTabs). */
interface PagerBarProps {
  state: { index: number; key: string; routes: { key: string; name: string }[] };
  navigation: {
    emit: (e: { type: 'tabPress'; target: string; canPreventDefault: true }) => {
      defaultPrevented: boolean;
    };
    dispatch: (action: object) => void;
  };
  position: Animated.AnimatedInterpolation<number>;
}

/**
 * The league shell: a header (back, league, GW, settings) with the
 * STATUS · MATCH · TEAM · PLAYERS · LEAGUE tabs under it, over a pager.
 * On a phone (and any touch screen) the pages follow the finger and snap;
 * a desktop pointer clicks the tabs instead. From 768px the header and the
 * tabs share one bar. Each page keeps its own scroll position and nested
 * stack; the pages either side of the visible one stay rendered, so a
 * swipe or a tap to a neighbour is instant.
 */
export function LeagueTabs({ leagueId }: { leagueId: string }) {
  const { colors } = useTheme();
  const { isWide } = useLayout();
  const { width } = useWindowDimensions();
  const finePointer = useFinePointer();
  const [switching, setSwitching] = useState(false);
  const mode: NavMode = isWide ? 'wide' : 'phone';
  // A mouse clicks tabs: no swipe, the switch is instant, and only the
  // visible page is rendered (the neighbours' data is prefetched instead).
  const swipe = Platform.OS !== 'web' || !finePointer;
  // Reduced motion: a tapped tab jumps (the page and the underline); a swipe
  // still follows the finger.
  const reduced = useReducedMotion();

  return (
    // The header owns the top inset.
    <TopBarContext.Provider value>
      <TopTabs
        initialLayout={{ width }}
        style={{ backgroundColor: colors.background }}
        tabBar={(props: PagerBarProps) => (
          <LeagueTabBar
            {...props}
            mode={mode}
            leagueId={leagueId}
            prefetch={!swipe}
            onSwitch={() => setSwitching(true)}
          />
        )}
        screenOptions={{
          lazy: true,
          lazyPreloadDistance: swipe ? 1 : 0,
          swipeEnabled: swipe,
          animationEnabled: swipe && !reduced,
          sceneStyle: { backgroundColor: colors.background },
        }}
        screenLayout={({
          route,
          navigation,
          children,
        }: {
          route: { name: string };
          navigation: { getState: () => PagerBarProps['state'] };
          children: ReactNode;
        }) => {
          const state = navigation.getState();
          const active = state.routes[state.index]?.name ?? 'index';
          return (
            <PagerSceneContext.Provider value={{ scene: route.name, active }}>
              {swipe ? (
                <PagerScene shown={route.name === active}>{children}</PagerScene>
              ) : route.name === active ? (
                children
              ) : null}
            </PagerSceneContext.Provider>
          );
        }}
      >
        {LEAGUE_TABS.map((tab) => (
          <TopTabs.Screen key={tab.name} name={tab.name} options={{ title: tab.label }} />
        ))}
      </TopTabs>
      <LeagueSwitcherSheet
        visible={switching}
        onClose={() => setSwitching(false)}
        currentLeagueId={leagueId}
      />
    </TopBarContext.Provider>
  );
}

/** A plain click (no modifier, main button): handled in the app, not by the browser. */
function plainClick(e: GestureResponderEvent): boolean {
  const n = e.nativeEvent as unknown as {
    metaKey?: boolean;
    ctrlKey?: boolean;
    shiftKey?: boolean;
    altKey?: boolean;
    button?: number;
  };
  return !(n.metaKey || n.ctrlKey || n.shiftKey || n.altKey || (n.button ?? 0) !== 0);
}

function LeagueTabBar({
  state,
  navigation,
  position,
  mode,
  leagueId,
  onSwitch,
  prefetch,
}: PagerBarProps & { mode: NavMode; leagueId: string; onSwitch: () => void; prefetch: boolean }) {
  const shell = useLeagueShell(leagueId);
  usePrefetchLeagueNeighbours(leagueId, LEAGUE_TABS[state.index]?.name ?? 'index', prefetch);
  const trades = useTrades(leagueId);
  const myTeamId = shell.data?.team?.id;
  // News on PLAYERS: a trade offer waiting for my answer.
  const news = flattenUnique(trades.data?.pages, (t) => t.id).some(
    (t) => tradeBucket(t, myTeamId) === 'incoming',
  );

  // A tick on every page change, by swipe or tap (a no-op on web).
  const lastIndex = useRef(state.index);
  useEffect(() => {
    if (lastIndex.current !== state.index) haptics.selection();
    lastIndex.current = state.index;
  }, [state.index]);

  const press = (index: number) => (e: GestureResponderEvent) => {
    if (Platform.OS === 'web' && !plainClick(e)) return;
    e.preventDefault?.();
    const route = state.routes[index]!;
    const focused = state.index === index;
    // Pressing the tab you are on pops its stack to the top (the nested
    // stack listens for tabPress); another tab is jumped to, keeping its stack.
    const event = navigation.emit({ type: 'tabPress', target: route.key, canPreventDefault: true });
    if (!focused && !event.defaultPrevented) {
      navigation.dispatch({ ...CommonActions.navigate(route), target: state.key });
    }
  };

  const items = LEAGUE_TABS.map((tab, index) => ({
    ...tab,
    index,
    focused: state.index === index,
    dot: tab.name === 'players' && news,
    href: leagueTabHref(leagueId, tab.name),
    onPress: press(index),
  }));

  if (mode === 'wide') {
    return (
      <WideBar leagueId={leagueId} onSwitch={onSwitch}>
        {items.map((item) => (
          <WideTab key={item.name} {...item} />
        ))}
      </WideBar>
    );
  }
  return (
    <View>
      <TopBar leagueId={leagueId} onSwitch={onSwitch} />
      <TabStrip items={items} position={position} />
      <SyncObserver leagueId={leagueId} />
    </View>
  );
}

interface TabItem {
  name: LeagueTab;
  label: string;
  index: number;
  focused: boolean;
  dot: boolean;
  href: string;
  onPress: (e: GestureResponderEvent) => void;
}

/** Phone: back, the league (opens the switcher), the GW chip and settings. */
function TopBar({ leagueId, onSwitch }: { leagueId: string; onSwitch: () => void }) {
  const { colors, spacing, radii } = useTheme();
  const insets = useSafeAreaInsets();
  const router = useRouter();
  const shell = useLeagueShell(leagueId);
  return (
    <View
      testID="league-top-bar"
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        gap: spacing.xs,
        paddingTop: insets.top,
        paddingLeft: spacing.xs + insets.left,
        paddingRight: spacing.xs + insets.right,
        minHeight: 52 + insets.top,
        backgroundColor: colors.background,
      }}
    >
      <IconButton
        icon={ChevronLeft}
        label="All leagues"
        onPress={() => router.navigate('/leagues')}
        testID="back-to-leagues"
      />
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${shell.data?.name ?? 'League'}. Switch league`}
        onPress={onSwitch}
        testID="league-switcher-button"
        style={(state: PressState) => [
          {
            flex: 1,
            minWidth: 0,
            minHeight: 44,
            overflow: 'hidden',
            flexDirection: 'row',
            alignItems: 'center',
            gap: spacing.sm,
            paddingHorizontal: spacing.xs,
            borderRadius: radii.md,
          },
          ...pressFeedback(colors, state),
        ]}
      >
        <Avatar name={shell.data?.name ?? '·'} size={30} square />
        <AppText
          variant="heading"
          weight="bold"
          numberOfLines={1}
          style={{ flexShrink: 1, minWidth: 0 }}
          testID="league-switcher-name"
        >
          {shell.data?.name ?? ' '}
        </AppText>
        <ChevronDown size={16} color={colors.textMuted} aria-hidden />
      </Pressable>
      {shell.data && shell.data.status === 'in_season' ? (
        // Pill pins itself to flex-start; centre it on the bar and never let it shrink.
        <View style={{ flexShrink: 0, alignSelf: 'center' }} testID="top-bar-gw">
          <Pill label={`GW ${shell.data.gameweek}`} tone="accent" />
        </View>
      ) : null}
      <IconButton
        icon={Settings}
        label="League settings"
        onPress={() => router.push(`/leagues/${leagueId}/settings` as Href)}
        testID="top-bar-settings"
      />
    </View>
  );
}

/** The underline's drawn width: scaled to each label's width. */
const INDICATOR_BASE = 100;

/**
 * Phone tabs: uppercase labels, a short underline under the active one
 * that follows the pager while you swipe (interpolated from its position,
 * so it moves with the finger, not on release) and slides when you tap. Scrolls
 * sideways only on a screen too narrow for all five.
 */
function TabStrip({
  items,
  position,
}: {
  items: TabItem[];
  position: Animated.AnimatedInterpolation<number>;
}) {
  const { colors, spacing } = useTheme();
  const [boxes, setBoxes] = useState<(LayoutRectangle | undefined)[]>([]);
  const [labels, setLabels] = useState<(number | undefined)[]>([]);
  const measured =
    items.length > 1 && items.every((_, i) => boxes[i] !== undefined && labels[i] !== undefined);
  const set =
    <T,>(setter: (f: (prev: T[]) => T[]) => void, i: number) =>
    (value: T) =>
      setter((prev) => {
        const next = [...prev];
        next[i] = value;
        return next;
      });

  let indicator: ReactNode = null;
  if (measured) {
    const inputRange = items.map((_, i) => i);
    const centres = items.map((_, i) => boxes[i]!.x + boxes[i]!.width / 2);
    const widths = labels.map((w) => w ?? 0);
    indicator = (
      <Animated.View
        aria-hidden
        pointerEvents="none"
        testID="league-tabs-indicator"
        style={{
          position: 'absolute',
          left: 0,
          bottom: 0,
          width: INDICATOR_BASE,
          height: 3,
          borderRadius: 2,
          backgroundColor: colors.accent,
          transform: [
            {
              translateX: position.interpolate({
                inputRange,
                outputRange: centres.map((c) => c - INDICATOR_BASE / 2),
                extrapolate: 'clamp',
              }),
            },
            {
              scaleX: position.interpolate({
                inputRange,
                outputRange: widths.map((w) => w / INDICATOR_BASE),
                extrapolate: 'clamp',
              }),
            },
          ],
        }}
      />
    );
  }

  return (
    <View
      role="navigation"
      aria-label="League sections"
      testID="league-tabs"
      style={{
        backgroundColor: colors.background,
        borderBottomWidth: 1,
        borderBottomColor: colors.border,
      }}
    >
      <ScrollView
        horizontal
        showsHorizontalScrollIndicator={false}
        contentContainerStyle={{ flexGrow: 1, paddingHorizontal: spacing.sm }}
      >
        <View accessibilityRole="tablist" style={{ flexDirection: 'row', flexGrow: 1 }}>
          {items.map((item, i) => (
            <Pressable
              key={item.name}
              testID={`tab-${item.name}`}
              accessibilityRole="tab"
              accessibilityLabel={item.label}
              accessibilityState={{ selected: item.focused }}
              aria-selected={item.focused}
              onPress={item.onPress}
              onLayout={(e) => set(setBoxes, i)(e.nativeEvent.layout)}
              style={(state: PressState) => [
                {
                  minHeight: 44,
                  flexGrow: 1,
                  alignItems: 'center',
                  justifyContent: 'center',
                  paddingHorizontal: spacing.sm,
                },
                ...pressFeedback(colors, state, { hoverFill: false }),
              ]}
            >
              <View
                style={{ flexDirection: 'row' }}
                onLayout={(e) => set(setLabels, i)(e.nativeEvent.layout.width)}
              >
                <AppText
                  variant="meta"
                  weight={item.focused ? 'bold' : 'semibold'}
                  color={item.focused ? colors.accent : colors.textMuted}
                  numberOfLines={1}
                  style={{ textTransform: 'uppercase', letterSpacing: 0.8 }}
                >
                  {item.label}
                </AppText>
                {item.dot ? <NewsDot /> : null}
              </View>
            </Pressable>
          ))}
          {/* In the row, so it shares the tabs' origin (not the scroll padding). */}
          {indicator}
        </View>
      </ScrollView>
    </View>
  );
}

/** The red dot on a tab with news. */
function NewsDot() {
  const { colors } = useTheme();
  return (
    <View
      testID="tab-news-dot"
      accessibilityLabel="New"
      style={{
        position: 'absolute',
        top: -2,
        right: -9,
        width: 7,
        height: 7,
        borderRadius: 4,
        backgroundColor: colors.danger,
      }}
    />
  );
}

interface WideBarProps {
  leagueId: string;
  onSwitch: () => void;
  children?: ReactNode;
}

/**
 * From 768px: one sticky bar across the top: home, the league switcher,
 * the tabs, then the GW, the draft and settings.
 */
function WideBar({ leagueId, onSwitch, children }: WideBarProps) {
  const { colors, spacing, layout, radii } = useTheme();
  const { isDesktop } = useLayout();
  const insets = useSafeAreaInsets();
  const shell = useLeagueShell(leagueId);
  const status = shell.data?.status;
  const showDraft = status === 'drafting' || status === 'pre_draft';
  // After the draft the same screen is the read-only results.
  const showResults = status === 'in_season' || status === 'complete';

  return (
    <View>
      <View
        role="navigation"
        aria-label="League sections"
        testID="top-nav"
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.xs,
          minHeight: layout.topNavHeight + insets.top,
          paddingTop: insets.top,
          paddingLeft: spacing.md + insets.left,
          paddingRight: spacing.md + insets.right,
          backgroundColor: colors.surface,
          borderBottomColor: colors.border,
          borderBottomWidth: 1,
          zIndex: 10,
        }}
      >
        <PressableLink
          href="/leagues"
          mode="navigate"
          accessibilityLabel={`${APP_NAME}: all leagues`}
          testID="nav-home"
          {...({ title: 'All leagues' } as object)}
          style={(state: PressState) => [
            {
              width: 44,
              height: 44,
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: radii.md,
            },
            ...pressFeedback(colors, state),
          ]}
        >
          <BrandMark size={28} />
        </PressableLink>
        <Pressable
          accessibilityRole="button"
          accessibilityLabel={`${shell.data?.name ?? 'League'}. Switch league`}
          onPress={onSwitch}
          testID="league-switcher-button"
          style={(state: PressState) => [
            {
              flexShrink: 1,
              minWidth: 0,
              maxWidth: isDesktop ? 260 : 180,
              minHeight: 44,
              flexDirection: 'row',
              alignItems: 'center',
              gap: spacing.sm,
              paddingHorizontal: spacing.sm,
              borderRadius: radii.md,
            },
            ...pressFeedback(colors, state),
          ]}
        >
          <Avatar name={shell.data?.name ?? '·'} size={28} square />
          <AppText
            weight="semibold"
            numberOfLines={1}
            style={{ flexShrink: 1, minWidth: 0 }}
            testID="league-switcher-name"
          >
            {shell.data?.name ?? ' '}
          </AppText>
          <ChevronDown size={16} color={colors.textMuted} aria-hidden />
        </Pressable>
        <View
          aria-hidden
          style={{ width: 1, height: 24, backgroundColor: colors.border, marginHorizontal: 4 }}
        />
        <View
          accessibilityRole="tablist"
          style={{ flexDirection: 'row', alignItems: 'stretch', alignSelf: 'stretch' }}
        >
          {children}
        </View>
        <View style={{ flex: 1 }} />
        {shell.data && shell.data.status === 'in_season' ? (
          <View style={{ flexShrink: 0 }} testID="top-nav-gw">
            <Pill label={`GW ${shell.data.gameweek}`} tone="accent" />
          </View>
        ) : null}
        {showResults ? (
          <BarLink
            href={`/leagues/${leagueId}/draft`}
            label="Draft results"
            icon={LayoutGrid}
            compact={!isDesktop}
            testID="nav-draft-results"
          />
        ) : null}
        {showDraft ? (
          <BarLink
            href={`/leagues/${leagueId}/draft`}
            label={status === 'drafting' ? 'Draft room · live' : 'Draft room'}
            icon={LayoutGrid}
            compact={!isDesktop}
            highlight={status === 'drafting'}
            testID="nav-draft-room"
          />
        ) : null}
        <BarLink
          href={`/leagues/${leagueId}/settings`}
          label="Settings"
          a11yLabel="League settings"
          icon={Settings}
          compact={!isDesktop}
          testID="nav-settings"
        />
      </View>
      <SyncObserver leagueId={leagueId} />
    </View>
  );
}

/** A link in the top bar: icon and label, or icon only (with a tooltip) on tablet. */
function BarLink({
  href,
  label,
  a11yLabel,
  icon: Icon,
  compact,
  highlight,
  testID,
}: {
  href: string;
  label: string;
  a11yLabel?: string;
  icon: LucideIcon;
  compact?: boolean;
  highlight?: boolean;
  testID?: string;
}) {
  const { colors, spacing, radii } = useTheme();
  const fg = highlight ? colors.accent : colors.textMuted;
  return (
    <PressableLink
      href={href}
      accessibilityLabel={a11yLabel ?? label}
      testID={testID}
      {...({ title: a11yLabel ?? label } as object)}
      style={(state: PressState) => [
        {
          flexDirection: 'row',
          alignItems: 'center',
          justifyContent: 'center',
          gap: spacing.sm,
          minHeight: 44,
          minWidth: 44,
          paddingHorizontal: compact ? 0 : spacing.md,
          borderRadius: radii.md,
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <Icon size={20} color={fg} aria-hidden />
      {compact ? null : (
        <AppText weight="medium" color={fg}>
          {label}
        </AppText>
      )}
    </PressableLink>
  );
}

/** A text tab in the wide bar, underlined when active. */
function WideTab({ name, label, focused, dot, onPress }: TabItem) {
  const { colors, spacing } = useTheme();
  return (
    <Pressable
      testID={`tab-${name}`}
      onPress={onPress}
      accessibilityRole="tab"
      accessibilityLabel={label}
      accessibilityState={{ selected: focused }}
      aria-selected={focused}
      style={(state: PressState) => [
        {
          minHeight: 44,
          justifyContent: 'center',
          paddingHorizontal: spacing.md,
          borderBottomWidth: 3,
          borderBottomColor: focused ? colors.accent : 'transparent',
          borderTopWidth: 3,
          borderTopColor: 'transparent',
        },
        ...pressFeedback(colors, state, { hoverFill: !focused }),
      ]}
    >
      <View style={{ flexDirection: 'row' }}>
        <AppText
          variant="meta"
          color={focused ? colors.text : colors.textMuted}
          weight={focused ? 'bold' : 'semibold'}
          style={{ textTransform: 'uppercase', letterSpacing: 0.8 }}
        >
          {label}
        </AppText>
        {dot ? <NewsDot /> : null}
      </View>
    </Pressable>
  );
}
