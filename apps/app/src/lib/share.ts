import * as Clipboard from 'expo-clipboard';
import { Platform, Share } from 'react-native';

/** Copies text; false if the platform refused (insecure context, permissions). */
export async function copyText(text: string): Promise<boolean> {
  try {
    await Clipboard.setStringAsync(text);
    return true;
  } catch {
    return false;
  }
}

/** True where a native share sheet exists (iOS, Android, and browsers with Web Share). */
export function canShare(): boolean {
  if (Platform.OS !== 'web') return true;
  return typeof navigator !== 'undefined' && typeof navigator.share === 'function';
}

/** Opens the share sheet; resolves false if the user cancelled or sharing failed. */
export async function shareLink(title: string, message: string, url: string): Promise<boolean> {
  try {
    if (Platform.OS === 'web') {
      await navigator.share({ title, text: message, url });
      return true;
    }
    const result = await Share.share(
      Platform.OS === 'ios' ? { title, message, url } : { title, message: `${message} ${url}` },
    );
    return result.action === Share.sharedAction;
  } catch {
    return false;
  }
}
