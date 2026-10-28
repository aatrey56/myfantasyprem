import { useLeagueSetting } from './useLeagueSetting';
import {
  DEFAULT_VIEW,
  parseViewSettings,
  viewStorageKey,
  type ViewSettings,
} from '@/lib/playerView';

/**
 * The Players list's View and Available toggle for a league, kept on the
 * device like the sort (usePlayerSort). `ready` is false until the stored
 * settings are read (or the read failed); a change made before then wins.
 */
export function usePlayerView(
  leagueId: string,
): [ViewSettings, (next: ViewSettings) => void, boolean] {
  return useLeagueSetting(leagueId, viewStorageKey, parseViewSettings, DEFAULT_VIEW);
}
