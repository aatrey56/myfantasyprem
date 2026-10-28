/**
 * The import screen's example shortcut: Tab on an empty input (desktop web)
 * or the "Use example" button (native, touch web) fills the placeholder link
 * and selects just the id so the owner can type over it.
 */
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { fireEvent, render, screen } from '@testing-library/react-native';
import { Platform } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import ImportLeagueScreen from '@/app/import';

jest.mock('@/api/config', () => ({ API_BASE_URL: 'http://api.test' }));
jest.mock('expo-router', () => ({
  useRouter: () => ({ replace: jest.fn(), push: jest.fn(), navigate: jest.fn() }),
}));
jest.mock('expo-router/head', () => ({ __esModule: true, default: () => null }));
jest.mock('@/components/PressableLink', () => {
  const { Pressable } = jest.requireActual('react-native');
  return { PressableLink: Pressable };
});

const EXAMPLE = 'https://draft.premierleague.com/league/12345';
const ID_RANGE = { start: EXAMPLE.length - 5, end: EXAMPLE.length };
const originalOS = Platform.OS;
const originalMatchMedia = window.matchMedia;

/** `finePointer`: what `(hover: hover) and (pointer: fine)` answers on web. */
async function open(os: 'web' | 'ios', finePointer = true) {
  Platform.OS = os;
  window.matchMedia = ((query: string) => ({
    matches: finePointer && query === '(hover: hover) and (pointer: fine)',
    media: query,
    addEventListener: () => {},
    removeEventListener: () => {},
  })) as unknown as typeof window.matchMedia;
  await render(
    <SafeAreaProvider
      initialMetrics={{
        frame: { x: 0, y: 0, width: 375, height: 812 },
        insets: { top: 0, left: 0, right: 0, bottom: 0 },
      }}
    >
      <QueryClientProvider client={new QueryClient()}>
        <ImportLeagueScreen />
      </QueryClientProvider>
    </SafeAreaProvider>,
  );
  return screen.getByTestId('league-input');
}

afterEach(() => {
  Platform.OS = originalOS;
  window.matchMedia = originalMatchMedia;
});

describe('import example shortcut', () => {
  // The Tab shortcut itself is a native DOM keydown listener (react-native-web's
  // onKeyPress preventDefault does not stop Safari's focus move), so it is
  // covered by e2e/import-shortcut.spec.ts on chromium and webkit.
  it('web: shows the Tab hint and focuses the input on open', async () => {
    const input = await open('web');
    expect(screen.getByTestId('example-hint')).toHaveTextContent(
      'Press Tab to use the example, then type your league id',
    );
    expect(screen.queryByTestId('use-example-button')).toBeNull();
    expect(input.props.autoFocus).toBe(true);
  });

  it('touch web (phone, tablet): no Tab hint, no autofocus; the button fills the example', async () => {
    const input = await open('web', false);
    expect(screen.queryByTestId('example-hint')).toBeNull();
    expect(input.props.autoFocus).toBe(false);
    await fireEvent.press(screen.getByTestId('use-example-button'));
    expect(screen.getByTestId('league-input').props.value).toBe(EXAMPLE);
  });

  it('native: the Use example button fills the example and selects the id', async () => {
    await open('ios');
    expect(screen.queryByTestId('example-hint')).toBeNull();
    expect(screen.getByTestId('league-input').props.autoFocus).toBe(false);
    await fireEvent.press(screen.getByTestId('use-example-button'));
    expect(screen.getByTestId('league-input').props.value).toBe(EXAMPLE);
    expect(screen.getByTestId('league-input').props.selection).toEqual(ID_RANGE);
  });

  it('explains a pasted league page link that has no id', async () => {
    const input = await open('web');
    await fireEvent.changeText(input, 'https://draft.premierleague.com/en/league');
    await fireEvent.press(screen.getByTestId('find-button'));
    expect(screen.getByTestId('input-error')).toHaveTextContent(
      "That page's link doesn't include your league ID. Type just your league's ID number instead.",
    );
  });
});
