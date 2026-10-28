import type { Schemas } from '@myfantasyprem/api-client';
import { QueryClientContext } from '@tanstack/react-query';
import { useContext, type ComponentProps } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { pressFeedback, type PressState } from '../pressable';
import { PressableLink } from '../PressableLink';
import { WinChanceBar } from '../projections/WinChance';
import { Emblem, HeroBars, heroCentreWidth, ResultChip, Subline } from './MatchupHero';
import { queryKeys } from '@/api/hooks';
import { usePressPrefetch } from '@/api/prefetch';
import {
  leadOf,
  orient,
  resultFor,
  sideName,
  sidePoints,
  teamOf,
  teamRecords,
  type AnySide,
  type TeamRecord,
} from '@/lib/matchup';
import { useTheme } from '@/theme';

type Side = AnySide;

interface Props {
  leagueId: string;
  matchups: readonly (Schemas['MatchupSummary'] | Schemas['ScheduledMatchup'])[];
  myTeamId?: string;
  /** The matchup open on screen, highlighted. */
  selectedId?: string;
  /** Record, streak and rank by team id; read from the cached league home when omitted. */
  records?: ReadonlyMap<string, TeamRecord>;
  /** Two cards a row (wide screens). */
  columns?: 1 | 2;
  /** Win odds by matchup id (ADR 0014): a win-chance bar under each card's names. */
  odds?: ReadonlyMap<string, Schemas['MatchupOdds']>;
  testID?: string;
  /** Given: a card selects its matchup in place (the Match tab's carousel) instead of opening it. */
  onSelect?: (matchupId: string) => void;
}

/**
 * A gameweek's matchups as compact hero cards: round avatars at the outer
 * edges (a medal for the top three), each side's share of the live points
 * as bars growing toward a `VS` square, the scores at the bars' inner ends,
 * then bold names with manager, record, streak and rank, and the win chance
 * when odds are given. Scheduled matchups (no scores yet) show empty bars. Each
 * card opens the matchup; mine and the open one have an accent border.
 */
export function MatchupList({
  leagueId,
  matchups,
  myTeamId,
  selectedId,
  records,
  columns = 1,
  odds,
  testID,
  onSelect,
}: Props) {
  const { colors, spacing, radii } = useTheme();
  // The league home is already cached on every screen that lists matchups;
  // read its standings passively instead of starting another query.
  const client = useContext(QueryClientContext);
  const cached = client?.getQueryData<{ data: Schemas['LeagueHome'] }>(queryKeys.league(leagueId));
  const lines = records ?? teamRecords(cached?.data?.standings);
  const prefetch = usePressPrefetch();
  return (
    <View
      testID={testID}
      style={
        columns === 2
          ? { flexDirection: 'row', flexWrap: 'wrap', gap: spacing.sm }
          : { gap: spacing.sm }
      }
    >
      {matchups.map((m) => {
        const { left, right } = orient<Side>(m, myTeamId);
        const state: Schemas['ScoreState'] = 'state' in m ? m.state : 'upcoming';
        const lp = sidePoints(left);
        const rp = sidePoints(right);
        const live = state === 'live';
        const mine =
          myTeamId !== undefined &&
          (teamOf(left)?.id === myTeamId || teamOf(right)?.id === myTeamId);
        const label = `${sideName(left)} ${lp ?? ''} versus ${sideName(right)} ${rp ?? ''}${
          live ? ', live' : ''
        }`;
        const score = (
          pts: number | null,
          other: number | null,
          res: ReturnType<typeof resultFor>,
          side: 'left' | 'right',
        ) =>
          pts === null ? null : (
            <View
              style={{
                flexDirection: side === 'left' ? 'row' : 'row-reverse',
                alignItems: 'center',
                gap: spacing.xs,
              }}
            >
              <ResultChip result={res} testID={`matchup-row-${m.id}-${side}-result`} />
              <AppText
                variant="heading"
                numeric
                weight="bold"
                muted={leadOf(pts, other) === 'trail'}
              >
                {String(pts)}
              </AppText>
            </View>
          );
        const highlighted = selectedId === m.id || mine;
        return (
          <Card
            key={m.id}
            href={`/leagues/${leagueId}/matchup/${m.gameweek}/${m.id}`}
            onSelect={onSelect ? () => onSelect(m.id) : undefined}
            testID={`matchup-row-${m.id}`}
            onPressIn={() => prefetch.matchup(leagueId, m.gameweek, m.id)}
            accessibilityLabel={label}
            accessibilityState={{ selected: selectedId === m.id }}
            style={(s: PressState) => [
              {
                gap: spacing.sm,
                padding: spacing.md,
                // Two a row: half the width less half the gap.
                ...(columns === 2 ? { flexBasis: '48%', flexGrow: 1 } : null),
                borderRadius: radii.lg,
                borderWidth: 1,
                borderColor: highlighted ? colors.accent : colors.border,
                backgroundColor: colors.surface,
              },
              ...pressFeedback(colors, s),
            ]}
          >
            <HeroBars
              left={left}
              right={right}
              leftPoints={lp}
              rightPoints={rp}
              size="sm"
              records={lines}
              leftInner={score(lp, rp, resultFor(lp, rp, state), 'left')}
              rightInner={score(rp, lp, resultFor(rp, lp, state), 'right')}
              emblem={
                <Emblem size="sm" highlight={live}>
                  {live ? (
                    <View
                      aria-hidden
                      style={{
                        width: 6,
                        height: 6,
                        borderRadius: 3,
                        backgroundColor: colors.accent,
                        marginBottom: 1,
                      }}
                    />
                  ) : null}
                  <AppText variant="eyebrow" muted>
                    VS
                  </AppText>
                </Emblem>
              }
            />
            <View style={{ flexDirection: 'row', alignItems: 'flex-start' }}>
              <Names side={left} records={lines} align="left" />
              <View style={{ width: heroCentreWidth('sm') }} />
              <Names side={right} records={lines} align="right" />
            </View>
            <WinChanceBar
              odds={odds?.get(m.id)}
              leftIsHome={left === m.home}
              compact
              testID={`win-chance-${m.id}`}
            />
          </Card>
        );
      })}
    </View>
  );
}

/** A matchup card: a link to the matchup, or a button that selects it in place. */
function Card({
  href,
  onSelect,
  ...rest
}: Omit<ComponentProps<typeof PressableLink>, 'onPress'> & { onSelect?: () => void }) {
  return onSelect ? (
    <Pressable accessibilityRole="button" {...rest} onPress={onSelect} />
  ) : (
    <PressableLink href={href} {...rest} />
  );
}

function Names({
  side,
  records,
  align,
}: {
  side: Side;
  records: ReadonlyMap<string, TeamRecord>;
  align: 'left' | 'right';
}) {
  return (
    <View
      style={{
        flex: 1,
        minWidth: 0,
        gap: 2,
        alignItems: align === 'left' ? 'flex-start' : 'flex-end',
      }}
    >
      <AppText weight="bold" numberOfLines={1} style={{ textAlign: align, maxWidth: '100%' }}>
        {sideName(side)}
      </AppText>
      <Subline side={side} records={records} align={align} />
    </View>
  );
}
