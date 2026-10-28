import { useEffect, useRef, type ReactNode } from 'react';
import { Platform, View } from 'react-native';

/**
 * One page of the league pager. On touch screens the pages either side of
 * the visible one stay rendered (off-screen, so a swipe shows them at once);
 * those are hidden from screen readers (VoiceOver would otherwise read and
 * focus three tabs' content) and take no taps or focus until they are shown.
 */
export function PagerScene({ shown, children }: { shown: boolean; children: ReactNode }) {
  const ref = useRef<View>(null);
  // react-native-web does not pass `inert` through; set it on the element.
  useEffect(() => {
    if (Platform.OS !== 'web') return;
    const el = ref.current as unknown as HTMLElement | null;
    if (shown) el?.removeAttribute('inert');
    else el?.setAttribute('inert', '');
  }, [shown]);
  return (
    <View
      ref={ref}
      testID="pager-scene"
      style={{ flex: 1, pointerEvents: shown ? 'auto' : 'none' }}
      aria-hidden={!shown}
      importantForAccessibility={shown ? 'auto' : 'no-hide-descendants'}
      accessibilityElementsHidden={!shown}
    >
      {children}
    </View>
  );
}
