import type { Schemas } from '@myfantasyprem/api-client';
import { useEffect } from 'react';
import { View } from 'react-native';

import { useCommissionerMoves } from '@/api/commissioner-hooks';
import { AppText } from '../AppText';
import { Button } from '../Button';
import { Card } from '../Card';
import { EmptyState } from '../EmptyState';
import { Shield } from '../icons';
import { ListRow } from '../ListRow';
import { QueryStatus } from '../QueryStatus';
import { SectionHeader } from '../SectionHeader';
import { SkeletonRows } from '../Skeleton';
import { moveByline } from '@/lib/commissioner-moves';
import { formatAgo, formatDateTime } from '@/lib/time';
import { useTheme } from '@/theme';

/** One commissioner move: what happened, who did it and when. */
function MoveRow({
  move,
  divider,
  now,
}: {
  move: Schemas['CommissionerMove'];
  divider: boolean;
  now: number;
}) {
  const { colors } = useTheme();
  const at = Date.parse(move.createdAt);
  return (
    <ListRow
      divider={divider}
      testID={`commissioner-move-${move.id}`}
      left={<Shield size={18} color={colors.accent} aria-hidden />}
      title={<AppText weight="semibold">{move.summary}</AppText>}
      subtitle={
        <AppText variant="meta" muted>
          {`${moveByline(move)} · ${formatAgo(at, now)} · ${formatDateTime(at)}`}
        </AppText>
      }
    />
  );
}

/**
 * The commissioner's moves, newest first: every use of a power a manager
 * does not have (docs/adr/0017). Sits under League Rules in League settings.
 */
export function CommissionerMovesSection({
  leagueId,
  now,
  onLoaded,
}: {
  leagueId: string;
  now: number;
  /** Called once the first page is on screen (the Status banner scrolls here). */
  onLoaded?: () => void;
}) {
  const { spacing } = useTheme();
  const moves = useCommissionerMoves(leagueId);
  const loaded = moves.data !== undefined;
  useEffect(() => {
    if (loaded) onLoaded?.();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- once, when the first page arrives
  }, [loaded]);
  const items = moves.data?.pages.flatMap((p) => p.data) ?? [];
  return (
    <View style={{ gap: spacing.md }} testID="commissioner-moves-section">
      <SectionHeader title="Commissioner Moves" />
      <QueryStatus
        query={{ ...moves, data: moves.data?.pages[0] }}
        loading={
          <Card flush>
            <SkeletonRows count={3} />
          </Card>
        }
      >
        {() => {
          if (items.length === 0) {
            return (
              <EmptyState
                title="No commissioner moves yet"
                body="When the commissioner overrides a trade, drafts for a team or changes the rules, it is listed here."
              />
            );
          }
          return (
            <>
              <Card flush testID="commissioner-moves-list">
                {items.map((m, i) => (
                  <MoveRow key={m.id} move={m} divider={i > 0} now={now} />
                ))}
              </Card>
              {moves.hasNextPage ? (
                <Button
                  label="Show older moves"
                  variant="secondary"
                  loading={moves.isFetchingNextPage}
                  onPress={() => void moves.fetchNextPage()}
                  style={{ alignSelf: 'flex-start' }}
                  testID="commissioner-moves-more"
                />
              ) : null}
            </>
          );
        }}
      </QueryStatus>
    </View>
  );
}
