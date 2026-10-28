import { useRouter } from 'expo-router';

import { Avatar } from './Badges';
import { Button } from './Button';
import { Card } from './Card';
import { Pill } from './Chip';
import { ListRow } from './ListRow';
import { SectionHeader } from './SectionHeader';
import { Sheet } from './Sheet';
import { SkeletonRows } from './Skeleton';
import { AppText } from './AppText';
import { useMyLeagues } from '@/api/account-hooks';
import { describeError } from '@/lib/errors';
import { leagueHref, leagueStatus, sortMyLeagues } from '@/lib/leagues';

interface Props {
  visible: boolean;
  onClose: () => void;
  currentLeagueId: string;
}

/** Switch league without going back to the hub (BRIEF §2). */
export function LeagueSwitcherSheet({ visible, onClose, currentLeagueId }: Props) {
  const router = useRouter();
  const leagues = useMyLeagues();
  const go = (href: string) => {
    onClose();
    router.push(href as never);
  };
  return (
    <Sheet
      visible={visible}
      onClose={onClose}
      title="Your leagues"
      testID="league-switcher"
      presentation="center"
    >
      <SectionHeader title="Switch league" />
      <Card flush>
        {leagues.data ? (
          sortMyLeagues(leagues.data.data).map((row, i) => {
            const st = leagueStatus(row.league.status);
            return (
              <ListRow
                key={row.league.id}
                divider={i > 0}
                selected={row.league.id === currentLeagueId}
                left={<Avatar name={row.league.name} size={32} square />}
                title={row.league.name}
                subtitle={row.team.name}
                right={<Pill label={st.label} tone={st.tone} />}
                onPress={() => go(leagueHref(row))}
              />
            );
          })
        ) : leagues.isError ? (
          <AppText muted style={{ padding: 16 }}>
            {describeError(leagues.error)}
          </AppText>
        ) : (
          <SkeletonRows count={3} />
        )}
      </Card>
      <Button label="All leagues" variant="secondary" onPress={() => go('/leagues')} />
    </Sheet>
  );
}
