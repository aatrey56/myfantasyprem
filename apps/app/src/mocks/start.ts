/**
 * Native: there is no service worker, so the mock build routes the API's
 * requests through the same MSW handlers in-process: `fetch` is wrapped and
 * any `/v1/...` request is answered by `getResponse(handlers, request)`.
 * The generated client looks `fetch` up per call, so wrapping it here (before
 * the first render, see MockGate) is enough. Other requests go to the
 * network. Production builds compile this branch away (inline env check).
 */
export async function startMocks(): Promise<void> {
  if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true') {
    await import('./native-polyfills');
    const [{ getResponse }, { handlers }] = await Promise.all([
      import('msw'),
      import('./handlers'),
    ]);
    const networkFetch = globalThis.fetch;
    const mockFetch: typeof fetch = async (input, init) => {
      const request = new Request(input, init);
      if (/^https?:\/\/[^/]+\/v1\//.test(request.url)) {
        const response = await getResponse(handlers, request);
        if (response) return response;
      }
      return networkFetch(input, init);
    };
    globalThis.fetch = mockFetch;
  }
}
