/**
 * Mock builds on native only: MSW's core module touches a few browser
 * globals at load time (its WebSocket support extends MessageEvent and opens
 * a BroadcastChannel) that Hermes does not have. The mocks never use
 * WebSockets, so minimal stand-ins are enough. Imported before `msw`.
 */
const g = globalThis as Record<string, unknown>;

if (typeof g.Event === 'undefined') {
  g.Event = class Event {
    readonly type: string;
    defaultPrevented = false;
    target: unknown = null;
    constructor(type: string) {
      this.type = type;
    }
    preventDefault() {
      this.defaultPrevented = true;
    }
    stopPropagation() {}
    stopImmediatePropagation() {}
  };
}

if (typeof g.EventTarget === 'undefined') {
  type Listener = (event: unknown) => void;
  g.EventTarget = class EventTarget {
    private listeners = new Map<string, Set<Listener>>();
    addEventListener(type: string, listener: Listener) {
      if (!this.listeners.has(type)) this.listeners.set(type, new Set());
      this.listeners.get(type)!.add(listener);
    }
    removeEventListener(type: string, listener: Listener) {
      this.listeners.get(type)?.delete(listener);
    }
    dispatchEvent(event: { type: string }) {
      this.listeners.get(event.type)?.forEach((l) => l(event));
      return true;
    }
  };
}

const BaseEvent = g.Event as new (type: string, init?: unknown) => object;

if (typeof g.MessageEvent === 'undefined') {
  g.MessageEvent = class MessageEvent extends BaseEvent {
    readonly data: unknown;
    readonly origin: string;
    constructor(type: string, init: { data?: unknown; origin?: string } = {}) {
      super(type, init);
      this.data = init.data;
      this.origin = init.origin ?? '';
    }
  };
}

if (typeof g.CloseEvent === 'undefined') {
  g.CloseEvent = class CloseEvent extends BaseEvent {
    readonly code: number;
    readonly reason: string;
    readonly wasClean: boolean;
    constructor(type: string, init: { code?: number; reason?: string; wasClean?: boolean } = {}) {
      super(type, init);
      this.code = init.code ?? 0;
      this.reason = init.reason ?? '';
      this.wasClean = init.wasClean ?? false;
    }
  };
}

if (typeof g.BroadcastChannel === 'undefined') {
  const Target = g.EventTarget as new () => object;
  g.BroadcastChannel = class BroadcastChannel extends Target {
    readonly name: string;
    constructor(name: string) {
      super();
      this.name = name;
    }
    postMessage() {}
    close() {}
  };
}

// React Native's fetch polyfill predates Headers.getSetCookie (MSW calls it
// on every mocked response).
const HeadersProto = (g.Headers as { prototype: Record<string, unknown> } | undefined)?.prototype;
if (HeadersProto && typeof HeadersProto.getSetCookie !== 'function') {
  HeadersProto.getSetCookie = function getSetCookie(this: Headers) {
    const value = this.get('set-cookie');
    return value ? [value] : [];
  };
}

export {};
