import type { Schemas } from '@myfantasyprem/api-client';
import { useLocalSearchParams, useRouter } from 'expo-router';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { useFreshness, useNow } from '@/api/freshness';
import { useLeagueContext } from '@/api/hooks';
import { useRespondToTrade, useTrades, type TradeResponse } from '@/api/moves-hooks';
import { Button } from '@/components/Button';
import { Card } from '@/components/Card';
import { EmptyState } from '@/components/EmptyState';
import { AppText } from '@/components/AppText';
import { ArrowLeftRight } from '@/components/icons';
import { PlayersNav } from '@/components/players/PlayersNav';
import { pressFeedback, type PressState } from '@/components/pressable';
import { QueryStatus } from '@/components/QueryStatus';
import { Screen } from '@/components/Screen';
import { ConfirmDialog } from '@/components/Sheet';
import { useToast } from '@/components/Toast';
import { TradeBlockCarousel } from '@/components/trades/TradeBlock';
import { confirmCopy, TradeCard } from '@/components/trades/TradeCard';
import { moveErrorText } from '@/lib/errors';
import { splitTrades, tradeStatusLabel } from '@/lib/trade';
import { useTheme } from '@/theme';
import { flattenUnique } from '@/lib/pages';
import { parseOrigin, withOrigin } from '@/lib/navigation';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

const EMPTY: Record<Tab, { title: string; body: string }> = {
  active: {
    title: 'No active trades',
    body: 'Offers to you and from you, and trades in review, show up here. Propose one with Trade.',
  },
  history: { title: 'No past trades', body: 'Processed, declined, expired and vetoed trades.' },
};

/** The two lists: everything still open (newest first), and decided trades. */
type Tab = 'active' | 'history';

/** `?tab=` from older links (`incoming`, `review`...) opens Active; `history` or `past` opens Past. */
function parseTab(tab: string | undefined): Tab {
  return tab === 'history' || tab === 'past' ? 'history' : 'active';
}

/**
 * The Players tab's Trades view (SLEEPER-OBSERVED): the nav card, then one
 * Active Trades list, newest first, each tagged INCOMING or OUTGOING when
 * it is mine (others' trades in review show too, to vote on), and a small
 * Past tab for decided trades.
 */
export default function TradesScreen() {
  const { leagueId, tab, from } = useLocalSearchParams<{
    leagueId: string;
    tab?: string;
    from?: string;
  }>();
  const router = useRouter();
  const toast = useToast();
  const { spacing, colors, radii } = useTheme();
  const now = useNow(30_000);
  const home = useLeagueContext(leagueId);
  const myTeamId = home.data?.data.viewer.team?.id;
  const trades = useTrades(leagueId);
  const respond = useRespondToTrade(leagueId);
  const [list, setList] = useState<Tab>(parseTab(tab));
  const [pending, setPending] = useState<{ trade: Schemas['Trade']; action: TradeResponse } | null>(
    null,
  );
  const first = trades.data?.pages[0];
  const freshness = useFreshness({ ...trades, data: first });
  const all = flattenUnique(trades.data?.pages, (t) => t.id);
  const { active, past } = splitTrades(all, myTeamId);
  const shown = list === 'active' ? active : past;
  const copy = pending ? confirmCopy(pending.trade, pending.action) : null;

  return (
    <Screen title="Trades" hideHeader freshness={freshness}>
      <PlayersNav
        leagueId={leagueId}
        active="trades"
        onSelect={(view) => router.navigate(`/leagues/${leagueId}/players?view=${view}`)}
        onTrades={() => setList('active')}
      />
      <TradeBlockCarousel leagueId={leagueId} myTeamId={myTeamId} />
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <AppText
          variant="heading"
          weight="bold"
          style={{ flexShrink: 1 }}
          numberOfLines={1}
          testID="trades-title"
        >
          {list === 'active' ? 'Active Trades' : 'Past Trades'}
        </AppText>
        <SmallTabs
          value={list}
          onChange={setList}
          tabs={[
            {
              value: 'active',
              label: 'Active',
              count: active.length,
              testID: 'bucket-active',
            },
            { value: 'history', label: 'Past', testID: 'bucket-history' },
          ]}
        />
        <View style={{ flex: 1 }} />
        <Pressable
          accessibilityRole="button"
          accessibilityLabel="Propose a trade"
          onPress={() =>
            router.push(withOrigin(`/leagues/${leagueId}/players/trades/new`, parseOrigin(from)))
          }
          hitSlop={6}
          testID="new-trade"
          style={(state: PressState) => [
            {
              flexDirection: 'row',
              alignItems: 'center',
              gap: spacing.sm,
              minHeight: 36,
              paddingLeft: spacing.sm,
              borderRadius: radii.pill,
            },
            ...pressFeedback(colors, state, { hoverFill: false }),
          ]}
        >
          <AppText variant="eyebrow" color={colors.accentBright}>
            Trade
          </AppText>
          <View
            aria-hidden
            style={{
              width: 32,
              height: 32,
              borderRadius: 16,
              borderWidth: 1.5,
              borderColor: colors.accent,
              alignItems: 'center',
              justifyContent: 'center',
            }}
          >
            <ArrowLeftRight size={16} color={colors.accentBright} strokeWidth={2.4} />
          </View>
        </Pressable>
      </View>
      <QueryStatus query={{ ...trades, data: first }}>
        {() =>
          shown.length === 0 ? (
            <Card>
              <EmptyState
                icon={ArrowLeftRight}
                title={EMPTY[list].title}
                body={EMPTY[list].body}
                testID="trades-empty"
              />
            </Card>
          ) : (
            <View
              style={{ gap: spacing.md }}
              testID={list === 'active' ? 'trade-list' : 'trades-history'}
            >
              {shown.map((t) => (
                <TradeCard
                  key={t.id}
                  trade={t}
                  myTeamId={myTeamId}
                  now={now}
                  busy={respond.isPending}
                  onAction={(trade, action) => setPending({ trade, action })}
                />
              ))}
            </View>
          )
        }
      </QueryStatus>
      {trades.hasNextPage ? (
        <Button
          label="Show older trades"
          variant="secondary"
          onPress={() => void trades.fetchNextPage()}
          loading={trades.isFetchingNextPage}
        />
      ) : null}
      <ConfirmDialog
        visible={pending !== null}
        title={copy?.title ?? ''}
        message={copy?.message ?? ''}
        confirmLabel={copy?.confirm ?? 'OK'}
        destructive={copy?.destructive}
        onCancel={() => setPending(null)}
        onConfirm={() => {
          if (!pending) return;
          // Accept, decline and withdraw show at once; a refusal rolls back.
          setPending(null);
          respond.mutateAsync({ tradeId: pending.trade.id, action: pending.action }).then(
            (envelope) =>
              toast({
                message: `Trade ${tradeStatusLabel(envelope.data.status).label.toLowerCase()}`,
                tone: 'success',
                haptic: false,
              }),
            (e: unknown) => toast({ message: moveErrorText(e), tone: 'error', haptic: false }),
          );
        }}
        testID="trade-confirm"
      />
    </Screen>
  );
}

/** Small text tabs beside the title, the chosen one underlined in the accent. */
function SmallTabs<T extends string>({
  value,
  onChange,
  tabs,
}: {
  value: T;
  onChange: (v: T) => void;
  tabs: { value: T; label: string; count?: number; testID: string }[];
}) {
  const { colors, spacing } = useTheme();
  return (
    <View role="tablist" style={{ flexDirection: 'row', gap: spacing.xs }}>
      {tabs.map((t) => {
        const selected = t.value === value;
        return (
          <Pressable
            key={t.value}
            role="tab"
            accessibilityLabel={t.count ? `${t.label}, ${t.count}` : t.label}
            accessibilityState={{ selected }}
            aria-selected={selected}
            onPress={() => onChange(t.value)}
            hitSlop={6}
            testID={t.testID}
            style={(state: PressState) => [
              {
                minHeight: 32,
                justifyContent: 'center',
                paddingHorizontal: spacing.sm,
                borderBottomWidth: 2,
                borderBottomColor: selected ? colors.accent : 'transparent',
              },
              ...pressFeedback(colors, state, { hoverFill: false }),
            ]}
          >
            <AppText
              variant="meta"
              weight="semibold"
              color={selected ? colors.accentBright : colors.textMuted}
            >
              {t.count ? `${t.label} ${t.count}` : t.label}
            </AppText>
          </Pressable>
        );
      })}
    </View>
  );
}
