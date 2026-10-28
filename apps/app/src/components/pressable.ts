import { Platform, type ViewStyle } from 'react-native';

import { focusRing, type ColorTokens } from '@/theme';

/** What react-native-web passes to a Pressable style function (hovered/focused are web only). */
export interface PressState {
  pressed: boolean;
  hovered?: boolean;
  focused?: boolean;
}

/** Whether the last input was a key press (web), so a tap or click does not leave a focus ring. */
let keyboard = false;
if (Platform.OS === 'web' && typeof document !== 'undefined') {
  document.addEventListener('keydown', () => (keyboard = true), true);
  document.addEventListener('pointerdown', () => (keyboard = false), true);
}

/** The press state with `focused` kept only for keyboard focus (like :focus-visible). */
export function keyboardFocus(state: PressState): PressState {
  return { ...state, focused: Boolean(state.focused) && (Platform.OS !== 'web' || keyboard) };
}

/**
 * Shared feedback for every pressable: a raised fill on hover/press, a
 * visible focus ring for keyboard users only (not after a tap, or when a
 * sheet moves focus to its close button), and dimming when disabled.
 */
export function pressFeedback(
  colors: ColorTokens,
  state: PressState,
  options: { disabled?: boolean; hoverFill?: boolean } = {},
): ViewStyle[] {
  const { disabled, hoverFill = true } = options;
  const out: ViewStyle[] = [];
  if (!disabled && hoverFill && (state.hovered || state.pressed)) {
    out.push({ backgroundColor: colors.surfaceRaised });
  }
  if (state.pressed && !disabled) out.push({ opacity: 0.85 });
  if (disabled) out.push({ opacity: 0.5 });
  if (keyboardFocus(state).focused) out.push(focusRing(colors) as ViewStyle);
  return out;
}
