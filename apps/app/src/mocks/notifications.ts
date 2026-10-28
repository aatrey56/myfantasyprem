import type { Schemas } from '@myfantasyprem/api-client';
import { http, type DefaultBodyType } from 'msw';

import * as data from './data';

/** Mirrors the server's list (server/internal/notifications): Personal, then League. */
export const NOTIFICATION_TYPES: Schemas['NotificationTypeInfo'][] = [
  { type: 'trade_offered', group: 'personal', label: 'Trade offers to me' },
  { type: 'trade_accepted', group: 'personal', label: 'My trade accepted' },
  { type: 'trade_declined', group: 'personal', label: 'My trade declined' },
  { type: 'trade_reviewed', group: 'personal', label: 'My trade approved or vetoed' },
  { type: 'trade_processed', group: 'personal', label: 'My trade processed' },
  { type: 'waivers_due', group: 'personal', label: 'Waivers due in 1 hour' },
  { type: 'free_agency_closing', group: 'personal', label: 'Free agency closes in 1 hour' },
  { type: 'waiver_result', group: 'personal', label: 'My waiver results' },
  { type: 'lineup_invalid', group: 'personal', label: 'Lineup locks in 1 hour with a problem' },
  { type: 'draft_starting', group: 'personal', label: 'Draft starting' },
  { type: 'draft_your_pick', group: 'personal', label: "It's my pick" },
  { type: 'player_goal', group: 'personal', label: 'My players: goals' },
  { type: 'player_assist', group: 'personal', label: 'My players: assists' },
  { type: 'player_own_goal', group: 'personal', label: 'My players: own goals' },
  { type: 'player_red_card', group: 'personal', label: 'My players: red cards' },
  {
    type: 'player_defensive_contribution',
    group: 'personal',
    label: 'My players: defensive contribution points',
  },
  { type: 'player_clean_sheet', group: 'personal', label: 'My players: clean sheets' },
  { type: 'player_news', group: 'personal', label: 'My players: injury and status news' },
  { type: 'league_trade_processed', group: 'league', label: 'Any trade in the league' },
  { type: 'opponent_goal', group: 'league', label: "Opponent's players: goals" },
  { type: 'opponent_assist', group: 'league', label: "Opponent's players: assists" },
  { type: 'opponent_own_goal', group: 'league', label: "Opponent's players: own goals" },
  { type: 'opponent_red_card', group: 'league', label: "Opponent's players: red cards" },
  {
    type: 'opponent_defensive_contribution',
    group: 'league',
    label: "Opponent's players: defensive contribution points",
  },
  { type: 'opponent_clean_sheet', group: 'league', label: "Opponent's players: clean sheets" },
  { type: 'opponent_news', group: 'league', label: "Opponent's players: injury and status news" },
];

/** League id → the types switched off there (everything starts on). */
const off = new Map<string, Set<string>>();
/** Read inbox ids. */
const read = new Set<string>();
const subscriptions = new Map<string, Schemas['PushSubscriptionCreate']>();
let subSeq = 1;

export function resetMockNotifications() {
  off.clear();
  read.clear();
  subscriptions.clear();
  subSeq = 1;
}

function myLeagues(): { id: string; name: string }[] {
  return [...data.leagues.values()].flatMap((l) => {
    const row = l.myLeague();
    return row ? [{ id: row.league.id, name: row.league.name }] : [];
  });
}

function preferences(): Schemas['NotificationPreferences'] {
  return {
    types: NOTIFICATION_TYPES,
    leagues: myLeagues().map((l) => ({
      leagueId: l.id,
      leagueName: l.name,
      settings: NOTIFICATION_TYPES.map((t) => ({
        type: t.type,
        enabled: !off.get(l.id)?.has(t.type),
      })),
    })),
  };
}

const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000).toISOString();

/** The demo inbox: a few minutes to a couple of days old. */
function inbox(): Schemas['Notification'][] {
  const lg = data.LEAGUE_ID;
  // An injured player's news, linked the way older server rows were (`/league/...`).
  const hurt = data.PLAYERS.find((p) => p.availability === 'injured')!;
  const items: Omit<Schemas['Notification'], 'readAt'>[] = [
    {
      id: 'ntf_01k7m2xq8z0000000000000006',
      type: 'player_goal',
      leagueId: lg,
      title: 'Goal: Watkins',
      body: 'Watkins scored for Aston Villa (+4 for Sam’s Squad).',
      url: `/leagues/${lg}/matchup`,
      createdAt: ago(4),
    },
    {
      id: 'ntf_01k7m2xq8z0000000000000005',
      type: 'trade_offered',
      leagueId: lg,
      title: 'Trade offer',
      body: 'Pressing Matters offered you a trade.',
      url: `/leagues/${lg}/players/trades`,
      createdAt: ago(38),
    },
    {
      id: 'ntf_01k7m2xq8z0000000000000004',
      type: 'player_news',
      leagueId: lg,
      title: `News: ${hurt.displayName}`,
      body: hurt.news ?? '',
      url: `/league/${lg}/players/${hurt.id}`,
      createdAt: ago(60 * 5),
    },
    {
      id: 'ntf_01k7m2xq8z0000000000000003',
      type: 'waiver_result',
      leagueId: lg,
      title: 'Waiver claim won',
      body: 'You claimed Mbeumo, dropping Fernandes.',
      url: `/league/${lg}/waivers`,
      createdAt: ago(60 * 26),
    },
    {
      id: 'ntf_01k7m2xq8z0000000000000002',
      type: 'waiver_result',
      leagueId: lg,
      title: 'Waiver claim lost',
      body: 'Isak went to a team higher in the waiver order.',
      url: `/leagues/${lg}/activity`,
      createdAt: ago(60 * 26 + 1),
    },
    {
      id: 'ntf_01k7m2xq8z0000000000000001',
      type: 'league_trade_processed',
      leagueId: lg,
      title: 'Trade processed',
      body: 'Kick & Rush and Total Football swapped Saka for Palmer.',
      url: `/leagues/${lg}/activity`,
      createdAt: ago(60 * 50),
    },
  ];
  // The two oldest start read.
  return items.map((n, i) => ({
    ...n,
    readAt: read.has(n.id) || i >= items.length - 2 ? ago(60 * 24) : null,
  }));
}

async function body<T>(request: Request): Promise<T | null> {
  const b = (await request.json().catch(() => null)) as T | null;
  return b && typeof b === 'object' ? b : null;
}

type Ok = <T>(value: T) => Response;

/** /v1/me/notifications*, /v1/me/push-subscriptions*; stateful until resetMocks. */
export function notificationHandlers(
  ok: Ok,
  noContent: () => Response,
  created: <T>(value: T, location: string) => Response,
  invalid: (detail: string) => Response,
) {
  return [
    http.get<never, never, DefaultBodyType>('*/v1/me/notifications', ({ request }) => {
      const unreadOnly = new URL(request.url).searchParams.get('unread') === 'true';
      const all = inbox();
      return ok({
        items: unreadOnly ? all.filter((n) => n.readAt === null) : all,
        unreadCount: all.filter((n) => n.readAt === null).length,
      });
    }),
    http.post<never, Schemas['NotificationReadRequest'], DefaultBodyType>(
      '*/v1/me/notifications/read',
      async ({ request }) => {
        const b = await body<Schemas['NotificationReadRequest']>(request);
        const ids = b?.all ? inbox().map((n) => n.id) : (b?.ids ?? []);
        for (const id of ids) read.add(id);
        return noContent();
      },
    ),
    http.get<never, never, DefaultBodyType>('*/v1/me/notifications/preferences', () =>
      ok(preferences()),
    ),
    http.put<never, Schemas['NotificationPreferencesUpdate'], DefaultBodyType>(
      '*/v1/me/notifications/preferences',
      async ({ request }) => {
        const b = await body<Schemas['NotificationPreferencesUpdate']>(request);
        if (!b) return invalid('The body must be a JSON object.');
        const known = new Set(NOTIFICATION_TYPES.map((t) => t.type));
        const unknown = b.settings?.find((s) => !known.has(s.type));
        if (unknown) return invalid(`Unknown notification type ${unknown.type}.`);
        const leagues = myLeagues()
          .map((l) => l.id)
          .filter((id) => !b.leagueId || id === b.leagueId);
        for (const id of leagues) {
          const set = off.get(id) ?? new Set<string>();
          if (b.all === true) set.clear();
          if (b.all === false) for (const t of known) set.add(t);
          for (const s of b.settings ?? []) {
            if (s.enabled) set.delete(s.type);
            else set.add(s.type);
          }
          off.set(id, set);
        }
        return ok(preferences());
      },
    ),
    http.post<never, Schemas['PushSubscriptionCreate'], DefaultBodyType>(
      '*/v1/me/push-subscriptions',
      async ({ request }) => {
        const b = await body<Schemas['PushSubscriptionCreate']>(request);
        if (!b || (b.kind === 'webpush' && !b.endpoint)) return invalid('endpoint is required.');
        const id = `psub_01k7m2xq8z${String(subSeq++).padStart(16, '0')}`;
        subscriptions.set(id, b);
        return created(
          { id, kind: b.kind, createdAt: new Date().toISOString() },
          `/v1/me/push-subscriptions/${id}`,
        );
      },
    ),
    http.delete<{ subscriptionId: string }>(
      '*/v1/me/push-subscriptions/:subscriptionId',
      ({ params }) => {
        subscriptions.delete(params.subscriptionId);
        return noContent();
      },
    ),
  ];
}
