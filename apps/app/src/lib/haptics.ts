import * as Haptics from 'expo-haptics';
import { Platform } from 'react-native';

/**
 * Haptic feedback on iOS (and Android); a no-op on web. Fire and forget:
 * a device with haptics off, or without a Taptic Engine, just ignores it.
 */
const enabled = Platform.OS === 'ios' || Platform.OS === 'android';

function run(effect: () => Promise<void>) {
  if (enabled) void effect().catch(() => undefined);
}

export const haptics = {
  /** The server confirmed a move: a signing, a claim, a trade sent or answered. */
  success: () => run(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Success)),
  /** The server refused an action, or a move shown at once was rolled back. */
  error: () => run(() => Haptics.notificationAsync(Haptics.NotificationFeedbackType.Error)),
  /** A light tap: picking a player up, a swap, a star. */
  light: () => run(() => Haptics.impactAsync(Haptics.ImpactFeedbackStyle.Light)),
  /** Moving through a set of options (tabs). */
  selection: () => run(() => Haptics.selectionAsync()),
};
