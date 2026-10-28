import { delay, http, HttpResponse } from 'msw';

import { isInSeason, leagues } from './data';
import { ok, problem } from './handlers';
import { mockPhase, type InSeasonLeague } from './league';
import { leagueStatus } from './status';

const LEAGUE_HOME = '*/v1/leagues/:leagueId';

/**
 * Named failure overrides, prepended to the normal handlers with
 * `worker.use()`. Playwright picks one with
 * `localStorage.setItem('mfp.mockScenario', name)` before the page loads, or
 * at runtime through `window.__mfpMocks.use(name)` / `.reset()`.
 */
const ASSETS = '*/v1/assets/*';
const imageMissing = () => problem(404, 'Not found', 'No such image.');

/** How long a refused write takes, so a test sees the optimistic move before the rollback. */
const REFUSAL_DELAY_MS = 1500;

export const scenarios = {
  // The gameweek deadline passed and the gameweek is not finished: no
  // free-agent signings until the next waiver run. Every request marks the
  // mock window locked and falls through to the normal handlers.
  'transactions-locked': [
    http.all('*/v1/*', () => {
      mockPhase.locked = true;
    }),
    http.post('*/v1/leagues/:leagueId/free-agent-moves', () =>
      problem(
        409,
        'Transactions locked',
        'Free-agent moves are locked until waivers run.',
        'transactions-locked',
      ),
    ),
  ],
  // The server refuses a lineup save after a pause (optimistic swap, then rollback).
  'lineup-save-refused': [
    http.put('*/v1/leagues/:leagueId/teams/:teamId/lineup', async () => {
      await delay(REFUSAL_DELAY_MS);
      return problem(422, 'Invalid lineup', "That lineup isn't allowed.", 'invalid-lineup', [
        { code: 'forced', message: 'The server refused this lineup.', field: '/starters' },
      ]);
    }),
  ],
  // Someone else signed the player first (optimistic signing, then rollback).
  'free-agent-refused': [
    http.post('*/v1/leagues/:leagueId/free-agent-moves', async () => {
      await delay(REFUSAL_DELAY_MS);
      return problem(409, 'Player taken', 'Another team signed him first.', 'player-taken');
    }),
  ],
  // A trade offer refused: the first player asked for left their roster
  // (optimistic offer, then rollback and "Edit offer").
  'trade-refused': [
    http.post('*/v1/leagues/:leagueId/trades', async ({ request }) => {
      const body = (await request.json()) as { requestedPlayerIds?: string[] };
      await delay(REFUSAL_DELAY_MS);
      return problem(422, 'Invalid trade', 'That trade is not allowed.', 'trade-shape', [
        {
          code: 'player_not_on_roster',
          message: 'Not on their team.',
          field: '/requestedPlayerIds',
          playerId: body.requestedPlayerIds?.[0] ?? '',
        },
      ]);
    }),
  ],
  // The server's branded-assets switch off: generic shirts, no image requests.
  'branded-off': [http.get('*/v1/config', () => ok({ brandedAssets: false }))],
  // Switched on, but every image is missing: the generic shirts as fallback.
  'assets-missing': [http.get(ASSETS, imageMissing)],
  // The Status page moved on server-side (up 7 places): shows once the tab refetches.
  'status-rank-up': [
    http.get<{ leagueId: string }>('*/v1/leagues/:leagueId/status', ({ params }) => {
      const league = leagues.get(params.leagueId);
      if (!league || !isInSeason(league)) return problem(404, 'Not found', 'League not found.');
      return ok({ ...leagueStatus(league as InSeasonLeague), rankChange: 7 }, 'live');
    }),
  ],
  'league-500': [
    http.get(LEAGUE_HOME, () =>
      problem(500, 'Internal server error', 'Something went wrong on our side.', 'internal'),
    ),
  ],
  'league-404': [http.get(LEAGUE_HOME, () => problem(404, 'Not found', 'League not found.'))],
  'league-network-error': [http.get(LEAGUE_HOME, () => HttpResponse.error())],
  // The team news endpoint is unavailable (the News sheet's error state).
  'news-503': [
    http.get('*/v1/leagues/:leagueId/news', () =>
      problem(503, 'Service unavailable', 'News is unavailable right now.'),
    ),
  ],
  // A draft that finished after the season's last deadline: the league is
  // complete with nothing played (no matchups, empty standings).
  'league-season-over': [
    http.get<{ leagueId: string }>(LEAGUE_HOME, ({ params }) => {
      const league = leagues.get(params.leagueId);
      if (!league) return problem(404, 'Not found', 'League not found.');
      const home = league.home();
      return ok({
        ...home,
        league: { ...home.league, status: 'complete' },
        standings: home.standings.map((r) => ({
          ...r,
          played: 0,
          wins: 0,
          draws: 0,
          losses: 0,
          pointsFor: 0,
          pointsAgainst: 0,
          leaguePoints: 0,
          form: [],
        })),
        matchups: [],
      });
    }),
  ],
} as const;

export type ScenarioName = keyof typeof scenarios;

export const isScenario = (name: unknown): name is ScenarioName =>
  typeof name === 'string' && name in scenarios;
