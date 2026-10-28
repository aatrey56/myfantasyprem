import { useId, useRef, useState } from 'react';
import { TextInput, View } from 'react-native';

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
  /** Read out for the group and, with the position, for each box. */
  label?: string;
  length?: number;
  autoFocus?: boolean;
  /** Boxes get `${testID}-0` and on. */
  testID?: string;
}

/**
 * A one-time code in separate boxes, one digit each. Typing moves to the
 * next box and backspace in an empty one goes back; a pasted or
 * autofilled code fills every box at once (a whole code from the first
 * box, wherever it lands; a part from the box it lands in). Remount with
 * a new `key` to clear it.
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
  const [digits, setDigits] = useState<string[]>(() => Array(length).fill(''));
  const [focused, setFocused] = useState<number | null>(null);
  const boxes = useRef<(TextInput | null)[]>([]);
  const id = useId();

  const update = (next: string[], focusAt: number) => {
    setDigits(next);
    const code = next.join('');
    onChange(code);
    boxes.current[Math.min(focusAt, length - 1)]?.focus();
    if (code.length === length && next.every(Boolean)) onComplete?.(code);
  };

  const onChangeText = (i: number, text: string) => {
    let typed = text.replace(/\D/g, '');
    const prev = digits[i];
    if (!typed) {
      if (prev && !text)
        update(
          digits.map((d, j) => (j === i ? '' : d)),
          i,
        );
      return;
    }
    if (typed.length >= length) {
      update(typed.slice(0, length).split(''), length - 1);
      return;
    }
    // A digit typed into a filled box replaces it.
    if (prev && typed.length === 2 && typed.includes(prev)) {
      typed = typed[0] === prev ? typed[1]! : typed[0]!;
    }
    const next = [...digits];
    typed.split('').forEach((d, k) => {
      if (i + k < length) next[i + k] = d;
    });
    update(next, i + typed.length);
  };

  const onBackspace = (i: number) => {
    if (digits[i] || i === 0) return;
    update(
      digits.map((d, j) => (j === i - 1 ? '' : d)),
      i - 1,
    );
  };

  return (
    <View style={{ gap: spacing.xs }}>
      <View
        role="group"
        aria-label={label}
        accessibilityLabel={label}
        style={{ flexDirection: 'row', gap: spacing.sm, justifyContent: 'center' }}
      >
        {digits.map((d, i) => (
          <TextInput
            key={i}
            ref={(el) => {
              boxes.current[i] = el;
            }}
            testID={testID ? `${testID}-${i}` : undefined}
            value={d}
            onChangeText={(t) => onChangeText(i, t)}
            onKeyPress={(e) => {
              if (e.nativeEvent.key === 'Backspace') onBackspace(i);
            }}
            onFocus={() => setFocused(i)}
            onBlur={() => setFocused((f) => (f === i ? null : f))}
            accessibilityLabel={`${label}, digit ${i + 1} of ${length}`}
            aria-invalid={Boolean(error)}
            aria-describedby={error ? `${id}-error` : undefined}
            editable={!disabled}
            autoFocus={autoFocus && i === 0}
            selectTextOnFocus
            inputMode="numeric"
            keyboardType="number-pad"
            // The keyboard's code suggestion goes into the first box and fills all six.
            autoComplete={i === 0 ? 'one-time-code' : 'off'}
            textContentType={i === 0 ? 'oneTimeCode' : 'none'}
            style={{
              width: 44,
              height: layout.touch + 8,
              borderRadius: radii.md,
              borderWidth: 1,
              borderColor: error ? colors.loss : focused === i ? colors.accent : colors.border,
              backgroundColor: colors.background,
              color: colors.text,
              fontFamily: fontFamilies.monoSemibold,
              fontSize: fontSizes.xl,
              textAlign: 'center',
              // The border colour is the focus indicator; drop the browser outline.
              ...({ outlineStyle: 'none' } as object),
            }}
          />
        ))}
      </View>
      {error ? (
        <AppText
          variant="meta"
          color={colors.loss}
          nativeID={`${id}-error`}
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
