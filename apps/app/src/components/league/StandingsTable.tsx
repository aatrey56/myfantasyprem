import type { Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { Avatar } from '../Badges';
import { ArrowDown, ArrowUp, ChevronRight } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { PressableLink } from '../PressableLink';
import { useLayout } from '@/hooks/useLayout';
import { recordOf, sortStandings, streakOf, streakText, type StandingsKey } from '@/lib/standings';
import { tint, useTheme } from '@/theme';

type Row = Schemas['StandingRow'];

interface Props {
  leagueId: string;
  rows: readonly Row[];
  myTeamId?: string;
}

const COLUMNS: { key: StandingsKey; label: string; long: string; width: number }[] = [
  { key: 'record', label: 'W-D-L', long: 'Record', width: 72 },
  { key: 'pointsFor', label: 'PF', long: 'Points for', width: 56 },
  { key: 'pointsAgainst', label: 'PA', long: 'Points against', width: 56 },
  { key: 'streak', label: 'Strk', long: 'Streak', width: 52 },
];

/**
 * The league table. Desktop: a sortable table (rank, team, W-D-L, points
 * for, points against, streak, last five). Phone: compact rows (rank,
 * team and manager, W-D-L and points for, the last five as squares). Rows
 * open the team.
 */
export function StandingsTable({ leagueId, rows, myTeamId }: Props) {
  const { isWide } = useLayout();
  const [sort, setSort] = useState<{ key: StandingsKey; dir: 'desc' | 'asc' }>({
    key: 'rank',
    dir: 'desc',
  });
  const sorted = sortStandings(rows, sort.key, sort.dir);
  return isWide ? (
    <Table
      leagueId={leagueId}
      rows={sorted}
      myTeamId={myTeamId}
      sort={sort}
      onSort={(key) =>
        setSort((s) =>
          s.key === key ? { key, dir: s.dir === 'desc' ? 'asc' : 'desc' } : { key, dir: 'desc' },
        )
      }
    />
  ) : (
    <Compact leagueId={leagueId} rows={sorted} myTeamId={myTeamId} />
  );
}

function FormSquares({ form }: { form: readonly string[] }) {
  const { colors, radii } = useTheme();
  return (
    <View
      style={{ flexDirection: 'row', gap: 3 }}
      accessibilityLabel={`Last ${form.length}: ${form.join(' ')}`}
    >
      {form.map((r, i) => {
        const c = r === 'W' ? colors.win : r === 'L' ? colors.loss : colors.textMuted;
        return (
          <View
            key={i}
            aria-hidden
            style={{
              width: 16,
              height: 16,
              borderRadius: radii.sm - 2,
              alignItems: 'center',
              justifyContent: 'center',
              backgroundColor: tint(c, colors.surface),
            }}
          >
            <AppText variant="eyebrow" color={c} style={{ fontSize: 9, lineHeight: 12 }}>
              {r}
            </AppText>
          </View>
        );
      })}
    </View>
  );
}

function rowLabel(r: Row) {
  const s = streakOf(r.form);
  return `${r.rank}. ${r.team.name}, ${r.team.managerName}. ${r.wins} won, ${r.draws} drawn, ${r.losses} lost, ${r.leaguePoints} league points, ${r.pointsFor} points for, ${r.pointsAgainst} against${
    s ? `, streak ${s.result}${s.count}` : ''
  }`;
}

function Compact({ leagueId, rows, myTeamId }: Omit<Props, 'rows'> & { rows: Row[] }) {
  const { colors, spacing, radii } = useTheme();
  return (
    <View
      testID="standings"
      style={{
        borderRadius: radii.lg,
        borderWidth: 1,
        borderColor: colors.border,
        backgroundColor: colors.surface,
        overflow: 'hidden',
      }}
    >
      {rows.map((r, i) => {
        const mine = r.team.id === myTeamId;
        return (
          <PressableLink
            key={r.team.id}
            href={`/leagues/${leagueId}/team/${r.team.id}`}
            testID={`standing-${r.team.id}`}
            accessibilityLabel={rowLabel(r)}
            style={(s: PressState) => [
              {
                minHeight: 60,
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.sm,
                paddingHorizontal: spacing.md,
                paddingVertical: spacing.sm,
                borderTopWidth: i > 0 ? 1 : 0,
                borderTopColor: colors.border,
                backgroundColor: mine ? tint(colors.accent, colors.surface, 0.08) : 'transparent',
              },
              ...pressFeedback(colors, s),
            ]}
          >
            <AppText numeric weight="semibold" muted style={{ width: 20, textAlign: 'right' }}>
              {String(r.rank)}
            </AppText>
            <Avatar name={r.team.name} size={30} square />
            <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
              <AppText weight="semibold" numberOfLines={1}>
                {r.team.name}
              </AppText>
              <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
                <AppText variant="meta" muted numberOfLines={1} style={{ flexShrink: 1 }}>
                  {r.team.managerName}
                </AppText>
                <FormSquares form={r.form} />
              </View>
            </View>
            <View style={{ alignItems: 'flex-end', gap: 2 }}>
              <AppText numeric weight="semibold">
                {recordOf(r)}
              </AppText>
              <AppText variant="meta" muted numeric>
                {`${r.pointsFor} PF`}
              </AppText>
            </View>
          </PressableLink>
        );
      })}
    </View>
  );
}

function Table({
  leagueId,
  rows,
  myTeamId,
  sort,
  onSort,
}: Omit<Props, 'rows'> & {
  rows: Row[];
  sort: { key: StandingsKey; dir: 'desc' | 'asc' };
  onSort: (key: StandingsKey) => void;
}) {
  const { colors, spacing, radii } = useTheme();
  const header = (key: StandingsKey, label: string, long: string, width?: number) => {
    const active = sort.key === key;
    const Icon = sort.dir === 'desc' ? ArrowDown : ArrowUp;
    return (
      <Pressable
        key={key}
        accessibilityRole="button"
        accessibilityLabel={`Sort by ${long}`}
        accessibilityState={{ selected: active }}
        onPress={() => onSort(key)}
        testID={`sort-${key}`}
        style={(s: PressState) => [
          {
            width,
            flex: width ? undefined : 1,
            minHeight: 36,
            flexDirection: 'row',
            alignItems: 'center',
            justifyContent: width ? 'flex-end' : 'flex-start',
            gap: 2,
            borderRadius: radii.sm,
            paddingHorizontal: 4,
          },
          ...pressFeedback(colors, s),
        ]}
      >
        <AppText variant="eyebrow" color={active ? colors.accent : colors.textMuted}>
          {label}
        </AppText>
        {active ? <Icon size={12} color={colors.accent} aria-hidden /> : null}
      </Pressable>
    );
  };
  return (
    <View
      testID="standings"
      style={{
        borderRadius: radii.lg,
        borderWidth: 1,
        borderColor: colors.border,
        backgroundColor: colors.surface,
        overflow: 'hidden',
      }}
    >
      <View
        style={{
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.sm,
          paddingHorizontal: spacing.md,
          paddingVertical: spacing.xs,
          borderBottomWidth: 1,
          borderBottomColor: colors.border,
        }}
      >
        {header('rank', '#', 'Rank', 32)}
        {header('team', 'Team', 'Team')}
        {COLUMNS.map((c) => header(c.key, c.label, c.long, c.width))}
        <View style={{ width: 100, alignItems: 'flex-end', paddingHorizontal: 4 }}>
          <AppText variant="eyebrow" muted>
            Last 5
          </AppText>
        </View>
        <View style={{ width: 16 }} />
      </View>
      {rows.map((r, i) => {
        const mine = r.team.id === myTeamId;
        const cell = (text: string, width: number, strong?: boolean) => (
          <AppText
            numeric
            weight={strong ? 'semibold' : undefined}
            muted={!strong}
            style={{ width, textAlign: 'right', paddingHorizontal: 4 }}
          >
            {text}
          </AppText>
        );
        return (
          <PressableLink
            key={r.team.id}
            href={`/leagues/${leagueId}/team/${r.team.id}`}
            testID={`standing-${r.team.id}`}
            accessibilityLabel={rowLabel(r)}
            style={(s: PressState) => [
              {
                minHeight: 52,
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.sm,
                paddingHorizontal: spacing.md,
                borderTopWidth: i > 0 ? 1 : 0,
                borderTopColor: colors.border,
                backgroundColor: mine ? tint(colors.accent, colors.surface, 0.08) : 'transparent',
              },
              ...pressFeedback(colors, s),
            ]}
          >
            <AppText
              numeric
              weight="semibold"
              muted
              style={{ width: 32, textAlign: 'right', paddingHorizontal: 4 }}
            >
              {String(r.rank)}
            </AppText>
            <View
              style={{
                flex: 1,
                minWidth: 0,
                flexDirection: 'row',
                alignItems: 'center',
                gap: spacing.sm,
              }}
            >
              <Avatar name={r.team.name} size={28} square />
              <View style={{ flex: 1, minWidth: 0 }}>
                <AppText weight="semibold" numberOfLines={1}>
                  {r.team.name}
                </AppText>
                <AppText variant="meta" muted numberOfLines={1}>
                  {r.team.managerName}
                </AppText>
              </View>
            </View>
            {cell(recordOf(r), 72, true)}
            {cell(String(r.pointsFor), 56, true)}
            {cell(String(r.pointsAgainst), 56)}
            {cell(streakText(r.form), 52)}
            <View style={{ width: 100, alignItems: 'flex-end' }}>
              <FormSquares form={r.form} />
            </View>
            <ChevronRight size={16} color={colors.textMuted} aria-hidden />
          </PressableLink>
        );
      })}
    </View>
  );
}
