import type { Schemas } from '@myfantasyprem/api-client';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { ClubCrest } from '../Badges';
import { ChevronRight } from '../icons';
import { pressFeedback, type PressState } from '../pressable';
import { PressableLink } from '../PressableLink';
import { useNow } from '@/api/freshness';
import {
  fixtureA11yLabel,
  fixturePhase,
  fixtureTimeLabel,
  scoreText,
  winnerOf,
} from '@/lib/scores';
import { tint, useTheme } from '@/theme';

/**
 * One fixture on the scores list, Sleeper's row: the two clubs (crests
 * only with branded images on) and their scores, the clock or kickoff on
 * the left. A live match is drawn in the accent, with its minute or HT.
 * The whole row opens the box score.
 */
export function FixtureRow({ fixture: f }: { fixture: Schemas['GameweekFixture'] }) {
  const { colors, spacing, radii } = useTheme();
  const now = useNow(60_000);
  const phase = fixturePhase(f.status);
  const live = phase === 'live';
  const started = live || phase === 'done';
  const winner = winnerOf(f);
  const side = (club: Schemas['ClubRef'], score: number | null, which: 'home' | 'away') => {
    const lost = winner !== null && winner !== which;
    return (
      <View style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.sm }}>
        <ClubCrest code={club.code} size={18} />
        <AppText
          weight={winner === which ? 'bold' : 'medium'}
          numberOfLines={1}
          style={{ flex: 1, minWidth: 0 }}
          color={lost ? colors.textMuted : colors.text}
        >
          {club.name}
        </AppText>
        <AppText
          numeric
          weight="bold"
          testID={`fixture-score-${which}`}
          style={{ minWidth: 20, textAlign: 'right' }}
          color={live ? colors.accent : lost ? colors.textMuted : colors.text}
        >
          {started ? scoreText(score) : ''}
        </AppText>
      </View>
    );
  };
  return (
    <PressableLink
      href={`/scores/${f.fixtureId}`}
      testID={`fixture-${f.fixtureId}`}
      accessibilityLabel={fixtureA11yLabel(f, now)}
      style={(state: PressState) => [
        {
          flexDirection: 'row',
          alignItems: 'center',
          gap: spacing.md,
          padding: spacing.lg,
          borderRadius: radii.lg,
          borderWidth: 1,
          borderColor: live ? colors.accent : colors.border,
          backgroundColor: live ? tint(colors.accent, colors.surface, 0.06) : colors.surface,
        },
        ...pressFeedback(colors, state),
      ]}
    >
      <View style={{ width: 64, alignItems: 'flex-start', gap: 2 }}>
        {live ? (
          <View
            testID="fixture-live"
            style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.xs }}
          >
            <View
              aria-hidden
              style={{ width: 6, height: 6, borderRadius: 3, backgroundColor: colors.accent }}
            />
            <AppText variant="meta" weight="bold" numeric accent testID="fixture-clock">
              {fixtureTimeLabel(f)}
            </AppText>
          </View>
        ) : (
          <AppText
            variant="meta"
            muted={phase !== 'postponed'}
            color={phase === 'postponed' ? colors.dangerText : undefined}
            weight={phase === 'done' ? 'semibold' : undefined}
            numeric
            numberOfLines={1}
            testID="fixture-status"
          >
            {fixtureTimeLabel(f)}
          </AppText>
        )}
      </View>
      <View style={{ flex: 1, minWidth: 0, gap: spacing.xs }}>
        {side(f.home, f.homeScore, 'home')}
        {side(f.away, f.awayScore, 'away')}
      </View>
      <ChevronRight size={16} color={colors.textSubtle} aria-hidden />
    </PressableLink>
  );
}
