/**
 * Native mock builds only. React Native's Image loads its uri natively, past
 * the in-process mocks that wrap `fetch` (mocks/start.ts), so an image from
 * the placeholder mock origin never loads. This asks the wrapped `fetch` for
 * the image instead and hands back its bytes as a data URI: the same MSW
 * handlers (and fixture PNGs) as the web mocks. Null when the mocks answer
 * with an error, so the caller shows its fallback.
 */
const cache = new Map<string, Promise<string | null>>();

export function mockImageUri(uri: string): Promise<string | null> {
  let hit = cache.get(uri);
  if (!hit) {
    hit = load(uri).then((data) => {
      // A failure is not kept: the next mount asks again.
      if (data === null) cache.delete(uri);
      return data;
    });
    cache.set(uri, hit);
  }
  return hit;
}

async function load(uri: string): Promise<string | null> {
  try {
    const res = await globalThis.fetch(uri);
    if (!res.ok) return null;
    const type = res.headers.get('Content-Type') ?? 'image/png';
    const bytes = new Uint8Array(await res.arrayBuffer());
    let binary = '';
    for (let i = 0; i < bytes.length; i += 1) binary += String.fromCharCode(bytes[i]!);
    return `data:${type};base64,${btoa(binary)}`;
  } catch {
    return null;
  }
}

/** For tests: forget the loaded images. */
export function resetMockImages() {
  cache.clear();
}
