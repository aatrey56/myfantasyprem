import createClient from 'openapi-fetch';

import type { components, paths } from './schema';

export type { components, operations, paths } from './schema';

/** Shorthand for `components['schemas']`. */
export type Schemas = components['schemas'];
export type Meta = Schemas['Meta'];
export type Problem = Schemas['Problem'];

/** Every success body is `{ data, meta }`. */
export interface Envelope<TData = unknown, TMeta extends Meta = Meta> {
  data: TData;
  meta: TMeta;
}

export interface ApiClientOptions {
  /**
   * Origin of the API, for example `http://localhost:8080`. An empty string
   * means same-origin (relative URLs), the web default.
   */
  baseUrl: string;
  /** Override `fetch`, for tests. */
  fetch?: typeof fetch;
}

export function createApiClient(options: ApiClientOptions) {
  // Look `fetch` up per call, not at creation, so test doubles and polyfills
  // installed later (MSW, jest) are honoured.
  const lateBoundFetch: typeof fetch = (input, init) => globalThis.fetch(input, init);
  return createClient<paths>({ baseUrl: options.baseUrl, fetch: options.fetch ?? lateBoundFetch });
}

export type ApiClient = ReturnType<typeof createApiClient>;

/** Thrown by {@link unwrap} for any non-2xx response. */
export class ApiError extends Error {
  readonly status: number;
  readonly problem: Problem | undefined;

  constructor(status: number, problem?: Problem) {
    super(problem?.detail ?? problem?.title ?? `Request failed with status ${status}`);
    this.name = 'ApiError';
    this.status = status;
    this.problem = problem;
  }
}

interface FetchResult<TBody> {
  data?: TBody;
  error?: unknown;
  response: Response;
}

/**
 * Thrown by {@link unwrap} when a 2xx response is not a `{data, meta}`
 * envelope (an HTML error page from a proxy, a truncated body, a contract
 * mismatch). Retrying does not fix it.
 */
export class MalformedResponseError extends Error {
  readonly status: number;

  constructor(status: number, message = 'The server sent a response this app cannot read.') {
    super(message);
    this.name = 'MalformedResponseError';
    this.status = status;
  }
}

/**
 * Turns an openapi-fetch result into the success envelope or throws:
 * {@link ApiError} for non-2xx, {@link MalformedResponseError} for a 2xx body
 * that is not an envelope. Use it inside TanStack Query `queryFn`s so
 * failures reach `error`.
 */
export function unwrap<TBody>(result: FetchResult<TBody>): TBody {
  if (result.response.ok) {
    if (!isEnvelope(result.data)) throw new MalformedResponseError(result.response.status);
    return result.data;
  }
  const problem = isProblem(result.error) ? result.error : undefined;
  throw new ApiError(result.response.status, problem);
}

/**
 * Awaits an openapi-fetch call and {@link unwrap}s it. A 2xx body that is not
 * JSON makes openapi-fetch throw a `SyntaxError`; that becomes a
 * {@link MalformedResponseError} too. Network failures stay `TypeError`s.
 */
export async function send<TBody>(pending: Promise<FetchResult<TBody>>): Promise<TBody> {
  let result: FetchResult<TBody>;
  try {
    result = await pending;
  } catch (error) {
    if (error instanceof SyntaxError) throw new MalformedResponseError(200);
    throw error;
  }
  return unwrap(result);
}

function isProblem(value: unknown): value is Problem {
  return typeof value === 'object' && value !== null && 'title' in value && 'status' in value;
}

/** True when `value` looks like a `{data, meta}` success envelope. */
export function isEnvelope(value: unknown): value is Envelope {
  if (typeof value !== 'object' || value === null) return false;
  const meta = (value as { meta?: unknown }).meta;
  return (
    'data' in value &&
    typeof meta === 'object' &&
    meta !== null &&
    typeof (meta as { stale?: unknown }).stale === 'boolean' &&
    typeof (meta as { dataAsOf?: unknown }).dataAsOf === 'string' &&
    typeof (meta as { serverTime?: unknown }).serverTime === 'string'
  );
}
