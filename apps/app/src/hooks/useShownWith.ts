import { useState } from 'react';

/**
 * The value the list on screen was loaded with. While a new query loads,
 * keepPreviousData leaves the last list's rows on screen: anything that
 * describes those rows (a column's label, which value it shows) must stay
 * with the last list until the new one is in, or a "GW" column would show
 * the season list's rows in their season order. `loaded` is true once the
 * rows on screen are the answer for the current value (not placeholder data).
 */
export function useShownWith<T>(value: T, loaded: boolean, same: (a: T, b: T) => boolean): T {
  const [shown, setShown] = useState(value);
  if (loaded && !same(shown, value)) {
    setShown(value);
    return value;
  }
  return shown;
}
