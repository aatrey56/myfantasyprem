import { send } from '@myfantasyprem/api-client';
import { keepPreviousData, useQuery } from '@tanstack/react-query';

import { api } from './client';
import { refetchIntervalFor } from './query-client';
import { useScreenFocus } from './screen-focus';
import { showProjections } from '@/lib/projections';

/**
 * Fixture difficulty, Expected points, win odds and start/sit accuracy
 * (ADR 0014). League keys sit under `['league', id]`, so anything that
 * refreshes a league refreshes these too.
 */
export const projectionKeys = {
  difficulty: ['fixture-difficulty'] as const,
  league: (leagueId: string, gw: number | undefined) =>
    ['league', leagueId, 'projections', gw] as const,
  startSit: (leagueId: string) => ['league', leagueId, 'start-sit'] as const,
};

/** Clubs ranked per position by points conceded. Moves once a gameweek. */
export function useFixtureDifficulty(enabled = true) {
  const { subscribed } = useScreenFocus(projectionKeys.difficulty);
  return useQuery({
    queryKey: projectionKeys.difficulty,
    queryFn: () => send(api.GET('/v1/fixture-difficulty')),
    enabled,
    staleTime: 10 * 60_000,
    subscribed,
  });
}

/**
 * A gameweek's Expected points and win odds. `gw` undefined, or the
 * projections switch off (lib/projections showProjections), fetches
 * nothing, so every Expected and win chance stays hidden. Polls while the gameweek is live (the odds use points so far).
 */
export function useLeagueProjections(leagueId: string, gw: number | undefined) {
  const key = projectionKeys.league(leagueId, gw);
  const { subscribed, focused } = useScreenFocus(key);
  return useQuery({
    queryKey: key,
    queryFn: () =>
      send(
        api.GET('/v1/leagues/{leagueId}/gameweeks/{gw}/projections', {
          params: { path: { leagueId, gw: gw as number } },
        }),
      ),
    enabled: showProjections() && gw !== undefined && gw >= 1 && gw <= 38,
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

/** Every team's start/sit accuracy over the completed gameweeks. */
export function useStartSit(leagueId: string, enabled = true) {
  const key = projectionKeys.startSit(leagueId);
  const { subscribed } = useScreenFocus(key);
  return useQuery({
    queryKey: key,
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/start-sit', { params: { path: { leagueId } } })),
    enabled,
    staleTime: 5 * 60_000,
    subscribed,
  });
}
