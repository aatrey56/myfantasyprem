import { View } from 'react-native';

import { AppText } from '../AppText';
import { IconButton } from '../Button';
import { ChevronLeft, ChevronRight } from '../icons';
import { useTheme } from '@/theme';

const labelStyle = { fontSize: 16, lineHeight: 22, minWidth: 52, textAlign: 'center' } as const;

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
        style={[labelStyle, { fontFamily: fontFamilies.bold }]}
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

/**
 * The stepper's footprint with nothing in it: the same arrows' width and the
 * same label text (so it grows with the user's text size), invisible and out
 * of the accessibility tree. Holds the slot while the gameweek loads.
 */
export function GameweekSwitcherSlot({ gameweek }: { gameweek: number }) {
  const { fontFamilies, layout } = useTheme();
  return (
    <View
      style={{ flexDirection: 'row', alignItems: 'center', opacity: 0 }}
      pointerEvents="none"
      aria-hidden
      accessibilityElementsHidden
      importantForAccessibility="no-hide-descendants"
      testID="gw-switcher-slot"
    >
      <View style={{ width: layout.touch, height: layout.touch }} />
      <AppText numeric style={[labelStyle, { fontFamily: fontFamilies.bold }]}>
        {`GW ${gameweek}`}
      </AppText>
      <View style={{ width: layout.touch, height: layout.touch }} />
    </View>
  );
}
