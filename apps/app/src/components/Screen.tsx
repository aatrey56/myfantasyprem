import type { Href } from 'expo-router';
import Head from 'expo-router/head';
import { QueryClientContext } from '@tanstack/react-query';
import { FlashList } from '@shopify/flash-list';
import {
  Fragment,
  useCallback,
  useContext,
  useMemo,
  useRef,
  useState,
  type ReactElement,
  type ReactNode,
  type RefObject,
} from 'react';
import { Platform, RefreshControl, ScrollView, View, type ViewStyle } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { ChevronLeft } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { PressableLink } from './PressableLink';
import { StaleBanner } from './StaleBanner';
import { ScreenScrollContext, type ScreenScroll } from './screen-scroll-context';
import { useHasBottomTabBar, useHasTopBar } from './tab-bar-context';
import type { Freshness } from '@/api/freshness';
import { APP_NAME } from '@/constants/app';
import { useLayout } from '@/hooks/useLayout';
import { useTheme } from '@/theme';

interface HeaderProps {
  title: string;
  subtitle?: string;
  /** Small uppercase line above the title ("GW 8 · LIVE"). */
  eyebrow?: string;
  /** Renders a "‹ label" link above the title. */
  back?: { label: string; href: Href };
  /** Avatar or league badge left of the title. */
  left?: ReactNode;
  /** Icon buttons on the right. */
  actions?: ReactNode;
  /** testID for the title text. */
  titleTestID?: string;
}

/** Large bold title with an optional avatar on the left and actions on the right. */
export function PageHeader({
  title,
  subtitle,
  eyebrow,
  back,
  left,
  actions,
  titleTestID,
}: HeaderProps) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View style={{ gap: spacing.xs }}>
      {back ? (
        <PressableLink
          href={back.href}
          testID="back-link"
          // 32pt tall plus 6pt each way: a 44pt touch target.
          hitSlop={6}
          style={(state: PressState) => [
            {
              flexDirection: 'row',
              alignItems: 'center',
              alignSelf: 'flex-start',
              minHeight: 32,
              paddingRight: spacing.sm,
              borderRadius: radii.sm,
            },
            ...pressFeedback(colors, state),
          ]}
        >
          <ChevronLeft size={18} color={colors.accent} aria-hidden />
          <AppText accent weight="semibold">
            {back.label}
          </AppText>
        </PressableLink>
      ) : null}
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        {left}
        <View style={{ flex: 1, minWidth: 0 }}>
          {eyebrow ? (
            <AppText variant="eyebrow" accent testID="eyebrow">
              {eyebrow}
            </AppText>
          ) : null}
          <AppText
            variant="title"
            accessibilityRole="header"
            numberOfLines={2}
            testID={titleTestID}
          >
            {title}
          </AppText>
          {subtitle ? (
            <AppText muted numberOfLines={2}>
              {subtitle}
            </AppText>
          ) : null}
        </View>
        {actions ? (
          <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}>
            {actions}
          </View>
        ) : null}
      </View>
    </View>
  );
}

/**
 * A long list at the end of a screen (the players, the activity feed). On
 * native it is a FlashList, the screen's scroller, with the rest of the
 * screen above it as its header: only the rows on screen are drawn. On the
 * web the rows are drawn in order in the page's scroll view, so the Tab
 * order and screen readers follow the list (FlashList reuses its cells in
 * place, which would scramble the DOM order).
 */
export interface ScreenList<T> {
  data: readonly T[];
  keyExtractor: (item: T) => string;
  renderItem: (item: T, index: number) => ReactElement;
  /** Rows of different shapes (a heading, an entry): recycled apart on native. */
  getItemType?: (item: T) => string;
  /** Draws the rows as one flush card (border and rounded corners). */
  card?: boolean;
  /** Under the rows ("Show more", a count). */
  footer?: ReactNode;
  /** The rows' container (web); the list (native). */
  testID?: string;
}

interface Props<T> extends HeaderProps {
  /** Drives the screen's single freshness banner. */
  freshness?: Freshness;
  children?: ReactNode;
  /** False for screens that manage their own scrolling (the draft room). */
  scroll?: boolean;
  /**
   * With `scroll` false: no gutters or column, only the safe-area insets, so
   * the screen's own scroll view runs edge to edge (the phone draft room).
   */
  bleed?: boolean;
  /** Content column width; defaults to the 960px column. */
  maxWidth?: number;
  /** Hide the large header (the draft room draws its own). */
  hideHeader?: boolean;
  /** Document title override. */
  documentTitle?: string;
  /** Called when the user scrolls near the end (infinite lists). */
  onEndReached?: () => void;
  /**
   * Pull to refresh (native): resolves when the refetch settles. Defaults
   * to refetching the visible queries on screens that pass `freshness`.
   */
  onRefresh?: () => Promise<unknown>;
  /** A long list after the content (with `scroll`): see ScreenList. */
  list?: ScreenList<T>;
  /** The scroll view (with `scroll`, no `list`), to scroll it from the screen. */
  scrollRef?: RefObject<ScrollView | null>;
}

/** Pull-to-refresh state: spinner while `onRefresh` runs. Native only. */
function usePullToRefresh(onRefresh: (() => Promise<unknown>) | undefined, tint: string) {
  const [refreshing, setRefreshing] = useState(false);
  const run = useCallback(() => {
    if (!onRefresh) return;
    setRefreshing(true);
    void onRefresh().finally(() => setRefreshing(false));
  }, [onRefresh]);
  if (!onRefresh || Platform.OS === 'web') return undefined;
  return <RefreshControl refreshing={refreshing} onRefresh={run} tintColor={tint} />;
}

/**
 * Page frame: optional freshness banner, then a scrolling, centred
 * max-width column with the header. Pads for the safe-area insets: the
 * top inset is a fixed strip above the scroll view (the banner or the
 * league's top bar takes it when either shows), so content scrolled up
 * stops below the status bar and Dynamic Island instead of running under
 * them; the bottom tab bar takes the bottom inset when it shows.
 */
export function Screen<T = never>({
  freshness,
  children,
  scroll = true,
  bleed = false,
  maxWidth,
  hideHeader,
  documentTitle,
  onEndReached,
  onRefresh,
  list,
  scrollRef,
  ...header
}: Props<T>) {
  const { colors, spacing, layout, radii } = useTheme();
  const { isDesktop } = useLayout();
  const insets = useSafeAreaInsets();
  const hasTabBar = useHasBottomTabBar();
  const hasTopBar = useHasTopBar();
  // Screens with server data get pull-to-refresh by default: it refetches
  // every query on screen (the hidden tabs are unsubscribed).
  const queryClient = useContext(QueryClientContext);
  const refetchVisible = useCallback(
    () => queryClient?.refetchQueries({ type: 'active' }) ?? Promise.resolve(),
    [queryClient],
  );
  // The scroll view's offset and a version bumped on every size change, for
  // content that fits itself to the viewport (the team pitch).
  const ownScrollRef = useRef<ScrollView>(null);
  const scroller = scrollRef ?? ownScrollRef;
  const scrollY = useRef(0);
  const [scrollVersion, setScrollVersion] = useState(0);
  const bumpScroll = useCallback(() => setScrollVersion((v) => v + 1), []);
  const scrollContext = useMemo<ScreenScroll>(
    () => ({
      offset: () => {
        // The web's scrollTop is current even before its scroll event lands
        // (e.g. a shorter page clamping it).
        const node = Platform.OS === 'web' ? scroller.current?.getScrollableNode?.() : null;
        const live = (node as { scrollTop?: unknown } | null)?.scrollTop;
        return typeof live === 'number' ? live : scrollY.current;
      },
      version: scrollVersion,
    }),
    [scroller, scrollVersion],
  );
  const refreshControl = usePullToRefresh(
    onRefresh ?? (freshness ? refetchVisible : undefined),
    colors.textMuted,
  );
  const bannerShown = freshness !== undefined && freshness.kind !== 'fresh';
  const framed = scroll || !bleed;
  const gutter = framed ? (isDesktop ? spacing.xl : spacing.lg) : 0;
  const topInset = bannerShown || hasTopBar ? 0 : insets.top;
  const padding = {
    paddingTop: gutter,
    paddingBottom: (framed ? spacing.lg : 0) + (hasTabBar ? 0 : insets.bottom),
    paddingLeft: gutter + insets.left,
    paddingRight: gutter + insets.right,
  };
  const columnWidth = framed ? (maxWidth ?? layout.maxContentWidth) : undefined;
  const virtual = scroll && list !== undefined && Platform.OS !== 'web';
  const card: ViewStyle = {
    backgroundColor: colors.surface,
    borderColor: colors.border,
    borderWidth: 1,
    borderRadius: radii.lg,
    overflow: 'hidden',
  };
  const column = (
    <View
      style={{
        width: '100%',
        maxWidth: columnWidth,
        gap: spacing.lg,
        flex: scroll ? undefined : 1,
      }}
    >
      {hideHeader ? null : <PageHeader {...header} />}
      {children}
      {list && !virtual ? (
        <>
          {list.data.length > 0 ? (
            <View testID={list.testID} style={list.card ? card : undefined}>
              {list.data.map((item, i) => (
                <Fragment key={list.keyExtractor(item)}>{list.renderItem(item, i)}</Fragment>
              ))}
            </View>
          ) : null}
          {list.footer}
        </>
      ) : null}
    </View>
  );
  const keyboard = {
    keyboardShouldPersistTaps: 'handled',
    // iOS: the content inset follows the keyboard, and a drag dismisses it.
    automaticallyAdjustKeyboardInsets: true,
    keyboardDismissMode: Platform.OS === 'ios' ? 'interactive' : 'on-drag',
  } as const;
  return (
    <View
      testID="screen-frame"
      style={{ flex: 1, backgroundColor: colors.background, paddingTop: topInset }}
    >
      {Platform.OS === 'web' ? (
        // The document title; native Head is for Handoff, which needs an origin.
        <Head>
          <title>{`${documentTitle ?? header.title} · ${APP_NAME}`}</title>
        </Head>
      ) : null}
      {freshness ? <StaleBanner freshness={freshness} /> : null}
      {virtual ? (
        <FlashList
          testID={list.testID}
          data={list.data}
          keyExtractor={list.keyExtractor}
          getItemType={list.getItemType}
          // FlashList v2 anchors the first visible row by default: a filter
          // that adds rows above it (Players: Available -> All players)
          // would scroll the header with the filters off screen.
          maintainVisibleContentPosition={{ disabled: true }}
          {...keyboard}
          refreshControl={refreshControl}
          onEndReached={onEndReached}
          onEndReachedThreshold={1}
          ListHeaderComponent={
            <View
              style={{
                alignItems: 'center',
                paddingTop: padding.paddingTop,
                paddingLeft: padding.paddingLeft,
                paddingRight: padding.paddingRight,
                paddingBottom: list.data.length > 0 ? spacing.lg : 0,
              }}
            >
              {column}
            </View>
          }
          renderItem={({ item, index }) => {
            const first = index === 0;
            const last = index === list.data.length - 1;
            return (
              <View
                style={{
                  alignItems: 'center',
                  paddingLeft: padding.paddingLeft,
                  paddingRight: padding.paddingRight,
                }}
              >
                <View
                  style={[
                    { width: '100%', maxWidth: columnWidth },
                    list.card
                      ? {
                          ...card,
                          borderTopWidth: first ? 1 : 0,
                          borderBottomWidth: last ? 1 : 0,
                          borderRadius: 0,
                          borderTopLeftRadius: first ? radii.lg : 0,
                          borderTopRightRadius: first ? radii.lg : 0,
                          borderBottomLeftRadius: last ? radii.lg : 0,
                          borderBottomRightRadius: last ? radii.lg : 0,
                        }
                      : null,
                  ]}
                >
                  {list.renderItem(item, index)}
                </View>
              </View>
            );
          }}
          ListFooterComponent={
            <View
              style={{
                alignItems: 'center',
                paddingLeft: padding.paddingLeft,
                paddingRight: padding.paddingRight,
                paddingBottom: padding.paddingBottom,
                paddingTop: list.footer && list.data.length > 0 ? spacing.lg : 0,
              }}
            >
              <View style={{ width: '100%', maxWidth: columnWidth }}>{list.footer}</View>
            </View>
          }
        />
      ) : scroll ? (
        <ScrollView
          ref={scroller}
          testID="screen-scroll"
          style={{ flex: 1 }}
          {...keyboard}
          refreshControl={refreshControl}
          scrollEventThrottle={200}
          onScroll={({ nativeEvent: e }) => {
            scrollY.current = e.contentOffset.y;
            if (
              onEndReached &&
              e.layoutMeasurement.height + e.contentOffset.y >= e.contentSize.height - 600
            )
              onEndReached();
          }}
          onLayout={bumpScroll}
          onContentSizeChange={bumpScroll}
          contentContainerStyle={{ alignItems: 'center', ...padding }}
        >
          <ScreenScrollContext.Provider value={scrollContext}>
            {column}
          </ScreenScrollContext.Provider>
        </ScrollView>
      ) : (
        <View style={{ flex: 1, alignItems: 'center', ...padding }}>{column}</View>
      )}
    </View>
  );
}
