import type { Schemas } from '@myfantasyprem/api-client';
import { useRouter, type Href } from 'expo-router';
import { Pressable, ScrollView, View } from 'react-native';

import { AppText } from '../AppText';
import { positionLabel } from '../Badges';
import { Card } from '../Card';
import { EmptyState } from '../EmptyState';
import { ArrowLeftRight, X } from '../icons';
import { IconButton } from '../Button';
import { PlayerRow } from '../PlayerRow';
import { usePlayerCard } from '../players/PlayerCardContext';
import { pressFeedback, type PressState } from '../pressable';
import { QueryStatus } from '../QueryStatus';
import { PlayerAvatar } from '../RosterRow';
import { SectionHeader } from '../SectionHeader';
import { useToast } from '../Toast';
import { useSetTradeBlock, useTradeBlock } from '@/api/trade-block-hooks';
import { describeError } from '@/lib/errors';
import { formatAgo } from '@/lib/time';
import { carouselEntries, groupByTeam, tradeHref, wantedText } from '@/lib/trade-block';
import { positionColorKey, useTheme } from '@/theme';

type Entry = Schemas['TradeBlockEntry'];

const CARD_WIDTH = 168;

/**
 * The trade block carousel (Sleeper's): other teams' players on the block,
 * newest first, as small cards in a horizontal row. A card opens the trade
 * builder with his team and him already asked for. "See all" opens the
 * table.
 */
export function TradeBlockCarousel({
  leagueId,
  myTeamId,
}: {
  leagueId: string;
  myTeamId?: string;
}) {
  const { spacing } = useTheme();
  const router = useRouter();
  const block = useTradeBlock(leagueId);
  const entries = carouselEntries(block.data?.data ?? [], myTeamId);
  if (!block.data) return null;
  return (
    <View style={{ gap: spacing.xs }} testID="trade-block-carousel">
      <SectionHeader
        title="Trade Block"
        detail={entries.length ? String(entries.length) : undefined}
        action={{
          label: 'See all',
          onPress: () => router.push(`/leagues/${leagueId}/players/trades/block` as Href),
          testID: 'trade-block-all',
        }}
      />
      {entries.length === 0 ? (
        <AppText variant="meta" muted testID="trade-block-empty">
          No other team has a player on the block yet.
        </AppText>
      ) : (
        <ScrollView
          horizontal
          showsHorizontalScrollIndicator={false}
          contentContainerStyle={{ gap: spacing.sm, paddingVertical: 2 }}
          accessibilityLabel="Players on the trade block"
        >
          {entries.map((e) => (
            <BlockCard
              key={e.player.id}
              entry={e}
              onPress={() => router.push(tradeHref(leagueId, e) as Href)}
            />
          ))}
        </ScrollView>
      )}
    </View>
  );
}

function BlockCard({ entry, onPress }: { entry: Entry; onPress: () => void }) {
  const { colors, spacing, radii } = useTheme();
  const { player, team } = entry;
  const pos = positionLabel(player.position);
  return (
    <Pressable
      accessibilityRole="button"
      accessibilityLabel={`Trade for ${player.displayName} from ${team.name}`}
      accessibilityHint={[wantedText(entry.wanted), entry.note].filter(Boolean).join('. ')}
      onPress={onPress}
      testID={`block-card-${player.id}`}
      style={(state: PressState) => [
        {
          width: CARD_WIDTH,
          gap: spacing.xs,
          padding: spacing.md,
          borderRadius: radii.lg,
          borderWidth: 1,
          borderColor: colors.border,
          backgroundColor: colors.surface,
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <PlayerAvatar player={player} size={36} />
        <View style={{ flex: 1, minWidth: 0 }}>
          <AppText weight="bold" numberOfLines={1}>
            {player.displayName}
          </AppText>
          <AppText variant="meta" numberOfLines={1}>
            <AppText
              variant="meta"
              weight="semibold"
              color={colors[positionColorKey(player.position)]}
            >
              {pos}
            </AppText>
            <AppText variant="meta" muted>{` · ${player.club.code.toUpperCase()}`}</AppText>
          </AppText>
        </View>
      </View>
      <AppText variant="meta" muted numberOfLines={1}>
        {team.name}
      </AppText>
      <AppText variant="meta" weight="semibold" accent numberOfLines={1}>
        {wantedText(entry.wanted)}
      </AppText>
      {entry.note ? (
        <AppText variant="meta" subtle numberOfLines={2}>
          {`“${entry.note}”`}
        </AppText>
      ) : null}
    </Pressable>
  );
}

/**
 * The trade block table: every player on the block, by team (mine first).
 * Another team's row opens the trade builder for him; my own rows take
 * him off. The name opens the player card.
 */
export function TradeBlockTable({ leagueId, myTeamId }: { leagueId: string; myTeamId?: string }) {
  const { spacing } = useTheme();
  const router = useRouter();
  const toast = useToast();
  const openCard = usePlayerCard();
  const block = useTradeBlock(leagueId);
  const set = useSetTradeBlock(leagueId);
  return (
    <QueryStatus query={block}>
      {({ data }) => {
        const groups = groupByTeam(data, myTeamId);
        if (groups.length === 0) {
          return (
            <Card>
              <EmptyState
                icon={ArrowLeftRight}
                title="Nobody on the block"
                body="Put a player on your trade block from his card: Trade block."
                testID="trade-block-table-empty"
              />
            </Card>
          );
        }
        return (
          <View style={{ gap: spacing.lg }} testID="trade-block-table">
            {groups.map((g) => {
              const mine = g.team.id === myTeamId;
              return (
                <View
                  key={g.team.id}
                  style={{ gap: spacing.xs }}
                  testID={`block-team-${g.team.id}`}
                >
                  <SectionHeader
                    title={mine ? `${g.team.name} (you)` : g.team.name}
                    detail={String(g.entries.length)}
                  />
                  <Card flush>
                    {g.entries.map((e, i) => (
                      <PlayerRow
                        key={e.player.id}
                        player={e.player}
                        divider={i > 0}
                        detail={[wantedText(e.wanted), e.note, formatAgo(Date.parse(e.addedAt))]
                          .filter(Boolean)
                          .join(' · ')}
                        onOpenCard={openCard ? () => openCard(e.player.id) : undefined}
                        testID={`block-row-${e.player.id}`}
                        trailing={
                          mine ? (
                            <IconButton
                              icon={X}
                              label={`Take ${e.player.displayName} off the block`}
                              testID={`block-remove-${e.player.id}`}
                              onPress={() =>
                                set.mutate(
                                  { player: e.player, on: false },
                                  {
                                    onError: (err) =>
                                      toast({
                                        message: describeError(err),
                                        tone: 'error',
                                        haptic: false,
                                      }),
                                  },
                                )
                              }
                            />
                          ) : (
                            <IconButton
                              icon={ArrowLeftRight}
                              label={`Trade for ${e.player.displayName}`}
                              testID={`block-trade-${e.player.id}`}
                              onPress={() => router.push(tradeHref(leagueId, e) as Href)}
                            />
                          )
                        }
                      />
                    ))}
                  </Card>
                </View>
              );
            })}
          </View>
        );
      }}
    </QueryStatus>
  );
}
