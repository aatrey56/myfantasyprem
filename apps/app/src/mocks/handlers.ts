import type { Schemas } from '@myfantasyprem/api-client';
import {
  http,
  HttpResponse,
  type DefaultBodyType,
  type HttpResponseResolver,
  type PathParams,
} from 'msw';

import * as data from './data';
import { MockError } from './errors';
import type { ImportedLeague } from './imported';
import type { InSeasonLeague } from './league';
import type { PreseasonLeague } from './preseason';
import { notificationHandlers, resetMockNotifications } from './notifications';
import { crestPng, gkShirtPng, photoPng, shirtPng } from './images';
import { fixtureDifficulty, leagueProjections, startSit } from './projections';
import { gameweekAwards, leagueStatus, playersOfTheWeek } from './status';
import { LEAGUE_INPUT_MESSAGES, parseLeagueInput } from '@/api/league-input';
import type { PlayerSort } from '@/lib/playerSort';

type Meta = Schemas['Meta'];
/** `Problem` minus its open index signature, so MSW can type it as JSON. */
type Problem = Pick<
  Schemas['Problem'],
  'type' | 'title' | 'status' | 'detail' | 'instance' | 'errors'
>;

/** Dev switch: `localStorage.setItem('mfp.mockStale', '1')` makes every response stale. */
function isStale(): boolean {
  try {
    return (
      typeof document !== 'undefined' && globalThis.localStorage?.getItem('mfp.mockStale') === '1'
    );
  } catch {
    return false;
  }
}

function meta(scoreState?: Schemas['ScoreState']): Meta {
  const now = Date.now();
  const dataAsOf = new Date(isStale() ? now - 10 * 60_000 : now - 12_000);
  return {
    dataAsOf: dataAsOf.toISOString(),
    serverTime: new Date(now).toISOString(),
    stale: isStale(),
    ...(scoreState ? { scoreState } : {}),
  };
}

export function problem(
  status: number,
  title: string,
  detail: string,
  slug = title.toLowerCase().replace(/\s+/g, '-'),
  errors?: Schemas['ProblemError'][],
  extra?: Record<string, unknown>,
): HttpResponse<Problem> {
  const body: Problem = {
    type: `https://myfantasyprem.example/problems/${slug}`,
    title,
    status,
    detail,
    ...(errors ? { errors } : {}),
    ...extra,
  };
  return HttpResponse.json(body, {
    status,
    headers: { 'Content-Type': 'application/problem+json' },
  });
}

const notFound = (what: string) => problem(404, 'Not found', `${what} not found.`);
const badRequest = (detail: string) => problem(400, 'Bad request', detail);

const ID_PATTERN = (prefix: string) => new RegExp(`^${prefix}_[0-9a-hjkmnp-tv-z]{26}$`);
/** 10 characters; 8 for codes made before codes grew (the contract's InviteCode). */
const INVITE_CODE = /^[2-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{8}([2-9A-HJKMNP-TV-Za-hjkmnp-tv-z]{2})?$/;

function page<T>(items: T[], cursor: string | null, limit: number) {
  const offset = cursor ? Number.parseInt(atob(cursor), 10) : 0;
  const slice = items.slice(offset, offset + limit);
  const next = offset + limit < items.length ? btoa(String(offset + limit)) : null;
  return { slice, next };
}

function parseLimit(raw: string | null): number | undefined {
  if (raw === null) return 50;
  const n = Number(raw);
  return Number.isInteger(n) && n >= 1 && n <= 100 ? n : undefined;
}

const POSITIONS = ['GKP', 'DEF', 'MID', 'FWD'];

/** Turns a MockError thrown by a league action into problem+json. */
function fromError(e: unknown) {
  if (e instanceof MockError)
    return problem(e.status, e.title, e.detail, e.slug, e.errors, e.extra);
  throw e;
}

/**
 * Wraps a resolver so a thrown MockError becomes problem+json. Typed loosely
 * on purpose: each handler still declares its own params and body types.
 */
function guarded<P extends PathParams, B extends DefaultBodyType, R extends DefaultBodyType>(
  resolver: HttpResponseResolver<P, B, R>,
): HttpResponseResolver<P, B, R> {
  return async (info) => {
    try {
      return await resolver(info);
    } catch (e) {
      return fromError(e) as never;
    }
  };
}

// ---------------------------------------------------------------- imports

/** FPL league id the mocks treat as unknown upstream. */
export const UNKNOWN_FPL_LEAGUE_ID = 404404;

/**
 * FPL league id the mocks answer as a server without FPL's player data
 * would (POLLER_SOURCE=demo): previewed as not importable, import refused.
 */
export const NO_FPL_DATA_FPL_LEAGUE_ID = 409409;

const IMPORT_UNAVAILABLE = {
  status: 409,
  title: 'Import unavailable',
  detail:
    "This server isn't connected to live player data, so FPL Draft leagues can't be imported here.",
  slug: 'import-unavailable',
} as const;

interface MockImport {
  id: string;
  fplLeagueId: number;
  createdAt: number;
}

const imports = new Map<string, MockImport>();
let importSeq = 1;

/** Queued for 300 ms, running for about a second, then succeeded. */
function importView(job: MockImport): Schemas['Import'] {
  const age = Date.now() - job.createdAt;
  const total = 85;
  const status: Schemas['ImportStatus'] =
    age < 300 ? 'queued' : age < 1200 ? 'running' : 'succeeded';
  const done =
    status === 'queued'
      ? 0
      : status === 'running'
        ? Math.floor(((age - 300) / 900) * total)
        : total;
  return {
    id: job.id,
    fplLeagueId: job.fplLeagueId,
    status,
    progress: { done, total },
    error: null,
    leagueId: status === 'succeeded' ? data.IMPORTED_LEAGUE_ID : null,
    createdAt: new Date(job.createdAt).toISOString(),
    updatedAt: new Date(Math.min(Date.now(), job.createdAt + 1200)).toISOString(),
  };
}

/** Resolves a request's league reference, or the problem response to send. */
function resolveLeague(input: { draftLeagueUrl?: unknown; fplLeagueId?: unknown }) {
  const hasUrl = input.draftLeagueUrl !== undefined && input.draftLeagueUrl !== null;
  const hasId = input.fplLeagueId !== undefined && input.fplLeagueId !== null;
  if (hasUrl && hasId) return badRequest('Send a league link or a league ID, not both.');
  if (!hasUrl && !hasId) return badRequest(LEAGUE_INPUT_MESSAGES.empty);
  if (hasId) {
    const n = Number(input.fplLeagueId);
    if (!Number.isInteger(n) || n < 1) return badRequest(LEAGUE_INPUT_MESSAGES.invalid);
    return n === UNKNOWN_FPL_LEAGUE_ID ? notFound('FPL Draft league') : n;
  }
  const parsed = parseLeagueInput(String(input.draftLeagueUrl));
  if (parsed.kind === 'classic') {
    return problem(
      422,
      'Classic FPL league link',
      'That link is for a classic FPL league (fantasy.premierleague.com). Draft and classic league ids overlap, so paste your league link from draft.premierleague.com instead.',
      'classic-league-url',
    );
  }
  if (parsed.kind === 'invalid') return badRequest(parsed.reason);
  if (parsed.fplLeagueId === UNKNOWN_FPL_LEAGUE_ID) return notFound('FPL Draft league');
  return parsed.fplLeagueId;
}

const importHandlers = [
  http.get<never, never, Schemas['ImportPreviewResponse'] | Problem>(
    '*/v1/imports/preview',
    ({ request }) => {
      const q = new URL(request.url).searchParams;
      const id = resolveLeague({
        draftLeagueUrl: q.get('draftLeagueUrl') ?? undefined,
        fplLeagueId: q.get('fplLeagueId') ?? undefined,
      });
      if (typeof id !== 'number') return id;
      const unavailable = id === NO_FPL_DATA_FPL_LEAGUE_ID;
      return HttpResponse.json({
        data: {
          ...data.importPreview(),
          fplLeagueId: id,
          ...(unavailable
            ? {
                importable: false,
                problem: {
                  type: `https://myfantasyprem.example/problems/${IMPORT_UNAVAILABLE.slug}`,
                  title: IMPORT_UNAVAILABLE.title,
                  status: IMPORT_UNAVAILABLE.status,
                  detail: IMPORT_UNAVAILABLE.detail,
                },
              }
            : {}),
        },
        meta: meta(),
      });
    },
  ),

  http.get<{ importId: string }, never, Schemas['ImportResponse'] | Problem>(
    '*/v1/imports/:importId',
    ({ params }) => {
      if (!ID_PATTERN('imp').test(params.importId)) {
        return badRequest('importId is not a valid import id.');
      }
      const job = imports.get(params.importId);
      if (!job) return notFound('Import');
      return HttpResponse.json({ data: importView(job), meta: meta() });
    },
  ),

  http.post<never, Schemas['ImportRequest'], Schemas['ImportResponse'] | Problem>(
    '*/v1/imports',
    async ({ request }) => {
      const body = (await request.json().catch(() => null)) as Record<string, unknown> | null;
      if (!body || typeof body !== 'object') return badRequest('The body must be a JSON object.');
      const id = resolveLeague(body);
      if (typeof id !== 'number') return id;
      if (id === NO_FPL_DATA_FPL_LEAGUE_ID) {
        const { status, title, detail, slug } = IMPORT_UNAVAILABLE;
        return problem(status, title, detail, slug);
      }
      const seat = Number(body.seat);
      if (!Number.isInteger(seat) || seat < 1 || seat > data.importPreview().teamCount)
        return problem(
          422,
          'Invalid request',
          "seat must be one of the preview's seats.",
          'invalid-request',
        );
      // Idempotent per FPL league while an import is active or has succeeded.
      for (const job of imports.values()) {
        if (job.fplLeagueId === id && importView(job).status !== 'failed') {
          return HttpResponse.json({ data: importView(job), meta: meta() }, { status: 200 });
        }
      }
      const job: MockImport = {
        id: data.mockId('imp', importSeq++),
        fplLeagueId: id,
        createdAt: Date.now(),
      };
      imports.set(job.id, job);
      return HttpResponse.json(
        { data: importView(job), meta: meta() },
        { status: 202, headers: { Location: `/v1/imports/${job.id}`, 'Retry-After': '1' } },
      );
    },
  ),
];

/** Forget every mock import (tests). */
export function resetMockImports() {
  imports.clear();
  importSeq = 1;
}

// ---------------------------------------------------------------- helpers

type L = { leagueId: string };

export const ok = <T>(value: T, scoreState?: Schemas['ScoreState']) =>
  HttpResponse.json({ data: value, meta: meta(scoreState) });
/** A fixture PNG (./images) as /v1/assets/* serves images. */
export function imageResponse(base64: string) {
  const bytes = Uint8Array.from(atob(base64), (c) => c.charCodeAt(0));
  return new HttpResponse(bytes, {
    headers: { 'Content-Type': 'image/png', 'Cache-Control': 'public, max-age=604800' },
  });
}
const created = <T>(value: T, location: string) =>
  HttpResponse.json(
    { data: value, meta: meta() },
    { status: 201, headers: { Location: location } },
  );
const noContent = () => new HttpResponse(null, { status: 204 });

/** The demo user's watchlist by league: player id to addedAt. */
const watchlists = new Map<string, Map<string, string>>();
let watchClock = 0;
function watchlistOf(leagueId: string): Map<string, string> {
  let list = watchlists.get(leagueId);
  if (!list) {
    list = new Map();
    watchlists.set(leagueId, list);
  }
  return list;
}

// ------------------------------------------------- trade block, tags (Wave C)

type BlockEntry = {
  teamId: string;
  playerId: string;
  note: string;
  wanted: Schemas['Position'][];
  addedAt: string;
};

/** Each league's trade block, oldest first. Seeded with a few of other teams' players. */
const blocks = new Map<string, BlockEntry[]>();
let blockClock = 0;
const nextStamp = () => {
  blockClock = Math.max(blockClock + 1, Date.now());
  return new Date(blockClock).toISOString();
};

/** The team holding a player now, or null (as the server's owner). */
function holder(
  league: data.InSeasonLeague | data.ImportedLeague,
  playerId: string,
): string | null {
  const p = data.getPlayerRecord(playerId);
  return p ? (league.playerListItem(p).owner?.id ?? null) : null;
}

/**
 * The league's block with every entry whose team no longer holds the
 * player taken off (as the server's SaveState): a drop, a waiver run or a
 * trade clears it.
 */
function blockOf(league: data.InSeasonLeague | data.ImportedLeague): BlockEntry[] {
  let list = blocks.get(league.id);
  if (!list) {
    // Three other teams each offer their best-ranked forward, midfielder and defender.
    const mine = league.viewer().team?.id;
    const others = [...new Set(data.PLAYERS.map((p) => holder(league, p.id)))].filter(
      (t): t is string => Boolean(t) && t !== mine,
    );
    const seeds: [number, Schemas['Position'], string, Schemas['Position'][]][] = [
      [0, 'FWD', 'Want a keeper back', ['GKP']],
      [1, 'MID', '', []],
      [2, 'DEF', 'Open to offers', ['MID', 'FWD']],
    ];
    list = [];
    for (const [i, pos, note, wanted] of seeds) {
      const team = others[i];
      const p = team
        ? data.PLAYERS.find((x) => x.position === pos && holder(league, x.id) === team)
        : undefined;
      if (team && p) {
        list.push({
          teamId: team,
          playerId: p.id,
          note,
          wanted,
          addedAt: new Date(data.NOW - (3 - i) * data.HOUR).toISOString(),
        });
      }
    }
    blocks.set(league.id, list);
  }
  const kept = list.filter((e) => holder(league, e.playerId) === e.teamId);
  blocks.set(league.id, kept);
  return kept;
}

function blockEntryBody(
  league: data.InSeasonLeague | data.ImportedLeague,
  e: BlockEntry,
): Schemas['TradeBlockEntry'] {
  const p = data.getPlayerRecord(e.playerId)!;
  return {
    player: data.toPlayerRef(p),
    team: league.playerListItem(p).owner!,
    note: e.note,
    wanted: e.wanted,
    addedAt: e.addedAt,
  };
}

const POSITION_ORDER: Schemas['Position'][] = ['GKP', 'DEF', 'MID', 'FWD'];
const TAGS: Schemas['PlayerTag'][] = ['hot', 'cold', 'target', 'sell', 'avoid'];

/** The demo user's tags by league: player id to tag and time. */
const tagLists = new Map<string, Map<string, { tag: Schemas['PlayerTag']; updatedAt: string }>>();
function tagsOf(leagueId: string) {
  let list = tagLists.get(leagueId);
  if (!list) {
    list = new Map();
    tagLists.set(leagueId, list);
  }
  return list;
}

/** Chance of playing from the news line, as the server falls back to. */
function chanceOf(p: data.PlayerRecord): number | null {
  if (p.availability === 'available') return null;
  if (p.availability !== 'doubtful') return 0;
  const m = p.news?.match(/(\d{1,3})%/);
  const n = m ? Number(m[1]) : NaN;
  return Number.isFinite(n) && n <= 100 ? n : null;
}

const SORT_VALUES: string[] = [
  'total_points',
  'gameweek_points',
  'form',
  'form_rise',
  'form_fall',
  'points_per_match',
  'draft_rank',
  'name',
  'goals',
  'assists',
  'bonus',
  'bps',
  'clean_sheets',
  'creativity',
  'goals_conceded',
  'ict_index',
  'influence',
  'minutes',
  'own_goals',
  'penalties_missed',
  'penalties_saved',
  'red_cards',
  'saves',
  'threat',
  'yellow_cards',
  'starts',
  'expected_goals',
  'expected_assists',
  'expected_goal_involvements',
  'expected_goals_conceded',
  'clearances_blocks_interceptions',
  'recoveries',
  'tackles',
  'defensive_contribution',
];

/**
 * Orders two list items by a sort: desc is biggest first (draft rank and
 * name: rank 1 first, A to Z); asc reverses. No value sorts last either way.
 * 0 is a tie (the caller breaks it by total points, then name).
 */
function comparePlayers(
  a: Schemas['PlayerListItem'],
  b: Schemas['PlayerListItem'],
  sort: PlayerSort,
  order: string,
): number {
  const flip = order === 'asc' ? -1 : 1;
  if (sort === 'name') {
    return flip * a.player.displayName.localeCompare(b.player.displayName);
  }
  if (a.sortValue === null || b.sortValue === null) {
    return a.sortValue === b.sortValue ? 0 : a.sortValue === null ? 1 : -1;
  }
  const natural =
    sort === 'draft_rank' || sort === 'form_fall'
      ? a.sortValue - b.sortValue
      : b.sortValue - a.sortValue;
  return flip * natural;
}

const notMember = () => problem(403, 'Not a member', 'You are not in this league.', 'not-a-member');
const notCommissioner = () =>
  problem(403, 'Commissioner only', 'Only the commissioner can do that.', 'not-commissioner');
const seasonNotStarted = () =>
  problem(409, 'Season not started', 'This league has not drafted yet.', 'season-not-started');

/**
 * Finds a league the demo user can see: 400 for a malformed id, 404 for an
 * unknown one, 403 when the user is not in it.
 */
function findLeague(
  leagueId: string,
  { member = true } = {},
): { error: HttpResponse<Problem> } | { league: data.MockLeague } {
  if (!ID_PATTERN('lg').test(leagueId)) {
    return { error: badRequest('leagueId is not a valid league id.') };
  }
  const league = data.leagues.get(leagueId);
  if (!league) return { error: notFound('League') };
  if (member && !league.viewer().team) return { error: notMember() };
  // Let the simulated managers answer trade offers that are due.
  if (data.isInSeason(league)) league.settleTrades();
  return { league };
}

const badId = (name: string, prefix: string, value: string) =>
  ID_PATTERN(prefix).test(value) ? null : badRequest(`${name} is not a valid ${prefix}_ id.`);

async function jsonBody<T>(request: Request): Promise<T | null> {
  const body = (await request.json().catch(() => null)) as T | null;
  return body && typeof body === 'object' ? body : null;
}

function leagueBody(league: data.MockLeague): Schemas['League'] {
  const commissioner = league.members().find((m) => m.role === 'commissioner')!;
  return {
    league: league.summary(),
    settings: league.settings,
    commissioner: commissioner.user,
    viewer: league.viewer(),
    createdAt: data.isInSeason(league) ? league.createdAt : league.members()[0]!.joinedAt,
  };
}

// ---------------------------------------------------------------- auth & me

const demoUser: Schemas['User'] = {
  id: data.DEMO_USER_ID,
  displayName: data.DEMO_USER_NAME,
  email: 'sam@example.com',
  avatarColor: '#2f6fed',
  authProviders: ['apple', 'email'],
  createdAt: new Date(data.NOW - 90 * 24 * data.HOUR).toISOString(),
};

function session(isNewUser = false): Schemas['Session'] {
  const now = Date.now();
  return {
    accessToken: 'mock-access-token',
    tokenType: 'Bearer',
    expiresAt: new Date(now + data.HOUR).toISOString(),
    refreshToken: 'mock-refresh-token-0001',
    refreshExpiresAt: new Date(now + 60 * 24 * data.HOUR).toISOString(),
    user: demoUser,
    isNewUser,
  };
}

const EMAIL = /^[^@\s]+@[^@\s]+\.[^@\s]+$/;

const authHandlers = [
  http.post<never, Schemas['AppleSignInRequest'], DefaultBodyType>(
    '*/v1/auth/apple',
    async ({ request }) => {
      const body = await jsonBody<Schemas['AppleSignInRequest']>(request);
      if (!body?.identityToken || !body.authorizationCode || !body.nonce) {
        return problem(
          422,
          'Invalid request',
          'identityToken, authorizationCode and nonce are required.',
          'invalid-request',
        );
      }
      return ok(session());
    },
  ),
  http.post<never, Schemas['GoogleSignInRequest'], DefaultBodyType>(
    '*/v1/auth/google',
    async ({ request }) => {
      const body = await jsonBody<Schemas['GoogleSignInRequest']>(request);
      if (!body?.idToken)
        return problem(422, 'Invalid request', 'idToken is required.', 'invalid-request');
      return ok(session());
    },
  ),
  http.post<never, Schemas['EmailSignInRequest'], DefaultBodyType>(
    '*/v1/auth/email',
    async ({ request }) => {
      const body = await jsonBody<Schemas['EmailSignInRequest']>(request);
      if (!body || typeof body.email !== 'string' || !EMAIL.test(body.email)) {
        return problem(
          422,
          'Invalid request',
          'email must be an email address.',
          'invalid-request',
          [{ code: 'invalid_email', message: 'Enter a valid email address.', field: '/email' }],
        );
      }
      if (body.next !== undefined && !/^(\/[A-Za-z0-9_~-][A-Za-z0-9_.~-]*)+$/.test(body.next)) {
        return problem(
          422,
          'Invalid request',
          'next must be a path in the app.',
          'invalid-request',
          [
            {
              code: 'invalid_next',
              message: 'next must be a path like /join/K7QM2XRP4D.',
              field: '/next',
            },
          ],
        );
      }
      return HttpResponse.json(
        { data: { expiresAt: new Date(Date.now() + 15 * 60_000).toISOString() }, meta: meta() },
        { status: 202 },
      );
    },
  ),
  http.post<never, Schemas['EmailVerifyRequest'], DefaultBodyType>(
    '*/v1/auth/email/verify',
    async ({ request }) => {
      const body = await jsonBody<Schemas['EmailVerifyRequest']>(request);
      // Any 16+ character token signs in; "expired-token-0000" shows the failure.
      if (!body?.token || body.token.length < 16 || body.token.startsWith('expired')) {
        return problem(
          401,
          'Unauthorized',
          'This sign-in link has expired or was already used. Request a new one.',
          'unauthorized',
        );
      }
      return ok(session(false));
    },
  ),
  http.post<never, Schemas['EmailCodeVerifyRequest'], DefaultBodyType>(
    '*/v1/auth/email/code',
    async ({ request }) => {
      const body = await jsonBody<Schemas['EmailCodeVerifyRequest']>(request);
      if (!body || !EMAIL.test(body.email ?? '') || !/^[0-9]{6}$/.test(body.code ?? '')) {
        return problem(
          422,
          'Invalid request',
          'email and a 6-digit code are required.',
          'invalid-request',
        );
      }
      // Any code signs in; "000000" shows the wrong-code answer.
      if (body.code === '000000') {
        return problem(
          401,
          'Unauthorized',
          'That code is wrong or has expired. Check the latest email, or request a new one.',
          'unauthorized',
        );
      }
      return ok(session(false));
    },
  ),
  http.post<never, Schemas['RefreshRequest'], DefaultBodyType>(
    '*/v1/auth/refresh',
    async ({ request }) => {
      const body = await jsonBody<Schemas['RefreshRequest']>(request);
      if (!body?.refreshToken)
        return problem(401, 'Unauthorized', 'Sign in again.', 'unauthorized');
      return ok(session());
    },
  ),
  http.post('*/v1/auth/logout', () => noContent()),

  http.get<never, never, DefaultBodyType>('*/v1/me', () => ok(demoUser)),
  http.patch<never, Schemas['UserUpdate'], DefaultBodyType>('*/v1/me', async ({ request }) => {
    const body = await jsonBody<Schemas['UserUpdate']>(request);
    const errors: Schemas['ProblemError'][] = [];
    const name = body?.displayName?.trim();
    if (body?.displayName !== undefined && (!name || name.length > 32)) {
      errors.push({
        code: 'invalid_length',
        message: 'Display name must be 1 to 32 characters.',
        field: '/displayName',
      });
    }
    if (body?.avatarColor && !/^#[0-9a-fA-F]{6}$/.test(body.avatarColor)) {
      errors.push({
        code: 'invalid_color',
        message: 'avatarColor must look like #2f6fed.',
        field: '/avatarColor',
      });
    }
    if (!body || errors.length) {
      return problem(
        422,
        'Invalid request',
        'The profile is not valid.',
        'invalid-request',
        errors,
      );
    }
    if (name) demoUser.displayName = name;
    if (body.avatarColor !== undefined) demoUser.avatarColor = body.avatarColor;
    return ok(demoUser);
  }),
  http.delete('*/v1/me', () => noContent()),
  http.get<never, never, DefaultBodyType>('*/v1/me/leagues', () =>
    ok(
      [...data.leagues.values()].flatMap((l) => {
        const row = l.myLeague();
        return row ? [row] : [];
      }),
    ),
  ),
];

// ---------------------------------------------------------------- leagues

const leagueHandlers = [
  http.post<never, Schemas['LeagueCreate'], DefaultBodyType>(
    '*/v1/leagues',
    async ({ request }) => {
      const body = await jsonBody<Schemas['LeagueCreate']>(request);
      const errors: Schemas['ProblemError'][] = [];
      const len = (s: unknown) => (typeof s === 'string' ? s.trim().length : 0);
      if (!body || len(body.name) < 3 || len(body.name) > 40)
        errors.push({
          code: 'invalid_length',
          message: 'Name must be 3 to 40 characters.',
          field: '/name',
        });
      if (!body || !Number.isInteger(body.teamCount) || body.teamCount < 2 || body.teamCount > 20)
        errors.push({
          code: 'out_of_range',
          message: 'A league has 2 to 20 teams.',
          field: '/teamCount',
        });
      if (!body || len(body.teamName) < 1 || len(body.teamName) > 32)
        errors.push({
          code: 'invalid_length',
          message: 'Team name must be 1 to 32 characters.',
          field: '/teamName',
        });
      if (!body || errors.length) {
        return problem(
          422,
          'Invalid request',
          'The league is not valid.',
          'invalid-request',
          errors,
        );
      }
      const league = data.createMockLeague(body);
      return created(leagueBody(league), `/v1/leagues/${league.id}`);
    },
  ),

  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const live = data.isInSeason(found.league);
    return ok(found.league.home(), live ? 'live' : undefined);
  }),

  http.patch<L, Schemas['LeagueUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId',
    guarded(async ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const league = found.league;
      if (league.viewer().role !== 'commissioner') return notCommissioner();
      const body = await jsonBody<Schemas['LeagueUpdate']>(request);
      if (!body || (body.name === undefined && body.settings === undefined)) {
        return problem(422, 'Invalid request', 'Send name or settings.', 'invalid-request');
      }
      const s = body.settings;
      const started = league.status !== 'pre_draft';
      const lockedField = started
        ? s?.teamCount !== undefined
          ? 'teamCount'
          : s?.trades?.review !== undefined
            ? 'trades.review'
            : s?.draft !== undefined
              ? 'draft'
              : s?.startGameweek !== undefined
                ? 'startGameweek'
                : null
        : null;
      if (lockedField) {
        return problem(
          409,
          'Setting locked',
          `${lockedField} cannot change once the draft has started.`,
          'setting-locked',
        );
      }
      if (body.name !== undefined) {
        const name = body.name.trim();
        if (name.length < 3 || name.length > 40)
          return problem(
            422,
            'Invalid request',
            'Name must be 3 to 40 characters.',
            'invalid-request',
          );
        league.rename(name);
      }
      const settings = league.settings;
      if (s?.teamCount !== undefined) {
        if (s.teamCount < league.members().length)
          return problem(
            409,
            'Too many teams',
            'The league already has more teams than that.',
            'too-many-teams',
          );
        settings.teamCount = s.teamCount;
      }
      if (s?.draft?.pickTimerSeconds !== undefined)
        settings.draft.pickTimerSeconds = s.draft.pickTimerSeconds;
      if (s?.draft?.startsAt !== undefined) settings.draft.startsAt = s.draft.startsAt;
      if (s?.trades) {
        const t = s.trades;
        if (t.review !== undefined) {
          settings.trades.review = t.review;
          settings.trades.reviewHours = t.review === 'none' ? 0 : settings.trades.reviewHours || 24;
        }
        if (t.reviewHours !== undefined) settings.trades.reviewHours = t.reviewHours;
        if (t.vetoThreshold !== undefined) settings.trades.vetoThreshold = t.vetoThreshold;
        if (t.deadlineGameweek !== undefined) settings.trades.deadlineGameweek = t.deadlineGameweek;
      }
      if (s?.startGameweek !== undefined) settings.startGameweek = s.startGameweek;
      if (s?.waivers?.order !== undefined) {
        if (s.waivers.order !== 'reverse_standings' && s.waivers.order !== 'rolling')
          return problem(422, 'Invalid request', 'Unknown waiver order.', 'invalid-request');
        settings.waivers.order = s.waivers.order;
      }
      return ok(leagueBody(league));
    }),
  ),

  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/members', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    return ok(found.league.members());
  }),

  http.delete<L & { userId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/members/:userId',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('userId', 'usr', params.userId);
      if (bad) return bad;
      const league = found.league;
      if (league.viewer().role !== 'commissioner') return notCommissioner();
      if (data.isInSeason(league)) {
        if (params.userId === data.DEMO_USER_ID)
          return problem(
            409,
            'Commissioner cannot leave',
            'Hand the commissioner role to someone else first.',
            'commissioner-cannot-leave',
          );
        if (!league.members().some((m) => m.user.id === params.userId)) return notFound('Member');
        // In-season mock leagues do not model orphaned teams; nothing changes.
        return noContent();
      }
      league.removeMember(params.userId);
      return noContent();
    }),
  ),

  http.post<L, Schemas['CommissionerTransfer'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/commissioner',
    guarded(async ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const league = found.league;
      if (league.viewer().role !== 'commissioner') return notCommissioner();
      const body = await jsonBody<Schemas['CommissionerTransfer']>(request);
      if (!body || typeof body.userId !== 'string')
        return problem(422, 'Invalid request', 'userId is required.', 'invalid-request');
      const bad = badId('userId', 'usr', body.userId);
      if (bad) return bad;
      league.transferCommissioner(body.userId);
      return ok(leagueBody(league));
    }),
  ),

  http.post<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/leave',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const league = found.league;
      if (league.viewer().role === 'commissioner') {
        return problem(
          409,
          'Commissioner cannot leave',
          'Hand the commissioner role to someone else first.',
          'commissioner-cannot-leave',
        );
      }
      if (!data.isInSeason(league)) league.leave();
      // In-season mock leagues do not model orphaned teams; nothing changes.
      return noContent();
    }),
  ),

  http.get<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/invites',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const league = found.league;
      if (league.viewer().role !== 'commissioner') return notCommissioner();
      return ok(data.isInSeason(league) ? [] : league.invites());
    }),
  ),

  http.post<L, Schemas['InviteCreate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/invites',
    guarded(async ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const league = found.league;
      if (league.viewer().role !== 'commissioner') return notCommissioner();
      if (data.isInSeason(league)) {
        return problem(
          409,
          'League full',
          'Every team in this league has a manager.',
          'league-full',
        );
      }
      const body = (await jsonBody<Schemas['InviteCreate']>(request)) ?? {};
      const inv = league.createInvite(body.maxUses, body.expiresInHours);
      return created(inv, `/v1/leagues/${league.id}/invites/${inv.id}`);
    }),
  ),

  http.delete<L & { inviteId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/invites/:inviteId',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('inviteId', 'inv', params.inviteId);
      if (bad) return bad;
      const league = found.league;
      if (league.viewer().role !== 'commissioner') return notCommissioner();
      if (data.isInSeason(league)) return notFound('Invite');
      league.revokeInvite(params.inviteId);
      return noContent();
    }),
  ),

  http.get<{ code: string }, never, DefaultBodyType>('*/v1/invites/:code', ({ params }) => {
    if (!INVITE_CODE.test(params.code))
      return badRequest('code must be an 8-character invite code.');
    for (const l of data.leagues.values()) {
      const preview = data.isInSeason(l) ? undefined : l.invitePreview(params.code);
      if (preview) return ok(preview);
    }
    return notFound('Invite');
  }),

  http.post<{ code: string }, Schemas['InviteAccept'], DefaultBodyType>(
    '*/v1/invites/:code/accept',
    guarded(async ({ params, request }) => {
      if (!INVITE_CODE.test(params.code))
        return badRequest('code must be an 8-character invite code.');
      const body = await jsonBody<Schemas['InviteAccept']>(request);
      const teamName = body?.teamName?.trim() ?? '';
      if (teamName.length < 1 || teamName.length > 32) {
        return problem(
          422,
          'Invalid request',
          'Team name must be 1 to 32 characters.',
          'invalid-request',
        );
      }
      for (const l of data.leagues.values()) {
        if (data.isInSeason(l) || !l.invitePreview(params.code)) continue;
        const team = l.acceptInvite(params.code, teamName);
        return created<Schemas['Membership']>(
          { league: l.summary(), team, role: 'member' },
          `/v1/leagues/${l.id}`,
        );
      }
      return notFound('Invite');
    }),
  ),

  http.patch<L & { teamId: string }, Schemas['TeamUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/teams/:teamId',
    async ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('teamId', 'tm', params.teamId);
      if (bad) return bad;
      const team = found.league.teamById(params.teamId);
      if (!team) return notFound('Team');
      if (team.id !== found.league.viewer().team?.id) {
        return problem(
          403,
          'Not your team',
          "Only the team's manager can rename it.",
          'not-team-owner',
        );
      }
      const body = await jsonBody<Schemas['TeamUpdate']>(request);
      const name = body?.name?.trim() ?? '';
      if (name.length < 1 || name.length > 32) {
        return problem(
          422,
          'Invalid request',
          'Team name must be 1 to 32 characters.',
          'invalid-request',
        );
      }
      team.name = name;
      return ok(team);
    },
  ),

  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/standings', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    return ok(found.league.standings());
  }),

  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/schedule', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    return ok(data.isInSeason(found.league) ? found.league.schedule() : []);
  }),

  http.get<L & { gw: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/gameweeks/:gw/matchups',
    ({ params }) => {
      const gw = Number(params.gw);
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      if (!Number.isInteger(gw) || gw < 1 || gw > data.LAST_GW) {
        return badRequest('gw must be between 1 and 38.');
      }
      const league = found.league;
      if (!data.isInSeason(league) || !league.hasScores(gw))
        return notFound(`Gameweek ${gw} matchups`);
      return ok(league.matchupSummaries(gw), league.scoreStateFor(gw));
    },
  ),

  http.get<L & { gw: string; matchupId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/gameweeks/:gw/matchups/:matchupId/preview',
    ({ params }) => {
      const gw = Number(params.gw);
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('matchupId', 'mu', params.matchupId);
      if (bad) return bad;
      if (!Number.isInteger(gw) || gw < 1 || gw > data.LAST_GW)
        return badRequest('gw must be between 1 and 38.');
      const league = found.league;
      const m = league
        .schedule()
        .find((w) => w.gameweek === gw)
        ?.matchups.find((x) => x.id === params.matchupId);
      if (!m) return notFound('Matchup');
      const side = (o: Schemas['Opponent']): Schemas['PreviewSide'] => {
        if (o.kind !== 'team') return { kind: 'average', starters: [], bench: [] };
        // Another team's lineup stays private until its deadline: its latest locked one.
        const lineup = league.previewLineup(o.team.id, gw);
        const slot = (player: Schemas['PlayerRef'], i: number): Schemas['PreviewSlot'] => ({
          slot: i + 1,
          player,
          fixtures: league
            .fixturesInGameweek(player.id, gw)
            .filter((f) => f.gameweek === gw && f.kickoffAt !== null)
            .map((f) => ({
              fixtureId: f.fixtureId,
              opponent: f.opponent,
              isHome: f.isHome,
              kickoffAt: f.kickoffAt!,
            })),
        });
        return {
          kind: 'team',
          team: o.team,
          formation: lineup.formation,
          ...(lineup.lineupGameweek !== undefined && { lineupGameweek: lineup.lineupGameweek }),
          starters: lineup.starters.map((p, i) => slot(p, i)),
          bench: lineup.bench.map((p, i) => slot(p, i + 11)),
        };
      };
      return ok({ id: m.id, gameweek: gw, home: side(m.home), away: side(m.away) });
    },
  ),

  http.get<L & { gw: string; matchupId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/gameweeks/:gw/matchups/:matchupId',
    ({ params }) => {
      const gw = Number(params.gw);
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('matchupId', 'mu', params.matchupId);
      if (bad) return bad;
      const league = found.league;
      const detail =
        data.isInSeason(league) && Number.isInteger(gw)
          ? league.matchupDetail(gw, params.matchupId)
          : undefined;
      if (!detail) return notFound('Matchup');
      return ok(detail, detail.state);
    },
  ),

  http.get<L & { teamId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/teams/:teamId',
    ({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('teamId', 'tm', params.teamId);
      if (bad) return bad;
      const team = found.league.teamDetail(params.teamId);
      if (!team) return notFound('Team');
      return ok(team);
    },
  ),

  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/players', ({ params, request }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const league = found.league;
    const q = new URL(request.url).searchParams;
    const limit = parseLimit(q.get('limit'));
    const position = q.get('position');
    if (limit === undefined) return badRequest('limit must be between 1 and 100.');
    if (position && !POSITIONS.includes(position)) {
      return badRequest('position must be GKP, DEF, MID or FWD.');
    }
    const status = q.get('status') ?? (q.get('available') === 'true' ? 'available' : 'all');
    const club = q.get('club')?.toUpperCase();
    const teamId = q.get('teamId');
    const needle = q.get('q')?.toLowerCase();
    const sort = q.get('sort') ?? 'total_points';
    if (!SORT_VALUES.includes(sort)) return badRequest('sort is not one of the listed values.');
    const order = q.get('order') ?? 'desc';
    if (order !== 'asc' && order !== 'desc') return badRequest('order must be asc or desc.');
    const starred = q.get('watchlist') === 'true' ? watchlistOf(params.leagueId) : null;
    const tag = q.get('tag');
    if (tag !== null && !TAGS.includes(tag as Schemas['PlayerTag'])) {
      return badRequest('tag must be one of hot, cold, target, sell and avoid.');
    }
    const tagged = tag ? tagsOf(params.leagueId) : null;
    const items = data.PLAYERS.filter(
      (p) =>
        (!starred || starred.has(p.id)) &&
        (!tagged || tagged.get(p.id)?.tag === tag) &&
        (!position || p.position === position) &&
        (!club || p.club.code === club) &&
        (!needle || p.displayName.toLowerCase().includes(needle)),
    )
      .map((p) => ({
        ...league.playerListItem(p),
        sortValue: data.sortValueOf(p.id, sort as PlayerSort),
      }))
      .filter(
        (item) =>
          (status === 'all' ||
            (status === 'available' && item.rosterStatus !== 'rostered') ||
            item.rosterStatus === status) &&
          (!teamId || item.owner?.id === teamId),
      )
      .sort(
        (a, b) =>
          comparePlayers(a, b, sort as PlayerSort, order) ||
          b.totalPoints - a.totalPoints ||
          a.player.displayName.localeCompare(b.player.displayName),
      );
    const { slice, next } = page(items, q.get('cursor'), limit);
    return HttpResponse.json({ data: slice, meta: { ...meta(), nextCursor: next } });
  }),

  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/watchlist', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const entries = [...watchlistOf(params.leagueId).entries()]
      .sort((a, b) => b[1].localeCompare(a[1]) || a[0].localeCompare(b[0]))
      .flatMap(([id, addedAt]) => {
        const p = data.getPlayerRecord(id);
        return p ? [{ player: data.toPlayerRef(p), addedAt }] : [];
      });
    return ok(entries);
  }),
  http.put<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/watchlist/:playerId',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      const p = data.getPlayerRecord(params.playerId);
      if (!p) return notFound('Player');
      const list = watchlistOf(params.leagueId);
      if (!list.has(p.id)) {
        if (list.size >= 200) {
          return problem(409, 'Watchlist full', 'A watchlist holds 200 players.', 'watchlist-full');
        }
        // Strictly increasing, so the newest is first even within a millisecond.
        watchClock = Math.max(watchClock + 1, Date.now());
        list.set(p.id, new Date(watchClock).toISOString());
      }
      return ok({ player: data.toPlayerRef(p), addedAt: list.get(p.id)! });
    }),
  ),
  http.delete<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/watchlist/:playerId',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      watchlistOf(params.leagueId).delete(params.playerId);
      return noContent();
    }),
  ),

  // ----------------------------------------------- trade block, news, tags
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/trade-block', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const league = found.league;
    if (!data.isInSeason(league)) return ok([]);
    return ok(blockOf(league).map((e) => blockEntryBody(league, e)));
  }),
  http.put<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/trade-block/:playerId',
    guarded(async ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      const league = found.league;
      if (!data.isInSeason(league)) return seasonNotStarted();
      const body = (await jsonBody<Schemas['TradeBlockUpdate']>(request)) ?? {};
      const note = (body.note ?? '').trim();
      if ([...note].length > 140) {
        return problem(
          422,
          'Check the trade block entry',
          'A note is at most 140 characters.',
          'validation-failed',
        );
      }
      const wanted = POSITION_ORDER.filter((p) => body.wanted?.includes(p));
      if ((body.wanted ?? []).some((p) => !POSITION_ORDER.includes(p))) {
        return problem(
          422,
          'Check the trade block entry',
          'Wanted positions are GKP, DEF, MID and FWD.',
          'validation-failed',
        );
      }
      const mine = league.viewer().team!.id;
      if (holder(league, params.playerId) !== mine) {
        return problem(
          409,
          'Not on your roster',
          'Only players on your roster can go on your trade block.',
          'not-on-roster',
        );
      }
      const list = blockOf(league);
      let e = list.find((x) => x.playerId === params.playerId);
      if (e) Object.assign(e, { note, wanted });
      else {
        e = { teamId: mine, playerId: params.playerId, note, wanted, addedAt: nextStamp() };
        list.push(e);
      }
      return ok(blockEntryBody(league, e));
    }),
  ),
  http.delete<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/trade-block/:playerId',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      const league = found.league;
      if (!data.isInSeason(league)) return seasonNotStarted();
      const list = blockOf(league);
      const i = list.findIndex((x) => x.playerId === params.playerId);
      if (i >= 0 && list[i]!.teamId !== league.viewer().team?.id) {
        return problem(
          403,
          'Not your player',
          'Only his team can take a player off the trade block.',
          'not-your-player',
        );
      }
      if (i >= 0) list.splice(i, 1);
      return noContent();
    }),
  ),
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/news', ({ params, request }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const league = found.league;
    const q = new URL(request.url).searchParams;
    const teamId = q.get('teamId');
    const limit = parseLimit(q.get('limit'));
    if (limit === undefined) return badRequest('limit must be between 1 and 100.');
    if (teamId) {
      const bad = badId('teamId', 'tm', teamId);
      if (bad) return bad;
    }
    const owners = (p: data.PlayerRecord) =>
      data.isInSeason(league) ? (league.playerListItem(p).owner ?? null) : null;
    const items = data.PLAYERS.filter((p) => p.news && (!teamId || owners(p)?.id === teamId))
      .sort(
        (a, b) =>
          (b.newsAt ?? '').localeCompare(a.newsAt ?? '') ||
          a.displayName.localeCompare(b.displayName),
      )
      .slice(0, limit)
      .map((p) => ({
        player: data.toPlayerRef(p),
        news: p.news!,
        newsAt: p.newsAt ?? null,
        chanceOfPlaying: chanceOf(p),
        owner: owners(p),
      }));
    return ok(items);
  }),
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/tags', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const entries = [...tagsOf(params.leagueId).entries()]
      .sort((a, b) => b[1].updatedAt.localeCompare(a[1].updatedAt) || a[0].localeCompare(b[0]))
      .flatMap(([id, t]) => {
        const p = data.getPlayerRecord(id);
        return p ? [{ player: data.toPlayerRef(p), tag: t.tag, updatedAt: t.updatedAt }] : [];
      });
    return ok(entries);
  }),
  http.put<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/tags/:playerId',
    guarded(async ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      const body = await jsonBody<Schemas['PlayerTagUpdate']>(request);
      if (!body || !TAGS.includes(body.tag)) {
        return problem(
          422,
          'Invalid request',
          'tag must be one of hot, cold, target, sell and avoid.',
          'invalid-request',
        );
      }
      const p = data.getPlayerRecord(params.playerId);
      if (!p) return notFound('Player');
      const list = tagsOf(params.leagueId);
      const had = list.get(p.id);
      if (!had || had.tag !== body.tag) list.set(p.id, { tag: body.tag, updatedAt: nextStamp() });
      const t = list.get(p.id)!;
      return ok({ player: data.toPlayerRef(p), tag: t.tag, updatedAt: t.updatedAt });
    }),
  ),
  http.delete<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/tags/:playerId',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      tagsOf(params.leagueId).delete(params.playerId);
      return noContent();
    }),
  ),

  http.get<L & { playerId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/players/:playerId',
    ({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('playerId', 'plr', params.playerId);
      if (bad) return bad;
      const detail = found.league.playerDetail(params.playerId);
      if (!detail) return notFound('Player');
      return ok(detail, 'live');
    },
  ),

  http.get<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/transactions',
    ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const q = new URL(request.url).searchParams;
      const limit = parseLimit(q.get('limit'));
      if (limit === undefined) return badRequest('limit must be between 1 and 100.');
      const kind = q.get('kind');
      const all = found.league.transactions();
      const items = all.filter((t) => !kind || t.kind === kind);
      const { slice, next } = page([...items], q.get('cursor'), limit);
      return HttpResponse.json({ data: slice, meta: { ...meta(), nextCursor: next } });
    },
  ),

  http.get<never, never, DefaultBodyType>('*/v1/clubs', () => ok([...data.CLUBS_BY_NAME])),

  // Branded images on (scenario `branded-off` turns them off): small fixture
  // PNGs, never real crests, shirts or photos.
  http.get<never, never, DefaultBodyType>('*/v1/config', () =>
    ok({
      brandedAssets: true,
      // A throwaway P-256 key: the mocks never send a push.
      webPushPublicKey:
        'BNmYBLtPWZMm8yvbbIb_bcpdTnA-G-bRy3geLONz6j8KkyQkGHBrbM_tMD4aIr-S9ckpYux3utXJAosiIzi1h0E',
    }),
  ),
  http.get('*/v1/assets/players/:playerId/photo', () => imageResponse(photoPng)),
  http.get('*/v1/assets/clubs/:clubCode/crest', () => imageResponse(crestPng)),
  http.get('*/v1/assets/clubs/:clubCode/shirt', ({ request }) =>
    imageResponse(new URL(request.url).searchParams.get('gk') === 'true' ? gkShirtPng : shirtPng),
  ),

  http.get<never, never, DefaultBodyType>('*/v1/gameweeks/current', () =>
    ok(
      {
        current: data.gameweek(data.CURRENT_GW),
        next: data.gameweek(data.CURRENT_GW + 1),
        bonusAdded: false,
        dataChecked: false,
        waiversProcessed: true,
      },
      'live',
    ),
  ),
];

// ---------------------------------------------------------------- status tab

const statusHandlers = [
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/status', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    if (!data.isInSeason(found.league)) return seasonNotStarted();
    return ok(leagueStatus(found.league as InSeasonLeague), 'live');
  }),

  http.get<{ gw: string }, never, DefaultBodyType>('*/v1/gameweeks/:gw/awards', ({ params }) => {
    const gw = Number(params.gw);
    if (!Number.isInteger(gw) || gw < 1 || gw > data.LAST_GW) {
      return badRequest('gw must be between 1 and 38.');
    }
    const awards = gameweekAwards(gw);
    if (!awards) return notFound(`Gameweek ${gw} awards`);
    return ok(awards, awards.state);
  }),

  http.get<{ gw: string }, never, DefaultBodyType>('*/v1/gameweeks/:gw/fixtures', ({ params }) => {
    const gw = Number(params.gw);
    if (!Number.isInteger(gw) || gw < 1 || gw > data.LAST_GW) {
      return badRequest('gw must be between 1 and 38.');
    }
    const state = gw < data.CURRENT_GW ? 'final' : gw === data.CURRENT_GW ? 'live' : 'upcoming';
    return ok(data.gameweekFixtures(gw), state);
  }),

  http.get<{ fixtureId: string }, never, DefaultBodyType>(
    '*/v1/fixtures/:fixtureId',
    ({ params }) => {
      if (!/^fx_[0-9a-hjkmnp-tv-z]{26}$/.test(params.fixtureId)) {
        return badRequest('fixtureId must be a fx_ id.');
      }
      const fixture = data.findFixture(params.fixtureId);
      if (!fixture) return notFound('Fixture');
      const gw = fixture.gameweek;
      const state = gw < data.CURRENT_GW ? 'final' : gw === data.CURRENT_GW ? 'live' : 'upcoming';
      return ok(fixtureDetail(fixture), state);
    },
  ),

  http.get<never, never, DefaultBodyType>('*/v1/awards/players-of-the-week', () =>
    ok(playersOfTheWeek(), 'live'),
  ),
];

/**
 * A fixture's box score: who played (or was booked), and who of the two
 * clubs is owned in the demo user's in-season leagues, by points.
 */
function fixtureDetail(fixture: Schemas['GameweekFixture']): Schemas['FixtureDetail'] {
  const mine = [...data.leagues.values()].flatMap((l) => {
    const row = data.isInSeason(l) ? l.myLeague() : undefined;
    return row && data.isInSeason(l)
      ? [{ league: l, name: row.league.name, myTeamId: row.team.id }]
      : [];
  });
  const ownersOf = (playerId: string): Schemas['FixtureOwner'][] =>
    mine.flatMap(({ league, name, myTeamId }) => {
      const team = league.ownerOf(playerId);
      return team
        ? [{ leagueId: league.id, leagueName: name, team, mine: team.id === myTeamId }]
        : [];
    });
  const side = (club: Schemas['ClubRef'], lines: data.BoxScoreLine[]): Schemas['FixtureSide'] => ({
    club,
    players: lines
      .map(({ player, entry, stats }) => {
        const n = (v: number | null) => v ?? 0;
        return {
          player: data.toPlayerRef(player),
          minutes: entry?.minutes ?? 0,
          points: entry?.points ?? 0,
          bonusProvisional: entry?.bonusProvisional ?? false,
          stats: {
            goalsScored: n(stats.goalsScored),
            assists: n(stats.assists),
            ownGoals: n(stats.ownGoals),
            yellowCards: n(stats.yellowCards),
            redCards: n(stats.redCards),
            saves: n(stats.saves),
            penaltiesSaved: n(stats.penaltiesSaved),
            penaltiesMissed: n(stats.penaltiesMissed),
            bonus: n(stats.bonus),
          },
          breakdown: entry?.breakdown ?? [],
          owners: ownersOf(player.id),
        };
      })
      .filter(
        (p) =>
          p.minutes > 0 || p.stats.yellowCards > 0 || p.stats.redCards > 0 || p.owners.length > 0,
      )
      .sort(
        (a, b) =>
          b.points - a.points ||
          b.minutes - a.minutes ||
          a.player.displayName.localeCompare(b.player.displayName) ||
          a.player.id.localeCompare(b.player.id),
      ),
  });
  const lines = data.fixtureLines(fixture);
  return { fixture, home: side(fixture.home, lines.home), away: side(fixture.away, lines.away) };
}

// ---------------------------------------------------------------- projections (ADR 0014)

const projectionHandlers = [
  http.get<never, never, DefaultBodyType>('*/v1/fixture-difficulty', () => ok(fixtureDifficulty())),
  http.get<L & { gw: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/gameweeks/:gw/projections',
    ({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const gw = Number(params.gw);
      if (!Number.isInteger(gw) || gw < 1 || gw > data.LAST_GW) {
        return badRequest('gw must be between 1 and 38.');
      }
      if (!data.isInSeason(found.league)) return seasonNotStarted();
      const state = gw < data.CURRENT_GW ? 'final' : gw === data.CURRENT_GW ? 'live' : 'upcoming';
      return ok(leagueProjections(found.league as InSeasonLeague, gw), state);
    },
  ),
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/start-sit', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    if (!data.isInSeason(found.league)) return seasonNotStarted();
    return ok(startSit(found.league as InSeasonLeague));
  }),
];

// ---------------------------------------------------------------- draft

/** The draft of an in-season mock league: complete, with the board that built its squads. */
function completedDraft(
  league: Extract<data.MockLeague, { draftPicks: unknown }>,
): Schemas['Draft'] {
  const completedAt = data.NOW - 50 * 24 * data.HOUR;
  return {
    id: data.mockId('drf', 8_000_000 + [...data.leagues.keys()].indexOf(league.id)),
    status: 'complete',
    type: 'snake',
    rounds: 15,
    pickTimerSeconds: league.settings.draft.pickTimerSeconds,
    startsAt: null,
    order: league.members().map((m, i) => ({ slot: i + 1, team: m.team, autopick: false })),
    picks: league.draftPicks(completedAt - 15 * league.TEAMS.length * 60_000),
    currentPick: null,
    completedAt: new Date(completedAt).toISOString(),
  };
}

function preseason(
  leagueId: string,
): { error: HttpResponse<Problem> } | { league: PreseasonLeague } {
  const found = findLeague(leagueId);
  if ('error' in found) return { error: found.error };
  if (data.isInSeason(found.league)) {
    return {
      error: problem(409, 'Draft complete', 'This league has already drafted.', 'draft-complete'),
    } as const;
  }
  return { league: found.league as PreseasonLeague };
}

const draftHandlers = [
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/draft', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const l = found.league;
    return ok(data.isInSeason(l) ? completedDraft(l) : l.draft());
  }),
  http.patch<L, Schemas['DraftUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/draft',
    guarded(async ({ params, request }) => {
      const found = preseason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['DraftUpdate']>(request);
      if (!body) return problem(422, 'Invalid request', 'Send a JSON object.', 'invalid-request');
      return ok(found.league.updateDraft(body));
    }),
  ),
  ...(['start', 'pause', 'resume'] as const).map((action) =>
    http.post<L, never, DefaultBodyType>(
      `*/v1/leagues/:leagueId/draft/${action}`,
      guarded(({ params }) => {
        const found = preseason(params.leagueId);
        if ('error' in found) return found.error;
        const l = found.league;
        return ok(
          action === 'start'
            ? l.startDraft()
            : action === 'pause'
              ? l.pauseDraft()
              : l.resumeDraft(),
        );
      }),
    ),
  ),
  http.post<L, Schemas['DraftPickRequest'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/draft/picks',
    guarded(async ({ params, request }) => {
      const found = preseason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['DraftPickRequest']>(request);
      if (!body || !ID_PATTERN('plr').test(body.playerId))
        return badRequest('playerId must be a plr_ id.');
      const pick = found.league.makePick(body.playerId);
      return created(pick, `/v1/leagues/${params.leagueId}/draft`);
    }),
  ),
  http.get<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/draft/queue',
    guarded(({ params }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const l = found.league;
      // As the server: after the draft the queue is empty, not an error.
      if (data.isInSeason(l)) return ok({ team: l.myTeam, autopick: false, players: [] });
      return ok(l.queueView());
    }),
  ),
  http.put<L, Schemas['DraftQueueUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/draft/queue',
    guarded(async ({ params, request }) => {
      const found = preseason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['DraftQueueUpdate']>(request);
      if (!body || !Array.isArray(body.playerIds)) return badRequest('playerIds must be an array.');
      return ok(found.league.setQueue(body.playerIds));
    }),
  ),
  http.put<L, Schemas['DraftAutopickUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/draft/autopick',
    guarded(async ({ params, request }) => {
      const found = preseason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['DraftAutopickUpdate']>(request);
      const flag = (v: unknown) => v === undefined || typeof v === 'boolean';
      if (!body || (body.enabled === undefined && body.onTimeout === undefined))
        return badRequest('Send enabled, onTimeout or both.');
      if (!flag(body.enabled) || !flag(body.onTimeout))
        return badRequest('enabled and onTimeout must be true or false.');
      return ok(found.league.setAutopick(body));
    }),
  ),
];

// ---------------------------------------------------------------- lineups, waivers, trades

function inSeason(
  leagueId: string,
): { error: HttpResponse<Problem> } | { league: InSeasonLeague | ImportedLeague } {
  const found = findLeague(leagueId);
  if ('error' in found) return { error: found.error };
  const l = found.league;
  if (!data.isInSeason(l)) return { error: seasonNotStarted() };
  return { league: l };
}

const NEXT_GW = data.CURRENT_GW + 1;

const lineupHandlers = [
  http.get<L & { teamId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/teams/:teamId/lineup',
    guarded(({ params, request }) => {
      const bad = badId('teamId', 'tm', params.teamId);
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      if (bad) return bad;
      const raw = new URL(request.url).searchParams.get('gameweek');
      const gw = raw === null ? NEXT_GW : Number(raw);
      if (!Number.isInteger(gw) || gw < 1 || gw > data.LAST_GW)
        return badRequest('gameweek must be between 1 and 38.');
      return ok(found.league.lineupView(params.teamId, gw));
    }),
  ),
  http.put<L & { teamId: string }, Schemas['LineupUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/teams/:teamId/lineup',
    guarded(async ({ params, request }) => {
      const bad = badId('teamId', 'tm', params.teamId);
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      if (bad) return bad;
      const body = await jsonBody<Schemas['LineupUpdate']>(request);
      if (
        !body ||
        !Array.isArray(body.starters) ||
        !Array.isArray(body.bench) ||
        !Number.isInteger(body.gameweek)
      ) {
        return badRequest('Send gameweek, starters and bench.');
      }
      return ok(found.league.setLineup(params.teamId, body.gameweek, body.starters, body.bench));
    }),
  ),
];

const waiverHandlers = [
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/waivers', ({ params }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const l = found.league;
    if (data.isInSeason(l)) return ok(l.waiverState());
    return ok({
      type: 'rolling_priority' as const,
      order: l.settings.waivers.order,
      nextRunGameweek: null,
      nextRunAt: null,
      lastRunAt: null,
      priority: [...l.teams].reverse().map((team, i) => ({ rank: i + 1, team })),
    });
  }),
  http.get<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/waivers/claims',
    ({ params, request }) => {
      const found = findLeague(params.leagueId);
      if ('error' in found) return found.error;
      const status = new URL(request.url).searchParams.get('status') ?? 'pending';
      if (status !== 'pending' && status !== 'processed')
        return badRequest('status must be pending or processed.');
      const l = found.league;
      return ok(data.isInSeason(l) ? l.listClaims(status) : []);
    },
  ),
  http.post<L, Schemas['WaiverClaimCreate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/waivers/claims',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['WaiverClaimCreate']>(request);
      if (!body?.addPlayerId || !body.dropPlayerId)
        return badRequest('Send addPlayerId and dropPlayerId.');
      const claim = found.league.createClaim(body.addPlayerId, body.dropPlayerId);
      return created(claim, `/v1/leagues/${params.leagueId}/waivers/claims/${claim.id}`);
    }),
  ),
  http.patch<L & { claimId: string }, Schemas['WaiverClaimUpdate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/waivers/claims/:claimId',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('claimId', 'wv', params.claimId);
      if (bad) return bad;
      const body = await jsonBody<Schemas['WaiverClaimUpdate']>(request);
      if (!body?.dropPlayerId) return badRequest('Send dropPlayerId.');
      return ok(found.league.updateClaim(params.claimId, body.dropPlayerId));
    }),
  ),
  http.delete<L & { claimId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/waivers/claims/:claimId',
    guarded(({ params }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('claimId', 'wv', params.claimId);
      if (bad) return bad;
      found.league.cancelClaim(params.claimId);
      return noContent();
    }),
  ),
  http.put<L, Schemas['WaiverClaimOrder'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/waivers/claim-order',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['WaiverClaimOrder']>(request);
      if (!body || !Array.isArray(body.claimIds)) return badRequest('Send claimIds.');
      return ok(found.league.reorderClaims(body.claimIds));
    }),
  ),
  http.post<L, Schemas['FreeAgentMoveRequest'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/free-agent-moves',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['FreeAgentMoveRequest']>(request);
      if (!body?.addPlayerId || !body.dropPlayerId)
        return badRequest('Send addPlayerId and dropPlayerId.');
      const txn = found.league.addFreeAgent(body.addPlayerId, body.dropPlayerId);
      return created(txn, `/v1/leagues/${params.leagueId}/transactions`);
    }),
  ),
];

const TRADE_STATUSES = [
  'open',
  'offered',
  'accepted',
  'in_review',
  'processed',
  'declined',
  'withdrawn',
  'expired',
  'vetoed',
  'failed',
];

type T = L & { tradeId: string };

function tradeAction(action: 'accept' | 'decline' | 'withdraw') {
  return http.post<T, never, DefaultBodyType>(
    `*/v1/leagues/:leagueId/trades/:tradeId/${action}`,
    guarded(({ params }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('tradeId', 'trd', params.tradeId);
      if (bad) return bad;
      const l = found.league;
      const trade =
        action === 'accept'
          ? l.acceptTrade(params.tradeId)
          : action === 'decline'
            ? l.declineTrade(params.tradeId)
            : l.withdrawTrade(params.tradeId);
      return ok(trade);
    }),
  );
}

const tradeHandlers = [
  http.get<L, never, DefaultBodyType>('*/v1/leagues/:leagueId/trades', ({ params, request }) => {
    const found = findLeague(params.leagueId);
    if ('error' in found) return found.error;
    const q = new URL(request.url).searchParams;
    const limit = parseLimit(q.get('limit'));
    if (limit === undefined) return badRequest('limit must be between 1 and 100.');
    const status = q.get('status');
    if (status && !TRADE_STATUSES.includes(status)) return badRequest('Unknown status.');
    const l = found.league;
    const items = data.isInSeason(l) ? l.listTrades(status, q.get('teamId')) : [];
    const { slice, next } = page(items, q.get('cursor'), limit);
    return HttpResponse.json({ data: slice, meta: { ...meta(), nextCursor: next } });
  }),
  http.post<L, Schemas['TradeCreate'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/trades',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['TradeCreate']>(request);
      if (
        !body?.receivingTeamId ||
        !Array.isArray(body.offeredPlayerIds) ||
        !Array.isArray(body.requestedPlayerIds)
      ) {
        return badRequest('Send receivingTeamId, offeredPlayerIds and requestedPlayerIds.');
      }
      const trade = found.league.proposeTrade(
        body.receivingTeamId,
        body.offeredPlayerIds,
        body.requestedPlayerIds,
        body.message,
      );
      return created(trade, `/v1/leagues/${params.leagueId}/trades/${trade.id}`);
    }),
  ),
  http.get<T, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/trades/:tradeId',
    guarded(({ params }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('tradeId', 'trd', params.tradeId);
      if (bad) return bad;
      return ok(found.league.getTrade(params.tradeId));
    }),
  ),
  tradeAction('accept'),
  tradeAction('decline'),
  tradeAction('withdraw'),
  http.post<T, Schemas['TradeVoteRequest'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/trades/:tradeId/votes',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['TradeVoteRequest']>(request);
      if (body?.vote !== 'veto' && body?.vote !== 'allow')
        return badRequest('vote must be veto or allow.');
      return ok(found.league.voteOnTrade(params.tradeId, body.vote));
    }),
  ),
  http.post<T, Schemas['TradeReviewRequest'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/trades/:tradeId/review',
    guarded(async ({ params, request }) => {
      const found = inSeason(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['TradeReviewRequest']>(request);
      if (body?.decision !== 'approve' && body?.decision !== 'veto') {
        return badRequest('decision must be approve or veto.');
      }
      return ok(found.league.reviewTrade(params.tradeId, body.decision));
    }),
  ),
];

const realtimeHandlers = [
  http.post<never, Schemas['RealtimeTicketRequest'], DefaultBodyType>(
    '*/v1/realtime/tickets',
    async ({ request }) => {
      const body = await jsonBody<Schemas['RealtimeTicketRequest']>(request);
      if (body?.leagueId) {
        const found = findLeague(body.leagueId);
        if ('error' in found) return found.error;
      }
      // The mocks have no WebSocket server: poll the REST endpoints instead.
      return created(
        {
          ticket: 'mock-ticket',
          url: 'ws://localhost:8081/v1/realtime?ticket=mock-ticket',
          expiresAt: new Date(Date.now() + 60_000).toISOString(),
        },
        '/v1/realtime',
      );
    },
  ),
];

// ---------------------------------------------------------------- sync (ADR 0012)

const notImported = () =>
  problem(409, 'Not imported', 'This league was created here, not imported.', 'not-imported');

/** An imported league the caller is in, or the problem to send. */
function imported(leagueId: string): { error: HttpResponse<Problem> } | { league: ImportedLeague } {
  const found = findLeague(leagueId);
  if ('error' in found) return found;
  return data.isImported(found.league) ? { league: found.league } : { error: notImported() };
}

/**
 * ADR 0012: the status, re-sync, check, explanations and seats of an
 * imported league. Everything else works as in any league. Listed before
 * the league handlers so they win.
 */
const syncHandlers = [
  http.get<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/sync',
    guarded(({ params }) => {
      const found = imported(params.leagueId);
      return 'error' in found ? found.error : ok(found.league.status());
    }),
  ),
  http.post<L, Schemas['ResyncInput'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/sync',
    guarded(async ({ params, request }) => {
      const found = imported(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['ResyncInput']>(request);
      if (body?.confirm !== true)
        return problem(422, 'Invalid request', 'Send confirm: true.', 'invalid-request');
      return HttpResponse.json({ data: found.league.resync(), meta: meta() }, { status: 202 });
    }),
  ),
  http.post<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/sync/check',
    guarded(({ params }) => {
      const found = imported(params.leagueId);
      if ('error' in found) return found.error;
      return HttpResponse.json({ data: found.league.check(), meta: meta() }, { status: 202 });
    }),
  ),
  http.post<L, Schemas['SyncExplanationInput'], DefaultBodyType>(
    '*/v1/leagues/:leagueId/sync/explanations',
    guarded(async ({ params, request }) => {
      const found = imported(params.leagueId);
      if ('error' in found) return found.error;
      const body = await jsonBody<Schemas['SyncExplanationInput']>(request);
      const note = body?.note?.trim() ?? '';
      if (!body?.key || note.length < 1 || note.length > 500)
        return problem(422, 'Invalid request', 'Send the key and a note.', 'invalid-request');
      return ok(found.league.explain(body.key, note));
    }),
  ),
  http.get<L, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/seats',
    guarded(({ params }) => {
      const found = imported(params.leagueId);
      return 'error' in found ? found.error : ok(found.league.seatList());
    }),
  ),
  http.post<L & { teamId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/seats/:teamId/invite',
    guarded(({ params }) => {
      const found = imported(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('teamId', 'tm', params.teamId);
      if (bad) return bad;
      const inv = found.league.resendInvite(params.teamId);
      return created(inv, `/v1/leagues/${params.leagueId}/invites/${inv.id}`);
    }),
  ),
  http.post<L & { teamId: string }, never, DefaultBodyType>(
    '*/v1/leagues/:leagueId/seats/:teamId/signin-link',
    guarded(({ params }) => {
      const found = imported(params.leagueId);
      if ('error' in found) return found.error;
      const bad = badId('teamId', 'tm', params.teamId);
      if (bad) return bad;
      return HttpResponse.json(
        { data: found.league.issueSignInLink(params.teamId), meta: meta() },
        { status: 201 },
      );
    }),
  ),
  // A sign-in link (docs/adr/0013) signs the seat's manager in: the mocks
  // answer with a session for that manager (no email).
  http.post<never, Schemas['SignInLinkRequest'], DefaultBodyType>(
    '*/v1/auth/signin-link',
    guarded(async ({ request }) => {
      const body = await jsonBody<Schemas['SignInLinkRequest']>(request);
      const code = typeof body?.code === 'string' ? body.code : '';
      if (!/^[A-Za-z0-9_-]{43}$/.test(code))
        return problem(400, 'Bad request', 'That is not a sign-in link code.', 'bad-request');
      for (const l of data.leagues.values()) {
        if (!data.isImported(l)) continue;
        const owner = l.consumeSignInLink(code);
        if (!owner) continue;
        const s = session(false);
        return ok<Schemas['SignInLinkSession']>({
          session: {
            ...s,
            user: {
              ...s.user,
              id: owner.id,
              displayName: owner.displayName,
              email: null,
              authProviders: [],
            },
          },
          leagueId: l.id,
        });
      }
      return problem(
        401,
        'Unauthorized',
        'This sign-in link has expired, was already used or was replaced. Ask your commissioner for a new one.',
        'unauthorized',
      );
    }),
  ),
  // Claim links: the invite preview and accept for an imported league's seat.
  http.get<{ code: string }, never, DefaultBodyType>('*/v1/invites/:code', ({ params }) => {
    for (const l of data.leagues.values()) {
      const preview = data.isImported(l) ? l.invitePreview(params.code) : undefined;
      if (preview) return ok(preview);
    }
    return undefined;
  }),
  http.post<{ code: string }, never, DefaultBodyType>(
    '*/v1/invites/:code/accept',
    guarded(({ params }) => {
      for (const l of data.leagues.values()) {
        if (!data.isImported(l) || !l.invitePreview(params.code)) continue;
        return created<Schemas['Membership']>(l.claim(params.code), `/v1/leagues/${l.id}`);
      }
      return undefined;
    }),
  ),
  // A signed-out person claims a seat with only a name (docs/adr/0013): the
  // mocks sign the demo user in under that name.
  http.post<never, Schemas['ClaimSignInRequest'], DefaultBodyType>(
    '*/v1/auth/claim',
    guarded(async ({ request }) => {
      const body = await jsonBody<Schemas['ClaimSignInRequest']>(request);
      const name = typeof body?.displayName === 'string' ? body.displayName.trim() : '';
      if (name.length < 1 || name.length > 32) {
        return problem(
          422,
          'Invalid request',
          'The display name is not valid.',
          'invalid-request',
          [
            {
              code: 'invalid_length',
              message: 'Your name must be 1 to 32 characters.',
              field: '/displayName',
            },
          ],
        );
      }
      const code = typeof body?.code === 'string' ? body.code : '';
      for (const l of data.leagues.values()) {
        if (!data.isImported(l)) continue;
        // Not this league's link (a used one answers 409 from claim).
        if (!l.invitePreview(code) && !l.claimedWith(code)) continue;
        const membership = l.claim(code);
        const s = session(true);
        return ok<Schemas['ClaimSignIn']>({
          session: { ...s, user: { ...s.user, displayName: name, email: null, authProviders: [] } },
          membership,
        });
      }
      return notFound('Invite');
    }),
  ),
];

/** Forget every change the mutating handlers made (tests). */
export function resetMocks() {
  resetMockImports();
  data.resetMockLeagues();
  blocks.clear();
  tagLists.clear();
  resetMockNotifications();
}

// ---------------------------------------------------------------- handlers

/** Handlers typed against the generated schema types. */
export const handlers = [
  ...importHandlers,
  ...syncHandlers,
  ...authHandlers,
  ...leagueHandlers,
  ...statusHandlers,
  ...projectionHandlers,
  ...draftHandlers,
  ...lineupHandlers,
  ...waiverHandlers,
  ...tradeHandlers,
  ...realtimeHandlers,
  ...notificationHandlers(ok, noContent, created, (detail) =>
    problem(422, 'Invalid request', detail, 'invalid-request'),
  ),
  http.get<never, never, Schemas['Health']>('*/healthz', () =>
    HttpResponse.json({ status: 'ok', version: 'mock' }),
  ),
];
