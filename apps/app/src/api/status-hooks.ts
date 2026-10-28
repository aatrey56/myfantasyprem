import { send } from '@myfantasyprem/api-client';
import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { api } from './client';
import { refetchIntervalFor } from './query-client';
import { useScreenFocus } from './screen-focus';

export const statusKeys = {
  status: (leagueId: string) => ['league', leagueId, 'status'] as const,
  awards: (gw: number | null) => ['awards', 'gameweek', gw] as const,
  playersOfTheWeek: ['awards', 'players-of-the-week'] as const,
};

/** The status page query, shared with the league pager's prefetch. */
export const leagueStatusQuery = (leagueId: string) => ({
  queryKey: statusKeys.status(leagueId),
  queryFn: () => send(api.GET('/v1/leagues/{leagueId}/status', { params: { path: { leagueId } } })),
});

/** The Status tab's own data: attention, rank change, top available, injuries. */
export function useLeagueStatus(leagueId: string, enabled = true) {
  const { subscribed } = useScreenFocus(leagueStatusQuery(leagueId).queryKey);
  return useQuery({
    ...leagueStatusQuery(leagueId),
    enabled,
    subscribed,
  });
}

/** Player and Team of the Week for one gameweek; idle until `gw` is known. */
export function useGameweekAwards(gw: number | null) {
  const { subscribed, focused } = useScreenFocus(statusKeys.awards(gw));
  return useQuery({
    queryKey: statusKeys.awards(gw),
    queryFn: () =>
      send(api.GET('/v1/gameweeks/{gw}/awards', { params: { path: { gw: gw as number } } })),
    enabled: gw !== null,
    // A new gameweek or filter keeps the last answer on screen until it loads: no flash.
    placeholderData: keepPreviousData,
    // A live gameweek's awards move with the scores.
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

/** Player of the Week for every started gameweek, oldest first. */
export function usePlayersOfTheWeek(enabled = true) {
  const { subscribed } = useScreenFocus(statusKeys.playersOfTheWeek);
  return useQuery({
    queryKey: statusKeys.playersOfTheWeek,
    queryFn: () => send(api.GET('/v1/awards/players-of-the-week')),
    enabled,
    staleTime: 5 * 60_000,
    subscribed,
  });
}
