import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { positionLabel } from '../Badges';
import { EmptyState } from '../EmptyState';
import { Newspaper } from '../icons';
import { usePlayerCard } from '../players/PlayerCardContext';
import { pressFeedback, type PressState } from '../pressable';
import { PlayerAvatar, statusTag } from '../RosterRow';
import { newsTime } from '../players/CardExtras';
import { QueryStatus } from '../QueryStatus';
import { Sheet } from '../Sheet';
import { Skeleton } from '../Skeleton';
import { useTeamNews } from '@/api/news-hooks';
import { positionColorKey, useTheme } from '@/theme';

interface Props {
  visible: boolean;
  onClose: () => void;
  leagueId: string;
  teamId: string;
  isMine: boolean;
}

/**
 * The team card's News link: the squad's injury and availability news,
 * newest first, each with when it was added. A row opens the player's card.
 * "No news" only after the request succeeded: a failed one (offline, 503)
 * shows the error with a retry, never "all available".
 */
export function NewsSheet({ visible, onClose, leagueId, teamId, isMine }: Props) {
  const { colors, spacing, radii, fontFamilies } = useTheme();
  const openCard = usePlayerCard();
  const news = useTeamNews(leagueId, teamId, { enabled: visible });
  return (
    <Sheet
      visible={visible}
      onClose={onClose}
      title="News"
      testID="news-sheet"
      presentation="center"
    >
      <QueryStatus query={news} loading={<Skeleton height={120} />}>
        {({ data: items }) =>
          items.length === 0 ? (
            <EmptyState
              icon={Newspaper}
              title="No news"
              body={
                isMine
                  ? 'All your players are available.'
                  : 'Every player in this squad is available.'
              }
              testID="news-empty"
            />
          ) : (
            items.map(({ player, news: line, newsAt, chanceOfPlaying }) => {
              const tag = statusTag(player.availability, chanceOfPlaying);
              const at = newsTime(newsAt);
              const tone = tag?.tone === 'out' ? colors.loss : colors.warning;
              return (
                <Pressable
                  key={player.id}
                  testID={`news-row-${player.id}`}
                  accessibilityRole="button"
                  accessibilityLabel={`Player card for ${player.displayName}`}
                  accessibilityHint={[tag?.label, line, at].filter(Boolean).join(', ')}
                  disabled={!openCard}
                  onPress={() => {
                    onClose();
                    openCard?.(player.id);
                  }}
                  style={(state: PressState) => [
                    {
                      flexDirection: 'row',
                      alignItems: 'flex-start',
                      gap: spacing.md,
                      paddingVertical: spacing.sm,
                      borderRadius: radii.md,
                    },
                    ...pressFeedback(colors, state),
                  ]}
                >
                  <PlayerAvatar player={player} size={36} />
                  <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
                    <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
                      <AppText numberOfLines={1} style={{ flexShrink: 1 }}>
                        <AppText
                          style={{ fontFamily: fontFamilies.bold, fontSize: 16, lineHeight: 21 }}
                        >
                          {player.displayName}
                        </AppText>
                        <AppText
                          variant="meta"
                          weight="semibold"
                          color={colors[positionColorKey(player.position)]}
                        >
                          {`  ${positionLabel(player.position)}`}
                        </AppText>
                        <AppText variant="meta" muted>
                          {` - ${player.club.code.toUpperCase()}`}
                        </AppText>
                      </AppText>
                      {tag ? (
                        <AppText
                          variant="meta"
                          weight="bold"
                          numeric
                          color={tone}
                          testID="news-tag"
                        >
                          {tag.label}
                        </AppText>
                      ) : null}
                    </View>
                    <AppText variant="meta" muted testID="news-text">
                      {line}
                    </AppText>
                    {at ? (
                      <AppText variant="meta" subtle testID="news-time">
                        {at}
                      </AppText>
                    ) : null}
                  </View>
                </Pressable>
              );
            })
          )
        }
      </QueryStatus>
    </Sheet>
  );
}
