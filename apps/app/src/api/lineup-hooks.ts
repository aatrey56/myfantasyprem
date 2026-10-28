import { send, type Schemas } from '@myfantasyprem/api-client';
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { api } from './client';
import { queryKeys } from './hooks';
import {
  cachedHome,
  othersPending,
  rollback,
  RuleError,
  snapshot,
  type Snapshot,
} from './optimistic';
import { refetchIntervalFor } from './query-client';
import { useScreenFocus } from './screen-focus';
import { haptics } from '@/lib/haptics';
import { DEFAULT_RULES, lineupProblems } from '@/lib/lineup';

type LineupEnvelope = { data: Schemas['Lineup']; meta: Schemas['Meta'] };

/** Refetch this long after the deadline, so the server has certainly locked. */
const LOCK_SLACK_MS = 1_000;
/** setTimeout overflows past ~24.8 days; re-plan at least this often. */
const MAX_TIMER_MS = 6 * 60 * 60_000;

/**
 * Milliseconds until an open lineup locks, on the server's clock: the
 * device clock may be off, so the deadline is measured from `serverTime`
 * plus the time since the answer arrived. Undefined when already locked.
 */
export function msUntilLock(
  envelope: LineupEnvelope | undefined,
  receivedAt: number,
  now = Date.now(),
): number | undefined {
  if (!envelope || envelope.data.locked) return undefined;
  const locksAt = Date.parse(envelope.data.locksAt);
  const serverTime = Date.parse(envelope.meta.serverTime);
  if (!Number.isFinite(locksAt)) return undefined;
  const serverNow = Number.isFinite(serverTime) ? serverTime + (now - receivedAt) : now;
  return Math.max(0, locksAt - serverNow);
}

/**
 * Whether the lineup can no longer change: the server said so, or its
 * deadline has passed on the server's clock since it answered. A lineup
 * fetched before gameweek N's deadline still says `locked: false`; the
 * clock makes it read locked at once, so the screen refetches and moves on
 * to gameweek N+1 instead of refusing every edit against N's deadline.
 */
export function lineupLocked(
  envelope: LineupEnvelope | undefined,
  receivedAt: number,
  now = Date.now(),
): boolean {
  if (!envelope) return false;
  return envelope.data.locked || msUntilLock(envelope, receivedAt, now) === 0;
}

/** The lineup query, shared with the gameweek prefetch (api/prefetch.ts). */
export const lineupQuery = (leagueId: string, teamId: string, gw: number | undefined) => ({
  queryKey: queryKeys.lineup(leagueId, teamId, gw),
  queryFn: () =>
    send(
      api.GET('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
        params: {
          path: { leagueId, teamId },
          query: gw === undefined ? {} : { gameweek: gw },
        },
      }),
    ),
});

/**
 * A team's lineup for a gameweek; `gw` undefined means the next unlocked
 * gameweek (the server's default). 403 `lineup-private` for another team
 * before the deadline. Once the gameweek has started it carries each
 * player's points and auto-sub state and the team's totals, and polls
 * while they can move.
 */
export function useLineup(leagueId: string, teamId: string | undefined, gw: number | undefined) {
  const { subscribed, focused } = useScreenFocus(lineupQuery(leagueId, teamId ?? '', gw).queryKey);
  return useQuery({
    ...lineupQuery(leagueId, teamId ?? '', gw),
    enabled: Boolean(teamId),
    // A gameweek step keeps the last lineup on screen until the next loads
    // (the team screen does not edit a kept one: isPlaceholderData).
    placeholderData: keepPreviousData,
    refetchInterval: (q) => {
      if (!focused) return false;
      const scores = refetchIntervalFor(
        q.state.data && {
          ...q.state.data.meta,
          scoreState: q.state.data.meta.scoreState ?? q.state.data.data.scoreState,
        },
      );
      // An open lineup: fetch again just after the deadline so editing
      // stops when the server locks it.
      const untilLock = msUntilLock(q.state.data, q.state.dataUpdatedAt);
      if (untilLock === undefined) return scores;
      const atLock = Math.min(untilLock + LOCK_SLACK_MS, MAX_TIMER_MS);
      return scores === false ? atLock : Math.min(scores, atLock);
    },
    subscribed,
  });
}

export interface LineupInput {
  gameweek: number;
  starters: string[];
  bench: string[];
}

/** Lineup saves for one team run one at a time, in the order they were made. */
const lineupMutationKey = (leagueId: string, teamId: string) => [
  'lineup',
  'save',
  leagueId,
  teamId,
];

/**
 * The XI and bench a save would show, built from the cached lineup, or a
 * `RuleError` for a move FPL Draft's rules refuse: a locked gameweek (the
 * deadline passed on the server's clock) or a formation outside the
 * league's limits. Undefined when the cache cannot tell (nothing cached,
 * or players the cache does not know): the server decides alone.
 */
export function plannedLineup(
  cached: LineupEnvelope | undefined,
  receivedAt: number,
  input: LineupInput,
  rules: Schemas['LineupRules'] = DEFAULT_RULES,
  now = Date.now(),
): Schemas['Lineup'] | RuleError | undefined {
  if (!cached || cached.data.gameweek !== input.gameweek) return undefined;
  if (lineupLocked(cached, receivedAt, now)) {
    return new RuleError([`The GW ${input.gameweek} deadline has passed: the lineup is locked.`]);
  }
  const byId = new Map(
    [...cached.data.starters, ...cached.data.bench].map((e) => [e.player.id, e]),
  );
  const pick = (ids: string[], first: number) =>
    ids.map((id, i) => {
      const e = byId.get(id);
      return e && { ...e, slot: first + i };
    });
  const starters = pick(input.starters, 1);
  const bench = pick(input.bench, 12);
  if ([...starters, ...bench].some((e) => !e)) return undefined;
  const next = {
    starters: starters as Schemas['LineupEntry'][],
    bench: bench as Schemas['LineupEntry'][],
  };
  const problems = lineupProblems(next, rules);
  if (problems.length) return new RuleError(problems);
  return { ...cached.data, ...next };
}

/**
 * Saves the XI and bench, optimistically: the cached lineup (this
 * gameweek's and, when it is the next one, the default) shows the move at
 * once, a move the rules refuse throws `RuleError` before anything is
 * shown or sent, and a refusal from the server (422 with `errors`, 409
 * locked) puts the cache back exactly as it was and taps an error.
 */
export function useSetLineup(leagueId: string, teamId: string, rules?: Schemas['LineupRules']) {
  const queryClient = useQueryClient();
  const mutationKey = lineupMutationKey(leagueId, teamId);
  const keysFor = (gameweek: number) => {
    const gwKey = queryKeys.lineup(leagueId, teamId, gameweek);
    const nextKey = queryKeys.lineup(leagueId, teamId, undefined);
    const next = queryClient.getQueryData<LineupEnvelope>(nextKey);
    return next?.data.gameweek === gameweek ? [gwKey, nextKey] : [gwKey];
  };
  return useMutation({
    mutationKey,
    scope: { id: mutationKey.join(':') },
    mutationFn: (input: LineupInput) =>
      send(
        api.PUT('/v1/leagues/{leagueId}/teams/{teamId}/lineup', {
          params: { path: { leagueId, teamId } },
          body: input,
        }),
      ),
    onMutate: async (input) => {
      const keys = keysFor(input.gameweek);
      const saved: Snapshot = [];
      for (const key of keys)
        saved.push(...(await snapshot(queryClient, { queryKey: key, exact: true })));
      const source = keys
        .map((key) => ({ key, state: queryClient.getQueryState<LineupEnvelope>(key) }))
        .find((s) => s.state?.data);
      const plan = plannedLineup(
        source?.state?.data,
        source?.state?.dataUpdatedAt ?? 0,
        input,
        rules ?? cachedHome(queryClient, leagueId)?.settings.lineup,
      );
      if (plan instanceof RuleError) throw plan;
      if (plan) {
        for (const key of keys) {
          queryClient.setQueryData<LineupEnvelope>(key, (d) => (d ? { ...d, data: plan } : d));
        }
      }
      return { saved };
    },
    onError: (_e, _input, ctx) => {
      // A later save is queued on top of this one: let the refetch below set things right.
      if (othersPending(queryClient, mutationKey)) haptics.error();
      else rollback(queryClient, ctx?.saved);
    },
    onSuccess: (envelope, input) => {
      if (othersPending(queryClient, mutationKey)) return;
      for (const key of keysFor(input.gameweek)) {
        queryClient.setQueryData<LineupEnvelope>(key, envelope);
      }
    },
    onSettled: (_d, error, input) => {
      if (othersPending(queryClient, mutationKey)) return;
      // Later gameweeks carry this lineup over; a refused save refetches its own too.
      const own = new Set(error ? [] : keysFor(input.gameweek).map((k) => String(k[4])));
      void queryClient.invalidateQueries({
        predicate: (q) =>
          q.queryKey[0] === 'league' &&
          q.queryKey[1] === leagueId &&
          q.queryKey[2] === 'lineup' &&
          q.queryKey[3] === teamId &&
          !own.has(String(q.queryKey[4])),
      });
      void queryClient.invalidateQueries({ queryKey: queryKeys.team(leagueId, teamId) });
      // A new XI moves the team's Expected and win odds (ADR 0014).
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'projections'] });
      // Previews show each side's XI.
      void queryClient.invalidateQueries({ queryKey: ['league', leagueId, 'matchup-preview'] });
    },
  });
}
