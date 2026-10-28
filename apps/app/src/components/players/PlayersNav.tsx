import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { Card } from '../Card';
import {
  ArrowLeftRight,
  ChartColumn,
  Search,
  TrendingUp,
  UserPlus,
  type LucideIcon,
} from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { useLeagueContext } from '@/api/hooks';
import { useTrades } from '@/api/moves-hooks';
import { flattenUnique } from '@/lib/pages';
import { tradeBucket } from '@/lib/trade';
import { useTheme } from '@/theme';

/** The Players tab's views; Trades is its own screen. */
export type PlayersView = 'search' | 'trend' | 'available' | 'leaders';

const VIEWS: { view: PlayersView; icon: LucideIcon; label: string }[] = [
  { view: 'search', icon: Search, label: 'Search' },
  { view: 'trend', icon: TrendingUp, label: 'Trend' },
  { view: 'available', icon: UserPlus, label: 'Available' },
  { view: 'leaders', icon: ChartColumn, label: 'Leaders' },
];

/** Parses a `view` route param; unknown values fall back to Available. */
export function parseView(v: string | string[] | undefined): PlayersView {
  return VIEWS.some((x) => x.view === v) ? (v as PlayersView) : 'available';
}

/**
 * The card at the top of the Players tab (SLEEPER-OBSERVED): Search ·
 * Trend · Available · Leaders · Trades with a count of offers to me. The
 * active one is in the accent. Trades shows once the league is in season.
 */
export function PlayersNav({
  leagueId,
  active,
  onSelect,
  onTrades,
}: {
  leagueId: string;
  active: PlayersView | 'trades' | null;
  onSelect: (view: PlayersView) => void;
  onTrades: () => void;
}) {
  const { spacing } = useTheme();
  const home = useLeagueContext(leagueId);
  const myTeamId = home.data?.data.viewer.team?.id;
  const status = home.data?.data.league.status;
  const inSeason = status === 'in_season' || status === 'complete';
  const trades = useTrades(leagueId);
  const incoming = flattenUnique(trades.data?.pages, (t) => t.id).filter(
    (t) => tradeBucket(t, myTeamId) === 'incoming',
  ).length;
  return (
    <Card
      flush
      testID="players-nav"
      role="navigation"
      accessibilityLabel="Players"
      style={{ flexDirection: 'row', padding: spacing.xs }}
    >
      {VIEWS.map((v) => (
        <NavButton
          key={v.view}
          icon={v.icon}
          label={v.label}
          active={active === v.view}
          onPress={() => onSelect(v.view)}
          testID={`open-${v.view}`}
        />
      ))}
      {inSeason ? (
        <NavButton
          icon={ArrowLeftRight}
          label="Trades"
          active={active === 'trades'}
          badge={incoming}
          a11yLabel={incoming > 0 ? `Trades, ${incoming} new` : 'Trades'}
          onPress={onTrades}
          testID="open-trades"
        />
      ) : null}
    </Card>
  );
}

/** One icon + label button in the card, with an optional count badge. */
function NavButton({
  icon: Icon,
  label,
  active,
  badge,
  a11yLabel,
  onPress,
  testID,
}: {
  icon: LucideIcon;
  label: string;
  active?: boolean;
  badge?: number;
  a11yLabel?: string;
  onPress: () => void;
  testID?: string;
}) {
  const { colors, spacing, radii } = useTheme();
  const fg = active ? colors.accentBright : colors.textMuted;
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={a11yLabel ?? label}
      accessibilityState={{ selected: Boolean(active) }}
      aria-current={active ? 'page' : undefined}
      onPress={onPress}
      testID={testID}
      style={(state: PressState) => [
        {
          flex: 1,
          minWidth: 0,
          minHeight: 60,
          alignItems: 'center',
          justifyContent: 'center',
          gap: spacing.xs,
          paddingVertical: spacing.sm,
          borderRadius: radii.md,
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <View>
        <Icon size={22} color={fg} strokeWidth={active ? 2.4 : 2} aria-hidden />
        {badge !== undefined && badge > 0 ? (
          <View
            aria-hidden
            testID={testID ? `${testID}-badge` : undefined}
            style={{
              position: 'absolute',
              top: -6,
              right: -10,
              minWidth: 16,
              height: 16,
              paddingHorizontal: 4,
              borderRadius: 8,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: colors.accent,
            }}
          >
            <AppText
              weight="bold"
              numeric
              color={colors.onAccent}
              style={{ fontSize: 10, lineHeight: 14 }}
            >
              {String(badge)}
            </AppText>
          </View>
        ) : null}
      </View>
      <AppText
        variant="meta"
        weight={active ? 'semibold' : 'medium'}
        color={fg}
        numberOfLines={1}
        style={{ fontSize: 12, lineHeight: 16 }}
      >
        {label}
      </AppText>
    </Pressable>
  );
}
