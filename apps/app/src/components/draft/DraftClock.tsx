import type { Schemas } from '@myfantasyprem/api-client';
import { Pressable, ScrollView, View } from 'react-native';

import { AppText } from '../AppText';
import { positionLabel } from '../Badges';
import { Pill } from '../Chip';
import { Bot, Clock } from '../icons';
import { playerCardLabel, usePlayerCard } from '../players/PlayerCardContext';
import { pressFeedback, type PressState } from '../pressable';
import { useNow } from '@/api/freshness';
import { clockRemaining, pickLabel, picksUntilTurn } from '@/lib/draft';
import { formatCountdown, formatDateTime } from '@/lib/time';
import { positionColorKey, tint, useTheme } from '@/theme';

interface ClockProps {
  draft: Schemas['Draft'];
  myTeamId: string | null;
  /** serverTime minus the device time the draft arrived. */
  offsetMs: number;
  autopick: boolean;
  /** One slim row (the phone's sticky clock while the draft runs). */
  compact?: boolean;
}

/**
 * Who is on the clock and how long they have. Amber when it's my turn.
 * Ticks every second on the server's clock.
 */
export function DraftClock({ draft, myTeamId, offsetMs, autopick, compact }: ClockProps) {
  const { colors, spacing, radii } = useTheme();
  const now = useNow(1000);
  const cp = draft.currentPick;
  const mine = Boolean(cp && myTeamId && cp.team.id === myTeamId);
  const remaining = cp ? clockRemaining(cp.deadlineAt, offsetMs, now) : null;
  const until = myTeamId ? picksUntilTurn(draft, myTeamId) : null;
  const urgent = remaining !== null && remaining < 15_000;

  let title: string;
  let detail: string;
  switch (draft.status) {
    case 'scheduled':
      title = draft.startsAt
        ? `Draft starts ${formatDateTime(Date.parse(draft.startsAt))}`
        : 'Draft not scheduled';
      detail = `${draft.rounds} rounds · ${draft.pickTimerSeconds}s per pick · snake order`;
      break;
    case 'paused':
      title = 'Draft paused';
      detail = 'The commissioner paused the draft. Picks resume with a full clock.';
      break;
    case 'complete':
      title = 'Draft complete';
      detail = 'Every squad is full. Set your lineup from the Team tab.';
      break;
    case 'in_progress':
      title = mine ? "You're on the clock" : `${cp?.team.name ?? 'Next team'} is picking`;
      detail = cp
        ? `Round ${cp.round} · pick ${pickLabel(cp.round, cp.pickInRound)} · ${cp.overall} overall`
        : '';
      break;
    default:
      title = 'Draft';
      detail = '';
  }

  const bg = mine ? colors.accent : colors.surface;
  const fg = mine ? colors.onAccent : colors.text;
  const untilText =
    draft.status === 'in_progress' && !mine && until !== null
      ? until === 1
        ? "You're up next"
        : `Your pick in ${until} picks`
      : null;
  return (
    <View
      testID="draft-clock"
      accessibilityLiveRegion="polite"
      aria-live="polite"
      style={{
        flexDirection: 'row',
        alignItems: 'center',
        flexWrap: compact ? 'nowrap' : 'wrap',
        gap: compact ? spacing.sm : spacing.md,
        paddingVertical: compact ? spacing.sm : spacing.md,
        paddingHorizontal: spacing.md,
        borderRadius: compact ? radii.md : radii.lg,
        backgroundColor: bg,
        borderWidth: 1,
        borderColor: mine ? colors.accent : colors.border,
      }}
    >
      <View style={{ flex: 1, minWidth: compact ? 0 : 180, gap: compact ? 0 : 2 }}>
        <AppText
          variant={compact ? 'body' : 'heading'}
          weight={compact ? 'semibold' : undefined}
          color={fg}
          numberOfLines={compact ? 1 : undefined}
          testID="draft-clock-title"
        >
          {title}
        </AppText>
        {compact && untilText ? null : (
          <AppText
            variant="meta"
            color={mine ? colors.onAccent : colors.textMuted}
            numberOfLines={compact ? 1 : undefined}
          >
            {detail}
          </AppText>
        )}
        {untilText ? (
          <AppText variant="meta" accent numberOfLines={1} testID="picks-until-turn">
            {untilText}
          </AppText>
        ) : null}
      </View>
      {autopick && draft.status !== 'complete' ? (
        compact ? (
          <View testID="autopick-indicator" accessibilityLabel="Autopick on" role="img">
            <Bot size={16} color={mine ? colors.onAccent : colors.accent} aria-hidden />
          </View>
        ) : (
          <Pill label="Autopick on" tone="accent" icon={Bot} testID="autopick-indicator" />
        )
      ) : null}
      {draft.status === 'in_progress' && remaining !== null ? (
        <View
          style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}
          accessibilityLabel={`${Math.ceil(remaining / 1000)} seconds left`}
        >
          <Clock
            size={18}
            color={mine ? colors.onAccent : urgent ? colors.loss : colors.textMuted}
            aria-hidden
          />
          <AppText
            variant={compact ? 'title' : 'display'}
            numeric
            testID="pick-timer"
            color={mine ? colors.onAccent : urgent ? colors.loss : colors.text}
          >
            {formatCountdown(remaining)}
          </AppText>
        </View>
      ) : null}
    </View>
  );
}

/** The latest picks, newest first, in one scrolling line. Each opens the player's card. */
export function PickTicker({ picks }: { picks: readonly Schemas['DraftPick'][] }) {
  const { colors, spacing, radii } = useTheme();
  const openCard = usePlayerCard();
  const recent = [...picks].slice(-12).reverse();
  if (recent.length === 0) return null;
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      testID="pick-ticker"
      accessibilityLabel="Recent picks"
      contentContainerStyle={{ gap: spacing.sm }}
      style={{ flexGrow: 0 }}
    >
      {recent.map((p, i) => {
        const pos = colors[positionColorKey(p.player.position)];
        const label = pickLabel(p.round, p.pickInRound);
        return (
          <Pressable
            key={p.id}
            testID={i === 0 ? 'ticker-latest' : `ticker-pick-${p.player.id}`}
            disabled={!openCard}
            accessibilityRole={openCard ? 'button' : undefined}
            accessibilityLabel={openCard ? playerCardLabel(p.player.displayName) : undefined}
            accessibilityHint={`Pick ${label}, ${positionLabel(p.player.position)}, ${p.team.name}`}
            onPress={() => openCard?.(p.player.id)}
            style={(state: PressState) => [
              {
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.xs,
                paddingHorizontal: spacing.sm,
                paddingVertical: 4,
                borderRadius: radii.pill,
                backgroundColor: i === 0 ? tint(colors.accent, colors.surface) : colors.surface,
                borderWidth: 1,
                borderColor: colors.border,
              },
              ...pressFeedback(colors, state, { hoverFill: false }),
            ]}
          >
            <AppText variant="eyebrow" subtle numeric style={{ letterSpacing: 0 }}>
              {label}
            </AppText>
            <AppText variant="meta" weight="semibold" numberOfLines={1}>
              {p.player.displayName}
            </AppText>
            <AppText variant="eyebrow" color={pos}>
              {positionLabel(p.player.position)}
            </AppText>
            <AppText variant="meta" muted numberOfLines={1}>
              {p.team.name}
            </AppText>
            {p.method === 'queue' || p.method === 'rank' ? (
              <Bot size={12} color={colors.textMuted} accessibilityLabel="Autopicked" />
            ) : null}
          </Pressable>
        );
      })}
    </ScrollView>
  );
}
