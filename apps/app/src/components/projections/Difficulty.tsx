import type { Schemas } from '@myfantasyprem/api-client';
import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { View } from 'react-native';

import { AppText } from '../AppText';
import { useFixtureDifficulty } from '@/api/projections';
import {
  difficultyA11y,
  difficultyLookup,
  difficultyText,
  difficultyTier,
} from '@/lib/projections';
import { tint, useTheme, type ColorTokens } from '@/theme';

type Lookup = (position: Schemas['Position'], club: string) => number | null;

const none: Lookup = () => null;
export const DifficultyContext = createContext<Lookup>(none);

/**
 * Loads the fixture difficulty table once for a league's screens. Rows
 * below it show a rank chip on each fixture; without it (tests, screens
 * outside a league) they show none.
 */
export function DifficultyProvider({ children }: { children: ReactNode }) {
  const q = useFixtureDifficulty();
  const lookup = useMemo(() => difficultyLookup(q.data?.data), [q.data]);
  return <DifficultyContext.Provider value={lookup}>{children}</DifficultyContext.Provider>;
}

/** The opponent's rank for a position, or null. */
export function useDifficultyRank(): Lookup {
  return useContext(DifficultyContext);
}

/** Red (hardest) to green (easiest), Sleeper's chips, in the theme's win and loss colours. */
export function tierColors(colors: ColorTokens, rank: number) {
  switch (difficultyTier(rank)) {
    case 1:
      return { fg: colors.loss, bg: tint(colors.loss, colors.surface, 0.24) };
    case 2:
      return { fg: colors.dangerText, bg: tint(colors.loss, colors.surface, 0.1) };
    case 3:
      return { fg: colors.textMuted, bg: colors.surfaceRaised };
    case 4:
      return { fg: colors.win, bg: tint(colors.win, colors.surface, 0.1) };
    default:
      return { fg: colors.win, bg: tint(colors.win, colors.surface, 0.24) };
  }
}

/**
 * "(18th)" after a fixture, coloured by how hard the opponent is for the
 * player's position: ranked by points conceded to it over the last six
 * gameweeks, 1st the fewest. Nothing when the rank is unknown, or when
 * `rank` is not given and no table is loaded.
 */
export function FixtureRank({
  position,
  opponent,
  rank: given,
  testID = 'fixture-difficulty',
}: {
  position: Schemas['Position'];
  opponent: string;
  /** A rank the caller already has (projections); otherwise the loaded table's. */
  rank?: number | null;
  testID?: string;
}) {
  const { colors, radii } = useTheme();
  const lookup = useDifficultyRank();
  const rank = given !== undefined ? given : lookup(position, opponent);
  if (rank === null) return null;
  const { fg, bg } = tierColors(colors, rank);
  return (
    <View
      testID={testID}
      accessibilityLabel={difficultyA11y(rank)}
      style={{ paddingHorizontal: 4, borderRadius: radii.sm, backgroundColor: bg }}
    >
      <AppText
        variant="meta"
        numeric
        weight="semibold"
        color={fg}
        style={{ fontSize: 11, lineHeight: 15 }}
      >
        {difficultyText(rank)}
      </AppText>
    </View>
  );
}
