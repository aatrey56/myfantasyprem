/**
 * @jest-environment jsdom
 */
import { act, useState, type ReactNode } from 'react';

import { scrollKeyboardProps } from './scroll-keyboard';

// Neither package ships types here. The web build's own components: the bug
// lives in react-native-web, which the native jest preset never loads.
type Root = { render: (node: ReactNode) => void; unmount: () => void };
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { createRoot } = require('react-dom/client') as { createRoot: (el: Element) => Root };
// eslint-disable-next-line @typescript-eslint/no-require-imports
const { ScrollView, TextInput } = require('react-native-web') as typeof import('react-native');

type Dismiss = ReturnType<typeof scrollKeyboardProps>['keyboardDismissMode'];

(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true;

function Form({ dismiss }: { dismiss: Dismiss }) {
  const [message, setMessage] = useState('');
  return (
    <ScrollView
      testID="scroll"
      scrollEventThrottle={200}
      {...scrollKeyboardProps('web')}
      keyboardDismissMode={dismiss}
    >
      <TextInput testID="message" value={message} onChangeText={setMessage} />
    </ScrollView>
  );
}

let root: Root;
let host: HTMLDivElement;

beforeEach(() => {
  jest.useFakeTimers();
  host = document.createElement('div');
  document.body.appendChild(host);
  root = createRoot(host);
});

afterEach(() => {
  act(() => root.unmount());
  host.remove();
  jest.useRealTimers();
});

/** Tap the field, then the page shifts under it (a step change, an error line). */
function focusThenScroll(dismiss: Dismiss) {
  act(() => root.render(<Form dismiss={dismiss} />));
  const input = host.querySelector<HTMLInputElement>('[data-testid="message"]')!;
  const scroller = host.querySelector<HTMLElement>('[data-testid="scroll"]')!;
  act(() => input.focus());
  expect(document.activeElement).toBe(input);
  act(() => {
    scroller.dispatchEvent(new Event('scroll'));
    // The trailing scroll event lands up to scrollEventThrottle ms later.
    jest.advanceTimersByTime(300);
  });
  return input;
}

describe('scroll view keyboard props on the web', () => {
  it('react-native-web blurs the focused input on any scroll with on-drag', () => {
    // The behaviour the web must not opt into: this is what dropped the typing.
    expect(document.activeElement).not.toBe(focusThenScroll('on-drag'));
  });

  it('keeps the field focused when the page scrolls, so typing lands in it', () => {
    const { keyboardDismissMode } = scrollKeyboardProps('web');
    expect(keyboardDismissMode).toBe('none');
    const input = focusThenScroll(keyboardDismissMode);
    expect(document.activeElement).toBe(input);
  });

  it('still dismisses the keyboard on a drag on iOS and Android', () => {
    expect(scrollKeyboardProps('ios').keyboardDismissMode).toBe('interactive');
    expect(scrollKeyboardProps('android').keyboardDismissMode).toBe('on-drag');
  });
});
