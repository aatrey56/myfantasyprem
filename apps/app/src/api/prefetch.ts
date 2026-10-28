import type { Schemas } from '@myfantasyprem/api-client';
import {
  QueryClientContext,
  useQuery,
  useQueryClient,
  type QueryClient,
} from '@tanstack/react-query';
import { useContext, useEffect, useMemo } from 'react';

import { BUILD_BRANDED_ASSETS, clubCrestUrl, playerPhotoUrl } from './assets';
import {
  leagueHomeQuery,
  leaguePlayerQuery,
  matchupQuery,
  matchupsQuery,
  PLAYERS_FIRST_FILTER,
  playersQuery,
  queryKeys,
  teamQuery,
} from './hooks';
import { prefetchImages } from './image-prefetch';
import { lineupQuery } from './lineup-hooks';
import { leagueStatusQuery } from './status-hooks';
import { LeagueIdContext } from '@/hooks/useLeagueId';
import { LEAGUE_TABS, leagueTabNeighbours, type LeagueTab } from '@/lib/league-tabs';
import { findMyMatchup, leagueGameweek, matchupUpcoming } from '@/lib/matchup';

type Home = Schemas['LeagueHome'];

/**
 * A gameweek's matchups and the one the Match tab opens on (mine, else the
 * first), as MatchupScreen asks for them. Nothing for a gameweek outside
 * the league or still ahead (the tab shows the schedule then).
 */
export async function prefetchGameweekMatchups(
  queryClient: QueryClient,
  leagueId: string,
  gw: number,
  home: Home,
): Promise<void> {
  if (gw < home.settings.startGameweek || gw > home.settings.endGameweek) return;
  if (matchupUpcoming(home, gw)) return;
  await queryClient.prefetchQuery(matchupsQuery(leagueId, gw));
  const list = queryClient.getQueryData<{ data: Schemas['MatchupSummary'][] }>(
    queryKeys.matchups(leagueId, gw),
  )?.data;
  const mine = list && (findMyMatchup(list, home.viewer.team?.id) ?? list[0]);
  if (mine) await queryClient.prefetchQuery(matchupQuery(leagueId, gw, mine.id));
}

/** Rows a screen shows before any scrolling: their images are worth warming. */
export const IMAGE_PREFETCH_ROWS = 20;

/**
 * Warms the photos and crests of the first rows of a player list, only
 * when official images are switched on (build and server, as
 * BrandedAssetsProvider decides). A no-op on web (see image-prefetch.web.ts).
 */
export function prefetchPlayerImages(
  queryClient: QueryClient,
  players: readonly Pick<Schemas['PlayerRef'], 'id' | 'club'>[],
): void {
  const config = queryClient.getQueryData<{ data: { brandedAssets: boolean } }>(['config']);
  if (!BUILD_BRANDED_ASSETS || config?.data.brandedAssets !== true) return;
  const urls = new Set<string>();
  for (const p of players.slice(0, IMAGE_PREFETCH_ROWS)) {
    urls.add(playerPhotoUrl(p.id));
    urls.add(clubCrestUrl(p.club.code));
  }
  prefetchImages([...urls]);
}

/**
 * Loads what a tab shows first into the cache: its main query, keyed
 * exactly as the tab's screen asks for it. Fresh entries are not fetched
 * again (the client's staleTime).
 */
export async function prefetchLeagueTab(
  queryClient: QueryClient,
  leagueId: string,
  tab: LeagueTab,
  home: Home,
): Promise<void> {
  const inSeason = home.league.status === 'in_season' || home.league.status === 'complete';
  switch (tab) {
    case 'index':
      if (inSeason) await queryClient.prefetchQuery(leagueStatusQuery(leagueId));
      return;
    case 'matchup':
      if (inSeason)
        await prefetchGameweekMatchups(queryClient, leagueId, leagueGameweek(home), home);
      return;
    case 'team': {
      if (!home.viewer.team) return;
      await queryClient.prefetchQuery(teamQuery(leagueId, home.viewer.team.id));
      const team = queryClient.getQueryData<{ data: Schemas['TeamDetail'] }>(
        queryKeys.team(leagueId, home.viewer.team.id),
      )?.data;
      prefetchPlayerImages(queryClient, team?.roster.map((r) => r.player) ?? []);
      return;
    }
    case 'players': {
      await queryClient.prefetchInfiniteQuery(playersQuery(leagueId, PLAYERS_FIRST_FILTER));
      const first = queryClient.getQueryData<{
        pages: { data: { player: Schemas['PlayerRef'] }[] }[];
      }>(queryKeys.players(leagueId, PLAYERS_FIRST_FILTER))?.pages[0]?.data;
      prefetchPlayerImages(queryClient, first?.map((r) => r.player) ?? []);
      return;
    }
    case 'league':
      // The league home, which every tab already has.
      return;
  }
}

/**
 * Prefetches the tabs either side of `active` once the league home is in,
 * so a tap to a neighbour opens on data. The pager renders neighbours
 * itself on touch screens; this covers a mouse, where only the visible
 * page is rendered.
 */
export function usePrefetchLeagueNeighbours(leagueId: string, active: string, enabled: boolean) {
  const queryClient = useQueryClient();
  const homeKey = useLeagueHomeKey(leagueId);
  useEffect(() => {
    const home = queryClient.getQueryData<{ data: Home }>(queryKeys.league(leagueId))?.data;
    if (!enabled || !homeKey || !home) return;
    for (const tab of leagueTabNeighbours(active)) {
      void prefetchLeagueTab(queryClient, leagueId, tab, home).catch(() => undefined);
    }
  }, [enabled, homeKey, active, leagueId, queryClient]);
}

/**
 * A passive read of the league home (the tabs fetch and poll it), reduced
 * to what the prefetches depend on, so a poll of the home does not re-run
 * them. Undefined until the home is in.
 */
function useLeagueHomeKey(leagueId: string): string | undefined {
  return useQuery({
    // Never fetches from here, but whichever observer comes first defines
    // the query, so it carries the real fetch for a later refetch.
    ...leagueHomeQuery(leagueId),
    enabled: false,
    select: (envelope: { data: Home }) =>
      `${envelope.data.league.status}/${envelope.data.currentGameweek.number}/${envelope.data.viewer.team?.id ?? ''}`,
  }).data;
}

/**
 * Opening a league loads all five top tabs (Status, Match, Team, Players,
 * League) once its home is in, so the first visit to each paints on data.
 * Fresh entries are skipped (staleTime), so the tab already showing costs
 * nothing extra.
 */
export function usePrefetchLeague(leagueId: string) {
  const queryClient = useQueryClient();
  const homeKey = useLeagueHomeKey(leagueId);
  useEffect(() => {
    const home = queryClient.getQueryData<{ data: Home }>(queryKeys.league(leagueId))?.data;
    if (!homeKey || !home) return;
    for (const { name } of LEAGUE_TABS) {
      void prefetchLeagueTab(queryClient, leagueId, name, home).catch(() => undefined);
    }
  }, [homeKey, leagueId, queryClient]);
}

/**
 * On the Match tab at gameweek N: loads N-1 and N+1 (their matchups and the
 * one the tab would open on), so the stepper flips without a spinner.
 */
export function usePrefetchAdjacentGameweeks(leagueId: string, gw: number | undefined) {
  const queryClient = useQueryClient();
  const homeKey = useLeagueHomeKey(leagueId);
  useEffect(() => {
    const home = queryClient.getQueryData<{ data: Home }>(queryKeys.league(leagueId))?.data;
    if (gw === undefined || !homeKey || !home) return;
    for (const next of [gw + 1, gw - 1]) {
      void prefetchGameweekMatchups(queryClient, leagueId, next, home).catch(() => undefined);
    }
  }, [gw, homeKey, leagueId, queryClient]);
}

/** On a lineup at gameweek N: loads the same team's N-1 and N+1 within `[min, max]`. */
export function usePrefetchAdjacentLineups(
  leagueId: string,
  teamId: string,
  gw: number | undefined,
  min: number,
  max: number,
) {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (gw === undefined) return;
    for (const next of [gw + 1, gw - 1]) {
      if (next < min || next > max) continue;
      // Another team's lineup before its deadline is a 403: kept as an
      // error, so its screen asks again rather than flashing it.
      void queryClient.prefetchQuery(lineupQuery(leagueId, teamId, next));
    }
  }, [gw, min, max, leagueId, teamId, queryClient]);
}

/**
 * Press-in prefetches: a finger down (or mouse down) starts the next
 * screen's request ~100 ms before the press lands. Each is a no-op for
 * fresh data, and outside a QueryClientProvider (component tests).
 */
export function usePressPrefetch() {
  const queryClient = useContext(QueryClientContext);
  return useMemo(() => {
    const prefetch = (query: Parameters<QueryClient['prefetchQuery']>[0]) =>
      void queryClient?.prefetchQuery(query);
    return {
      /** A league row: its home, and the Status tab it opens on once drafted. */
      league: (leagueId: string, status?: Schemas['LeagueStatus']) => {
        prefetch(leagueHomeQuery(leagueId));
        if (status === 'in_season' || status === 'complete') prefetch(leagueStatusQuery(leagueId));
      },
      /** A player row: the player card's data. */
      player: (leagueId: string, playerId: string) =>
        prefetch(leaguePlayerQuery(leagueId, playerId)),
      /** A matchup card: its head-to-head. */
      matchup: (leagueId: string, gw: number, matchupId: string) =>
        prefetch(matchupQuery(leagueId, gw, matchupId)),
    };
  }, [queryClient]);
}

/** Press-in handler for a control that opens `playerId`'s card; undefined outside a league. */
export function usePlayerPressPrefetch(playerId: string): (() => void) | undefined {
  const leagueId = useContext(LeagueIdContext);
  const { player } = usePressPrefetch();
  return useMemo(
    () => (leagueId ? () => player(leagueId, playerId) : undefined),
    [leagueId, player, playerId],
  );
}
