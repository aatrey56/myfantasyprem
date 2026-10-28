/**
 * Opening a player's card from a list: the name (or row body) is its own
 * "Player card for X" button, separate from the row's actions.
 */
import type { Schemas } from '@myfantasyprem/api-client';
import { fireEvent, render, screen } from '@testing-library/react-native';
import { Pressable } from 'react-native';

import { PlayerRow } from '../PlayerRow';
import { PlayerName } from './PlayerCardContext';

const player: Schemas['PlayerRef'] = {
  id: 'plr_01k7m2xq800000000000000001',
  displayName: 'R. Calder',
  club: { code: 'NOR', name: 'Norwich' },
  position: 'MID',
  availability: 'available',
};

describe('PlayerRow with onOpenCard', () => {
  it('opens the card from the row body; the action beside it does only its own thing', async () => {
    const onOpenCard = jest.fn();
    const onAction = jest.fn();
    await render(
      <PlayerRow
        player={player}
        testID="row"
        onOpenCard={onOpenCard}
        trailing={
          <Pressable accessibilityRole="button" accessibilityLabel="Draft" onPress={onAction} />
        }
      />,
    );
    await fireEvent.press(screen.getByLabelText('Draft'));
    expect(onAction).toHaveBeenCalledTimes(1);
    expect(onOpenCard).not.toHaveBeenCalled();
    await fireEvent.press(screen.getByRole('button', { name: 'Player card for R. Calder' }));
    expect(onOpenCard).toHaveBeenCalledTimes(1);
    expect(onAction).toHaveBeenCalledTimes(1);
  });

  it('with a row action (swap) too, the card is a separate info button', async () => {
    const onOpenCard = jest.fn();
    const onPress = jest.fn();
    await render(<PlayerRow player={player} onPress={onPress} onOpenCard={onOpenCard} />);
    await fireEvent.press(screen.getByRole('button', { name: 'Player card for R. Calder' }));
    expect(onOpenCard).toHaveBeenCalledTimes(1);
    expect(onPress).not.toHaveBeenCalled();
    await fireEvent.press(screen.getByText('R. Calder'));
    expect(onPress).toHaveBeenCalledTimes(1);
    expect(onOpenCard).toHaveBeenCalledTimes(1);
  });
});

describe('PlayerName', () => {
  it('is plain text outside a league (no card to open)', async () => {
    await render(<PlayerName player={player} />);
    expect(screen.getByText('R. Calder')).toBeTruthy();
    expect(screen.queryByRole('button')).toBeNull();
  });
});
