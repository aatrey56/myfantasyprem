import { Image } from 'expo-image';
import { useEffect, useState, type ReactNode } from 'react';
import { View } from 'react-native';

import { mockImageUri } from './mock-image';
import { useTheme } from '@/theme';

export interface RemoteImageProps {
  uri: string;
  width: number;
  height: number;
  /** Shown when the image fails (404, offline): the generic shirt. */
  fallback: ReactNode;
  /** Placeholder colour while loading. Default: the raised surface. */
  placeholder?: string;
  /** What the image is, for tests (`data-kind` on web). */
  kind: 'photo' | 'crest' | 'shirt';
  testID?: string;
}

type LoadState = 'loading' | 'loaded' | 'error';

/** Fade-in for a freshly loaded image; a cached one is shown at once. */
const IMAGE_FADE_MS = 120;

/**
 * An image from our API (/v1/assets) in a fixed box: a soft placeholder
 * while it loads, the fallback if it fails. Decorative: hidden from screen
 * readers (the name or club code next to it carries the meaning). Native
 * uses expo-image: memory and disk cache (a photo seen once paints at once
 * next time, offline too), a short fade in, and a recycling key so a list
 * row reused for another player never flashes the last one's photo.
 * RemoteImage.web.tsx is a lazy <img> (the browser's HTTP cache).
 */
export function RemoteImage(props: RemoteImageProps) {
  // A new uri starts over.
  return <RemoteImageInner key={props.uri} {...props} />;
}

function RemoteImageInner({ uri, width, height, fallback, placeholder, testID }: RemoteImageProps) {
  const { colors } = useTheme();
  const [state, setState] = useState<LoadState>('loading');
  // Mock builds load the image through the in-process mocks (mock-image.ts).
  // Inline env checks: production builds fold them away.
  const [source, setSource] = useState<string | null>(
    process.env.EXPO_PUBLIC_USE_MOCKS === 'true' ? null : uri,
  );
  useEffect(() => {
    if (process.env.EXPO_PUBLIC_USE_MOCKS !== 'true') return;
    let live = true;
    void mockImageUri(uri).then((data) => {
      if (!live) return;
      if (data) setSource(data);
      else setState('error');
    });
    return () => {
      live = false;
    };
  }, [uri]);
  if (state === 'error') return <>{fallback}</>;
  return (
    <View
      aria-hidden
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      style={{
        width,
        height,
        borderRadius: 4,
        backgroundColor: state === 'loading' ? (placeholder ?? colors.surfaceRaised) : undefined,
      }}
    >
      {source === null ? null : (
        <Image
          testID={testID ?? 'branded-image'}
          source={{ uri: source }}
          cachePolicy="memory-disk"
          recyclingKey={uri}
          contentFit="contain"
          transition={IMAGE_FADE_MS}
          style={{ width, height, opacity: state === 'loaded' ? 1 : 0 }}
          onLoad={() => setState('loaded')}
          onError={() => setState('error')}
        />
      )}
    </View>
  );
}
