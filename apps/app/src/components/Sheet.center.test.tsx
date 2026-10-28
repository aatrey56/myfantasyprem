import { render, screen } from '@testing-library/react-native';
import { Dimensions } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { Sheet } from './Sheet';

const PHONE = {
  frame: { x: 0, y: 0, width: 375, height: 812 },
  insets: { top: 47, left: 0, right: 0, bottom: 34 },
};

beforeEach(() => {
  jest
    .spyOn(Dimensions, 'get')
    .mockReturnValue({ width: 375, height: 812, scale: 3, fontScale: 1 });
});
afterEach(() => jest.restoreAllMocks());

const open = (presentation?: 'sheet' | 'center') =>
  render(
    <SafeAreaProvider initialMetrics={PHONE}>
      <Sheet visible onClose={() => {}} title="Filters" testID="sheet" presentation={presentation}>
        <AppText>Body</AppText>
      </Sheet>
    </SafeAreaProvider>,
  );

describe('Sheet presentation', () => {
  it('is a bottom sheet with a grabber on a phone by default', async () => {
    await open();
    expect(screen.getByTestId('sheet-grabber', { includeHiddenElements: true })).toBeTruthy();
  });

  it('center: a centred dialog on a phone too, closed by the ✕ or the backdrop', async () => {
    await open('center');
    expect(screen.queryByTestId('sheet-grabber', { includeHiddenElements: true })).toBeNull();
    expect(screen.getByTestId('sheet-close', { includeHiddenElements: true })).toBeTruthy();
    expect(screen.getByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeTruthy();
    const style = [screen.getByTestId('sheet', { includeHiddenElements: true }).props.style]
      .flat(Infinity)
      .reduce((a, b) => ({ ...a, ...(b ?? {}) }), {});
    expect(style.position).not.toBe('absolute');
    expect(style.width).toBe(520);
    expect(style.maxWidth).toBe('100%');
  });
});
