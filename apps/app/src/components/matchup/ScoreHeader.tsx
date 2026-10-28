import type { Schemas } from '@myfantasyprem/api-client';
import type { ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { Card } from '../Card';
import { Pill } from '../Chip';
import { pressFeedback, type PressState } from '../pressable';
import { PressableLink } from '../PressableLink';
import { Emblem, HeroBars, heroCentreWidth, ResultChip, Subline } from './MatchupHero';
import {
  leadOf,
  pairStarters,
  pointsShare,
  resultFor,
  scoreStateLabel,
  sideName,
  sidePoints,
  teamOf,
  yetToPlay,
  type TeamRecord,
} from '@/lib/matchup';
import { useTheme } from '@/theme';

type Side = Schemas['MatchupDetailSide'] | Schemas['MatchupSide'];

interface Props {
  leagueId: string;
  left: Side;
  right: Side;
  state: Schemas['ScoreState'];
  gameweek: number;
  /** Record, streak and rank by team id, from the standings. */
  records?: ReadonlyMap<string, TeamRecord>;
  /** The badge between the avatars (the gameweek picker); defaults to `GW n`. */
  emblem?: ReactNode;
  /** False hides the thin points-share bar (a future gameweek's preview). */
  shareBar?: boolean;
  /** Under the scores, inside the card (the carousel's season strip). */
  footer?: ReactNode;
  /** False leaves out the yet-to-play row under the card (the carousel draws it once). */
  toPlay?: boolean;
  testID?: string;
  /** A carousel page off to the side: its test ids get a `peek-` prefix so they stay unique. */
  peek?: boolean;
}

/**
 * The head-to-head hero: round avatars at the outer edges (a medal for the
 * top three), each side's share of the live points as a thick bar growing
 * toward a gameweek square in the centre, bold team names with manager,
 * record, streak and rank, and the score state straddling a divider; under
 * it, both scores split by a rule. Below the card, how many starters each
 * side still has to play with a dot per starter, filled once he has played.
 * W/D/L show once final. Team names open the team page.
 */
export function ScoreHeader({
  leagueId,
  left,
  right,
  state,
  gameweek,
  records,
  emblem,
  shareBar = true,
  footer,
  toPlay = true,
  testID = 'score-header',
  peek = false,
}: Props) {
  const tid = (id: string) => (peek ? `peek-${id}` : id);
  const { colors, spacing } = useTheme();
  const st = scoreStateLabel(state);
  const lp = sidePoints(left);
  const rp = sidePoints(right);
  // The points share is what has been scored, not a win chance: it shows
  // with projections off too (the owner, 2026-10-28).
  const share = shareBar ? pointsShare(lp, rp) : null;
  const summary = `Gameweek ${gameweek}, ${st.label}: ${sideName(left)} ${lp ?? 'no score'}, ${sideName(right)} ${rp ?? 'no score'}${
    share ? `. ${sideName(left)} has ${share.left}% of the points scored` : ''
  }`;
  return (
    <View style={{ gap: spacing.sm }}>
      <Card testID={testID} style={{ padding: 0, gap: 0 }}>
        <View accessibilityLabel={summary}>
          <View
            style={{
              paddingHorizontal: spacing.md,
              paddingTop: spacing.md,
              paddingBottom: spacing.sm,
              gap: spacing.sm,
            }}
          >
            <HeroBars
              left={left}
              right={right}
              leftPoints={lp}
              rightPoints={rp}
              size="lg"
              records={records}
              leftInner={
                <ResultChip result={resultFor(lp, rp, state)} testID={tid('score-left-result')} />
              }
              rightInner={
                <ResultChip result={resultFor(rp, lp, state)} testID={tid('score-right-result')} />
              }
              emblem={
                emblem ?? (
                  <Emblem size="lg" highlight={state === 'live'}>
                    <AppText variant="eyebrow" muted style={{ fontSize: 10, lineHeight: 12 }}>
                      GW
                    </AppText>
                    <AppText weight="bold" numeric style={{ fontSize: 18, lineHeight: 20 }}>
                      {String(gameweek)}
                    </AppText>
                  </Emblem>
                )
              }
            />
            <View style={{ flexDirection: 'row', alignItems: 'flex-start' }}>
              <Identity
                leagueId={leagueId}
                side={left}
                records={records}
                align="left"
                prefix={peek ? 'peek-' : ''}
              />
              <View style={{ width: heroCentreWidth('lg') }} />
              <Identity
                leagueId={leagueId}
                side={right}
                records={records}
                align="right"
                prefix={peek ? 'peek-' : ''}
              />
            </View>
            {shareBar ? <PointsSplit left={lp} right={rp} testID={tid('points-split')} /> : null}
          </View>
          {/* The state pill straddles the divider. */}
          <View style={{ flexDirection: 'row', alignItems: 'center' }}>
            <View style={{ flex: 1, height: 1, backgroundColor: colors.border }} />
            <View
              style={{
                borderRadius: 999,
                borderWidth: 1,
                borderColor: colors.border,
                backgroundColor: colors.surface,
                padding: 2,
              }}
            >
              <Pill label={st.label} tone={st.tone} dot={st.dot} testID={tid('score-state')} />
            </View>
            <View style={{ flex: 1, height: 1, backgroundColor: colors.border }} />
          </View>
          <View
            style={{
              flexDirection: 'row',
              alignItems: 'center',
              justifyContent: 'center',
              paddingHorizontal: spacing.md,
              paddingVertical: spacing.md,
              gap: spacing.md,
            }}
          >
            <Score points={lp} testID={tid('score-left-points')} align="right" />
            <View style={{ width: 1, alignSelf: 'stretch', backgroundColor: colors.border }} />
            <Score points={rp} testID={tid('score-right-points')} align="left" />
          </View>
        </View>
        {footer}
      </Card>
      {toPlay ? <ToPlayRow left={left} right={right} /> : null}
    </View>
  );
}

/**
 * A thin bar split by each side's share of the points scored so far, the
 * percentages at either end: the side ahead green, the side behind red,
 * both grey when level or before any points. A points share, never a win
 * chance.
 */
export function PointsSplit({
  left,
  right,
  testID = 'points-split',
}: {
  left: number | null;
  right: number | null;
  testID?: string;
}) {
  const { colors, spacing } = useTheme();
  const share = pointsShare(left, right);
  const tone = (mine: number | null, theirs: number | null) => {
    const lead = leadOf(mine, theirs);
    return lead === 'lead' ? colors.win : lead === 'trail' ? colors.loss : colors.textSubtle;
  };
  const lc = share ? tone(left, right) : colors.textSubtle;
  const rc = share ? tone(right, left) : colors.textSubtle;
  return (
    <View
      testID={testID}
      accessibilityLabel={
        share
          ? `Points share: ${share.left}% to ${share.right}%`
          : 'Points share: no points scored yet'
      }
      style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}
    >
      <AppText
        variant="meta"
        weight="semibold"
        numeric
        color={share ? lc : undefined}
        muted={!share}
        style={{ minWidth: 34, textAlign: 'left' }}
        testID={`${testID}-left`}
      >
        {share ? `${share.left}%` : '–'}
      </AppText>
      <View
        aria-hidden
        style={{
          flex: 1,
          height: 4,
          borderRadius: 2,
          overflow: 'hidden',
          flexDirection: 'row',
          gap: 2,
          backgroundColor: share ? 'transparent' : colors.surfaceSelected,
        }}
      >
        {share ? (
          <>
            <View
              testID={`${testID}-bar-left`}
              style={{ flex: share.left, backgroundColor: lc, borderRadius: 2 }}
            />
            <View
              testID={`${testID}-bar-right`}
              style={{ flex: share.right, backgroundColor: rc, borderRadius: 2 }}
            />
          </>
        ) : null}
      </View>
      <AppText
        variant="meta"
        weight="semibold"
        numeric
        color={share ? rc : undefined}
        muted={!share}
        style={{ minWidth: 34, textAlign: 'right' }}
        testID={`${testID}-right`}
      >
        {share ? `${share.right}%` : '–'}
      </AppText>
    </View>
  );
}

/** Both sides' yet-to-play counts and starter dots, side by side. */
export function ToPlayRow({ left, right }: { left: Side; right: Side }) {
  const { spacing } = useTheme();
  return (
    <View style={{ flexDirection: 'row', alignItems: 'flex-start', gap: spacing.md }}>
      <ToPlay side={left} align="left" />
      <ToPlay side={right} align="right" />
    </View>
  );
}

function Score({
  points,
  testID,
  align,
}: {
  points: number | null;
  testID: string;
  align: 'left' | 'right';
}) {
  return (
    <AppText
      variant="display"
      numeric
      testID={testID}
      style={{ fontSize: 34, lineHeight: 40, minWidth: 44, textAlign: align }}
    >
      {points === null ? '–' : String(points)}
    </AppText>
  );
}

/**
 * `yet to play (4)`, the positions still to play, and a dot per starter
 * (GK→FWD) filled once he has no game left to play; omitted without a
 * lineup.
 */
function ToPlay({ side, align }: { side: Side; align: 'left' | 'right' }) {
  const { colors } = useTheme();
  const lineup = 'starters' in side ? side.starters : null;
  const left = lineup ? yetToPlay(lineup) : null;
  const items = align === 'left' ? 'flex-start' : 'flex-end';
  const dots = lineup
    ? pairStarters(lineup, null)
        .map((r) => r.left)
        .filter((s) => s !== null)
        .map((s) => ({
          id: s.player.id,
          played: !s.fixtures.some((f) => f.status === 'scheduled'),
        }))
    : [];
  return (
    <View style={{ flex: 1, minWidth: 0, alignItems: items, gap: 2 }}>
      {left ? (
        <>
          <AppText
            variant="meta"
            muted
            numberOfLines={2}
            style={{ textAlign: align }}
            testID={`yet-to-play-${align}`}
            accessibilityLabel={`${left.count} ${left.count === 1 ? 'starter' : 'starters'} yet to play`}
          >
            {`yet to play (${left.count})`}
          </AppText>
          {left.positions ? (
            <AppText variant="meta" subtle numberOfLines={2} style={{ textAlign: align }}>
              {left.positions}
            </AppText>
          ) : null}
          <View
            testID={`played-dots-${align}`}
            accessibilityLabel={`${dots.length - left.count} of ${dots.length} starters played`}
            style={{
              flexDirection: align === 'left' ? 'row' : 'row-reverse',
              flexWrap: 'wrap',
              gap: 4,
              marginTop: 4,
            }}
          >
            {dots.map((d) => (
              <View
                key={d.id}
                aria-hidden
                testID={d.played ? 'dot-played' : 'dot-to-play'}
                style={{
                  width: 7,
                  height: 7,
                  borderRadius: 3.5,
                  borderWidth: 1,
                  borderColor: d.played ? colors.accent : colors.textSubtle,
                  backgroundColor: d.played ? colors.accent : 'transparent',
                }}
              />
            ))}
          </View>
        </>
      ) : null}
    </View>
  );
}

function Identity({
  leagueId,
  side,
  records,
  align,
  prefix = '',
}: {
  leagueId: string;
  side: Side;
  records?: ReadonlyMap<string, TeamRecord>;
  align: 'left' | 'right';
  prefix?: string;
}) {
  const { colors, spacing, radii } = useTheme();
  const team = teamOf(side);
  const testID = `${prefix}${align === 'left' ? 'score-left' : 'score-right'}`;
  // Both sides read left to right, so a long name wraps (never cut short) on
  // either side; the column fills its half so the text has its full width.
  const items = 'stretch';
  const identity = (
    <>
      <AppText
        weight="bold"
        style={{ fontSize: 16, lineHeight: 20, textAlign: 'left', width: '100%' }}
        testID={`${testID}-name`}
      >
        {sideName(side)}
      </AppText>
      <Subline side={side} records={records} align="left" testID={`subline-${align}`} />
    </>
  );
  return (
    <View style={{ flex: 1, minWidth: 0, alignItems: items }} testID={testID}>
      {team ? (
        <PressableLink
          href={`/leagues/${leagueId}/team/${team.id}`}
          accessibilityLabel={`${team.name}, managed by ${team.managerName}. Open team`}
          style={(state: PressState) => [
            {
              alignItems: items,
              gap: 2,
              borderRadius: radii.sm,
              paddingVertical: spacing.xs,
              width: '100%',
            },
            ...pressFeedback(colors, state),
          ]}
        >
          {identity}
        </PressableLink>
      ) : (
        <View style={{ alignItems: items, gap: 2, paddingVertical: spacing.xs }}>{identity}</View>
      )}
    </View>
  );
}
