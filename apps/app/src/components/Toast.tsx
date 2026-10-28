import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react';
import { Pressable, View } from 'react-native';
import { useSafeAreaInsets } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { CircleAlert, CircleCheck, Info, X } from './icons';
import { useLayout } from '@/hooks/useLayout';
import { haptics } from '@/lib/haptics';
import { useTheme } from '@/theme';

export type ToastTone = 'info' | 'success' | 'error';

export interface ToastInput {
  message: string;
  tone?: ToastTone;
  /** Milliseconds; errors stay a little longer. */
  duration?: number;
  /**
   * False when the action already gave its own haptic (the optimistic
   * mutations: the hook taps on confirm and on rollback).
   */
  haptic?: boolean;
  /** A button beside the message ("Edit offer"); pressing it also dismisses the toast. */
  action?: ToastAction;
}

export interface ToastAction {
  label: string;
  onPress: () => void;
}

interface ToastItem extends Required<Omit<ToastInput, 'duration' | 'haptic' | 'action'>> {
  id: number;
  action?: ToastAction;
}

const ToastContext = createContext<(t: ToastInput) => void>(() => undefined);

/** `const toast = useToast(); toast({ message: 'Saved', tone: 'success' })`. */
export const useToast = () => useContext(ToastContext);

/**
 * Short, polite status messages at the bottom (above the tab bar on phone,
 * bottom right on desktop). One at a time; a new one replaces the old.
 */
export function ToastProvider({ children }: { children: ReactNode }) {
  const [item, setItem] = useState<ToastItem | null>(null);
  const seq = useRef(0);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const show = useCallback((t: ToastInput) => {
    const tone = t.tone ?? 'info';
    // Every confirmed action (pick made, offer sent, lineup saved) and every
    // refusal toasts, so the toast is where the haptic goes.
    if (t.haptic !== false) {
      if (tone === 'success') haptics.success();
      else if (tone === 'error') haptics.error();
    }
    seq.current += 1;
    setItem({ id: seq.current, message: t.message, tone, action: t.action });
    if (timer.current) clearTimeout(timer.current);
    // An action needs time to be read and reached.
    const wait = t.duration ?? (t.action ? 10000 : tone === 'error' ? 6000 : 4000);
    timer.current = setTimeout(() => setItem(null), wait);
  }, []);

  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const value = useMemo(() => show, [show]);
  return (
    <ToastContext.Provider value={value}>
      {children}
      <ToastView item={item} onClose={() => setItem(null)} />
    </ToastContext.Provider>
  );
}

function ToastView({ item, onClose }: { item: ToastItem | null; onClose: () => void }) {
  const { colors, spacing, radii, layout } = useTheme();
  const insets = useSafeAreaInsets();
  const { isWide } = useLayout();
  const Icon = item?.tone === 'error' ? CircleAlert : item?.tone === 'success' ? CircleCheck : Info;
  const tone =
    item?.tone === 'error' ? colors.loss : item?.tone === 'success' ? colors.win : colors.accent;
  return (
    <View
      pointerEvents="box-none"
      // Always mounted so screen readers pick up changes.
      aria-live="polite"
      accessibilityLiveRegion="polite"
      style={{
        position: 'absolute',
        left: 0,
        right: 0,
        // Above the app's bottom bar, at every width.
        bottom: insets.bottom + layout.bottomTabsHeight + spacing.md,
        alignItems: isWide ? 'flex-end' : 'center',
        paddingHorizontal: isWide ? spacing.xl : spacing.lg,
      }}
    >
      {item ? (
        <View
          key={item.id}
          testID="toast"
          role={item.tone === 'error' ? 'alert' : 'status'}
          style={{
            flexDirection: 'row',
            alignItems: 'center',
            gap: spacing.sm,
            maxWidth: 440,
            width: isWide ? undefined : '100%',
            paddingLeft: spacing.md,
            paddingRight: spacing.xs,
            paddingVertical: spacing.xs,
            borderRadius: radii.md,
            borderWidth: 1,
            borderColor: colors.border,
            backgroundColor: colors.surfaceRaised,
            shadowColor: '#000',
            shadowOpacity: 0.35,
            shadowRadius: 16,
            shadowOffset: { width: 0, height: 6 },
            elevation: 6,
          }}
        >
          <Icon size={18} color={tone} aria-hidden />
          <AppText variant="meta" style={{ flex: 1 }} testID="toast-message">
            {item.message}
          </AppText>
          {item.action ? (
            <Pressable
              accessibilityRole="button"
              onPress={() => {
                onClose();
                item.action!.onPress();
              }}
              hitSlop={4}
              testID="toast-action"
              style={{ minHeight: 36, justifyContent: 'center', paddingHorizontal: spacing.sm }}
            >
              <AppText variant="meta" weight="semibold" color={colors.accent}>
                {item.action.label}
              </AppText>
            </Pressable>
          ) : null}
          <Pressable
            accessibilityRole="button"
            accessibilityLabel="Dismiss"
            onPress={onClose}
            style={{ width: 36, height: 36, alignItems: 'center', justifyContent: 'center' }}
            hitSlop={4}
          >
            <X size={16} color={colors.textMuted} />
          </Pressable>
        </View>
      ) : null}
    </View>
  );
}
