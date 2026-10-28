import { render, screen } from '@testing-library/react-native';
import type { ReactNode } from 'react';
import { StyleSheet } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { Screen } from './Screen';
import { TopBarContext } from './tab-bar-context';
import { AppText } from './AppText';
import { RouteError } from './RouteError';

/** An iPhone with a Dynamic Island: the status bar is 59pt tall. */
const ISLAND = 59;

const phone = (children: ReactNode) => (
  <SafeAreaProvider
    initialMetrics={{
      frame: { x: 0, y: 0, width: 393, height: 852 },
      insets: { top: ISLAND, left: 0, right: 0, bottom: 34 },
    }}
  >
    {children}
  </SafeAreaProvider>
);

const frameTop = () =>
  StyleSheet.flatten(screen.getByTestId('screen-frame').props.style).paddingTop;
const contentTop = () =>
  StyleSheet.flatten(screen.getByTestId('screen-scroll').props.contentContainerStyle).paddingTop;

describe('Screen and the top safe area (iOS)', () => {
  it('keeps scrolled content below the status bar on screens outside the league shell', async () => {
    // The import, sign-in, join, account, leagues and settings screens.
    await render(
      phone(
        <Screen title="Import a league">
          <AppText>Which team is yours?</AppText>
        </Screen>,
      ),
    );
    // A fixed strip above the scroll view, not padding that scrolls away.
    expect(frameTop()).toBe(ISLAND);
    expect(contentTop()).toBeLessThan(ISLAND);
  });

  it("leaves the inset to the league shell's top bar", async () => {
    await render(
      phone(
        <TopBarContext.Provider value>
          <Screen title="Team" />
        </TopBarContext.Provider>,
      ),
    );
    expect(frameTop()).toBe(0);
  });

  it('leaves the inset to the freshness banner when it shows', async () => {
    await render(phone(<Screen title="Status" freshness={{ kind: 'offline' }} />));
    expect(frameTop()).toBe(0);
    expect(
      StyleSheet.flatten(screen.getByTestId('offline-banner').props.style).paddingTop,
    ).toBeGreaterThan(ISLAND);
  });

  it('keeps the inset when the screen scrolls itself (the draft room)', async () => {
    await render(phone(<Screen title="Draft room" hideHeader scroll={false} bleed />));
    expect(frameTop()).toBe(ISLAND);
  });
});

describe('Screen with a list (native)', () => {
  it('does not anchor the first visible row when the rows change', async () => {
    // Otherwise a filter that adds rows above it scrolls the header away.
    const list = {
      data: ['a', 'b'],
      keyExtractor: (s: string) => s,
      renderItem: (s: string) => <AppText>{s}</AppText>,
      testID: 'rows',
    };
    await render(phone(<Screen title="Players" list={list} />));
    // Let FlashList lay out its rows first.
    await screen.findByText('b');
    // FlashList passes the scroll view nothing to anchor when it is disabled.
    expect(screen.getByTestId('rows').props).toHaveProperty(
      'maintainVisibleContentPosition',
      undefined,
    );
  });
});

describe('RouteError and the top safe area (iOS)', () => {
  it('starts below the status bar outside the league shell', async () => {
    await render(phone(<RouteError error={new Error('Boom')} retry={async () => undefined} />));
    expect(
      StyleSheet.flatten(screen.getByTestId('route-error').props.style).paddingTop,
    ).toBeGreaterThan(ISLAND);
  });
});
