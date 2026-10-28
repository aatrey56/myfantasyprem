import AsyncStorage from '@react-native-async-storage/async-storage';
import { useCallback, useEffect, useState } from 'react';

interface Held<T> {
  leagueId: string;
  value: T;
  /** Read from storage (or the read failed), or set by the user. */
  ready: boolean;
  /** Set by the user: a stored value read later does not replace it. */
  edited: boolean;
}

/**
 * A per-league setting kept on the device (AsyncStorage; localStorage on
 * web). Starts on `fallback` until the league's stored value is read; a
 * change made before then wins. Everything is held per league: a new
 * league id starts over, so one league's choice never shows in another.
 * `ready` is false until that read is done (or failed).
 */
export function useLeagueSetting<T>(
  leagueId: string,
  key: (leagueId: string) => string,
  parse: (raw: string | null) => T,
  fallback: T,
): [T, (next: T) => void, boolean] {
  const fresh = (id: string): Held<T> => ({
    leagueId: id,
    value: fallback,
    ready: false,
    edited: false,
  });
  const [held, setHeld] = useState<Held<T>>(() => fresh(leagueId));
  useEffect(() => {
    let live = true;
    AsyncStorage.getItem(key(leagueId)).then(
      (raw) => {
        if (!live) return;
        setHeld((h) => {
          if (h.leagueId === leagueId && h.edited) return { ...h, ready: true };
          return { leagueId, value: parse(raw), ready: true, edited: false };
        });
      },
      () => {
        if (!live) return;
        setHeld((h) => ({ ...(h.leagueId === leagueId ? h : fresh(leagueId)), ready: true }));
      },
    );
    return () => {
      live = false;
    };
    // key, parse and fallback are module constants.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [leagueId]);
  const update = useCallback(
    (next: T) => {
      setHeld({ leagueId, value: next, ready: true, edited: true });
      AsyncStorage.setItem(key(leagueId), JSON.stringify(next)).catch(() => {});
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [leagueId],
  );
  const current = held.leagueId === leagueId ? held : fresh(leagueId);
  return [current.value, update, current.ready];
}
