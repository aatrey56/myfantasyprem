import { useLocalSearchParams } from 'expo-router';
import { createContext, useContext } from 'react';

/** The league being shown, set by the league layout for everything under it. */
export const LeagueIdContext = createContext<string | null>(null);

/**
 * The `[leagueId]` of the league screen this is in. A pager page rendered
 * ahead of a swipe has not been navigated to yet, so its route has no
 * params: the league layout's id fills in.
 */
export function useLeagueId(): string {
  const { leagueId } = useLocalSearchParams<{ leagueId?: string }>();
  const fromLayout = useContext(LeagueIdContext);
  return leagueId ?? fromLayout ?? '';
}
