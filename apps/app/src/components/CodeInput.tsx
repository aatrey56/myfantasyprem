import { useRef, useState } from 'react';
import { Pressable, StyleSheet, TextInput, View } from 'react-native';

import { AppText } from './AppText';
import { useTheme } from '@/theme';

interface Props {
  /** The digits so far, in order (fewer than `length` while incomplete). */
  onChange: (code: string) => void;
  /** Called once every box has a digit. */
  onComplete?: (code: string) => void;
  /** Shown under the boxes in red, and announced. */
  error?: string | null;
  disabled?: boolean;
  /** Read out for the field. */
  label?: string;
  length?: number;
  autoFocus?: boolean;
  /** The field gets `${testID}-input`, the boxes' digits `${testID}-0` and on. */
  testID?: string;
}

/**
 * Native: a one-time code shown in separate boxes but typed into one
 * invisible field laid over them. iOS moves focus between fields
 * asynchronously, so one field per box drops or misplaces digits typed
 * quickly; a single field also takes the keyboard's one-time-code
 * suggestion, a paste and backspace as they come. The web keeps one input
 * per box (`CodeInput.web.tsx`). Remount with a new `key` to clear it.
 */
export function CodeInput({
  onChange,
  onComplete,
  error,
  disabled,
  label = 'Sign-in code',
  length = 6,
  autoFocus,
  testID,
}: Props) {
  const { colors, spacing, radii, fontSizes, layout, fontFamilies } = useTheme();
  const [code, setCode] = useState('');
  const [focused, setFocused] = useState(false);
  const input = useRef<TextInput>(null);

  const onChangeText = (text: string) => {
    const next = text.replace(/\D/g, '').slice(0, length);
    if (next === code) return;
    setCode(next);
    onChange(next);
    if (next.length === length) onComplete?.(next);
  };

  // The box the next digit goes in (the last one once the code is full).
  const active = Math.min(code.length, length - 1);

  return (
    <View style={{ gap: spacing.xs }}>
      <Pressable
        onPress={() => input.current?.focus()}
        disabled={disabled}
        accessible={false}
        style={{ flexDirection: 'row', gap: spacing.sm, justifyContent: 'center' }}
      >
        {Array.from({ length }, (_, i) => (
          <View
            key={i}
            importantForAccessibility="no-hide-descendants"
            accessibilityElementsHidden
            style={{
              width: 44,
              height: layout.touch + 8,
              alignItems: 'center',
              justifyContent: 'center',
              borderRadius: radii.md,
              borderWidth: 1,
              borderColor: error
                ? colors.loss
                : focused && i === active
                  ? colors.accent
                  : colors.border,
              backgroundColor: colors.background,
            }}
          >
            <AppText
              testID={testID ? `${testID}-${i}` : undefined}
              style={{
                fontFamily: fontFamilies.monoSemibold,
                fontSize: fontSizes.xl,
                lineHeight: fontSizes.xl * 1.3,
              }}
              maxFontSizeMultiplier={1}
            >
              {code[i] ?? ''}
            </AppText>
          </View>
        ))}
        <TextInput
          ref={input}
          testID={testID ? `${testID}-input` : undefined}
          value={code}
          onChangeText={onChangeText}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          accessibilityLabel={label}
          aria-invalid={Boolean(error)}
          editable={!disabled}
          autoFocus={autoFocus}
          maxLength={length}
          inputMode="numeric"
          keyboardType="number-pad"
          autoComplete="one-time-code"
          textContentType="oneTimeCode"
          caretHidden
          // Laid over the boxes: taps land here; its own text stays invisible.
          style={[StyleSheet.absoluteFill, { color: 'transparent', fontSize: 1 }]}
          selectionColor="transparent"
        />
      </Pressable>
      {error ? (
        <AppText
          variant="meta"
          color={colors.loss}
          role="alert"
          testID={testID ? `${testID}-error` : undefined}
          style={{ textAlign: 'center' }}
        >
          {error}
        </AppText>
      ) : null}
    </View>
  );
}
