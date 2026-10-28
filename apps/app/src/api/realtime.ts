import { send, type Schemas } from '@myfantasyprem/api-client';
import { useQueryClient, type QueryClient, type QueryKey } from '@tanstack/react-query';
import { useEffect, useSyncExternalStore } from 'react';

import { api } from './client';
import { queryKeys } from './hooks';
import { tradeBlockKey } from './trade-block-hooks';

/**
 * The realtime channel (docs/realtime.md): one WebSocket per league screen
 * stack. Events only invalidate the matching queries; REST stays the source
 * of truth and polling stays the fallback (the draft room polls less while
 * a socket is up). The mocks serve no socket and Jest has none, so the
 * channel is off there.
 */
export const REALTIME_ENABLED =
  process.env.EXPO_PUBLIC_USE_MOCKS !== 'true' && process.env.NODE_ENV !== 'test';

/** Client heartbeat; the server drops sockets silent for 60 s. */
export const PING_MS = 25_000;

type ServerMessage = Schemas['RealtimeEvent'] | Schemas['RealtimePong'];

/** Reconnect delay: 1 s doubling to 30 s, with up to 50% jitter. */
export function backoffMs(attempt: number, random: () => number = Math.random): number {
  const base = Math.min(1_000 * 2 ** attempt, 30_000);
  return Math.round(base / 2 + (base / 2) * random());
}

/** The queries an event makes stale. Unknown types (open enum) touch nothing. */
export function invalidationsFor(leagueId: string, type: string): QueryKey[] {
  const league = queryKeys.league(leagueId);
  switch (type) {
    case 'draft.updated':
    case 'draft.pick_made':
      return [queryKeys.draft(leagueId), [...league, 'players']];
    case 'matchups.updated':
    case 'score.corrected':
      // The poller moved live points: the SCORES fixtures and box scores
      // (['gameweek', ...], not the league's) moved with them.
      return [
        league,
        [...league, 'matchups'],
        [...league, 'lineup'],
        queryKeys.standings(leagueId),
        ['gameweek'],
      ];
    case 'trade.updated':
      // A processed trade takes its players off the trade block, and their
      // news moves with them (every team's news, by prefix).
      return [
        queryKeys.trades(leagueId),
        queryKeys.transactions(leagueId).slice(0, 3),
        tradeBlockKey(leagueId),
        [...league, 'news'],
        [...league, 'matchup-preview'],
      ];
    case 'waivers.processed':
      return [
        queryKeys.waivers(leagueId),
        [...league, 'players'],
        [...league, 'team'],
        queryKeys.transactions(leagueId).slice(0, 3),
        tradeBlockKey(leagueId),
        [...league, 'news'],
        [...league, 'matchup-preview'],
      ];
    case 'lineup.locked':
      return [
        [...league, 'lineup'],
        [...league, 'matchups'],
        [...league, 'matchup-preview'],
      ];
    case 'league.updated':
      return [league, queryKeys.members(leagueId), queryKeys.myLeagues];
    case 'trade_block.updated':
      return [tradeBlockKey(leagueId)];
    default:
      return [];
  }
}

// Connection state per league, for useRealtimeConnected.
const connected = new Map<string, boolean>();
const listeners = new Set<() => void>();
function setConnected(leagueId: string, value: boolean) {
  connected.set(leagueId, value);
  listeners.forEach((l) => l());
}

/** Whether the league's socket is open (the draft room polls less then). */
export function useRealtimeConnected(leagueId: string): boolean {
  return useSyncExternalStore(
    (l) => {
      listeners.add(l);
      return () => listeners.delete(l);
    },
    () => connected.get(leagueId) ?? false,
    () => false,
  );
}

/**
 * Connects to the league's events until the returned function is called,
 * reconnecting with backoff and replaying from the last seq seen. A gap in
 * seq (events the server no longer keeps) refetches the whole league.
 */
export function connectLeague(leagueId: string, queryClient: QueryClient): () => void {
  let closed = false;
  let socket: WebSocket | null = null;
  let attempt = 0;
  let lastSeq: number | null = null;
  let ping: ReturnType<typeof setInterval> | undefined;
  let retry: ReturnType<typeof setTimeout> | undefined;

  const invalidate = (keys: QueryKey[]) =>
    keys.forEach((queryKey) => void queryClient.invalidateQueries({ queryKey }));

  const schedule = () => {
    if (closed) return;
    retry = setTimeout(open, backoffMs(attempt++));
  };

  async function open() {
    if (closed) return;
    let url: string;
    try {
      const ticket = await send(api.POST('/v1/realtime/tickets', { body: { leagueId } }));
      url = ticket.data.url;
    } catch {
      schedule();
      return;
    }
    if (closed) return;
    const ws = new WebSocket(url);
    socket = ws;
    ws.onopen = () => {
      attempt = 0;
      setConnected(leagueId, true);
      if (lastSeq !== null) {
        ws.send(JSON.stringify({ op: 'subscribe', leagueId, afterSeq: lastSeq }));
      }
      ping = setInterval(() => ws.send(JSON.stringify({ op: 'ping' })), PING_MS);
    };
    ws.onmessage = (msg) => {
      let m: ServerMessage;
      try {
        m = JSON.parse(String(msg.data)) as ServerMessage;
      } catch {
        return;
      }
      if (m.type === 'pong' || !('leagueId' in m) || m.leagueId !== leagueId) return;
      if (lastSeq !== null && m.seq > lastSeq + 1) {
        invalidate([queryKeys.league(leagueId)]); // missed events: refetch everything
      }
      if (lastSeq === null || m.seq > lastSeq) lastSeq = m.seq;
      invalidate(invalidationsFor(leagueId, m.type));
    };
    ws.onclose = () => {
      clearInterval(ping);
      setConnected(leagueId, false);
      if (socket === ws) socket = null;
      schedule();
    };
    ws.onerror = () => ws.close();
  }

  void open();
  return () => {
    closed = true;
    clearTimeout(retry);
    clearInterval(ping);
    socket?.close();
    setConnected(leagueId, false);
  };
}

/** Keeps the league's realtime channel open while the component is mounted. */
export function useLeagueRealtime(leagueId: string | undefined) {
  const queryClient = useQueryClient();
  useEffect(() => {
    if (!REALTIME_ENABLED || !leagueId || typeof WebSocket === 'undefined') return;
    return connectLeague(leagueId, queryClient);
  }, [leagueId, queryClient]);
}
