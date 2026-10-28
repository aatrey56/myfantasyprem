import { fireEvent, render, screen } from '@testing-library/react-native';

import { GameweekPicker } from './GameweekPicker';

describe('GameweekPicker', () => {
  it('opens a grid of every gameweek and picks one', async () => {
    const onChange = jest.fn();
    await render(<GameweekPicker gameweek={8} min={1} max={38} current={8} onChange={onChange} />);
    expect(screen.queryByTestId('gw-option-1')).toBeNull();
    await fireEvent.press(screen.getByTestId('gw-picker'));
    expect(screen.getAllByTestId(/^gw-option-\d+$/)).toHaveLength(38);
    expect(screen.getByTestId('gw-option-8')).toHaveProp(
      'accessibilityState',
      expect.objectContaining({ selected: true }),
    );
    await fireEvent.press(screen.getByTestId('gw-option-3'));
    expect(onChange).toHaveBeenCalledWith(3);
    expect(screen.queryByTestId('gw-option-1')).toBeNull();
  });

  it('picking the shown gameweek just closes it', async () => {
    const onChange = jest.fn();
    await render(<GameweekPicker gameweek={5} min={3} max={10} onChange={onChange} />);
    await fireEvent.press(screen.getByTestId('gw-picker'));
    expect(screen.getAllByTestId(/^gw-option-\d+$/)).toHaveLength(8);
    await fireEvent.press(screen.getByTestId('gw-option-5'));
    expect(onChange).not.toHaveBeenCalled();
  });
});
