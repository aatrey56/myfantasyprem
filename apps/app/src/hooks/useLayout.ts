import { useWindowDimensions } from 'react-native';

import { layout } from '@/theme';

/**
 * Breakpoints (BRIEF §2): phone below 768px (bottom tab bar), tablet from
 * 768px (horizontal top nav bar), desktop from 1024px (side panels).
 * `isWide` means "not a phone".
 */
export function useLayout() {
  const { width, height } = useWindowDimensions();
  const isWide = width >= layout.breakpoints.tablet;
  const isDesktop = width >= layout.breakpoints.wide;
  return { width, height, isPhone: !isWide, isWide, isDesktop };
}
