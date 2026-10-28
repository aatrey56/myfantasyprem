import { Platform } from 'react-native';

/**
 * Browser push (Web Push) for the web build and home-screen PWAs. The
 * service worker is public/push/sw.js, registered with scope /push/ so it
 * never takes over from the mock builds' MSW worker at /.
 */
export type PushSupport =
  /** Native app: push comes with the iOS app (Expo push, not yet on). */
  | 'native'
  /** This browser has no Push API. */
  | 'unsupported'
  /** Push needs HTTPS (or localhost). */
  | 'insecure'
  /** iPhone/iPad Safari: push works only from the Home Screen app. */
  | 'needs-home-screen'
  | 'available';

const SW_URL = '/push/sw.js';
const SW_SCOPE = '/push/';
const STORED = 'mfp.pushSubscription';

interface Env {
  userAgent: string;
  standalone: boolean;
  secure: boolean;
  hasPush: boolean;
}

function webEnv(): Env {
  const nav = navigator as Navigator & { standalone?: boolean };
  return {
    userAgent: nav.userAgent,
    standalone:
      nav.standalone === true || window.matchMedia?.('(display-mode: standalone)').matches === true,
    secure: window.isSecureContext,
    hasPush: 'serviceWorker' in nav && 'PushManager' in window && 'Notification' in window,
  };
}

/** Whether this device can turn on push, and if not, why. */
export function pushSupport(env: Env | null = null): PushSupport {
  if (Platform.OS !== 'web') return 'native';
  if (!env && typeof window === 'undefined') return 'unsupported';
  const e = env ?? webEnv();
  const ios = /iPhone|iPad|iPod/.test(e.userAgent);
  if (!e.secure) return 'insecure';
  if (ios && !e.standalone) return 'needs-home-screen';
  if (!e.hasPush) return 'unsupported';
  return 'available';
}

/** The browser's permission: 'default' until the user answers. */
export function pushPermission(): NotificationPermission | 'unsupported' {
  if (Platform.OS !== 'web' || typeof Notification === 'undefined') return 'unsupported';
  return Notification.permission;
}

/** base64url (the VAPID public key) to bytes for applicationServerKey. */
export function base64UrlToBytes(s: string): Uint8Array<ArrayBuffer> {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/') + '='.repeat((4 - (s.length % 4)) % 4);
  const bin = atob(b64);
  const out = new Uint8Array(new ArrayBuffer(bin.length));
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

export interface WebPushSubscription {
  endpoint: string;
  keys: { p256dh: string; auth: string };
}

/**
 * Asks for permission, registers the push worker and subscribes with the
 * server's VAPID key; `register` sends the subscription to the API and
 * returns its id, kept so turning push off can remove it.
 */
export async function enableWebPush(
  publicKey: string,
  register: (sub: WebPushSubscription) => Promise<string>,
): Promise<'enabled' | 'denied'> {
  const permission = await Notification.requestPermission();
  if (permission !== 'granted') return 'denied';
  const reg = await navigator.serviceWorker.register(SW_URL, { scope: SW_SCOPE });
  const sub =
    (await reg.pushManager.getSubscription()) ??
    (await reg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: base64UrlToBytes(publicKey),
    }));
  const json = sub.toJSON();
  const id = await register({
    endpoint: sub.endpoint,
    keys: { p256dh: json.keys?.p256dh ?? '', auth: json.keys?.auth ?? '' },
  });
  try {
    localStorage.setItem(STORED, id);
  } catch {
    // Without storage the subscription still works; it just can't be removed from here.
  }
  return 'enabled';
}

/** Unsubscribes this browser; `unregister` removes the server's copy. */
export async function disableWebPush(unregister: (id: string) => Promise<void>): Promise<void> {
  const reg = await navigator.serviceWorker.getRegistration(SW_SCOPE);
  await (await reg?.pushManager.getSubscription())?.unsubscribe();
  let id: string | null = null;
  try {
    id = localStorage.getItem(STORED);
    localStorage.removeItem(STORED);
  } catch {
    // Nothing stored.
  }
  if (id) await unregister(id);
}

/** Whether this browser has a push subscription registered from here. */
export function hasStoredSubscription(): boolean {
  try {
    return Platform.OS === 'web' && localStorage.getItem(STORED) !== null;
  } catch {
    return false;
  }
}
