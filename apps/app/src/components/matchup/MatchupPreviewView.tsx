import type { Schemas } from '@myfantasyprem/api-client';
import type { ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Card } from '../Card';
import { SectionHeader } from '../SectionHeader';
import { PairBadge } from './H2HLineups';
import { pairInOrder } from '@/lib/h2h-pairing';
import { pairBench, sideName } from '@/lib/matchup';
import { formatKickoff } from '@/lib/time';
import { useTheme } from '@/theme';

type PreviewSide = Schemas['PreviewSide'];
type PreviewSlot = Schemas['PreviewSlot'];

interface Props {
  preview: Schemas['MatchupPreview'];
  myTeamId?: string;
  stepper?: ReactNode;
  /** The Expected strip and win chance, when projections are on. */
  projection?: (sides: {
    leftTeamId: string | null;
    rightTeamId: string | null;
    leftIsHome: boolean;
  }) => ReactNode;
}

const asSide = (s: PreviewSide): Schemas['Opponent'] =>
  s.kind === 'team' && s.team
    ? { kind: 'team', team: s.team }
    : ({ kind: 'average' } as Schemas['Opponent']);

/**
 * A future gameweek's starters and bench, head to head: each side's squad
 * as it stands and every player's fixtures in that gameweek with kickoffs
 * as scheduled now. No points; the projection strip when it is passed.
 */
export function MatchupPreviewView({ preview, myTeamId, stepper, projection }: Props) {
  const { spacing } = useTheme();
  // Mine on the left, else home (the league average is only ever away).
  const homeLeft = !(myTeamId && preview.away.team?.id === myTeamId);
  const left = homeLeft ? preview.home : preview.away;
  const right = homeLeft ? preview.away : preview.home;
  return (
    <View style={{ gap: spacing.sm }} testID="matchup-preview">
      <SectionHeader title={`GW ${preview.gameweek} preview`} right={stepper} />
      <AppText variant="meta" muted>
        Current squads and scheduled kickoffs. Scores show once the gameweek starts.
      </AppText>
      {projection?.({
        leftTeamId: left.team?.id ?? null,
        rightTeamId: right.team?.id ?? null,
        leftIsHome: homeLeft,
      })}
      <Card flush>
        <Rows label="Starters" rows={pairInOrder(left.starters, right.starters)} />
      </Card>
      <SectionHeader title="Bench" />
      <Card flush>
        <Rows label="Bench" rows={pairBench(left.bench, right.bench)} />
      </Card>
      <View style={{ flexDirection: 'row', justifyContent: 'space-between' }}>
        <AppText variant="meta" subtle>
          {`${sideName(asSide(left))}${left.formation ? ` · ${left.formation}` : ''}`}
        </AppText>
        <AppText variant="meta" subtle>
          {`${right.formation ? `${right.formation} · ` : ''}${sideName(asSide(right))}`}
        </AppText>
      </View>
    </View>
  );
}

/**
 * Starters pair in order (keepers, then DEF→MID→FWD row by row, a split
 * badge where the positions differ), as on a scored matchup; the benches
 * pair in bench order.
 */
function Rows({
  label,
  rows,
}: {
  label: 'Starters' | 'Bench';
  rows: readonly { left: PreviewSlot | null; right: PreviewSlot | null; order?: number }[];
}) {
  const { colors, spacing } = useTheme();
  if (rows.length === 0)
    return (
      <AppText variant="meta" muted style={{ padding: spacing.md }}>
        No lineup
      </AppText>
    );
  return (
    <View accessibilityLabel={label}>
      {rows.map((row, i) => (
        <View
          key={i}
          testID={label === 'Starters' ? 'preview-starter-row' : 'preview-bench-row'}
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            gap: spacing.sm,
            paddingHorizontal: spacing.md,
            paddingVertical: spacing.sm,
            borderTopWidth: i === 0 ? 0 : 1,
            borderTopColor: colors.border,
          }}
        >
          <Slot slot={row.left} align="left" />
          {label === 'Starters' ? (
            <PairBadge left={row.left} right={row.right} />
          ) : (
            <AppText variant="eyebrow" muted style={{ width: 32, textAlign: 'center' }}>
              {row.order ?? i + 1}
            </AppText>
          )}
          <Slot slot={row.right} align="right" />
        </View>
      ))}
    </View>
  );
}

function Slot({ slot, align }: { slot: PreviewSlot | null; align: 'left' | 'right' }) {
  if (!slot) return <View style={{ flex: 1 }} />;
  const fixtures = slot.fixtures.length
    ? slot.fixtures
        .map(
          (f) =>
            `${f.opponent.code}${f.isHome ? ' (H)' : ' (A)'} ${formatKickoff(Date.parse(f.kickoffAt))}`,
        )
        .join(', ')
    : 'No fixture';
  return (
    <View
      style={{ flex: 1, minWidth: 0, alignItems: align === 'left' ? 'flex-start' : 'flex-end' }}
    >
      <AppText
        weight="semibold"
        numberOfLines={1}
        style={{ textAlign: align }}
        testID={`preview-name-${align}`}
      >
        {slot.player.displayName}
      </AppText>
      <AppText
        variant="meta"
        muted
        numberOfLines={2}
        style={{ textAlign: align }}
        testID="preview-fixture"
      >
        {fixtures}
      </AppText>
    </View>
  );
}
