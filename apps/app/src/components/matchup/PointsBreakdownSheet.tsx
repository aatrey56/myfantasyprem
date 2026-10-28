import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { ClubTag, PositionBadge } from '../Badges';
import { Pill } from '../Chip';
import { Sheet } from '../Sheet';
import { availabilityText, SubChip } from '../PlayerRow';
import { autoSubOf, fixtureState, statLabel } from '@/lib/matchup';
import { fixtureLabel } from '@/lib/fixtures';
import { formatKickoff } from '@/lib/time';
import { useTheme } from '@/theme';

interface Props {
  slot: Schemas['LineupSlot'] | null;
  onClose: () => void;
  /** Shown under the name: the team the player is on. */
  teamName?: string;
}

/**
 * Why a player has the points they have: one block per fixture (two in a
 * double gameweek) with FPL's stat lines and the auto-sub state. A bottom
 * sheet on phone, the side panel on desktop.
 */
export function PointsBreakdownSheet({ slot, onClose, teamName }: Props) {
  return (
    <Sheet
      visible={slot !== null}
      onClose={onClose}
      title={slot ? slot.player.displayName : 'Points'}
      testID="points-breakdown"
      presentation="center"
    >
      {slot ? <Breakdown slot={slot} teamName={teamName} /> : null}
    </Sheet>
  );
}

function Breakdown({ slot, teamName }: { slot: Schemas['LineupSlot']; teamName?: string }) {
  const { colors, spacing, radii } = useTheme();
  const sub = autoSubOf(slot.autoSub);
  const status = availabilityText(slot.player.availability);
  return (
    <View style={{ gap: spacing.lg }}>
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        <PositionBadge position={slot.player.position} />
        <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
          <ClubTag code={slot.player.club.code} name={slot.player.club.name} />
          <AppText variant="meta" muted numberOfLines={2}>
            {[teamName, slot.slot > 11 ? `Bench ${slot.slot - 11}` : 'Starter', status]
              .filter(Boolean)
              .join(' · ')}
          </AppText>
        </View>
        <View style={{ alignItems: 'flex-end' }}>
          <AppText variant="display" numeric testID="breakdown-total">
            {slot.points}
          </AppText>
          <AppText variant="eyebrow" subtle>
            pts
          </AppText>
        </View>
      </View>
      {sub.kind ? (
        <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
          <SubChip kind={sub.kind} projected={sub.projected} />
          <AppText variant="meta" muted style={{ flex: 1 }}>
            {sub.kind === 'out'
              ? `Auto-subbed out${sub.projected ? ' (projected)' : ''}: his points do not count.`
              : `Auto-subbed in${sub.projected ? ' (projected)' : ''}: his points count.`}
          </AppText>
        </View>
      ) : null}
      {slot.player.news ? (
        <AppText variant="meta" muted>
          {slot.player.news}
        </AppText>
      ) : null}
      {slot.fixtures.length === 0 ? (
        <AppText muted testID="breakdown-blank">
          No fixture this gameweek.
        </AppText>
      ) : (
        slot.fixtures.map((f) => {
          const st = fixtureState(f);
          return (
            <View
              key={f.fixtureId}
              testID="breakdown-fixture"
              style={{
                borderWidth: 1,
                borderColor: colors.border,
                borderRadius: radii.md,
                overflow: 'hidden',
              }}
            >
              <View
                style={{
                  flexDirection: 'row',
                  alignItems: 'center',
                  gap: spacing.sm,
                  padding: spacing.md,
                  backgroundColor: colors.surface,
                }}
              >
                <AppText weight="semibold" style={{ flex: 1 }} numberOfLines={1}>
                  {`${fixtureLabel(f)}${
                    typeof f.homeScore === 'number' && typeof f.awayScore === 'number'
                      ? ` · ${f.homeScore}-${f.awayScore}`
                      : ''
                  }`}
                </AppText>
                {st.live ? (
                  <Pill label={`Live ${st.text}`} tone="live" dot />
                ) : (
                  <AppText variant="meta" muted>
                    {f.status === 'scheduled' ? formatKickoff(Date.parse(f.kickoffAt)) : st.text}
                  </AppText>
                )}
              </View>
              {f.breakdown.length === 0 ? (
                <AppText variant="meta" muted style={{ padding: spacing.md }}>
                  {f.status === 'scheduled' ? 'Not played yet.' : 'Did not play.'}
                </AppText>
              ) : (
                f.breakdown.map((line, i) => {
                  return (
                    <View
                      key={`${line.stat}-${i}`}
                      testID="breakdown-line"
                      accessibilityLabel={`${statLabel(line.stat)}: ${line.value}, ${line.points} points`}
                      style={{
                        flexDirection: 'row',
                        alignItems: 'center',
                        gap: spacing.sm,
                        paddingHorizontal: spacing.md,
                        paddingVertical: spacing.sm,
                        borderTopWidth: 1,
                        borderTopColor: colors.border,
                      }}
                    >
                      <AppText variant="meta" style={{ flex: 1 }}>
                        {statLabel(line.stat)}
                      </AppText>
                      <AppText
                        variant="meta"
                        muted
                        numeric
                        style={{ minWidth: 32, textAlign: 'right' }}
                      >
                        {String(line.value)}
                      </AppText>
                      <View style={{ minWidth: 40, alignItems: 'flex-end' }}>
                        <AppText
                          variant="meta"
                          weight="semibold"
                          numeric
                          color={line.points < 0 ? colors.loss : colors.text}
                        >
                          {line.points > 0 ? `+${line.points}` : String(line.points)}
                        </AppText>
                      </View>
                    </View>
                  );
                })
              )}
            </View>
          );
        })
      )}
    </View>
  );
}
