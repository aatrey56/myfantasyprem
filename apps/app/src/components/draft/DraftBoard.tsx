import type { Schemas } from '@myfantasyprem/api-client';
import { memo, type ReactNode } from 'react';
import { Pressable, ScrollView, View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest, positionLabel } from '../Badges';
import { Bot } from '../icons';
import { playerCardLabel, usePlayerCard } from '../players/PlayerCardContext';
import { buildBoard, pickLabel, type BoardCell } from '@/lib/draft';
import { positionColorKey, tint, useTheme } from '@/theme';

const CELL_W = 112;
const CELL_H = 60;
const HEAD_H = 52;
const ROUND_W = 36;

interface Props {
  draft: Schemas['Draft'];
  myTeamId: string | null;
  /** False on a scrolling page: the rounds scroll with it instead of on their own. */
  scroll?: boolean;
}

/**
 * Rounds × teams. Scrolls both ways; in a panel the team header row stays
 * put while the rounds scroll. Each pick is tinted by position; the pick on the clock
 * is outlined in amber; my column is marked.
 */
export function DraftBoard({ draft, myTeamId, scroll = true }: Props) {
  const { colors, spacing } = useTheme();
  const board = buildBoard(draft);
  const current = draft.currentPick?.overall ?? null;
  if (draft.order.length === 0) {
    return (
      <View style={{ padding: spacing.lg }} testID="draft-board">
        <AppText muted>The draft order is set when the draft starts.</AppText>
      </View>
    );
  }
  return (
    <ScrollView
      horizontal
      testID="draft-board"
      style={scroll ? { flex: 1 } : undefined}
      contentContainerStyle={{ flexGrow: 1 }}
      accessibilityLabel="Draft board"
    >
      <View style={{ flex: 1 }}>
        <View
          style={{
            flexDirection: 'row',
            height: HEAD_H,
            borderBottomWidth: 1,
            borderBottomColor: colors.border,
          }}
        >
          <View style={{ width: ROUND_W }} />
          {draft.order.map((slot) => {
            const mine = slot.team.id === myTeamId;
            return (
              <View
                key={slot.team.id}
                testID={mine ? 'board-my-column' : undefined}
                style={{
                  width: CELL_W,
                  paddingHorizontal: spacing.xs,
                  justifyContent: 'center',
                  borderBottomWidth: 2,
                  borderBottomColor: mine ? colors.accent : 'transparent',
                }}
              >
                <View style={{ flexDirection: 'row', alignItems: 'center', gap: 4 }}>
                  <AppText
                    variant="meta"
                    weight="semibold"
                    numberOfLines={1}
                    style={{ flexShrink: 1 }}
                    color={mine ? colors.accent : colors.text}
                  >
                    {slot.team.name}
                  </AppText>
                  {slot.autopick ? (
                    <Bot size={14} color={colors.textMuted} accessibilityLabel="Autopick on" />
                  ) : null}
                </View>
                <AppText
                  variant="eyebrow"
                  subtle
                  numberOfLines={1}
                  style={{ textTransform: 'none', letterSpacing: 0 }}
                >
                  {slot.team.managerName}
                </AppText>
              </View>
            );
          })}
        </View>
        <Rounds scroll={scroll}>
          {board.map((row) => (
            <View key={row[0]!.round} style={{ flexDirection: 'row' }}>
              <View
                style={{
                  width: ROUND_W,
                  height: CELL_H,
                  alignItems: 'center',
                  justifyContent: 'center',
                }}
              >
                <AppText variant="eyebrow" subtle>
                  {`R${row[0]!.round}`}
                </AppText>
              </View>
              {row.map((cell) => (
                <Cell key={cell.overall} cell={cell} current={cell.overall === current} />
              ))}
            </View>
          ))}
        </Rounds>
      </View>
    </ScrollView>
  );
}

function Rounds({ scroll, children }: { scroll: boolean; children: ReactNode }) {
  const { spacing } = useTheme();
  return scroll ? (
    <ScrollView style={{ flex: 1 }} contentContainerStyle={{ paddingBottom: spacing.lg }}>
      {children}
    </ScrollView>
  ) : (
    <View style={{ paddingBottom: spacing.sm }}>{children}</View>
  );
}

const Cell = memo(function Cell({ cell, current }: { cell: BoardCell; current: boolean }) {
  const { colors, radii } = useTheme();
  const openCard = usePlayerCard();
  const pick = cell.pick;
  const posColor = pick ? colors[positionColorKey(pick.player.position)] : colors.textSubtle;
  const label = pickLabel(cell.round, cell.pickInRound);
  const pickText = pick
    ? `Pick ${label}: ${pick.player.displayName}, ${positionLabel(pick.player.position)}, ${pick.team.name}`
    : current
      ? `Pick ${label}, on the clock`
      : `Pick ${label}`;
  // A pick opens the player's card.
  const Box = pick && openCard ? Pressable : View;
  return (
    <Box
      testID={pick ? `board-pick-${pick.player.id}` : current ? 'board-current' : undefined}
      {...(pick && openCard
        ? {
            accessibilityRole: 'button' as const,
            accessibilityLabel: playerCardLabel(pick.player.displayName),
            accessibilityHint: pickText,
            onPress: () => openCard(pick.player.id),
          }
        : { accessibilityLabel: pickText })}
      style={{ width: CELL_W, height: CELL_H, padding: 2 }}
    >
      <View
        style={{
          flex: 1,
          borderRadius: radii.sm,
          paddingHorizontal: 6,
          paddingVertical: 4,
          justifyContent: 'space-between',
          backgroundColor: pick ? tint(posColor, colors.surface) : colors.surface,
          borderWidth: current ? 2 : 1,
          borderColor: current ? colors.accent : pick ? 'transparent' : colors.border,
          borderStyle: pick || current ? 'solid' : 'dashed',
        }}
      >
        {pick ? (
          <>
            <AppText variant="meta" weight="semibold" numberOfLines={1}>
              {pick.player.displayName}
            </AppText>
            <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: 3 }}>
                <ClubCrest code={pick.player.club.code} size={11} />
                <AppText variant="eyebrow" color={posColor}>
                  {`${positionLabel(pick.player.position)} · ${pick.player.club.code}`}
                </AppText>
              </View>
              <AppText variant="eyebrow" subtle numeric style={{ letterSpacing: 0 }}>
                {label}
              </AppText>
            </View>
          </>
        ) : (
          <AppText
            variant="eyebrow"
            color={current ? colors.accent : colors.textSubtle}
            numeric
            style={{ letterSpacing: 0 }}
          >
            {current ? `${label} · on the clock` : label}
          </AppText>
        )}
      </View>
    </Box>
  );
});
