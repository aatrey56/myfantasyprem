import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';

/** Choices kept for the signed-in session only (not stored, gone on sign-out). */
export interface SessionPrefs {
  /** The team screen's lineup view, last picked with its toggle. */
  lineupView?: 'list' | 'pitch';
}

interface SessionPrefsValue {
  prefs: SessionPrefs;
  set: <K extends keyof SessionPrefs>(key: K, value: SessionPrefs[K]) => void;
}

const SessionPrefsContext = createContext<SessionPrefsValue | null>(null);

/** Holds the session's choices; a different user (or none) starts from nothing. */
export function SessionPrefsProvider({
  userId,
  children,
}: {
  userId: string | null;
  children: ReactNode;
}) {
  const [state, setState] = useState<{ userId: string | null; prefs: SessionPrefs }>({
    userId,
    prefs: {},
  });
  if (state.userId !== userId) setState({ userId, prefs: {} });
  const set = useCallback<SessionPrefsValue['set']>(
    (key, value) => setState((s) => ({ ...s, prefs: { ...s.prefs, [key]: value } })),
    [],
  );
  const value = useMemo(
    () => ({ prefs: state.userId === userId ? state.prefs : {}, set }),
    [state, userId, set],
  );
  return <SessionPrefsContext.Provider value={value}>{children}</SessionPrefsContext.Provider>;
}

/**
 * A choice that holds for the session: the provider's value, else the
 * fallback. Outside a provider it is local state.
 */
export function useSessionPref<K extends keyof SessionPrefs>(
  key: K,
  fallback: NonNullable<SessionPrefs[K]>,
): [NonNullable<SessionPrefs[K]>, (value: NonNullable<SessionPrefs[K]>) => void] {
  const context = useContext(SessionPrefsContext);
  const [local, setLocal] = useState<NonNullable<SessionPrefs[K]>>(fallback);
  const value = context ? (context.prefs[key] ?? fallback) : local;
  const set = useCallback(
    (next: NonNullable<SessionPrefs[K]>) => (context ? context.set(key, next) : setLocal(next)),
    [context, key],
  );
  return [value as NonNullable<SessionPrefs[K]>, set];
}
