import { useLeagueSetting } from './useLeagueSetting';
import { DEFAULT_SORT, parseSortChoice, sortStorageKey, type SortChoice } from '@/lib/playerSort';

/**
 * The Players list's sort for a league, kept on the device (AsyncStorage;
 * localStorage on web). Starts on the default until the stored choice is
 * read; a pick made before then wins. `ready` is false until that read is
 * done (or failed): hold the list's query until then, or a stored sort
 * first asks for the default list.
 */
export function usePlayerSort(leagueId: string): [SortChoice, (next: SortChoice) => void, boolean] {
  return useLeagueSetting(leagueId, sortStorageKey, parseSortChoice, DEFAULT_SORT);
}
