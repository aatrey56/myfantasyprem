import { execFileSync } from 'node:child_process';
import path from 'node:path';

const ENTITLEMENT = 'com.apple.developer.applesignin';
const appDir = path.resolve(__dirname, '../..');

function introspect(appleSignIn: 'true' | 'false', extra: Record<string, string> = {}): string {
  return execFileSync('npx', ['expo', 'config', '--type', 'introspect', '--json'], {
    cwd: appDir,
    env: { ...process.env, APPLE_SIGN_IN: appleSignIn, FORCE_COLOR: '0', ...extra },
    encoding: 'utf8',
  });
}

type Ats = {
  NSAllowsArbitraryLoads: boolean;
  NSAllowsLocalNetworking: boolean;
  NSExceptionDomains?: Record<string, object>;
};
const ats = (json: string): Ats =>
  (JSON.parse(json) as { ios: { infoPlist: { NSAppTransportSecurity: Ats } } }).ios.infoPlist
    .NSAppTransportSecurity;

describe('app.config Sign in with Apple', () => {
  it('omits the entitlement when APPLE_SIGN_IN=false', () => {
    expect(introspect('false')).not.toContain(ENTITLEMENT);
  });

  it('includes the entitlement when APPLE_SIGN_IN=true', () => {
    expect(introspect('true')).toContain(ENTITLEMENT);
  });
});

describe('app.config plain-HTTP hosts', () => {
  it('allows local networking only by default', () => {
    expect(ats(introspect('true', { APP_HTTP_HOSTS: '' }))).toEqual({
      NSAllowsArbitraryLoads: false,
      NSAllowsLocalNetworking: true,
    });
  });

  it('adds an exception per APP_HTTP_HOSTS entry, never arbitrary loads', () => {
    const got = ats(introspect('true', { APP_HTTP_HOSTS: 'tail1234.ts.net, lan.example' }));
    expect(got.NSAllowsArbitraryLoads).toBe(false);
    expect(got.NSExceptionDomains).toEqual({
      'tail1234.ts.net': { NSIncludesSubdomains: true, NSExceptionAllowsInsecureHTTPLoads: true },
      'lan.example': { NSIncludesSubdomains: true, NSExceptionAllowsInsecureHTTPLoads: true },
    });
  });

  it('refuses them in a production build', () => {
    expect(() =>
      introspect('true', {
        APP_HTTP_HOSTS: 'tail1234.ts.net',
        EAS_BUILD_PROFILE: 'production',
        EXPO_PUBLIC_API_BASE_URL: 'https://api.example',
      }),
    ).toThrow(/APP_HTTP_HOSTS is for development builds only/);
  });
});

describe('app.config over-the-air updates', () => {
  type Updates = { updates?: Record<string, unknown>; runtimeVersion?: unknown };
  const updates = (extra: Record<string, string>): Updates => {
    const { updates, runtimeVersion } = JSON.parse(introspect('true', extra)) as Updates;
    return { updates, runtimeVersion };
  };
  const URL = 'http://mac.tail1234.ts.net:8090/updates/manifest';

  it('are off without APP_UPDATES_URL: no url, no runtime version', () => {
    expect(updates({ APP_UPDATES_URL: '' })).toEqual({ updates: { enabled: false } });
  });

  it('check on launch, apply on the next one, matched by the native fingerprint', () => {
    expect(updates({ APP_UPDATES_URL: URL, APP_RUNTIME_VERSION: '' })).toEqual({
      updates: {
        enabled: true,
        url: URL,
        checkAutomatically: 'ON_LOAD',
        fallbackToCacheTimeout: 0,
      },
      runtimeVersion: { policy: 'fingerprint' },
    });
  });

  it('take a pinned runtime version from APP_RUNTIME_VERSION', () => {
    expect(updates({ APP_UPDATES_URL: URL, APP_RUNTIME_VERSION: 'r7' }).runtimeVersion).toBe('r7');
  });

  it('refuse a malformed URL, and plain HTTP in a production build', () => {
    expect(() => introspect('true', { APP_UPDATES_URL: 'mac:8090' })).toThrow(/full http\(s\) URL/);
    expect(() =>
      introspect('true', {
        APP_UPDATES_URL: URL,
        EAS_BUILD_PROFILE: 'production',
        EXPO_PUBLIC_API_BASE_URL: 'https://api.example',
      }),
    ).toThrow(/HTTPS in a production build/);
  });
});
