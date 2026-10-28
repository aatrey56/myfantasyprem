import { createContext, useContext, type ReactNode } from 'react';

import { BUILD_BRANDED_ASSETS, useAppConfig } from '@/api/assets';

const BrandedAssetsContext = createContext(false);

/**
 * Turns official crests, shirts and player photos on when both the build
 * (EXPO_PUBLIC_BRANDED_ASSETS) and the server (GET /v1/config) allow them.
 * Off until the server answers, and off on any error: the generic shirts.
 */
export function BrandedAssetsProvider({ children }: { children: ReactNode }) {
  const config = useAppConfig();
  const on = BUILD_BRANDED_ASSETS && config.data?.data.brandedAssets === true;
  return <BrandedAssetsContext.Provider value={on}>{children}</BrandedAssetsContext.Provider>;
}

/** Whether to show official images. False outside the provider (tests, previews). */
export function useBrandedAssets(): boolean {
  return useContext(BrandedAssetsContext);
}

/** For tests and stories: force the switch. */
export const BrandedAssetsOverride = BrandedAssetsContext.Provider;
