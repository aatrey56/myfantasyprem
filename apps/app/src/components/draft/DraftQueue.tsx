import type { Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { ScrollView, Switch, View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar, PositionBadge, positionLabel } from '../Badges';
import { Button, IconButton } from '../Button';
import { Chip } from '../Chip';
import { EmptyState } from '../EmptyState';
import { ArrowDown, ArrowUp, ListOrdered, X } from '../icons';
import { PlayerRow } from '../PlayerRow';
import { usePlayerCard } from '../players/PlayerCardContext';
import { hasRoomFor, POSITIONS, SQUAD, type QueueItem } from '@/lib/draft';
import { positionColorKey, useTheme } from '@/theme';

interface QueueProps {
  queue: readonly QueueItem[];
  autopick: boolean;
  /** "Autopick when my clock runs out" (on by default). */
  autopickOnTimeout: boolean;
  autopickPending: boolean;
  onAutopick: (enabled: boolean) => void;
  onAutopickOnTimeout: (on: boolean) => void;
  onMove: (playerId: string, delta: number) => void;
  onRemove: (playerId: string) => void;
  myCounts: Record<'GKP' | 'DEF' | 'MID' | 'FWD', number> | null;
  canPick: boolean;
  pickingId: string | null;
  onPick: (player: Schemas['PlayerRef']) => void;
  /** False on a scrolling page: plain content instead of a scroll view. */
  scroll?: boolean;
}

/** Pane width below which a queue row puts its controls on a second line. */
const COMPACT_BELOW = 400;

/**
 * My queue, in the order autopick tries it. Reorder with the arrow buttons
 * (keyboard and screen-reader friendly), remove with ×, and draft the top
 * player when on the clock.
 */
export function DraftQueue({
  queue,
  autopick,
  autopickOnTimeout,
  autopickPending,
  onAutopick,
  onAutopickOnTimeout,
  onMove,
  onRemove,
  myCounts,
  canPick,
  pickingId,
  onPick,
  scroll = true,
}: QueueProps) {
  const { colors, spacing } = useTheme();
  const openCard = usePlayerCard();
  // A narrow pane (phones down to 320px) has no room for the name and four
  // controls on one line: the controls move to a line of their own.
  const [paneWidth, setPaneWidth] = useState(0);
  const compact = paneWidth > 0 && paneWidth < COMPACT_BELOW;
  const Body = scroll ? ScrollView : View;
  return (
    <Body
      style={scroll ? { flex: 1 } : undefined}
      testID="draft-queue"
      onLayout={(e) => setPaneWidth(e.nativeEvent.layout.width)}
    >
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.md,
          padding: spacing.md,
          borderBottomWidth: 1,
          borderBottomColor: colors.border,
        }}
      >
        <View style={{ flex: 1 }}>
          <AppText weight="semibold" nativeID="autopick-label">
            Autopick
          </AppText>
          <AppText variant="meta" muted>
            Picks for you at once: first from this queue, then by rank.
          </AppText>
        </View>
        <Switch
          value={autopick}
          disabled={autopickPending}
          onValueChange={onAutopick}
          accessibilityLabel="Autopick"
          aria-labelledby="autopick-label"
          testID="autopick-switch"
          trackColor={{ true: colors.accent, false: colors.surfaceRaised }}
          thumbColor={colors.text}
          {...({ activeThumbColor: colors.onAccent } as object)}
        />
      </View>
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.md,
          padding: spacing.md,
          borderBottomWidth: 1,
          borderBottomColor: colors.border,
        }}
      >
        <View style={{ flex: 1 }}>
          <AppText weight="semibold" nativeID="autopick-timeout-label">
            Autopick when my clock runs out
          </AppText>
          <AppText variant="meta" muted>
            {autopickOnTimeout
              ? 'A missed clock picks for you and turns autopick on.'
              : 'A missed clock still picks for you, so the draft keeps moving, but your next pick gets a full clock.'}
          </AppText>
        </View>
        <Switch
          value={autopickOnTimeout}
          disabled={autopickPending}
          onValueChange={onAutopickOnTimeout}
          accessibilityLabel="Autopick when my clock runs out"
          aria-labelledby="autopick-timeout-label"
          testID="autopick-timeout-switch"
          trackColor={{ true: colors.accent, false: colors.surfaceRaised }}
          thumbColor={colors.text}
          {...({ activeThumbColor: colors.onAccent } as object)}
        />
      </View>
      {queue.length === 0 ? (
        <EmptyState
          icon={ListOrdered}
          title="Your queue is empty"
          body="Add players from the list with the + button. Autopick takes them in order."
          testID="queue-empty"
        />
      ) : (
        queue.map((item, i) => {
          const room = myCounts ? hasRoomFor(myCounts, item.player.position) : true;
          const controls = (
            <View
              style={[
                { flexDirection: 'row', alignItems: 'center' },
                compact && {
                  justifyContent: 'flex-end',
                  paddingHorizontal: spacing.sm,
                  paddingBottom: spacing.xs,
                },
              ]}
            >
              {canPick && i === 0 ? (
                <Button
                  label="Draft"
                  size="sm"
                  disabled={!room}
                  loading={pickingId === item.player.id}
                  onPress={() => onPick(item.player)}
                  accessibilityLabel={`Draft ${item.player.displayName}`}
                />
              ) : null}
              <IconButton
                icon={ArrowUp}
                label={`Move ${item.player.displayName} up`}
                disabled={i === 0}
                onPress={() => onMove(item.player.id, -1)}
                testID={`queue-up-${item.player.id}`}
                size={18}
              />
              <IconButton
                icon={ArrowDown}
                label={`Move ${item.player.displayName} down`}
                disabled={i === queue.length - 1}
                onPress={() => onMove(item.player.id, 1)}
                testID={`queue-down-${item.player.id}`}
                size={18}
              />
              <IconButton
                icon={X}
                label={`Remove ${item.player.displayName} from queue`}
                onPress={() => onRemove(item.player.id)}
                testID={`queue-remove-${item.player.id}`}
                size={18}
                color={colors.textMuted}
              />
            </View>
          );
          const row = (
            <PlayerRow
              key={item.player.id}
              testID={compact ? undefined : `queue-item-${item.player.id}`}
              divider={!compact && i > 0}
              player={item.player}
              onOpenCard={openCard ? () => openCard(item.player.id) : undefined}
              detail={`Rank ${item.draftRank}${room ? '' : ' · position full'}`}
              dimmed={!room}
              leading={
                <AppText variant="meta" subtle numeric style={{ width: 20, textAlign: 'right' }}>
                  {i + 1}
                </AppText>
              }
              trailing={compact ? undefined : controls}
            />
          );
          return compact ? (
            <View
              key={item.player.id}
              testID={`queue-item-${item.player.id}`}
              style={{ borderTopWidth: i > 0 ? 1 : 0, borderTopColor: colors.border }}
            >
              {row}
              {controls}
            </View>
          ) : (
            row
          );
        })
      )}
    </Body>
  );
}

/** My picks so far, grouped by position, with the empty places. */
export function DraftRoster({
  picks,
  myTeam,
  scroll = true,
}: {
  picks: readonly Schemas['DraftPick'][];
  myTeam: Schemas['TeamRef'] | null;
  /** False on a scrolling page: plain content instead of a scroll view. */
  scroll?: boolean;
}) {
  const { colors, spacing } = useTheme();
  const openCard = usePlayerCard();
  if (!myTeam) {
    return <EmptyState title="You're watching" body="You don't have a team in this league." />;
  }
  const mine = picks.filter((p) => p.team.id === myTeam.id);
  const content = (
    <>
      <View
        style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md, padding: spacing.md }}
      >
        <Avatar name={myTeam.name} size={36} square />
        <View style={{ flex: 1 }}>
          <AppText weight="semibold">{myTeam.name}</AppText>
          <AppText variant="meta" muted>{`${mine.length} of 15 picked`}</AppText>
        </View>
      </View>
      {POSITIONS.map((pos) => {
        const list = mine.filter((p) => p.player.position === pos);
        return (
          <View key={pos} style={{ borderTopWidth: 1, borderTopColor: colors.border }}>
            <View
              style={{
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.sm,
                paddingHorizontal: spacing.md,
                paddingVertical: spacing.sm,
              }}
            >
              <PositionBadge position={pos} size="sm" />
              <AppText
                variant="meta"
                color={colors[positionColorKey(pos)]}
                weight="semibold"
                numeric
              >
                {`${list.length}/${SQUAD[pos]}`}
              </AppText>
            </View>
            {list.map((p) => (
              <PlayerRow
                key={p.id}
                testID={`draft-roster-${p.player.id}`}
                player={p.player}
                detail={`Pick ${p.overall}`}
                onOpenCard={openCard ? () => openCard(p.player.id) : undefined}
              />
            ))}
            {Array.from({ length: Math.max(0, SQUAD[pos] - list.length) }, (_, i) => (
              <View
                key={`empty-${i}`}
                style={{
                  height: 40,
                  marginHorizontal: spacing.lg,
                  marginBottom: spacing.xs,
                  borderRadius: 8,
                  borderWidth: 1,
                  borderStyle: 'dashed',
                  borderColor: colors.border,
                  justifyContent: 'center',
                  paddingHorizontal: spacing.md,
                }}
              >
                <AppText variant="meta" subtle>{`Empty ${positionLabel(pos)} place`}</AppText>
              </View>
            ))}
          </View>
        );
      })}
    </>
  );
  return scroll ? (
    <ScrollView
      style={{ flex: 1 }}
      testID="draft-roster"
      contentContainerStyle={{ paddingBottom: spacing.lg }}
    >
      {content}
    </ScrollView>
  ) : (
    <View testID="draft-roster" style={{ paddingBottom: spacing.sm }}>
      {content}
    </View>
  );
}

/** A row of team chips: pick whose picks the roster pane shows (the finished draft). */
export function DraftTeamChips({
  teams,
  selectedId,
  onSelect,
}: {
  teams: readonly Schemas['TeamRef'][];
  selectedId: string | null;
  onSelect: (teamId: string) => void;
}) {
  const { spacing } = useTheme();
  return (
    <ScrollView
      horizontal
      showsHorizontalScrollIndicator={false}
      testID="draft-team-chips"
      accessibilityLabel="Teams"
      style={{ flexGrow: 0 }}
      contentContainerStyle={{ gap: spacing.sm, padding: spacing.md }}
    >
      {teams.map((t) => (
        <Chip
          key={t.id}
          label={t.name}
          selected={t.id === selectedId}
          onPress={() => onSelect(t.id)}
          testID={`draft-team-${t.id}`}
        />
      ))}
    </ScrollView>
  );
}
