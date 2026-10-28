import { fireEvent, render, screen } from '@testing-library/react-native';
import { TextInput } from 'react-native';

import { CodeInput } from './CodeInput.web';

// The jest TextInput mock shares one focus() across instances; `this` says which box.
const focus = TextInput.prototype.focus as jest.Mock;
const lastFocused = () =>
  (focus.mock.contexts.at(-1) as { props: { testID?: string } } | undefined)?.props.testID;

const box = (i: number) => screen.getByLabelText(`Sign-in code, digit ${i} of 6`);

async function setup() {
  const onChange = jest.fn();
  const onComplete = jest.fn();
  await render(<CodeInput onChange={onChange} onComplete={onComplete} testID="code" />);
  return { onChange, onComplete };
}

beforeEach(() => focus.mockClear());

describe('CodeInput', () => {
  it('labels each of the six boxes and the group', async () => {
    await setup();
    for (let i = 1; i <= 6; i++) expect(box(i)).toBeOnTheScreen();
    expect(screen.getByLabelText('Sign-in code')).toBeOnTheScreen();
    // The first box takes the code from the keyboard's one-time-code suggestion.
    expect(box(1).props.autoComplete).toBe('one-time-code');
    expect(box(1).props.textContentType).toBe('oneTimeCode');
    expect(box(1).props.inputMode).toBe('numeric');
  });

  it('moves to the next box after each digit and finishes on the sixth', async () => {
    const { onChange, onComplete } = await setup();
    await fireEvent.changeText(box(1), '4');
    expect(lastFocused()).toBe('code-1');
    expect(onChange).toHaveBeenLastCalledWith('4');
    for (const [i, d] of [
      [2, '2'],
      [3, '0'],
      [4, '9'],
      [5, '1'],
    ] as const) {
      await fireEvent.changeText(box(i), d);
      expect(lastFocused()).toBe(`code-${i}`);
    }
    expect(onComplete).not.toHaveBeenCalled();
    await fireEvent.changeText(box(6), '7');
    expect(onComplete).toHaveBeenCalledWith('420917');
    expect(box(6).props.value).toBe('7');
  });

  it('takes a whole pasted code in one go, whatever box it lands in', async () => {
    const { onComplete } = await setup();
    await fireEvent.changeText(box(3), '123 456');
    expect(onComplete).toHaveBeenCalledWith('123456');
    expect([1, 2, 3, 4, 5, 6].map((i) => box(i).props.value).join('')).toBe('123456');
  });

  it('spreads a partial paste from the box it lands in', async () => {
    const { onChange, onComplete } = await setup();
    await fireEvent.changeText(box(1), '12');
    await fireEvent.changeText(box(3), '3-4');
    expect(onChange).toHaveBeenLastCalledWith('1234');
    expect(lastFocused()).toBe('code-4');
    expect(onComplete).not.toHaveBeenCalled();
  });

  it('ignores anything but digits', async () => {
    const { onChange } = await setup();
    await fireEvent.changeText(box(1), 'a');
    expect(box(1).props.value).toBe('');
    expect(onChange).not.toHaveBeenCalled();
    expect(focus).not.toHaveBeenCalled();
  });

  it('typing over a filled box replaces its digit', async () => {
    const { onChange } = await setup();
    await fireEvent.changeText(box(1), '1');
    await fireEvent.changeText(box(1), '17');
    expect(box(1).props.value).toBe('7');
    expect(onChange).toHaveBeenLastCalledWith('7');
  });

  it('backspace in an empty box clears and returns to the previous one', async () => {
    const { onChange } = await setup();
    await fireEvent.changeText(box(1), '12');
    focus.mockClear();
    await fireEvent(box(3), 'keyPress', { nativeEvent: { key: 'Backspace' } });
    expect(lastFocused()).toBe('code-1');
    expect(box(2).props.value).toBe('');
    expect(onChange).toHaveBeenLastCalledWith('1');
  });

  it('shows an error and marks the boxes invalid', async () => {
    await render(<CodeInput onChange={jest.fn()} error="That code is wrong." testID="code" />);
    expect(screen.getByRole('alert')).toHaveTextContent('That code is wrong.');
    expect(box(1).props['aria-invalid']).toBe(true);
  });
});
