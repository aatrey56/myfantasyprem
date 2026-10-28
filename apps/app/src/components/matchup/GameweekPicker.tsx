import { useState } from 'react';
import { Modal, Pressable, View } from 'react-native';

import { AppText } from '../AppText';
import { pressFeedback, type PressState } from '../pressable';
import { useTheme } from '@/theme';

interface Props {
  gameweek: number;
  min: number;
  max: number;
  /** The league's current gameweek: marked in the grid. */
  current?: number;
  onChange: (gw: number) => void;
  live?: boolean;
  testID?: string;
}

/**
 * The header's `GW 8` badge as a button: it opens a centred grid of the
 * league's gameweeks; a pick closes it and shows that gameweek.
 */
export function GameweekPicker({
  gameweek,
  min,
  max,
  current,
  onChange,
  live,
  testID = 'gw-picker',
}: Props) {
  const { colors, spacing, radii } = useTheme();
  const [open, setOpen] = useState(false);
  const weeks = Array.from({ length: Math.max(0, max - min + 1) }, (_, i) => min + i);
  return (
    <>
      <Pressable
        testID={testID}
        accessibilityRole="button"
        accessibilityLabel={`Gameweek ${gameweek}. Choose a gameweek`}
        onPress={() => setOpen(true)}
        style={(s: PressState) => [
          {
            width: 48,
            height: 48,
            borderRadius: 13,
            borderWidth: 1,
            borderColor: live ? colors.accent : colors.border,
            backgroundColor: colors.surfaceRaised,
            alignItems: 'center',
            justifyContent: 'center',
          },
          ...pressFeedback(colors, s),
        ]}
      >
        <AppText variant="eyebrow" muted style={{ fontSize: 10, lineHeight: 12 }}>
          GW ▾
        </AppText>
        <AppText weight="bold" numeric style={{ fontSize: 18, lineHeight: 20 }}>
          {String(gameweek)}
        </AppText>
      </Pressable>
      <Modal visible={open} transparent animationType="fade" onRequestClose={() => setOpen(false)}>
        <Pressable
          testID={`${testID}-backdrop`}
          accessibilityLabel="Close"
          onPress={() => setOpen(false)}
          style={{
            flex: 1,
            backgroundColor: 'rgba(0,0,0,0.5)',
            alignItems: 'center',
            justifyContent: 'center',
            padding: spacing.md,
          }}
        >
          <Pressable
            testID={`${testID}-sheet`}
            accessibilityRole="none"
            onPress={() => {}}
            style={{
              width: '100%',
              maxWidth: 360,
              borderRadius: radii.lg,
              backgroundColor: colors.surface,
              borderWidth: 1,
              borderColor: colors.border,
              padding: spacing.md,
              gap: spacing.sm,
            }}
          >
            <AppText variant="heading">Choose a gameweek</AppText>
            <View style={{ flexDirection: 'row', flexWrap: 'wrap', gap: spacing.xs }}>
              {weeks.map((gw) => {
                const selected = gw === gameweek;
                return (
                  <Pressable
                    key={gw}
                    testID={`gw-option-${gw}`}
                    accessibilityRole="button"
                    accessibilityLabel={`Gameweek ${gw}${gw === current ? ', current' : ''}`}
                    accessibilityState={{ selected }}
                    onPress={() => {
                      setOpen(false);
                      if (gw !== gameweek) onChange(gw);
                    }}
                    style={(s: PressState) => [
                      {
                        width: 44,
                        height: 40,
                        borderRadius: radii.sm,
                        alignItems: 'center',
                        justifyContent: 'center',
                        borderWidth: 1,
                        borderColor: gw === current ? colors.accent : colors.border,
                        backgroundColor: selected ? colors.accent : 'transparent',
                      },
                      ...pressFeedback(colors, s),
                    ]}
                  >
                    <AppText
                      weight="semibold"
                      numeric
                      color={selected ? colors.onAccent : undefined}
                    >
                      {String(gw)}
                    </AppText>
                  </Pressable>
                );
              })}
            </View>
          </Pressable>
        </Pressable>
      </Modal>
    </>
  );
}
