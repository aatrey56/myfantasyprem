import { useLeagueShell } from '@/api/hooks';
import { useLeagueSync } from '@/api/sync-hooks';

/**
 * Renders nothing. For a league imported from FPL it keeps the sync status
 * observed while any league tab is open, so a re-sync started in settings
 * still refreshes the league's data when it finishes (useLeagueSync polls a
 * pending job and refreshes on completion), even without realtime. The
 * visible status lives only in League settings.
 */
export function SyncObserver({ leagueId }: { leagueId: string }) {
  const shell = useLeagueShell(leagueId);
  useLeagueSync(leagueId, shell.data?.source === 'fpl_import');
  return null;
}
