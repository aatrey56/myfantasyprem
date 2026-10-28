import type { Schemas } from '@myfantasyprem/api-client';
import type { ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest, PositionBadge } from '../Badges';
import { Card } from '../Card';
import { Pill } from '../Chip';
import { Activity, ArrowLeftRight, Bot, LayoutGrid, ListOrdered, UserPlus } from '../icons';
import { PlayerName } from '../players/PlayerCardContext';
import { useNow } from '@/api/freshness';
import { useLayout } from '@/hooks/useLayout';
import { deniedText, describeTransaction, type FeedEntry } from '@/lib/activity';
import { formatAgo } from '@/lib/time';
import { tradeStatusLabel } from '@/lib/trade';
import { useTheme } from '@/theme';

/** One line of a league's activity: a move, a trade, an autopick or the draft finishing. */
export function ActivityEntry({ entry }: { entry: FeedEntry }) {
  if (entry.kind === 'draft') return <DraftRow at={entry.at} />;
  const t = entry.txn;
  if (t.kind === 'trade') return <TradeRow t={t} />;
  if (t.kind === 'waiver' || t.kind === 'free_agent') return <MoveRow t={t} />;
  if (t.kind === 'draft_autopick') return <AutopickRow t={t} />;
  return null;
}

function AutopickRow({ t }: { t: Schemas['DraftAutopickTransaction'] }) {
  const { spacing } = useTheme();
  return (
    <Card
      testID={`txn-${t.id}`}
      accessibilityLabel={describeTransaction(t)}
      style={{ gap: spacing.xs, paddingVertical: spacing.md }}
    >
      <Header
        icon={Bot}
        title={t.team.name}
        at={t.occurredAt}
        right={<Pill label="Clock ran out" tone="warning" />}
      />
      <PlayerLine p={t.player} sign="+" />
      <AppText variant="meta" subtle>
        {`Autopicked ${t.method === 'queue' ? 'from the queue' : 'by rank'} · round ${t.round}, pick ${t.overall} overall`}
      </AppText>
    </Card>
  );
}

function Header({
  icon: Icon,
  title,
  at,
  right,
}: {
  icon: typeof Activity;
  title: string;
  at: string;
  right?: ReactNode;
}) {
  const { colors, spacing } = useTheme();
  const now = useNow(60_000);
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
      <Icon size={16} color={colors.textMuted} aria-hidden />
      <View style={{ flex: 1, minWidth: 0 }}>
        <AppText weight="semibold" numberOfLines={1}>
          {title}
        </AppText>
        <AppText variant="meta" subtle testID="activity-time">
          {formatAgo(Date.parse(at), now)}
        </AppText>
      </View>
      {right}
    </View>
  );
}

function PlayerLine({ p, sign }: { p: Schemas['PlayerRef']; sign: '+' | '−' }) {
  const { colors, spacing } = useTheme();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
      <AppText
        weight="bold"
        color={sign === '+' ? colors.win : colors.loss}
        style={{ width: 12, textAlign: 'center' }}
        aria-hidden
      >
        {sign}
      </AppText>
      <PositionBadge position={p.position} size="sm" />
      <PlayerName player={p} muted={sign === '−'} />
      <ClubCrest code={p.club.code} size={12} />
      <AppText variant="meta" subtle>
        {p.club.code}
      </AppText>
    </View>
  );
}

function MoveRow({ t }: { t: Schemas['WaiverTransaction'] | Schemas['FreeAgentTransaction'] }) {
  const { spacing } = useTheme();
  const denied = t.result.outcome === 'denied';
  const waiver = t.kind === 'waiver';
  return (
    <Card
      testID={`txn-${t.id}`}
      accessibilityLabel={describeTransaction(t)}
      style={{ gap: spacing.xs, paddingVertical: spacing.md, opacity: denied ? 0.8 : 1 }}
    >
      <Header
        icon={waiver ? ListOrdered : UserPlus}
        title={t.team.name}
        at={t.occurredAt}
        right={
          denied ? (
            <Pill label="Failed" tone="loss" />
          ) : (
            <Pill label={waiver ? 'Waiver' : 'Free agent'} />
          )
        }
      />
      <PlayerLine p={t.playerIn} sign="+" />
      <PlayerLine p={t.playerOut} sign="−" />
      {denied && t.result.outcome === 'denied' ? (
        <AppText variant="meta" muted>
          {`${deniedText(t.result.reason)}${waiver ? ` · priority ${t.priority}` : ''}`}
        </AppText>
      ) : waiver ? (
        <AppText variant="meta" subtle>{`Won at priority ${t.priority}`}</AppText>
      ) : null}
    </Card>
  );
}

function TradeRow({ t }: { t: Schemas['TradeTransaction'] }) {
  const { spacing } = useTheme();
  const { isWide } = useLayout();
  const st = tradeStatusLabel(t.status);
  const side = (team: Schemas['TeamRef'], gets: readonly Schemas['PlayerRef'][]) => (
    <View style={{ flex: 1, minWidth: 0, gap: spacing.xs }}>
      <AppText variant="eyebrow" muted numberOfLines={1}>
        {`${team.name} gets`}
      </AppText>
      {gets.map((p) => (
        <PlayerLine key={p.id} p={p} sign="+" />
      ))}
    </View>
  );
  return (
    <Card
      testID={`txn-${t.id}`}
      accessibilityLabel={describeTransaction(t)}
      style={{ gap: spacing.sm, paddingVertical: spacing.md }}
    >
      <Header
        icon={ArrowLeftRight}
        title="Trade"
        at={t.occurredAt}
        right={<Pill label={st.label} tone={st.tone} testID="txn-trade-status" />}
      />
      <View style={{ flexDirection: isWide ? 'row' : 'column', gap: spacing.md }}>
        {side(t.offeringTeam, t.requested)}
        {side(t.receivingTeam, t.offered)}
      </View>
    </Card>
  );
}

function DraftRow({ at }: { at: string }) {
  return (
    <Card testID="txn-draft">
      <Header icon={LayoutGrid} title="Draft completed" at={at} />
      <AppText variant="meta" muted>
        Every squad was drafted. The season schedule started.
      </AppText>
    </Card>
  );
}
