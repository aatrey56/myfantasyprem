import { useId, useState } from 'react';
import { Pressable, View, type TextInputProps } from 'react-native';

import { AppText, INPUT_MAX_FONT_SCALE } from './AppText';
import { Minus, Plus } from './icons';
import { pressFeedback, type PressState } from './pressable';
import { useSheetTextInput } from './sheet-input';
import { useTheme } from '@/theme';

interface Props extends Omit<TextInputProps, 'style'> {
  label: string;
  /** Shown under the field in red, and announced. */
  error?: string | null;
  hint?: string;
  testID?: string;
}

/** A labelled text input with hint and error text. */
export function TextField({ label, error, hint, testID, onFocus, onBlur, ...rest }: Props) {
  const { colors, spacing, radii, fontSizes, layout } = useTheme();
  const [focused, setFocused] = useState(false);
  // Inside a native sheet: its own input, so the sheet rises above the keyboard.
  const renderInput = useSheetTextInput();
  const id = useId();
  return (
    <View style={{ gap: spacing.xs }}>
      <AppText variant="meta" weight="semibold" nativeID={`${id}-label`}>
        {label}
      </AppText>
      {renderInput({
        maxFontSizeMultiplier: INPUT_MAX_FONT_SCALE,
        ...rest,
        testID: testID,
        accessibilityLabel: label,
        'aria-labelledby': `${id}-label`,
        ...({
          'aria-invalid': Boolean(error),
          'aria-describedby': error ? `${id}-error` : hint ? `${id}-hint` : undefined,
        } as object),
        placeholderTextColor: colors.textSubtle,
        onFocus: (e) => {
          setFocused(true);
          onFocus?.(e);
        },
        onBlur: (e) => {
          setFocused(false);
          onBlur?.(e);
        },
        style: {
          minHeight: layout.touch,
          paddingHorizontal: spacing.md,
          borderRadius: radii.md,
          borderWidth: 1,
          borderColor: error ? colors.loss : focused ? colors.accent : colors.border,
          backgroundColor: colors.background,
          color: colors.text,
          fontSize: fontSizes.md,
          // The border colour is the focus indicator; drop the browser outline.
          ...({ outlineStyle: 'none' } as object),
        },
      })}
      {error ? (
        <AppText variant="meta" color={colors.loss} nativeID={`${id}-error`} role="alert">
          {error}
        </AppText>
      ) : hint ? (
        <AppText variant="meta" muted nativeID={`${id}-hint`}>
          {hint}
        </AppText>
      ) : null}
    </View>
  );
}

interface StepperProps {
  label: string;
  value: number;
  min: number;
  max: number;
  onChange: (value: number) => void;
  /** Formats the value, e.g. "10 teams". */
  format?: (value: number) => string;
  testID?: string;
}

/** − value + with keyboard-reachable buttons, for small integer settings. */
export function Stepper({ label, value, min, max, onChange, format, testID }: StepperProps) {
  const { colors, spacing, radii, layout } = useTheme();
  const button = (delta: number) => {
    const next = value + delta;
    const disabled = next < min || next > max;
    const Icon = delta < 0 ? Minus : Plus;
    return (
      <Pressable
        accessibilityRole="button"
        accessibilityLabel={`${delta < 0 ? 'Fewer' : 'More'} ${label.toLowerCase()}`}
        accessibilityState={{ disabled }}
        disabled={disabled}
        onPress={() => onChange(next)}
        testID={testID ? `${testID}-${delta < 0 ? 'minus' : 'plus'}` : undefined}
        style={(state: PressState) => [
          {
            width: layout.touch,
            height: layout.touch,
            alignItems: 'center',
            justifyContent: 'center',
            borderRadius: radii.md,
            borderWidth: 1,
            borderColor: colors.border,
          },
          ...pressFeedback(colors, state, { disabled }),
        ]}
      >
        <Icon size={18} color={colors.text} />
      </Pressable>
    );
  };
  return (
    <View style={{ gap: spacing.xs }}>
      <AppText variant="meta" weight="semibold">
        {label}
      </AppText>
      <View
        accessibilityRole="adjustable"
        accessibilityValue={{ min, max, now: value, text: format ? format(value) : String(value) }}
        style={{ flexDirection: 'row', alignItems: 'center', gap: spacing.md }}
      >
        {button(-1)}
        <AppText
          variant="heading"
          numeric
          testID={testID ? `${testID}-value` : undefined}
          style={{ minWidth: 88, textAlign: 'center' }}
        >
          {format ? format(value) : String(value)}
        </AppText>
        {button(1)}
      </View>
    </View>
  );
}
