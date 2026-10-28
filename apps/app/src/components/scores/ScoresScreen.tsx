import { View } from 'react-native';

import { Card } from '../Card';
import { EmptyState } from '../EmptyState';
import { CalendarX } from '../icons';
import { QueryStatus } from '../QueryStatus';
import { Screen } from '../Screen';
import { SectionHeader } from '../SectionHeader';
import { Skeleton } from '../Skeleton';
import { GameweekSwitcher } from '../team/GameweekSwitcher';
import { FixtureRow } from './FixtureRow';
import { useFreshness, useNow } from '@/api/freshness';
import { useCurrentGameweek, useGameweekFixtures } from '@/api/hooks';
import { groupFixturesByDay, liveCount } from '@/lib/scores';
import { useTheme } from '@/theme';

interface Props {
  /** The gameweek to show; undefined means the current one. */
  gameweek?: number;
  onChangeGameweek: (gw: number) => void;
}

/**
 * SCORES, Sleeper's scores screen mapped onto the Premier League: the
 * `‹ GW 7 ›` stepper (the current gameweek first), the fixtures under a
 * heading per day with kickoff times, live scores and clocks. A fixture
 * opens its box score. Polls while scores are live and the screen has
 * focus (useGameweekFixtures); a league's socket refreshes it sooner.
 */
export function ScoresScreen({ gameweek, onChangeGameweek }: Props) {
  const { spacing } = useTheme();
  const current = useCurrentGameweek();
  const gw = gameweek ?? current.data?.data.current.number;
  const fixtures = useGameweekFixtures(gw);
  const freshness = useFreshness(gw === undefined ? current : fixtures);
  const now = useNow(60_000);
  const live = liveCount(fixtures.data?.data ?? []);

  return (
    <Screen
      title="Scores"
      subtitle={
        gw === undefined ? undefined : live > 0 ? `Gameweek ${gw} · ${live} live` : `Gameweek ${gw}`
      }
      freshness={freshness}
      maxWidth={640}
      actions={
        gw !== undefined ? (
          <GameweekSwitcher gameweek={gw} min={1} max={38} onChange={onChangeGameweek} />
        ) : null
      }
    >
      <QueryStatus
        query={gw === undefined ? { ...current, data: undefined } : fixtures}
        loading={
          <View style={{ gap: spacing.sm }} testID="scores-loading">
            {Array.from({ length: 6 }, (_, i) => (
              <Skeleton key={i} height={64} radius={16} />
            ))}
          </View>
        }
      >
        {({ data }) =>
          data.length === 0 ? (
            <Card>
              <EmptyState
                icon={CalendarX}
                title="No fixtures"
                body={`No Premier League matches are scheduled in gameweek ${gw}. Fixtures moved here later show up once they are rearranged.`}
                testID="scores-empty"
              />
            </Card>
          ) : (
            <View style={{ gap: spacing.lg }} testID="scores-list">
              {groupFixturesByDay(data, now).map((day) => (
                <View key={day.key} style={{ gap: spacing.sm }} testID={`scores-day-${day.key}`}>
                  <SectionHeader
                    title={day.title}
                    detail={day.key === 'postponed' ? 'Counts when replayed' : undefined}
                    testID="scores-day"
                  />
                  {day.fixtures.map((f) => (
                    <FixtureRow key={f.fixtureId} fixture={f} />
                  ))}
                </View>
              ))}
            </View>
          )
        }
      </QueryStatus>
    </Screen>
  );
}
