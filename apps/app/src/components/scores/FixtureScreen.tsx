import type { Schemas } from '@myfantasyprem/api-client';
import { useState } from 'react';
import { Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest, PositionBadge } from '../Badges';
import { Card } from '../Card';
import { Pill } from '../Chip';
import { EmptyState } from '../EmptyState';
import { CalendarX, Clock } from '../icons';
import { PlayerSheet } from '../players/PlayerCard';
import { playerCardLabel } from '../players/PlayerCardContext';
import { pressFeedback, type PressState } from '../pressable';
import { QueryStatus } from '../QueryStatus';
import { Screen } from '../Screen';
import { SectionHeader } from '../SectionHeader';
import { Skeleton, SkeletonRows } from '../Skeleton';
import { useMyLeagues } from '@/api/account-hooks';
import { useFreshness, useNow } from '@/api/freshness';
import { useFixture } from '@/api/hooks';
import { useLayout } from '@/hooks/useLayout';
import {
  cardLeagueFor,
  matchEvents,
  mineCount,
  minutesText,
  ownerBadges,
  type MatchEvent,
} from '@/lib/fixture-detail';
import {
  fixtureA11yLabel,
  fixturePhase,
  fixtureStatusLabel,
  scoreText,
  winnerOf,
} from '@/lib/scores';
import { formatDateTime } from '@/lib/time';
import { tint, useTheme } from '@/theme';

type Player = Schemas['FixturePlayer'];

/**
 * A fixture's box score (SCORES → a fixture): the scoreboard, then each
 * club's players with what they did and their FPL points. Players on a
 * team in one of my leagues carry that team's initials; my own are
 * emphasised. A player opens his card in the league that has him (mine
 * first), else in my first league. Polls while the gameweek is live and
 * the screen has focus (useFixture).
 */
export function FixtureScreen({ fixtureId }: { fixtureId: string }) {
  const { spacing } = useTheme();
  const { isWide } = useLayout();
  const fixture = useFixture(fixtureId);
  const freshness = useFreshness(fixture);
  const leagues = useMyLeagues();
  const [card, setCard] = useState<{ playerId: string; leagueId: string } | null>(null);
  const firstLeague =
    leagues.data?.data.find(
      (r) => r.league.status === 'in_season' || r.league.status === 'complete',
    )?.league.id ?? null;
  const f = fixture.data?.data.fixture;
  const open = (p: Player) => {
    const leagueId = cardLeagueFor(p.owners, firstLeague);
    return leagueId ? () => setCard({ playerId: p.player.id, leagueId }) : undefined;
  };

  return (
    <Screen
      title={f ? `${f.home.name} v ${f.away.name}` : 'Fixture'}
      subtitle={f ? `Gameweek ${f.gameweek}` : undefined}
      back={{ label: 'Scores', href: f ? `/scores?gw=${f.gameweek}` : '/scores' }}
      freshness={freshness}
      maxWidth={isWide ? 960 : 640}
    >
      <QueryStatus
        query={fixture}
        loading={
          <View style={{ gap: spacing.md }} testID="fixture-loading">
            <Skeleton height={120} radius={16} />
            <Card flush>
              <SkeletonRows count={8} />
            </Card>
          </View>
        }
      >
        {({ data }) => (
          <View style={{ gap: spacing.lg }} testID="fixture-detail">
            <FixtureHero fixture={data.fixture} mine={mineCount(data)} />
            <BoxScore detail={data} open={open} wide={isWide} />
          </View>
        )}
      </QueryStatus>
      <PlayerSheet
        leagueId={card?.leagueId ?? ''}
        playerId={card?.playerId ?? null}
        onClose={() => setCard(null)}
      />
    </Screen>
  );
}

/** The scoreboard: both clubs, the score (or kickoff), and the clock. */
function FixtureHero({ fixture: f, mine }: { fixture: Schemas['GameweekFixture']; mine: number }) {
  const { colors, spacing } = useTheme();
  const now = useNow(60_000);
  const phase = fixturePhase(f.status);
  const live = phase === 'live';
  const started = live || phase === 'done';
  const winner = winnerOf(f);
  const club = (c: Schemas['ClubRef'], which: 'home' | 'away') => (
    <View style={{ flex: 1, minWidth: 0, alignItems: 'center', gap: spacing.xs }}>
      <ClubCrest code={c.code} size={40} />
      <AppText
        weight={winner === which ? 'bold' : 'semibold'}
        numberOfLines={2}
        style={{ textAlign: 'center' }}
        color={winner !== null && winner !== which ? colors.textMuted : colors.text}
      >
        {c.name}
      </AppText>
    </View>
  );
  const status =
    phase === 'upcoming'
      ? f.kickoffAt
        ? formatDateTime(Date.parse(f.kickoffAt), now)
        : 'Kickoff to be confirmed'
      : phase === 'postponed'
        ? 'Postponed'
        : phase === 'done'
          ? 'Full time'
          : fixtureStatusLabel(f, now);
  return (
    <Card
      testID="fixture-hero"
      accessibilityLabel={fixtureA11yLabel(f, now)}
      style={{
        gap: spacing.md,
        borderColor: live ? colors.accent : colors.border,
        backgroundColor: live ? tint(colors.accent, colors.surface, 0.06) : colors.surface,
      }}
    >
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}>
        {club(f.home, 'home')}
        <View style={{ alignItems: 'center', gap: spacing.xs, minWidth: 96 }}>
          {started ? (
            <AppText
              variant="display"
              numeric
              testID="fixture-hero-score"
              color={live ? colors.accent : colors.text}
            >
              {`${scoreText(f.homeScore)}–${scoreText(f.awayScore)}`}
            </AppText>
          ) : null}
          {live ? (
            <Pill label={status} tone="live" dot testID="fixture-hero-status" />
          ) : (
            <AppText
              variant="meta"
              muted={phase !== 'postponed'}
              color={phase === 'postponed' ? colors.dangerText : undefined}
              weight="semibold"
              style={{ textAlign: 'center' }}
              testID="fixture-hero-status"
            >
              {status}
            </AppText>
          )}
        </View>
        {club(f.away, 'away')}
      </View>
      {mine > 0 ? (
        <View style={{ flexDirection: 'row', justifyContent: 'center' }}>
          <Pill
            label={mine === 1 ? '1 of your players' : `${mine} of your players`}
            tone="accent"
            testID="fixture-mine-count"
          />
        </View>
      ) : null}
    </Card>
  );
}

/** Both clubs' players: side by side wide, one after the other on phones. */
function BoxScore({
  detail,
  open,
  wide,
}: {
  detail: Schemas['FixtureDetail'];
  open: (p: Player) => (() => void) | undefined;
  wide: boolean;
}) {
  const { spacing } = useTheme();
  const phase = fixturePhase(detail.fixture.status);
  const empty = detail.home.players.length === 0 && detail.away.players.length === 0;
  // Postponed: the notice, then any of my leagues' players it would have had.
  const notice =
    phase === 'postponed' ? (
      <Card>
        <EmptyState
          icon={CalendarX}
          title="Match postponed"
          body="It counts in the gameweek it is replayed: points show up then."
          testID="fixture-postponed"
        />
      </Card>
    ) : empty ? (
      <Card>
        <EmptyState
          icon={Clock}
          title={phase === 'upcoming' ? 'Not kicked off' : 'No players yet'}
          body="Players and their points show up once the match is under way. None of these clubs' players are on your teams."
          testID="fixture-empty"
        />
      </Card>
    ) : null;
  if (empty) return notice;
  return (
    <View style={{ gap: spacing.lg }}>
      {notice}
      <View style={{ flexDirection: wide ? 'row' : 'column', gap: spacing.lg }}>
        {[detail.home, detail.away].map((side, i) => (
          <View key={side.club.code} style={{ flex: wide ? 1 : undefined, gap: spacing.sm }}>
            <SectionHeader
              title={side.club.name}
              detail={i === 0 ? 'Home' : 'Away'}
              testID={`fixture-side-${i === 0 ? 'home' : 'away'}`}
            />
            {side.players.length === 0 ? (
              <Card>
                <AppText muted variant="meta">
                  No players yet.
                </AppText>
              </Card>
            ) : (
              <Card flush>
                {side.players.map((p, j) => (
                  <FixturePlayerRow
                    key={p.player.id}
                    player={p}
                    first={j === 0}
                    onPress={open(p)}
                  />
                ))}
              </Card>
            )}
          </View>
        ))}
      </View>
    </View>
  );
}

/**
 * One player: position, name, what he did, minutes, the owners' initials
 * and his FPL points. On one of my teams: an accent edge and tint.
 */
function FixturePlayerRow({
  player: p,
  first,
  onPress,
}: {
  player: Player;
  first: boolean;
  onPress?: () => void;
}) {
  const { colors, spacing } = useTheme();
  const badges = ownerBadges(p.owners);
  const mine = badges.some((b) => b.mine);
  const events = matchEvents(p.stats);
  const ownedBy = badges.map((b) => b.a11y).join(', ');
  const label = [
    playerCardLabel(p.player.displayName),
    `${p.points} points`,
    p.minutes > 0 ? `${p.minutes} minutes` : 'not played',
    ...events.map((e) => e.a11y),
    ownedBy ? `owned by ${ownedBy}` : '',
  ]
    .filter(Boolean)
    .join(', ');
  return (
    <Pressable
      testID={`fx-player-${p.player.id}`}
      accessibilityRole={onPress ? 'button' : undefined}
      accessibilityLabel={label}
      disabled={!onPress}
      onPress={onPress}
      style={(state: PressState) => [
        {
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.sm,
          paddingVertical: spacing.sm,
          paddingRight: spacing.lg,
          paddingLeft: spacing.lg - 3,
          borderLeftWidth: 3,
          borderLeftColor: mine ? colors.accent : 'transparent',
          borderTopWidth: first ? 0 : 1,
          borderTopColor: colors.border,
          backgroundColor: mine ? tint(colors.accent, colors.surface, 0.08) : undefined,
        },
        ...(onPress ? pressFeedback(colors, state) : []),
      ]}
    >
      <PositionBadge position={p.player.position} size="sm" />
      <View style={{ flex: 1, minWidth: 0, gap: 2 }}>
        <AppText weight={mine ? 'bold' : 'medium'} numberOfLines={1}>
          {p.player.displayName}
        </AppText>
        <View
          style={{ flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: spacing.xs }}
        >
          <AppText variant="caption" muted numeric>
            {minutesText(p.minutes)}
          </AppText>
          {events.map((e) => (
            <EventTag key={e.key} event={e} />
          ))}
        </View>
      </View>
      {badges.length > 0 ? (
        <View
          testID="owner-badges"
          style={{
            flexDirection: 'row',
            flexWrap: 'wrap',
            justifyContent: 'flex-end',
            gap: 4,
            maxWidth: 112,
          }}
        >
          {badges.map((b) => (
            <OwnerBadgeView key={b.key} initials={b.initials} mine={b.mine} />
          ))}
        </View>
      ) : null}
      <AppText
        numeric
        weight="bold"
        testID="fx-player-points"
        style={{ minWidth: 28, textAlign: 'right' }}
        color={mine ? colors.accent : colors.text}
      >
        {String(p.points)}
      </AppText>
    </Pressable>
  );
}

function EventTag({ event: e }: { event: MatchEvent }) {
  const { colors, radii } = useTheme();
  const fg = e.tone === 'good' ? colors.win : e.tone === 'bad' ? colors.loss : colors.textMuted;
  return (
    <View
      testID={`event-${e.key}`}
      style={{
        paddingHorizontal: 4,
        borderRadius: radii.sm,
        backgroundColor: tint(fg, colors.surface),
      }}
    >
      <AppText variant="caption" weight="semibold" numeric color={fg}>
        {e.label}
      </AppText>
    </View>
  );
}

/** A team's initials in a square badge: solid accent for mine, outlined otherwise. */
function OwnerBadgeView({ initials, mine }: { initials: string; mine: boolean }) {
  const { colors, radii } = useTheme();
  return (
    <View
      testID={mine ? 'owner-badge-mine' : 'owner-badge'}
      style={{
        minWidth: 24,
        height: 20,
        paddingHorizontal: 3,
        alignItems: 'center',
        justifyContent: 'center',
        borderRadius: radii.sm,
        borderWidth: 1,
        borderColor: mine ? colors.accent : colors.border,
        backgroundColor: mine ? colors.accent : colors.surfaceRaised,
      }}
    >
      <AppText variant="caption" weight="bold" color={mine ? colors.onAccent : colors.textMuted}>
        {initials}
      </AppText>
    </View>
  );
}
