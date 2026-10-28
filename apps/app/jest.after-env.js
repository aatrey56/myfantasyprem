/* global afterAll */
// TanStack Query keeps a garbage-collection timer per query and per mutation
// (5 minutes by default), and `queryClient.clear()` does not cancel a
// mutation's. Run in band (one worker: a 2-core CI runner), Jest then waits
// for those timers after the last test and fails. Route the library's timers
// through a provider that remembers them, and cancel what is left after each
// test file. The provider reads the globals at call time, so fake timers still
// drive it.
const { timeoutManager } = require('@tanstack/react-query');

// Each pending timer, with the clear function of the clock that set it (a
// test may switch between fake and real timers in between).
const pending = new Map();
const forget = (id) => {
  const clear = pending.get(id);
  pending.delete(id);
  clear?.(id);
};

timeoutManager.setTimeoutProvider({
  setTimeout: (callback, delay) => {
    const id = setTimeout(() => {
      pending.delete(id);
      callback();
    }, delay);
    const clear = clearTimeout;
    pending.set(id, (x) => clear(x));
    return id;
  },
  clearTimeout: (id) => (pending.has(id) ? forget(id) : clearTimeout(id)),
  setInterval: (callback, delay) => {
    const id = setInterval(callback, delay);
    const clear = clearInterval;
    pending.set(id, (x) => clear(x));
    return id;
  },
  clearInterval: (id) => (pending.has(id) ? forget(id) : clearInterval(id)),
});

afterAll(() => {
  for (const id of [...pending.keys()]) forget(id);
});
