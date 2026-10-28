import { ApiError, send, type Schemas } from '@myfantasyprem/api-client';
import {
  keepPreviousData,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';

import { api, timeoutFetchFor } from './client';
import type { PlayerSort } from '@/lib/playerSort';
import { refetchIntervalFor, shouldRetry } from './query-client';
import { useScreenFocus } from './screen-focus';

export const queryKeys = {
  gameweek: ['gameweek', 'current'] as const,
  fixtures: (gw: number | undefined) => ['gameweek', gw, 'fixtures'] as const,
  /** Under ['gameweek'] with the fixture lists: a live score refreshes both. */
  fixture: (fixtureId: string) => ['gameweek', 'fixture', fixtureId] as const,
  league: (leagueId: string) => ['league', leagueId] as const,
  matchups: (leagueId: string, gw: number | undefined) =>
    ['league', leagueId, 'matchups', gw] as const,
  matchup: (leagueId: string, gw: number, matchupId: string) =>
    ['league', leagueId, 'matchups', gw, matchupId] as const,
  /** Squads move with lineups and rosters: lineup saves and roster moves refresh it. */
  matchupPreview: (leagueId: string, gw: number, matchupId: string) =>
    ['league', leagueId, 'matchup-preview', gw, matchupId] as const,
  team: (leagueId: string, teamId: string) => ['league', leagueId, 'team', teamId] as const,
  players: (leagueId: string, filter: object) => ['league', leagueId, 'players', filter] as const,
  player: (leagueId: string, playerId: string) => ['league', leagueId, 'player', playerId] as const,
  transactions: (leagueId: string, kind?: string) =>
    ['league', leagueId, 'transactions', kind ?? 'all'] as const,
  standings: (leagueId: string) => ['league', leagueId, 'standings'] as const,
  schedule: (leagueId: string) => ['league', leagueId, 'schedule'] as const,
  clubs: ['clubs'] as const,
  waivers: (leagueId: string) => ['league', leagueId, 'waivers'] as const,
  claims: (leagueId: string, status: 'pending' | 'processed') =>
    ['league', leagueId, 'waivers', 'claims', status] as const,
  trades: (leagueId: string) => ['league', leagueId, 'trades'] as const,
  me: ['me'] as const,
  myLeagues: ['me', 'leagues'] as const,
  invite: (code: string) => ['invite', code] as const,
  invites: (leagueId: string) => ['league', leagueId, 'invites'] as const,
  members: (leagueId: string) => ['league', leagueId, 'members'] as const,
  draft: (leagueId: string) => ['league', leagueId, 'draft'] as const,
  draftQueue: (leagueId: string) => ['league', leagueId, 'draft', 'queue'] as const,
  lineup: (leagueId: string, teamId: string, gw: number | undefined) =>
    ['league', leagueId, 'lineup', teamId, gw ?? 'next'] as const,
  importPreview: (input: object) => ['import', 'preview', input] as const,
  sync: (leagueId: string) => ['league', leagueId, 'sync'] as const,
  seats: (leagueId: string) => ['league', leagueId, 'seats'] as const,
  import: (importId: string) => ['import', importId] as const,
};

export function useCurrentGameweek() {
  const { subscribed } = useScreenFocus(queryKeys.gameweek);
  return useQuery({
    queryKey: queryKeys.gameweek,
    queryFn: () => send(api.GET('/v1/gameweeks/current')),
    subscribed,
  });
}

/** A gameweek's Premier League fixtures with live scores (SCORES). Polls while live. */
export function useGameweekFixtures(gw: number | undefined) {
  const { subscribed, focused } = useScreenFocus(queryKeys.fixtures(gw));
  return useQuery({
    queryKey: queryKeys.fixtures(gw),
    queryFn: () =>
      send(api.GET('/v1/gameweeks/{gw}/fixtures', { params: { path: { gw: gw as number } } })),
    enabled: gw !== undefined,
    // A new gameweek or filter keeps the last answer on screen until it loads: no flash.
    placeholderData: keepPreviousData,
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

/** One fixture's box score with my leagues' owners (SCORES). Polls while live, as the list does. */
export function useFixture(fixtureId: string) {
  const { subscribed, focused } = useScreenFocus(queryKeys.fixture(fixtureId));
  return useQuery({
    queryKey: queryKeys.fixture(fixtureId),
    queryFn: () => send(api.GET('/v1/fixtures/{fixtureId}', { params: { path: { fixtureId } } })),
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

export function useLeagueHome(leagueId: string) {
  const { subscribed, focused } = useScreenFocus(queryKeys.league(leagueId));
  return useQuery({
    ...leagueHomeQuery(leagueId),
    // Poll while scores can move (see refetchIntervalFor).
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

/**
 * The league home for screens that only need its context (my team, the
 * settings, the current gameweek, standings): same cache entry as
 * {@link useLeagueHome}, but this observer never polls, so a tab that is
 * not the League tab does not keep the league home refreshing.
 */
export function useLeagueContext(leagueId: string) {
  const { subscribed } = useScreenFocus(queryKeys.league(leagueId));
  return useQuery({
    ...leagueHomeQuery(leagueId),
    subscribed,
  });
}

/**
 * What the league shell (top bar, top nav) shows: name, status, the
 * caller's role and team. A passive observer of the league-home cache
 * entry: no polling, no focus refetch, so the nav never drives requests on
 * every screen.
 */
export function useLeagueShell(leagueId: string) {
  return useQuery({
    ...leagueHomeQuery(leagueId),
    select: (envelope) => ({
      name: envelope.data.league.name,
      status: envelope.data.league.status,
      source: envelope.data.league.source,
      role: envelope.data.viewer.role,
      team: envelope.data.viewer.team,
      gameweek: envelope.data.currentGameweek.number,
    }),
    staleTime: Infinity,
    refetchInterval: false,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
    // No `retry` override: this observer often starts the shared fetch, and
    // the fetch runs with the starting observer's retry policy.
  });
}

/** `gw` is undefined until the current gameweek is known; nothing is fetched until then. */
export function useMatchups(leagueId: string, gw: number | undefined) {
  const { subscribed, focused } = useScreenFocus(queryKeys.matchups(leagueId, gw));
  return useQuery({
    ...matchupsQuery(leagueId, gw),
    enabled: gw !== undefined,
    // A new gameweek or filter keeps the last answer on screen until it loads: no flash.
    placeholderData: keepPreviousData,
    // Poll while scores can move (see refetchIntervalFor).
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

export function useMatchup(leagueId: string, gw: number, matchupId: string) {
  const { subscribed, focused } = useScreenFocus(queryKeys.matchup(leagueId, gw, matchupId));
  return useQuery({
    ...matchupQuery(leagueId, gw, matchupId),
    enabled: Number.isInteger(gw),
    // A gameweek step keeps the last matchup on screen until the next loads.
    placeholderData: keepPreviousData,
    // Poll while scores can move (see refetchIntervalFor).
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

/** A scheduled matchup's squads and fixtures, no scores (a future gameweek's preview). */
export function useMatchupPreview(leagueId: string, gw: number, matchupId: string) {
  const key = queryKeys.matchupPreview(leagueId, gw, matchupId);
  const { subscribed } = useScreenFocus(key);
  return useQuery({
    queryKey: key,
    queryFn: () =>
      send(
        api.GET('/v1/leagues/{leagueId}/gameweeks/{gw}/matchups/{matchupId}/preview', {
          params: { path: { leagueId, gw, matchupId } },
        }),
      ),
    enabled: Number.isInteger(gw) && matchupId !== '',
    placeholderData: keepPreviousData,
    staleTime: 60_000,
    subscribed,
  });
}

export function useTeam(leagueId: string, teamId: string) {
  const { subscribed } = useScreenFocus(queryKeys.team(leagueId, teamId));
  return useQuery({
    ...teamQuery(leagueId, teamId),
    enabled: Boolean(teamId),
    subscribed,
  });
}

export interface PlayersFilter {
  available?: boolean;
  status?: 'all' | 'available' | 'free_agent' | 'waivers' | 'rostered';
  position?: Schemas['Position'];
  club?: string;
  q?: string;
  /** Only my watchlist. */
  watchlist?: boolean;
  /** Only players I tagged with this tag. */
  tag?: Schemas['PlayerTag'];
  sort?: PlayerSort;
  limit?: number;
}

/*
 * Query definitions shared by the hooks below and by the league pager's
 * prefetch (api/prefetch.ts), so both hit the same cache entries.
 */
export const leagueHomeQuery = (leagueId: string) => ({
  queryKey: queryKeys.league(leagueId),
  queryFn: () => send(api.GET('/v1/leagues/{leagueId}', { params: { path: { leagueId } } })),
});

export const leaguePlayerQuery = (leagueId: string, playerId: string) => ({
  queryKey: queryKeys.player(leagueId, playerId),
  queryFn: () =>
    send(
      api.GET('/v1/leagues/{leagueId}/players/{playerId}', {
        params: { path: { leagueId, playerId } },
      }),
    ),
});

export const matchupsQuery = (leagueId: string, gw: number | undefined) => ({
  queryKey: queryKeys.matchups(leagueId, gw),
  queryFn: () =>
    send(
      api.GET('/v1/leagues/{leagueId}/gameweeks/{gw}/matchups', {
        params: { path: { leagueId, gw: gw as number } },
      }),
    ),
});

export const matchupQuery = (leagueId: string, gw: number, matchupId: string) => ({
  queryKey: queryKeys.matchup(leagueId, gw, matchupId),
  queryFn: () =>
    send(
      api.GET('/v1/leagues/{leagueId}/gameweeks/{gw}/matchups/{matchupId}', {
        params: { path: { leagueId, gw, matchupId } },
      }),
    ),
});

export const teamQuery = (leagueId: string, teamId: string) => ({
  queryKey: queryKeys.team(leagueId, teamId),
  queryFn: () =>
    send(
      api.GET('/v1/leagues/{leagueId}/teams/{teamId}', {
        params: { path: { leagueId, teamId } },
      }),
    ),
});

export const playersQuery = (leagueId: string, filter: PlayersFilter) => ({
  queryKey: queryKeys.players(leagueId, filter),
  initialPageParam: undefined as string | undefined,
  queryFn: ({ pageParam }: { pageParam: string | undefined }) =>
    send(
      api.GET('/v1/leagues/{leagueId}/players', {
        params: { path: { leagueId }, query: { ...filter, cursor: pageParam } },
      }),
    ),
  getNextPageParam: (last: { meta: { nextCursor?: string | null } }) =>
    last.meta.nextCursor ?? undefined,
});

/** What the Players tab asks for first: available players by total points. */
export const PLAYERS_FIRST_FILTER = {
  status: 'available',
  sort: 'total_points',
  limit: 40,
} as const satisfies PlayersFilter;

export function useLeaguePlayers(
  leagueId: string,
  filter: PlayersFilter = {},
  { enabled = true }: { enabled?: boolean } = {},
) {
  const { subscribed } = useScreenFocus(queryKeys.players(leagueId, filter));
  return useInfiniteQuery({
    ...playersQuery(leagueId, filter),
    enabled,
    // A new gameweek or filter keeps the last answer on screen until it loads: no flash.
    placeholderData: keepPreviousData,
    subscribed,
  });
}

export function useLeaguePlayer(leagueId: string, playerId: string) {
  const { subscribed, focused } = useScreenFocus(queryKeys.player(leagueId, playerId));
  return useQuery({
    ...leaguePlayerQuery(leagueId, playerId),
    // Poll while scores can move (see refetchIntervalFor).
    refetchInterval: (q) => (focused ? refetchIntervalFor(q.state.data?.meta) : false),
    subscribed,
  });
}

/** The activity feed, newest first, one cursor page at a time; `kind` filters it. */
export function useTransactions(
  leagueId: string,
  kind?: Schemas['TransactionKind'],
  limit?: number,
) {
  const queryKey = [...queryKeys.transactions(leagueId, kind), limit ?? 'default'];
  const { subscribed } = useScreenFocus(queryKey);
  return useInfiniteQuery({
    queryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) =>
      send(
        api.GET('/v1/leagues/{leagueId}/transactions', {
          params: { path: { leagueId }, query: { cursor: pageParam, kind, limit } },
        }),
      ),
    getNextPageParam: (last) => last.meta.nextCursor ?? undefined,
    subscribed,
  });
}

/** The league table (real teams only, completed gameweeks). */
export function useStandings(leagueId: string, enabled = true) {
  const { subscribed } = useScreenFocus(queryKeys.standings(leagueId));
  return useQuery({
    queryKey: queryKeys.standings(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/standings', { params: { path: { leagueId } } })),
    enabled,
    subscribed,
  });
}

/** Every gameweek's pairings; empty before the draft. Changes rarely. */
export function useSchedule(leagueId: string, enabled = true) {
  const { subscribed } = useScreenFocus(queryKeys.schedule(leagueId));
  return useQuery({
    queryKey: queryKeys.schedule(leagueId),
    queryFn: () =>
      send(api.GET('/v1/leagues/{leagueId}/schedule', { params: { path: { leagueId } } })),
    enabled,
    staleTime: 10 * 60_000,
    subscribed,
  });
}

/** Clubs by name, for the players filter. */
export function useClubs() {
  return useQuery({
    queryKey: queryKeys.clubs,
    queryFn: () => send(api.GET('/v1/clubs')),
    staleTime: 60 * 60_000,
  });
}

export type LeagueRef = { draftLeagueUrl: string } | { fplLeagueId: number };
/** An import request: the league and the caller's own seat in it (from the preview). */
export type ImportInput = LeagueRef & { seat: number };

/**
 * How long the preview may take. The server asks FPL live through a
 * rate-paced client and gives up after 35 s (previewTimeout in
 * server/internal/httpapi/imports.go); this waits a little longer.
 */
export const IMPORT_PREVIEW_TIMEOUT = { ms: 45_000 };

/**
 * "Is this your league?" lookup. Disabled until `input` is set. Never
 * retried on its own: each try is another FPL call; the screen offers a retry.
 */
export function useImportPreview(input: LeagueRef | null) {
  return useQuery({
    queryKey: queryKeys.importPreview(input ?? {}),
    queryFn: ({ signal }) =>
      send(
        api.GET('/v1/imports/preview', {
          params: { query: input ?? {} },
          signal,
          fetch: timeoutFetchFor(IMPORT_PREVIEW_TIMEOUT.ms),
        }),
      ),
    enabled: input !== null,
    retry: false,
    staleTime: 60_000,
  });
}

/** Seconds from a `Retry-After` header, as milliseconds; undefined when absent or a date. */
function retryAfterMs(response: Response): number | undefined {
  const seconds = Number(response.headers.get('Retry-After'));
  return Number.isFinite(seconds) && seconds > 0 ? seconds * 1000 : undefined;
}

export function useCreateImport() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: ImportInput) => {
      const result = await api.POST('/v1/imports', { body: input });
      const envelope = await send(Promise.resolve(result));
      // The contract: poll no sooner than Retry-After.
      return { ...envelope, retryAfterMs: retryAfterMs(result.response) };
    },
    onSuccess: ({ data, meta }) =>
      queryClient.setQueryData(queryKeys.import(data.id), { data, meta }),
  });
}

/** How often an active import is polled, unless the server asked for longer. */
export const IMPORT_POLL_MS = 1_000;

const ACTIVE_IMPORT = new Set(['queued', 'running']);

/**
 * Polls an import while it is queued or running, no more often than the
 * server's Retry-After. Stops on a client error (a 404 will not fix itself).
 */
export function useImport(importId: string | undefined, minIntervalMs = IMPORT_POLL_MS) {
  return useQuery({
    queryKey: queryKeys.import(importId ?? ''),
    queryFn: () =>
      send(api.GET('/v1/imports/{importId}', { params: { path: { importId: importId! } } })),
    enabled: importId !== undefined,
    // The POST answer seeds the cache; the first poll waits like the others.
    staleTime: minIntervalMs,
    refetchInterval: (q) => {
      const error = q.state.error;
      if (error instanceof ApiError && !shouldRetry(0, error)) return false;
      const status = q.state.data?.data.status;
      return status && ACTIVE_IMPORT.has(status) ? Math.max(IMPORT_POLL_MS, minIntervalMs) : false;
    },
  });
}
