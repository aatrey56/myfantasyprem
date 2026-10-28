import { View } from 'react-native';

import { AppText } from '../AppText';
import { IconButton } from '../Button';
import { ChevronLeft, ChevronRight } from '../icons';
import { useTheme } from '@/theme';

interface Props {
  gameweek: number;
  min: number;
  max: number;
  onChange: (gw: number) => void;
}

/**
 * `‹ GW 8 ›` in the accent, with no box around it (the week stepper in
 * docs/design/SLEEPER-OBSERVED.md): the one stepper for the team, matchup,
 * league and status pages. The arrows are 44pt, keyboard-reachable buttons;
 * a disabled arrow fades.
 */
export function GameweekSwitcher({ gameweek, min, max, onChange }: Props) {
  const { colors, fontFamilies } = useTheme();
  return (
    <View testID="gw-switcher" style={{ flexDirection: 'row', alignItems: 'center' }}>
      <IconButton
        icon={ChevronLeft}
        label="Previous gameweek"
        disabled={gameweek <= min}
        onPress={() => onChange(gameweek - 1)}
        testID="gw-prev"
        size={20}
        color={colors.accent}
      />
      <AppText
        numeric
        testID="gw-label"
        accessibilityRole="text"
        aria-live="polite"
        color={colors.accent}
        style={{
          fontFamily: fontFamilies.bold,
          fontSize: 16,
          lineHeight: 22,
          minWidth: 52,
          textAlign: 'center',
        }}
      >
        {`GW ${gameweek}`}
      </AppText>
      <IconButton
        icon={ChevronRight}
        label="Next gameweek"
        disabled={gameweek >= max}
        onPress={() => onChange(gameweek + 1)}
        testID="gw-next"
        size={20}
        color={colors.accent}
      />
    </View>
  );
}
