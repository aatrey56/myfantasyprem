import type { PlatformOSType, ScrollViewProps } from 'react-native';

/**
 * How a screen's scroll view treats the keyboard. iOS: the content inset
 * follows the keyboard and a drag dismisses it; Android: a drag dismisses it.
 *
 * The web gets no dismiss mode. react-native-web reads `on-drag` as "blur the
 * focused input on any scroll event", programmatic ones included, and sends a
 * trailing scroll event up to `scrollEventThrottle` ms late. A step change or
 * a validation error that shifts the page then blurs the field the user has
 * just tapped, and what they type next goes nowhere (iPhone WebKit dropped
 * the trade message and the league name). Mobile browsers handle the
 * keyboard on scroll themselves.
 */
export function scrollKeyboardProps(os: PlatformOSType) {
  return {
    keyboardShouldPersistTaps: 'handled',
    automaticallyAdjustKeyboardInsets: true,
    keyboardDismissMode: os === 'ios' ? 'interactive' : os === 'android' ? 'on-drag' : 'none',
  } as const satisfies ScrollViewProps;
}
