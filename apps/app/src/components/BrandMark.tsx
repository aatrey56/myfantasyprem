import { View } from 'react-native';

import { AppText } from './AppText';
import brand from '../../brand.json';
import { useTheme } from '@/theme';

/** Our placeholder mark: the brand initials on an amber tile. Not any league's or app's logo. */
export function BrandMark({ size = 40 }: { size?: number }) {
  const { colors } = useTheme();
  return (
    <View
      aria-hidden
      style={{
        width: size,
        height: size,
        borderRadius: size * 0.28,
        backgroundColor: colors.accent,
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <AppText
        weight="bold"
        color={colors.onAccent}
        style={{
          fontSize: Math.round(size * 0.34),
          lineHeight: Math.round(size * 0.44),
          letterSpacing: -0.5,
        }}
      >
        {brand.initials}
      </AppText>
    </View>
  );
}
