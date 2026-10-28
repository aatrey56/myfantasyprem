import { PersistQueryClientProvider } from '@tanstack/react-query-persist-client';
import { DarkTheme, Stack, ThemeProvider } from 'expo-router';
import { StatusBar } from 'expo-status-bar';
import { useEffect, useState, type ReactNode } from 'react';
import { GestureHandlerRootView } from 'react-native-gesture-handler';

import { setupQueryLifecycle } from '@/api/lifecycle';
import { persistOptions, setupPersistFlush } from '@/api/persist';
import { createQueryClient } from '@/api/query-client';
import { AuthProvider } from '@/auth/AuthProvider';
import { SheetProvider } from '@/components/NativeSheet';
import { ToastProvider } from '@/components/Toast';
import { BrandedAssetsProvider } from '@/components/branded/BrandedAssets';
import { useAppFonts, useTheme } from '@/theme';

export { RouteError as ErrorBoundary } from '@/components/RouteError';

/**
 * Holds the first render until the MSW worker is running. The env check is
 * inline (not a shared constant) so production builds fold it to `false`
 * and drop the dynamic import, keeping mock code out of the bundle.
 */
function MockGate({ children }: { children: ReactNode }) {
  const [ready, setReady] = useState(process.env.EXPO_PUBLIC_USE_MOCKS !== 'true');
  useEffect(() => {
    if (process.env.EXPO_PUBLIC_USE_MOCKS === 'true') {
      void (async () => {
        const { startMocks } = await import('@/mocks/start');
        await startMocks();
        setReady(true);
      })();
    }
  }, []);
  return ready ? <>{children}</> : null;
}

// ExpoRoot already provides the SafeAreaProvider, so there is none here.
export default function RootLayout() {
  const [queryClient] = useState(createQueryClient);
  useEffect(() => setupQueryLifecycle(), []);
  useEffect(() => setupPersistFlush(queryClient), [queryClient]);
  const theme = useTheme();
  const fontsReady = useAppFonts();
  const navTheme = {
    ...DarkTheme,
    colors: {
      ...DarkTheme.colors,
      background: theme.colors.background,
      card: theme.colors.surface,
      text: theme.colors.text,
      border: theme.colors.border,
      primary: theme.colors.accent,
    },
  };

  // Hold the first paint until Geist is ready so text does not reflow.
  if (!fontsReady) return null;

  return (
    // Restores the saved cache before queries run: screens paint from it,
    // then revalidate what is stale (api/persist.ts).
    <PersistQueryClientProvider client={queryClient} persistOptions={persistOptions}>
      <ThemeProvider value={navTheme}>
        <StatusBar style="light" />
        {/* The gesture root the native sheets need (a plain View on web). */}
        <GestureHandlerRootView style={{ flex: 1, backgroundColor: theme.colors.background }}>
          <MockGate>
            <BrandedAssetsProvider>
              <AuthProvider>
                <ToastProvider>
                  <SheetProvider>
                    <Stack screenOptions={{ headerShown: false }} />
                  </SheetProvider>
                </ToastProvider>
              </AuthProvider>
            </BrandedAssetsProvider>
          </MockGate>
        </GestureHandlerRootView>
      </ThemeProvider>
    </PersistQueryClientProvider>
  );
}
