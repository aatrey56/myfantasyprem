import { fireEvent, render, screen } from '@testing-library/react-native';
import { Pressable, Text } from 'react-native';

import { SessionPrefsProvider, useSessionPref } from './SessionPrefs';

function View() {
  const [view, setView] = useSessionPref('lineupView', 'pitch');
  return (
    <Pressable testID="pick-list" onPress={() => setView('list')}>
      <Text testID="view">{view}</Text>
    </Pressable>
  );
}

describe('session prefs', () => {
  it('remember a choice for the session and forget it for the next user or a sign-out', async () => {
    const app = (userId: string | null, show = true) => (
      <SessionPrefsProvider userId={userId}>{show ? <View /> : null}</SessionPrefsProvider>
    );
    const { rerender } = await render(app('usr_a'));
    expect(screen.getByTestId('view')).toHaveTextContent('pitch');
    await fireEvent.press(screen.getByTestId('pick-list'));
    expect(screen.getByTestId('view')).toHaveTextContent('list');
    // Leaving the screen and coming back: still the list.
    await rerender(app('usr_a', false));
    await rerender(app('usr_a'));
    expect(screen.getByTestId('view')).toHaveTextContent('list');
    // Signed out, then someone else signs in: the default again.
    await rerender(app(null));
    await rerender(app('usr_b'));
    expect(screen.getByTestId('view')).toHaveTextContent('pitch');
  });

  it('start from the default in a new app (nothing leaks between renders)', async () => {
    await render(
      <SessionPrefsProvider userId="usr_a">
        <View />
      </SessionPrefsProvider>,
    );
    expect(screen.getByTestId('view')).toHaveTextContent('pitch');
    await fireEvent.press(screen.getByTestId('pick-list'));
    expect(screen.getByTestId('view')).toHaveTextContent('list');
  });

  it('works without a provider (a local choice)', async () => {
    await render(<View />);
    await fireEvent.press(screen.getByTestId('pick-list'));
    expect(screen.getByTestId('view')).toHaveTextContent('list');
  });
});
