/** @jest-environment node */
/**
 * Runs the real api-client against the MSW handlers. Proves the mock server
 * and the generated client agree on paths, params and envelopes.
 *
 * Requests are resolved with msw's `getResponse` through an injected fetch
 * rather than `msw/node`, whose interceptors do not load under jest-expo.
 */
import { createApiClient, isEnvelope, unwrap, ApiError } from '@myfantasyprem/api-client';
import { getResponse } from 'msw';

import { handlers, resetMockImports } from '@/mocks/handlers';
import { LEAGUE_ID, pointsFor } from '@/mocks/data';

const mockFetch: typeof fetch = async (input, init) => {
  const request = new Request(input, init);
  const response = await getResponse(handlers, request);
  if (!response) throw new Error(`No mock handler for ${request.method} ${request.url}`);
  return response;
};
const api = createApiClient({ baseUrl: 'http://api.test', fetch: mockFetch });

describe('mock API through the generated client', () => {
  it('returns the {data, meta} envelope', async () => {
    const res = unwrap(await api.GET('/v1/gameweeks/current'));
    expect(isEnvelope(res)).toBe(true);
    expect(res.data.current.number).toBe(7);
    expect(Date.parse(res.meta.serverTime)).not.toBeNaN();
    expect(res.meta.stale).toBe(false);
  });

  it('serves the client settings and fixture images (never real ones)', async () => {
    const config = unwrap(await api.GET('/v1/config'));
    expect(config.data.brandedAssets).toBe(true);
    for (const path of [
      '/v1/assets/players/plr_01k7m2xq8v00000000000000a1/photo',
      '/v1/assets/clubs/ARS/crest',
      '/v1/assets/clubs/ARS/shirt',
      '/v1/assets/clubs/ARS/shirt?gk=true',
    ]) {
      const res = await mockFetch(`http://api.test${path}`);
      expect(res.status).toBe(200);
      expect(res.headers.get('Content-Type')).toBe('image/png');
      const bytes = new Uint8Array(await res.arrayBuffer());
      expect([...bytes.slice(1, 4)].map((b) => String.fromCharCode(b)).join('')).toBe('PNG');
    }
  });

  it('serves league home with standings and matchups', async () => {
    const res = unwrap(
      await api.GET('/v1/leagues/{leagueId}', { params: { path: { leagueId: LEAGUE_ID } } }),
    );
    expect(res.data.standings).toHaveLength(9);
    expect(res.data.matchups).toHaveLength(5);
    expect(res.meta.scoreState).toBe('live');
  });

  it('serves the status page, gameweek awards and players of the week', async () => {
    const status = unwrap(
      await api.GET('/v1/leagues/{leagueId}/status', { params: { path: { leagueId: LEAGUE_ID } } }),
    );
    expect(status.data.gameweek).toBe(7);
    expect(status.data.injuries.length).toBeGreaterThan(0);
    const awards = unwrap(
      await api.GET('/v1/gameweeks/{gw}/awards', { params: { path: { gw: 6 } } }),
    );
    expect(awards.data.teamOfTheWeek).toHaveLength(11);
    expect(awards.data.state).toBe('final');
    const potw = unwrap(await api.GET('/v1/awards/players-of-the-week'));
    expect(potw.data.map((w) => w.gameweek)).toEqual([1, 2, 3, 4, 5, 6, 7]);
    const future = await api.GET('/v1/gameweeks/{gw}/awards', { params: { path: { gw: 9 } } });
    expect(future.response.status).toBe(404);
  });

  it('sorts players by form, (GW5 + GW6) / 2 while gameweek 7 is live', async () => {
    const res = unwrap(
      await api.GET('/v1/leagues/{leagueId}/players', {
        params: { path: { leagueId: LEAGUE_ID }, query: { sort: 'form', limit: 100 } },
      }),
    );
    const forms = res.data.map((p) => p.form!);
    expect(forms).toHaveLength(100);
    expect(forms).toEqual([...forms].sort((a, b) => b - a));
    for (const p of res.data.slice(0, 10)) {
      const sum = [5, 6].reduce((a, gw) => a + pointsFor(p.player.id, gw), 0);
      expect(p.form).toBe(Math.round((sum / 2) * 10) / 10);
    }
  });

  it('paginates players with a cursor and filters free agents', async () => {
    const query = { available: true, position: 'MID', limit: 10 } as const;
    const first = unwrap(
      await api.GET('/v1/leagues/{leagueId}/players', {
        params: { path: { leagueId: LEAGUE_ID }, query },
      }),
    );
    expect(first.data).toHaveLength(10);
    expect(first.data.every((p) => p.owner === null && p.player.position === 'MID')).toBe(true);
    expect(first.meta.nextCursor).toEqual(expect.any(String));
    const second = unwrap(
      await api.GET('/v1/leagues/{leagueId}/players', {
        params: {
          path: { leagueId: LEAGUE_ID },
          query: { ...query, cursor: first.meta.nextCursor! },
        },
      }),
    );
    expect(second.data[0]?.player.id).not.toBe(first.data[0]?.player.id);
  });

  it('answers unknown and malformed ids with problem+json', async () => {
    const unknown = await api.GET('/v1/leagues/{leagueId}', {
      params: { path: { leagueId: 'lg_00000000000000000000000000' } },
    });
    expect(unknown.response.headers.get('content-type')).toContain('application/problem+json');
    expect(() => unwrap(unknown)).toThrow(ApiError);
    try {
      unwrap(unknown);
    } catch (e) {
      expect((e as ApiError).status).toBe(404);
      expect((e as ApiError).problem?.title).toBe('Not found');
    }
    const malformed = await api.GET('/v1/leagues/{leagueId}', {
      params: { path: { leagueId: 'nope' } },
    });
    expect(malformed.response.status).toBe(400);
  });

  it('previews, imports and finishes an import, idempotently', async () => {
    resetMockImports();
    const draftLeagueUrl = 'https://draft.premierleague.com/league/4242';
    const preview = unwrap(
      await api.GET('/v1/imports/preview', { params: { query: { draftLeagueUrl } } }),
    );
    expect(preview.data).toMatchObject({ fplLeagueId: 4242, teamCount: 8, importable: true });
    const first = await api.POST('/v1/imports', { body: { draftLeagueUrl, seat: 1 } });
    expect(first.response.status).toBe(202);
    expect(first.response.headers.get('Location')).toBe(`/v1/imports/${first.data!.data.id}`);
    expect(Number(first.response.headers.get('Retry-After'))).toBeGreaterThanOrEqual(1);
    const again = await api.POST('/v1/imports', { body: { fplLeagueId: 4242, seat: 1 } });
    expect(again.response.status).toBe(200);
    expect(again.data!.data.id).toBe(first.data!.data.id);
    const jobs = unwrap(
      await api.GET('/v1/imports/{importId}', {
        params: { path: { importId: first.data!.data.id } },
      }),
    );
    expect(['queued', 'running', 'succeeded']).toContain(jobs.data.status);
  });

  it('rejects classic FPL league links with 422', async () => {
    const result = await api.POST('/v1/imports', {
      body: {
        draftLeagueUrl: 'https://fantasy.premierleague.com/leagues/314/standings/c',
        seat: 1,
      },
    });
    expect(result.response.status).toBe(422);
    expect((result.error as { type?: string }).type).toMatch(/classic-league-url$/);
  });
});
