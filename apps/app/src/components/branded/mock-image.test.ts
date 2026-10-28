import { getResponse } from 'msw';

import { mockImageUri, resetMockImages } from './mock-image';
import { handlers } from '@/mocks/handlers';
import { scenarios } from '@/mocks/scenarios';

const ORIGIN = 'http://mocks.invalid';
const realFetch = globalThis.fetch;
let calls: string[] = [];

/** The native mock build's fetch (mocks/start.ts): the MSW handlers in-process. */
function serve(extra: readonly (typeof handlers)[number][] = []) {
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = new Request(input, init);
    calls.push(request.url);
    return (
      (await getResponse([...extra, ...handlers], request)) ?? new Response(null, { status: 404 })
    );
  }) as typeof fetch;
}

beforeEach(() => {
  calls = [];
  resetMockImages();
});
afterEach(() => {
  globalThis.fetch = realFetch;
});

describe('mockImageUri (native mock builds)', () => {
  it('turns the mocked fixture PNG into a data URI', async () => {
    serve();
    const uri = await mockImageUri(`${ORIGIN}/v1/assets/clubs/ARS/crest`);
    expect(uri).toMatch(/^data:image\/png;base64,iVBORw0KGgo/);
  });

  it('serves the goalkeeper shirt apart from the outfield one', async () => {
    serve();
    const shirt = await mockImageUri(`${ORIGIN}/v1/assets/clubs/ARS/shirt`);
    const gk = await mockImageUri(`${ORIGIN}/v1/assets/clubs/ARS/shirt?gk=true`);
    expect(shirt).not.toBeNull();
    expect(gk).not.toBeNull();
    expect(gk).not.toBe(shirt);
  });

  it('loads each image once', async () => {
    serve();
    const url = `${ORIGIN}/v1/assets/players/plr_1/photo`;
    await Promise.all([mockImageUri(url), mockImageUri(url)]);
    await mockImageUri(url);
    expect(calls).toEqual([url]);
  });

  it('gives null for a missing image, and asks again next time', async () => {
    serve(scenarios['assets-missing']);
    const url = `${ORIGIN}/v1/assets/players/plr_1/photo`;
    expect(await mockImageUri(url)).toBeNull();
    expect(await mockImageUri(url)).toBeNull();
    expect(calls).toHaveLength(2);
  });
});
