import { withEntitlementsPlist, type ConfigPlugin } from 'expo/config-plugins';
import type { ExpoConfig } from 'expo/config';

import { load as loadEnvFiles } from '@expo/env';

import brand from './brand.json';

// The Expo CLI reads apps/app/.env* before evaluating this file, but Xcode's
// expo-updates build phase evaluates it without them (and without the shell
// that ran prebuild). Reading them here too gives prebuild, the Xcode build
// and `npm run updates:publish` the same config, so they agree on the
// runtime version (docs/IOS.md). Variables already set win.
loadEnvFiles(__dirname, { silent: true });

// Native identity is config driven so the final names can be picked when the
// app is named, without code changes (docs/IOS.md, "App identity"). Every
// value below is read at prebuild/build time, not at runtime.
const APP_NAME = brand.name;
const APP_SLUG = APP_NAME.toLowerCase();

/**
 * iOS bundle id and Android package. The default is a development id; the
 * store id is chosen when the app is named and set through EAS env
 * (`APP_BUNDLE_ID`). Changing it later makes a different app on the store.
 */
const BUNDLE_ID = process.env.APP_BUNDLE_ID ?? 'com.myfantasyprem.app.dev';

/** Custom URL scheme: `<scheme>://sign-in?token=...` opens the app. */
const SCHEME = process.env.APP_SCHEME ?? APP_SLUG;

/**
 * Domain for universal links (`https://<domain>/sign-in?token=...` opens
 * the app). Unset until the domain exists and serves
 * `/.well-known/apple-app-site-association`; a free personal team cannot
 * sign the associated-domains entitlement, so it stays off by default.
 */
const LINK_DOMAIN = process.env.APP_LINK_DOMAIN;

/**
 * Sign in with Apple needs a paid team's capability. `APPLE_SIGN_IN=false`
 * drops the entitlement for free-team device builds (docs/IOS.md).
 */
const APPLE_SIGN_IN = process.env.APPLE_SIGN_IN !== 'false';

/** expo-apple-authentication writes the entitlement on its own; this takes it out. */
const withoutAppleSignIn: ConfigPlugin = (c) =>
  withEntitlementsPlist(c, (m) => {
    delete m.modResults['com.apple.developer.applesignin'];
    return m;
  });

/**
 * Google's iOS OAuth client redirects to its reversed client id, which must
 * be a registered URL scheme. Only added when the id is configured.
 */
const GOOGLE_IOS_CLIENT_ID = process.env.EXPO_PUBLIC_GOOGLE_IOS_CLIENT_ID;
const googleScheme = GOOGLE_IOS_CLIENT_ID
  ? GOOGLE_IOS_CLIENT_ID.split('.').reverse().join('.')
  : undefined;

/**
 * Hosts a development or device build may reach over plain HTTP, such as
 * the owner's Tailscale machine (`APP_HTTP_HOSTS=tail1234.ts.net`, comma
 * separated, subdomains included). iOS blocks plain HTTP to anything but
 * local addresses otherwise. Never in a store build: use HTTPS there.
 */
const HTTP_HOSTS = (process.env.APP_HTTP_HOSTS ?? '')
  .split(',')
  .map((h: string) => h.trim())
  .filter(Boolean);
if (HTTP_HOSTS.length > 0 && process.env.EAS_BUILD_PROFILE === 'production') {
  throw new Error('APP_HTTP_HOSTS is for development builds only; production uses HTTPS.');
}

/**
 * Over-the-air JavaScript updates (expo-updates), self-hosted by the Go API
 * (`/updates/manifest`, docs/IOS.md "Over-the-air updates"). Unset: updates
 * are off and the app only ever runs the bundle it was built with (CI,
 * tests, web and every other build). Set: the app asks this URL on each
 * launch, downloads a newer update in the background and runs it on the
 * next launch; a failed check launches the cached or embedded bundle.
 */
const UPDATES_URL = process.env.APP_UPDATES_URL?.trim() || undefined;
if (UPDATES_URL && !/^https?:\/\/[^/]+\/./.test(UPDATES_URL)) {
  throw new Error(
    'APP_UPDATES_URL must be a full http(s) URL, e.g. http://host:8090/updates/manifest.',
  );
}
if (UPDATES_URL?.startsWith('http:') && process.env.EAS_BUILD_PROFILE === 'production') {
  throw new Error('APP_UPDATES_URL must use HTTPS in a production build.');
}

/**
 * The runtime version an update must match, so JavaScript never reaches a
 * binary with different native code: by default a fingerprint of the
 * native project (dependencies, config, plugins), which changes whenever a
 * reinstall is needed. `APP_RUNTIME_VERSION` pins it by hand instead; then
 * it must change with every native change, at prebuild and publish alike.
 */
const RUNTIME_VERSION = process.env.APP_RUNTIME_VERSION?.trim() || undefined;

// A native production build has no same-origin server to fall back to (the
// web build does), so it must be told where the API lives. EAS sets
// EAS_BUILD_PROFILE only for native builds.
if (process.env.EAS_BUILD_PROFILE === 'production' && !process.env.EXPO_PUBLIC_API_BASE_URL) {
  throw new Error(
    'EXPO_PUBLIC_API_BASE_URL must be set for production native builds (EAS profile "production").',
  );
}

const config: ExpoConfig = {
  name: APP_NAME,
  slug: APP_SLUG,
  scheme: googleScheme ? [SCHEME, googleScheme] : SCHEME,
  version: '0.1.0',
  orientation: 'portrait',
  // Dark only, whatever the device setting.
  userInterfaceStyle: 'dark',
  icon: './assets/images/icon.png',
  updates: UPDATES_URL
    ? { enabled: true, url: UPDATES_URL, checkAutomatically: 'ON_LOAD', fallbackToCacheTimeout: 0 }
    : { enabled: false },
  runtimeVersion: UPDATES_URL ? (RUNTIME_VERSION ?? { policy: 'fingerprint' }) : undefined,
  ios: {
    bundleIdentifier: BUNDLE_ID,
    // Phone only for v1: the layouts are tuned for phone and desktop web;
    // an iPad build would need its own screenshots and review.
    supportsTablet: false,
    usesAppleSignIn: APPLE_SIGN_IN,
    associatedDomains: LINK_DOMAIN ? [`applinks:${LINK_DOMAIN}`] : undefined,
    config: {
      // Only HTTPS/TLS from the OS: exempt, so no export compliance prompt.
      usesNonExemptEncryption: false,
    },
    infoPlist: {
      // Let ProMotion displays run lists and sheets at 120 Hz.
      CADisableMinimumFrameDurationOnPhone: true,
      // Expo's default (local networking only), plus any APP_HTTP_HOSTS.
      NSAppTransportSecurity: {
        NSAllowsArbitraryLoads: false,
        NSAllowsLocalNetworking: true,
        ...(HTTP_HOSTS.length > 0
          ? {
              NSExceptionDomains: Object.fromEntries(
                HTTP_HOSTS.map((h: string) => [
                  h,
                  { NSIncludesSubdomains: true, NSExceptionAllowsInsecureHTTPLoads: true },
                ]),
              ),
            }
          : {}),
      },
    },
    // No camera, photos, location, contacts or tracking: no usage strings.
    privacyManifests: {
      NSPrivacyTracking: false,
      NSPrivacyTrackingDomains: [],
      NSPrivacyCollectedDataTypes: [
        {
          NSPrivacyCollectedDataType: 'NSPrivacyCollectedDataTypeEmailAddress',
          NSPrivacyCollectedDataTypeLinked: true,
          NSPrivacyCollectedDataTypeTracking: false,
          NSPrivacyCollectedDataTypePurposes: ['NSPrivacyCollectedDataTypePurposeAppFunctionality'],
        },
        {
          NSPrivacyCollectedDataType: 'NSPrivacyCollectedDataTypeName',
          NSPrivacyCollectedDataTypeLinked: true,
          NSPrivacyCollectedDataTypeTracking: false,
          NSPrivacyCollectedDataTypePurposes: ['NSPrivacyCollectedDataTypePurposeAppFunctionality'],
        },
        {
          NSPrivacyCollectedDataType: 'NSPrivacyCollectedDataTypeUserID',
          NSPrivacyCollectedDataTypeLinked: true,
          NSPrivacyCollectedDataTypeTracking: false,
          NSPrivacyCollectedDataTypePurposes: ['NSPrivacyCollectedDataTypePurposeAppFunctionality'],
        },
        {
          NSPrivacyCollectedDataType: 'NSPrivacyCollectedDataTypeGameplayContent',
          NSPrivacyCollectedDataTypeLinked: true,
          NSPrivacyCollectedDataTypeTracking: false,
          NSPrivacyCollectedDataTypePurposes: ['NSPrivacyCollectedDataTypePurposeAppFunctionality'],
        },
      ],
      // Required-reason APIs used by React Native, Expo modules and their pods.
      NSPrivacyAccessedAPITypes: [
        {
          NSPrivacyAccessedAPIType: 'NSPrivacyAccessedAPICategoryUserDefaults',
          NSPrivacyAccessedAPITypeReasons: ['CA92.1'],
        },
        {
          NSPrivacyAccessedAPIType: 'NSPrivacyAccessedAPICategoryFileTimestamp',
          NSPrivacyAccessedAPITypeReasons: ['0A2A.1', '3B52.1', 'C617.1'],
        },
        {
          NSPrivacyAccessedAPIType: 'NSPrivacyAccessedAPICategorySystemBootTime',
          NSPrivacyAccessedAPITypeReasons: ['35F9.1'],
        },
        {
          NSPrivacyAccessedAPIType: 'NSPrivacyAccessedAPICategoryDiskSpace',
          NSPrivacyAccessedAPITypeReasons: ['E174.1', '85F4.1'],
        },
      ],
    },
  },
  android: {
    package: BUNDLE_ID,
    adaptiveIcon: {
      backgroundColor: brand.colors.background,
      foregroundImage: './assets/images/android-icon-foreground.png',
      backgroundImage: './assets/images/android-icon-background.png',
      monochromeImage: './assets/images/android-icon-monochrome.png',
    },
  },
  web: {
    // A single-page app: one index.html (public/index.html), client-side
    // routing, no pre-rendering.
    output: 'single',
    bundler: 'metro',
    favicon: './assets/images/favicon.png',
  },
  plugins: [
    'expo-router',
    'expo-secure-store',
    // expo-apple-authentication is auto-applied (it is installed) and writes the
    // applesignin entitlement, so omitting it is not enough; the default export
    // strips the entitlement when disabled.
    ...(APPLE_SIGN_IN ? ['expo-apple-authentication'] : []),
    'expo-web-browser',
    'expo-updates',
    [
      'expo-splash-screen',
      {
        backgroundColor: brand.colors.background,
        image: './assets/images/splash-icon.png',
        imageWidth: 120,
      },
    ],
  ],
  experiments: {
    typedRoutes: true,
  },
  // Deliberately no `extra.eas.projectId` or `owner`: the owner links the
  // project to their Expo account later (`eas init`).
};

export default APPLE_SIGN_IN ? config : withoutAppleSignIn(config);
