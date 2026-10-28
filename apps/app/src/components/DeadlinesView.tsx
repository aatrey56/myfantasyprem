import type { components } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from './AppText';
import { Card } from './Card';
import { Pill, type PillTone } from './Chip';
import { EmptyState } from './EmptyState';
import { Calendar, ChevronDown, ChevronRight } from './icons';
import { ListRow } from './ListRow';
import { SectionHeader } from './SectionHeader';
import { deviceTimeZone, formatCountdown, formatDateTime, formatRelative } from '@/lib/time';
import { focusRing, useTheme } from '@/theme';

type Gameweek = components['schemas']['Gameweek'];
type GameweekStatus = components['schemas']['GameweekStatus'];

interface Milestone {
  key: 'lock' | 'waivers' | 'trades';
  label: string;
  hint: string;
  at: number;
}

/** The gameweek's three deadlines in time order. */
function milestones(gw: Gameweek): Milestone[] {
  const list: Milestone[] = [
    {
      key: 'trades',
      label: 'Trade deadline',
      hint: 'Last moment to accept trades',
      at: Date.parse(gw.tradeDeadlineAt),
    },
    {
      key: 'waivers',
      label: 'Waivers process',
      hint: 'Claims run in priority order',
      at: Date.parse(gw.waiverDeadlineAt),
    },
    {
      key: 'lock',
      label: 'Lineup lock',
      hint: 'Your lineup locks',
      at: Date.parse(gw.deadlineAt),
    },
  ];
  return list.sort((a, b) => a.at - b.at);
}

function GameweekCard({ gw, now, id }: { gw: Gameweek; now: number; id: string }) {
  const rows = milestones(gw);
  return (
    <View style={{ gap: 8 }}>
      <SectionHeader title={gw.name} testID={`${id}-header`} />
      <Card flush testID={id}>
        {rows.map((m, i) => {
          const passed = m.at <= now;
          // Past a day, "2h ago" turns into the date, which the row already shows.
          const relative = passed && now - m.at >= 86_400_000 ? null : formatRelative(m.at, now);
          return (
            <ListRow
              key={m.key}
              divider={i > 0}
              title={m.label}
              subtitle={m.hint}
              accessibilityLabel={`${m.label}: ${formatDateTime(m.at, now)}, ${passed ? ['passed', relative].filter(Boolean).join(' ') : relative}`}
              testID={`${id}-${m.key}`}
              right={
                <View style={{ alignItems: 'flex-end', gap: 2 }}>
                  <AppText variant="meta" weight="semibold" numeric>
                    {formatDateTime(m.at, now)}
                  </AppText>
                  <AppText variant="caption" muted numeric>
                    {passed ? ['Passed', relative].filter(Boolean).join(' · ') : relative}
                  </AppText>
                </View>
              }
            />
          );
        })}
      </Card>
    </View>
  );
}

/** The big card for the next upcoming deadline, with a live countdown. */
function NextUp({ gw, now }: { gw: Gameweek; now: number }) {
  const { colors, spacing } = useTheme();
  const next = milestones(gw).find((m) => m.at > now);
  if (!next) return null;
  const remaining = next.at - now;
  const urgent = remaining < 3600_000;
  const tone: PillTone = urgent ? 'warning' : 'accent';
  // The visible clock ticks each second; the spoken label only changes each
  // minute so a screen reader is not interrupted constantly.
  const spoken = `${next.label} for ${gw.name} ${formatRelative(next.at, now)}`;
  return (
    <Card testID="deadline-next">
      <Pill label={`NEXT · ${gw.name.toUpperCase()}`} tone={tone} />
      <AppText variant="heading">{next.label}</AppText>
      <View accessible accessibilityRole="timer" accessibilityLabel={spoken} testID="countdown">
        <AppText
          variant="display"
          numeric
          color={urgent ? colors.accent : colors.text}
          style={{ marginVertical: spacing.xs }}
          aria-hidden
        >
          {formatCountdown(remaining)}
        </AppText>
      </View>
      <AppText muted>{formatDateTime(next.at, now)}</AppText>
    </Card>
  );
}

/**
 * Upcoming gameweeks with their lineup lock, waiver run and trade deadline in
 * the device's time zone; gameweeks whose lineup lock has passed are collapsed.
 * The contract only exposes the current and next gameweek, so that is what
 * shows. `now` is server time; when every known deadline has passed the
 * screen refetches, and this says "season over" only if the server has no
 * next gameweek.
 */
export function DeadlinesView({ status, now }: { status: GameweekStatus; now: number }) {
  const { colors, spacing, radii } = useTheme();
  const [showPast, setShowPast] = useState(false);
  const all = [status.current, ...(status.next ? [status.next] : [])];
  const upcoming = all.filter((g) => !g.finished && Date.parse(g.deadlineAt) > now);
  const past = all.filter((g) => !upcoming.includes(g));
  const zone = deviceTimeZone();
  const first = upcoming[0];
  // Only the server can say the season is over: no next gameweek and the
  // current one past its deadline. Otherwise a newer status is on its way.
  const seasonOver =
    !status.next && (status.current.finished || Date.parse(status.current.deadlineAt) <= now);

  return (
    <View style={{ gap: spacing.lg }}>
      {first ? <NextUp gw={first} now={now} /> : null}
      {upcoming.length === 0 && !seasonOver ? (
        <Card testID="deadlines-checking">
          <AppText muted>Checking for the next deadline…</AppText>
        </Card>
      ) : upcoming.length === 0 ? (
        <Card>
          <EmptyState
            icon={Calendar}
            title="No upcoming deadlines"
            body="There are no gameweeks left to play. Check back next season."
            testID="deadlines-empty"
          />
        </Card>
      ) : (
        upcoming.map((gw) => (
          <GameweekCard key={gw.number} gw={gw} now={now} id={`deadline-gw-${gw.number}`} />
        ))
      )}
      {past.length > 0 ? (
        <View style={{ gap: spacing.sm }}>
          <Pressable
            accessibilityRole="button"
            aria-expanded={showPast}
            accessibilityState={{ expanded: showPast }}
            accessibilityLabel={`Past gameweeks, ${past.length}`}
            onPress={() => setShowPast((v) => !v)}
            testID="past-toggle"
            style={({ focused }: { pressed: boolean; focused?: boolean }) => [
              {
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.xs,
                minHeight: 44,
                borderRadius: radii.sm,
              },
              focused ? focusRing(colors) : null,
            ]}
          >
            {showPast ? (
              <ChevronDown size={18} color={colors.textMuted} aria-hidden />
            ) : (
              <ChevronRight size={18} color={colors.textMuted} aria-hidden />
            )}
            <AppText variant="eyebrow" muted>
              {`PAST · ${past.length}`}
            </AppText>
          </Pressable>
          {showPast
            ? past.map((gw) => (
                <GameweekCard key={gw.number} gw={gw} now={now} id={`deadline-gw-${gw.number}`} />
              ))
            : null}
        </View>
      ) : null}
      <AppText variant="caption" muted testID="deadlines-zone">
        {zone ? `Times shown in your time zone (${zone}).` : 'Times shown in your local time.'}
      </AppText>
    </View>
  );
}
