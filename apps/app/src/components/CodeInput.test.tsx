import { fireEvent, render, screen } from '@testing-library/react-native';

import { CodeInput } from './CodeInput';

// The native code field (jest-expo runs as iOS); the web boxes are in CodeInput.web.test.tsx.
const field = () => screen.getByLabelText('Sign-in code');
// The boxes are hidden from screen readers (the field reads the code out).
const shown = () =>
  [0, 1, 2, 3, 4, 5].map(
    (i) => screen.getByTestId(`code-${i}`, { includeHiddenElements: true }).props.children,
  );

async function setup() {
  const onChange = jest.fn();
  const onComplete = jest.fn();
  await render(<CodeInput onChange={onChange} onComplete={onComplete} testID="code" />);
  return { onChange, onComplete };
}

describe('CodeInput (native)', () => {
  it('is one labelled field that takes the one-time-code suggestion', async () => {
    await setup();
    expect(field().props.textContentType).toBe('oneTimeCode');
    expect(field().props.autoComplete).toBe('one-time-code');
    expect(field().props.keyboardType).toBe('number-pad');
    expect(field().props.maxLength).toBe(6);
  });

  it('shows each digit in its own box and finishes on the sixth', async () => {
    const { onChange, onComplete } = await setup();
    // However fast the digits come, each one lands in order.
    for (const typed of ['4', '42', '420', '4209', '42091']) {
      await fireEvent.changeText(field(), typed);
    }
    expect(onChange).toHaveBeenLastCalledWith('42091');
    expect(onComplete).not.toHaveBeenCalled();
    expect(shown()).toEqual(['4', '2', '0', '9', '1', '']);
    await fireEvent.changeText(field(), '420917');
    expect(onComplete).toHaveBeenCalledWith('420917');
    expect(shown()).toEqual(['4', '2', '0', '9', '1', '7']);
  });

  it('takes a pasted code, dropping anything but digits and anything past six', async () => {
    const { onComplete } = await setup();
    await fireEvent.changeText(field(), 'code: 123 456 78');
    expect(onComplete).toHaveBeenCalledWith('123456');
    expect(field().props.value).toBe('123456');
  });

  it('ignores letters and clears a box on backspace', async () => {
    const { onChange } = await setup();
    await fireEvent.changeText(field(), 'a');
    expect(onChange).not.toHaveBeenCalled();
    await fireEvent.changeText(field(), '12');
    await fireEvent.changeText(field(), '1');
    expect(onChange).toHaveBeenLastCalledWith('1');
    expect(shown()).toEqual(['1', '', '', '', '', '']);
  });

  it('shows an error and marks the field invalid', async () => {
    await render(<CodeInput onChange={jest.fn()} error="That code is wrong." testID="code" />);
    expect(screen.getByRole('alert')).toHaveTextContent('That code is wrong.');
    expect(field().props['aria-invalid']).toBe(true);
  });
});
