import { render, renderHook, screen } from '@testing-library/react-native';
import { StyleSheet } from 'react-native';

import { darkColors, useTheme } from '.';
import { Card } from '@/components/Card';

// The device asks for light. The app is dark only, so nothing may follow it.
jest.mock('react-native/Libraries/Utilities/useColorScheme', () => ({
  __esModule: true,
  default: () => 'light',
}));

describe('theme', () => {
  it('uses the dark tokens even when the system is light', async () => {
    const { result } = await renderHook(() => useTheme());
    expect(result.current.colors).toEqual(darkColors);
  });

  it('draws components with the dark palette under a light system scheme', async () => {
    await render(<Card testID="card" />);
    const style = StyleSheet.flatten(screen.getByTestId('card').props.style);
    expect(style.backgroundColor).toBe(darkColors.surface);
    expect(style.borderColor).toBe(darkColors.border);
  });
});
