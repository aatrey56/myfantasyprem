import { useState } from 'react';
import { View } from 'react-native';

import type { RemoteImageProps } from './RemoteImage';
import { useTheme } from '@/theme';

type LoadState = 'loading' | 'loaded' | 'error';

/**
 * Web: a native lazy <img> (React Native Web's Image loads eagerly), so long
 * player lists only fetch what scrolls into view. Same contract as
 * RemoteImage.tsx: placeholder while loading, fallback on error.
 */
export function RemoteImage(props: RemoteImageProps) {
  return <RemoteImageInner key={props.uri} {...props} />;
}

function RemoteImageInner({
  uri,
  width,
  height,
  fallback,
  placeholder,
  kind,
  testID,
}: RemoteImageProps) {
  const { colors } = useTheme();
  const [state, setState] = useState<LoadState>('loading');
  if (state === 'error') return <>{fallback}</>;
  return (
    <View
      aria-hidden
      style={{
        width,
        height,
        borderRadius: 4,
        backgroundColor: state === 'loading' ? (placeholder ?? colors.surfaceRaised) : undefined,
      }}
    >
      <img
        src={uri}
        alt=""
        width={width}
        height={height}
        loading="lazy"
        decoding="async"
        draggable={false}
        data-testid={testID ?? 'branded-image'}
        data-kind={kind}
        data-state={state}
        onLoad={() => setState('loaded')}
        onError={() => setState('error')}
        style={{
          width,
          height,
          objectFit: 'contain',
          display: 'block',
          opacity: state === 'loaded' ? 1 : 0,
        }}
      />
    </View>
  );
}
