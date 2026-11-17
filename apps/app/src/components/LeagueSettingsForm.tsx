import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from './AppText';
import { Card } from './Card';
import { SegmentedControl } from './SegmentedControl';
import { SectionHeader } from './SectionHeader';
import { Stepper } from './TextField';
import { MIN_TEAMS } from '@/lib/draft';
import { nearestVetoThreshold, VETO_THRESHOLDS, vetoThresholdText } from '@/lib/trade';
import { waiverOrderText } from '@/lib/waivers';
import { useTheme } from '@/theme';

/** The settings a commissioner chooses. Everything else is fixed in v1. */
export interface EditableSettings {
  teamCount: number;
  pickTimerSeconds: number;
  review: Schemas['TradeReview'];
  reviewHours: number;
  /** League vote: the share of the managers outside a trade whose vetoes block it. */
  vetoThreshold: number;
  waiverOrder: Schemas['WaiverOrderRule'];
}

/** Sensible defaults: 10 teams, 60 s picks, a league veto vote of 24 hours (a third of the managers), reverse-standings waivers (the server's defaults). */
export const DEFAULT_SETTINGS: EditableSettings = {
  teamCount: 10,
  pickTimerSeconds: 60,
  review: 'league_vote',
  reviewHours: 24,
  vetoThreshold: 1 / 3,
  waiverOrder: 'reverse_standings',
};

export const PICK_TIMERS = [
  { value: '30', label: '30s' },
  { value: '60', label: '1 min' },
  { value: '90', label: '90s' },
  { value: '120', label: '2 min' },
  { value: '300', label: '5 min' },
] as const;

const REVIEW_HELP: Record<string, string> = {
  none: 'Accepted trades go through at once.',
  commissioner: 'The commissioner can veto within the review window; otherwise it goes through.',
  league_vote:
    'Other managers can vote to veto during the review; enough vetoes block the trade, otherwise it goes through when the review ends.',
};

/** The settings body of a create/edit request. */
export function settingsInput(s: EditableSettings): Schemas['LeagueSettingsInput'] {
  return {
    draft: { pickTimerSeconds: s.pickTimerSeconds },
    trades: {
      review: s.review,
      reviewHours: s.review === 'none' ? 0 : s.reviewHours,
      ...(s.review === 'league_vote' ? { vetoThreshold: s.vetoThreshold } : {}),
    },
    waivers: { order: s.waiverOrder },
  };
}

export function fromLeagueSettings(s: Schemas['LeagueSettings']): EditableSettings {
  return {
    teamCount: s.teamCount,
    pickTimerSeconds: s.draft.pickTimerSeconds,
    review: s.trades.review,
    reviewHours: s.trades.reviewHours || 24,
    vetoThreshold: s.trades.vetoThreshold,
    waiverOrder: s.waivers.order,
  };
}

interface Props {
  value: EditableSettings;
  onChange: (value: EditableSettings) => void;
  /** Draft-time settings lock once the draft starts. */
  locked?: boolean;
  /** Smallest team count allowed (the members already in). */
  minTeams?: number;
  /** Read only: members see the settings without controls. */
  readOnly?: boolean;
  /** When a waiver order change applies (the next run). */
  waiverNote?: string;
}

/** Team count, pick clock and trade review, plus the fixed rules as a summary. */
export function LeagueSettingsForm({
  value,
  onChange,
  locked,
  minTeams = MIN_TEAMS,
  readOnly,
  waiverNote,
}: Props) {
  const { spacing } = useTheme();
  const set = (patch: Partial<EditableSettings>) => onChange({ ...value, ...patch });
  const timerValue = PICK_TIMERS.some((t) => Number(t.value) === value.pickTimerSeconds)
    ? String(value.pickTimerSeconds)
    : '60';
  return (
    <View style={{ gap: spacing.lg }}>
      <Card style={{ gap: spacing.lg }}>
        {locked ? (
          <AppText variant="meta" muted testID="settings-locked">
            The draft has started, so team count, the pick clock and the kind of trade review are
            locked.
            {readOnly
              ? ''
              : ' The review period, veto threshold and waiver order can still change.'}
          </AppText>
        ) : null}
        {locked ? (
          <AppText>{`${value.teamCount} teams`}</AppText>
        ) : (
          <Stepper
            label="Teams"
            value={value.teamCount}
            min={Math.max(MIN_TEAMS, minTeams)}
            max={20}
            onChange={(teamCount) => set({ teamCount })}
            format={(n) => `${n} teams`}
            testID="team-count"
          />
        )}
        {value.teamCount % 2 === 1 ? (
          <AppText variant="meta" muted>
            Odd number of teams: each gameweek one team plays the league average.
          </AppText>
        ) : null}
        <View style={{ gap: spacing.xs }}>
          <AppText variant="meta" weight="semibold">
            Pick clock
          </AppText>
          {locked ? (
            <AppText>{`${value.pickTimerSeconds} seconds`}</AppText>
          ) : (
            <SegmentedControl
              kind="radio"
              accessibilityLabel="Pick clock"
              segments={PICK_TIMERS}
              value={timerValue}
              onChange={(v) => set({ pickTimerSeconds: Number(v) })}
              testID="pick-timer"
            />
          )}
        </View>
        <View style={{ gap: spacing.xs }}>
          <AppText variant="meta" weight="semibold">
            Trade review
          </AppText>
          {locked ? null : (
            <SegmentedControl
              kind="radio"
              accessibilityLabel="Trade review"
              segments={[
                { value: 'none', label: 'None' },
                { value: 'commissioner', label: 'Commissioner' },
                { value: 'league_vote', label: 'Vote' },
              ]}
              value={value.review}
              onChange={(review) => set({ review })}
              testID="trade-review"
            />
          )}
          <AppText variant="meta" muted>
            {REVIEW_HELP[value.review] ?? 'Custom review rules.'}
          </AppText>
        </View>
        {value.review !== 'none' && readOnly ? (
          <AppText variant="meta" testID="review-summary">
            {value.review === 'league_vote'
              ? `${value.reviewHours} hour review. ${vetoThresholdText(value.vetoThreshold, value.teamCount)}`
              : `${value.reviewHours} hour review.`}
          </AppText>
        ) : null}
        {value.review !== 'none' && !readOnly ? (
          <View style={{ gap: spacing.xs }}>
            <AppText variant="meta" weight="semibold">
              Review period
            </AppText>
            <SegmentedControl
              kind="radio"
              accessibilityLabel="Review period"
              segments={[
                { value: '12', label: '12h' },
                { value: '24', label: '24h' },
                { value: '48', label: '48h' },
                { value: '72', label: '72h' },
              ]}
              value={String(value.reviewHours)}
              onChange={(v) => set({ reviewHours: Number(v) })}
              testID="review-hours"
            />
          </View>
        ) : null}
        {value.review === 'league_vote' && !readOnly ? (
          <View style={{ gap: spacing.xs }}>
            <AppText variant="meta" weight="semibold">
              Veto threshold
            </AppText>
            <SegmentedControl
              kind="radio"
              accessibilityLabel="Veto threshold"
              segments={VETO_THRESHOLDS.map((t) => ({ value: String(t.value), label: t.label }))}
              value={String(nearestVetoThreshold(value.vetoThreshold))}
              onChange={(v) => set({ vetoThreshold: Number(v) })}
              testID="veto-threshold"
            />
            <AppText variant="meta" muted testID="veto-threshold-help">
              {vetoThresholdText(nearestVetoThreshold(value.vetoThreshold), value.teamCount)}
            </AppText>
          </View>
        ) : null}
      </Card>

      <Card style={{ gap: spacing.xs }}>
        <AppText variant="meta" weight="semibold">
          Waiver order
        </AppText>
        {readOnly ? (
          <AppText testID="waiver-order-value">{waiverOrderText(value.waiverOrder).label}</AppText>
        ) : (
          <SegmentedControl
            kind="radio"
            accessibilityLabel="Waiver order"
            segments={[
              {
                value: 'reverse_standings',
                label: 'Reverse standings',
                testID: 'waiver-order-reverse_standings',
              },
              { value: 'rolling', label: 'Rolling', testID: 'waiver-order-rolling' },
            ]}
            value={value.waiverOrder}
            onChange={(waiverOrder) => set({ waiverOrder })}
            testID="waiver-order"
          />
        )}
        <AppText variant="meta" muted testID="waiver-order-help">
          {waiverOrderText(value.waiverOrder).help}
        </AppText>
        {waiverNote ? (
          <AppText variant="meta" muted testID="waiver-order-effect">
            {waiverNote}
          </AppText>
        ) : null}
      </Card>

      <SectionHeader title="League rules" />
      <Card style={{ gap: spacing.xs }}>
        {[
          ['Scoring', 'Standard points'],
          ['Format', 'Head to head · win 3, draw 1'],
          ['Draft', 'Snake, 15 rounds'],
          ['Squad', '15: 2 GK, 5 DEF, 5 MID, 3 FWD'],
          ['Starting XI', '1 GK, 3-5 DEF, 2-5 MID, 1-3 FWD'],
          ['Waivers', 'Run 24 hours before each deadline'],
        ].map(([k, v]) => (
          <View
            key={k}
            style={{ flexDirection: 'row', justifyContent: 'space-between', gap: spacing.md }}
          >
            <AppText variant="meta" muted>
              {k}
            </AppText>
            <AppText variant="meta" style={{ flexShrink: 1, textAlign: 'right' }}>
              {v}
            </AppText>
          </View>
        ))}
      </Card>
    </View>
  );
}
