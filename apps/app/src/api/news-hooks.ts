import { send } from '@myfantasyprem/api-client';
import { useQuery } from '@tanstack/react-query';

import { api } from './client';
import { useScreenFocus } from './screen-focus';

/** A team's player news: under the league, so a league refresh covers it. */
export const newsKey = (leagueId: string, teamId: string) =>
  ['league', leagueId, 'news', teamId] as const;

/** The team's players with news, newest first (the Team tab's News). */
export function useTeamNews(
  leagueId: string,
  teamId: string,
  { enabled = true }: { enabled?: boolean } = {},
) {
  const { subscribed } = useScreenFocus(newsKey(leagueId, teamId));
  return useQuery({
    queryKey: newsKey(leagueId, teamId),
    queryFn: () =>
      send(
        api.GET('/v1/leagues/{leagueId}/news', {
          params: { path: { leagueId }, query: { teamId, limit: 50 } },
        }),
      ),
    enabled: enabled && Boolean(teamId),
    subscribed,
  });
}
