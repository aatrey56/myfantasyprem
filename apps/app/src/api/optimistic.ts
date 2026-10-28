import type { Schemas } from '@myfantasyprem/api-client';
import type { InfiniteData, QueryClient, QueryFilters, QueryKey } from '@tanstack/react-query';

import { queryKeys } from './hooks';
import { expandRowFixtures, toRowFixture } from '@/lib/fixtures';
import { haptics } from '@/lib/haptics';

/**
 * Optimistic mutations (Wave B): `onMutate` checks the client-side rules,
 * cancels the affected queries, snapshots them and writes the expected
 * result; `onError` puts the snapshot back exactly; `onSettled` refetches.
 * A move the rules refuse throws `RuleError` from `onMutate`, so it is
 * never shown and never sent.
 */
export class RuleError extends Error {
  readonly problems: string[];
  constructor(problems: string[]) {
    super(problems[0] ?? "That move isn't allowed.");
    this.name = 'RuleError';
    this.problems = problems;
  }
}

export type Snapshot = [QueryKey, unknown][];

/** Every query under the league whose third key part is one of `kinds`. */
export const leagueQueries = (leagueId: string, ...kinds: string[]): QueryFilters => ({
  predicate: (q) =>
    q.queryKey[0] === 'league' &&
    q.queryKey[1] === leagueId &&
    kinds.includes(String(q.queryKey[2])),
});

/** Stops in-flight fetches (they would overwrite the optimistic write) and copies the data. */
export async function snapshot(queryClient: QueryClient, filters: QueryFilters): Promise<Snapshot> {
  await queryClient.cancelQueries(filters);
  return queryClient.getQueriesData(filters);
}

/** Puts every snapshotted query back as it was, and taps the rollback. */
export function rollback(queryClient: QueryClient, saved: Snapshot | undefined) {
  haptics.error();
  for (const [key, data] of saved ?? []) queryClient.setQueryData(key, data);
}

/** True while another mutation with this key is still to settle (the current one counts). */
export const othersPending = (queryClient: QueryClient, mutationKey: QueryKey) =>
  queryClient.isMutating({ mutationKey }) > 1;

// ---------------------------------------------------------------- reads

type HomeEnvelope = { data: Schemas['LeagueHome'] };
type TeamEnvelope = { data: Schemas['TeamDetail'] };
type PlayersPage = { data: Schemas['PlayerListItem'][] };
type DetailEnvelope = { data: Schemas['PlayerDetail'] };

/** The league home from the cache (the league shell always loads it). */
export const cachedHome = (queryClient: QueryClient, leagueId: string) =>
  queryClient.getQueryData<HomeEnvelope>(queryKeys.league(leagueId))?.data;

export const cachedTeam = (queryClient: QueryClient, leagueId: string, teamId: string) =>
  queryClient.getQueryData<TeamEnvelope>(queryKeys.team(leagueId, teamId))?.data;

/** What the cache knows about a player: their ref, and their status and numbers if listed. */
export interface KnownPlayer {
  player: Schemas['PlayerRef'];
  rosterStatus?: Schemas['RosterStatus'];
  totalPoints?: number;
  gameweekPoints?: number;
  nextFixtures?: Schemas['RowFixture'][];
}

export function findPlayer(
  queryClient: QueryClient,
  leagueId: string,
  playerId: string,
): KnownPlayer | undefined {
  const detail = queryClient.getQueryData<DetailEnvelope>(queryKeys.player(leagueId, playerId));
  let found: KnownPlayer | undefined = detail && {
    player: detail.data.player,
    rosterStatus: detail.data.rosterStatus,
    totalPoints: detail.data.totalPoints,
    nextFixtures: detail.data.upcoming.map(toRowFixture),
  };
  for (const [, data] of queryClient.getQueriesData<InfiniteData<PlayersPage>>(
    leagueQueries(leagueId, 'players'),
  )) {
    const item = data?.pages?.flatMap((p) => p.data).find((i) => i.player.id === playerId);
    if (item) return { ...item, rosterStatus: found?.rosterStatus ?? item.rosterStatus };
  }
  if (found) return found;
  for (const [, data] of queryClient.getQueriesData<TeamEnvelope>(
    leagueQueries(leagueId, 'team'),
  )) {
    const entry = data?.data?.roster?.find((r) => r.player.id === playerId);
    if (entry) found = { ...entry, rosterStatus: 'rostered' };
  }
  return found;
}

// ---------------------------------------------------------------- writes

/** Rewrites the item for `playerId` in every cached player list and its detail. */
export function patchPlayerStatus(
  queryClient: QueryClient,
  leagueId: string,
  playerId: string,
  patch: Pick<Schemas['PlayerListItem'], 'rosterStatus' | 'owner'>,
) {
  queryClient.setQueriesData<InfiniteData<PlayersPage>>(leagueQueries(leagueId, 'players'), (d) =>
    d?.pages
      ? {
          ...d,
          pages: d.pages.map((p) => ({
            ...p,
            data: p.data.map((i) => (i.player.id === playerId ? { ...i, ...patch } : i)),
          })),
        }
      : d,
  );
  queryClient.setQueryData<DetailEnvelope>(queryKeys.player(leagueId, playerId), (d) =>
    d ? { ...d, data: { ...d.data, ...patch } } : d,
  );
}

type LineupEnvelope = { data: Schemas['Lineup'] };

/**
 * A free agent joins my squad and a same-position player leaves: the
 * arrival takes the departed player's roster slot and, in every lineup
 * still open, his lineup slot (as the server repairs a carried-over
 * lineup). Locked lineups are history and stay as they are.
 */
export function applyRosterMove(
  queryClient: QueryClient,
  leagueId: string,
  team: Schemas['TeamRef'],
  add: KnownPlayer,
  dropId: string,
) {
  queryClient.setQueryData<TeamEnvelope>(queryKeys.team(leagueId, team.id), (d) =>
    d
      ? {
          ...d,
          data: {
            ...d.data,
            roster: d.data.roster.map((r) =>
              r.player.id === dropId
                ? {
                    slot: r.slot,
                    player: add.player,
                    totalPoints: add.totalPoints ?? 0,
                    gameweekPoints: add.gameweekPoints ?? 0,
                    acquiredVia: 'free_agent',
                    nextFixtures: add.nextFixtures ?? [],
                  }
                : r,
            ),
          },
        }
      : d,
  );
  // A lineup entry lists that gameweek's fixtures only (full, with each
  // opponent's club); the list row has every remaining one, compact.
  const clubs = queryClient.getQueryData<{ data: Schemas['ClubRef'][] }>(queryKeys.clubs)?.data;
  const swapIn =
    (gameweek: number) =>
    (e: Schemas['LineupEntry']): Schemas['LineupEntry'] =>
      e.player.id === dropId
        ? {
            slot: e.slot,
            player: add.player,
            fixtures: expandRowFixtures(
              (add.nextFixtures ?? []).filter((f) => f.gameweek === gameweek),
              clubs,
            ),
          }
        : e;
  for (const [key, d] of queryClient.getQueriesData<LineupEnvelope>(
    leagueQueries(leagueId, 'lineup'),
  )) {
    if (key[3] !== team.id || !d || d.data.locked) continue;
    const swap = swapIn(d.data.gameweek);
    queryClient.setQueryData<LineupEnvelope>(key, {
      ...d,
      data: { ...d.data, starters: d.data.starters.map(swap), bench: d.data.bench.map(swap) },
    });
  }
  patchPlayerStatus(queryClient, leagueId, add.player.id, {
    rosterStatus: 'rostered',
    owner: team,
  });
  // A dropped player sits on waivers until the next run (FPL Draft).
  patchPlayerStatus(queryClient, leagueId, dropId, { rosterStatus: 'waivers', owner: null });
}

type TradesData = InfiniteData<{ data: Schemas['Trade'][]; meta: Schemas['Meta'] }>;

/** Rewrites the cached trade list (every loaded page). */
export function updateTrades(
  queryClient: QueryClient,
  leagueId: string,
  fn: (trades: Schemas['Trade'][], page: number) => Schemas['Trade'][],
) {
  queryClient.setQueryData<TradesData>(queryKeys.trades(leagueId), (d) =>
    d ? { ...d, pages: d.pages.map((p, i) => ({ ...p, data: fn(p.data, i) })) } : d,
  );
}

let tempSeq = 0;
/** A placeholder id for a row the server has not created yet. */
export const tempId = (prefix: string) => `${prefix}_optimistic_${++tempSeq}`;
