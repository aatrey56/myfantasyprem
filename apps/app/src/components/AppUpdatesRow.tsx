import * as ExpoUpdates from 'expo-updates';
import { useState } from 'react';

import { Button } from '@/components/Button';
import { ListRow } from '@/components/ListRow';
import { useToast } from '@/components/Toast';
import { formatAgo } from '@/lib/time';

/** The parts of expo-updates the row uses (tests pass a fake). */
export type UpdatesApi = Pick<
  typeof ExpoUpdates,
  | 'isEnabled'
  | 'updateId'
  | 'createdAt'
  | 'isEmbeddedLaunch'
  | 'runtimeVersion'
  | 'checkForUpdateAsync'
  | 'fetchUpdateAsync'
  | 'reloadAsync'
>;

type Phase = 'idle' | 'checking' | 'ready';

/**
 * Which over-the-air update the app runs, and a manual check
 * (docs/IOS.md, "Over-the-air updates"). The app also checks on every
 * launch and runs a downloaded update on the next one; this row fetches
 * one now and offers to restart into it. Nothing renders where updates are
 * off: web, development builds, builds without APP_UPDATES_URL.
 */
export function AppUpdatesRow({
  updates = ExpoUpdates,
  divider,
}: {
  updates?: UpdatesApi;
  divider?: boolean;
}) {
  const toast = useToast();
  const [phase, setPhase] = useState<Phase>('idle');
  if (!updates.isEnabled) return null;

  const running =
    updates.isEmbeddedLaunch || !updates.updateId
      ? 'Built-in version'
      : `Update ${updates.updateId.slice(0, 8)}` +
        (updates.createdAt ? `, published ${formatAgo(updates.createdAt.getTime())}` : '');

  const check = async () => {
    setPhase('checking');
    try {
      const result = await updates.checkForUpdateAsync();
      if (!result.isAvailable) {
        setPhase('idle');
        toast({ message: 'You have the latest version', tone: 'success' });
        return;
      }
      await updates.fetchUpdateAsync();
      setPhase('ready');
    } catch {
      setPhase('idle');
      toast({ message: "Couldn't check for updates. Try again later.", tone: 'error' });
    }
  };

  return (
    <ListRow
      divider={divider}
      title="App version"
      subtitle={`${running} · runtime ${updates.runtimeVersion?.slice(0, 8) ?? 'unknown'}`}
      testID="app-updates"
      right={
        phase === 'ready' ? (
          <Button
            label="Restart to update"
            size="sm"
            onPress={() => void updates.reloadAsync()}
            testID="app-updates-restart"
          />
        ) : (
          <Button
            label="Check for updates"
            size="sm"
            variant="secondary"
            loading={phase === 'checking'}
            onPress={() => void check()}
            testID="app-updates-check"
          />
        )
      }
    />
  );
}
