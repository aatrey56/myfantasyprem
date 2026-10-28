import { useRouter } from 'expo-router';
import { useState } from 'react';

import { AppText } from './AppText';
import { Button } from './Button';
import { Sheet } from './Sheet';
import { TextField } from './TextField';
import { normaliseInviteCode } from '@/lib/leagues';

/** Enter an invite code (or paste a link), then preview the league at /join/CODE. */
export function JoinLeagueSheet({ visible, onClose }: { visible: boolean; onClose: () => void }) {
  const router = useRouter();
  const [value, setValue] = useState('');
  const [error, setError] = useState<string | null>(null);
  const submit = () => {
    const code = normaliseInviteCode(value);
    if (!code) {
      setError('Invite codes are 10 letters and numbers, like K7QM2XRP4D.');
      return;
    }
    setError(null);
    setValue('');
    onClose();
    router.push(`/join/${code}`);
  };
  return (
    <Sheet
      visible={visible}
      onClose={onClose}
      title="Join a league"
      testID="join-sheet"
      presentation="center"
      footer={<Button label="Find league" onPress={submit} testID="join-code-submit" fullWidth />}
    >
      <AppText muted>
        Ask your commissioner for the invite code or link. You&apos;ll see the league before you
        join.
      </AppText>
      <TextField
        label="Invite code or link"
        value={value}
        onChangeText={(t) => {
          setValue(t);
          setError(null);
        }}
        placeholder="K7QM2XRP4D"
        autoCapitalize="characters"
        autoCorrect={false}
        onSubmitEditing={submit}
        returnKeyType="go"
        error={error}
        testID="join-code-input"
        autoFocus
      />
    </Sheet>
  );
}
