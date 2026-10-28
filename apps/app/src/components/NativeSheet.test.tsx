import { act, fireEvent, render, screen } from '@testing-library/react-native';
import { createRef } from 'react';
import { Dimensions } from 'react-native';
import { SafeAreaProvider } from 'react-native-safe-area-context';

import { AppText } from './AppText';
import { Sheet, useSheetClose, type SheetHandle } from './Sheet';
import { NATIVE_SHEET } from './sheet-snap';
import { LeagueIdContext, useLeagueId } from '@/hooks/useLeagueId';

/**
 * A stand-in for gorhom's modal: present() shows it (handle, content,
 * footer, backdrop), dismiss() hides it and reports onDismiss, as the
 * library does once its slide down ends.
 */
jest.mock('@gorhom/bottom-sheet', () => {
  const React = jest.requireActual<typeof import('react')>('react');
  const RN = jest.requireActual<typeof import('react-native')>('react-native');
  const calls: { present: number; dismiss: number; props: Record<string, unknown> | null } = {
    present: 0,
    dismiss: 0,
    props: null,
  };
  const BottomSheetModal = React.forwardRef(function BottomSheetModal(
    props: {
      children: React.ReactNode;
      onDismiss?: () => void;
      onChange?: (i: number) => void;
      handleComponent?: () => React.ReactNode;
      footerComponent?: (p: object) => React.ReactNode;
      backdropComponent?: (p: object) => React.ReactNode;
    },
    ref,
  ) {
    const [shown, setShown] = React.useState(false);
    React.useEffect(() => {
      calls.props = props as Record<string, unknown>;
    });
    React.useImperativeHandle(ref, () => ({
      present: () => {
        calls.present += 1;
        setShown(true);
        props.onChange?.(0);
      },
      dismiss: () => {
        calls.dismiss += 1;
        setShown((was) => {
          if (was) setTimeout(() => props.onDismiss?.(), 0);
          return false;
        });
      },
    }));
    if (!shown) return null;
    return (
      <RN.View testID="gorhom-modal">
        {props.backdropComponent?.({})}
        {props.handleComponent?.()}
        {props.children}
        {props.footerComponent?.({})}
      </RN.View>
    );
  });
  const Pass = ({ children }: { children?: React.ReactNode }) => <>{children}</>;
  return {
    __calls: calls,
    BottomSheetModal,
    BottomSheetModalProvider: Pass,
    BottomSheetBackdrop: () => <RN.View testID="gorhom-backdrop" />,
    BottomSheetFooter: Pass,
    BottomSheetView: RN.View,
    BottomSheetScrollView: RN.ScrollView,
    BottomSheetTextInput: RN.TextInput,
  };
});

const gorhom = jest.requireMock<{
  __calls: { present: number; dismiss: number; props: Record<string, unknown> | null };
}>('@gorhom/bottom-sheet');

const metrics = {
  frame: { x: 0, y: 0, width: 390, height: 844 },
  insets: { top: 47, left: 0, right: 0, bottom: 34 },
};

beforeAll(() => {
  NATIVE_SHEET.enabled = true;
});
afterAll(() => {
  NATIVE_SHEET.enabled = false;
});
beforeEach(() => {
  jest.useFakeTimers();
  gorhom.__calls.present = 0;
  gorhom.__calls.dismiss = 0;
});
afterEach(() => {
  jest.useRealTimers();
});

function LeagueLabel() {
  return <AppText testID="league">{useLeagueId()}</AppText>;
}

function CloseFromInside() {
  const close = useSheetClose();
  return (
    <AppText testID="inner-close" onPress={close}>
      close
    </AppText>
  );
}

async function renderSheet(props: Partial<Parameters<typeof Sheet>[0]> = {}) {
  const onClose = jest.fn();
  const ref = createRef<SheetHandle>();
  const ui = (p: Partial<Parameters<typeof Sheet>[0]>) => (
    <SafeAreaProvider initialMetrics={metrics}>
      <LeagueIdContext.Provider value="lg_1">
        <Sheet ref={ref} visible onClose={onClose} title="Pick" testID="pick" {...props} {...p}>
          <LeagueLabel />
          <CloseFromInside />
        </Sheet>
      </LeagueIdContext.Provider>
    </SafeAreaProvider>
  );
  const view = await render(ui({}));
  await act(async () => undefined);
  return {
    onClose,
    ref,
    rerender: async (p: Partial<Parameters<typeof Sheet>[0]>) => {
      await view.rerender(ui(p));
    },
  };
}

describe('Sheet on an iOS phone (gorhom)', () => {
  it('presents when visible, with the grabber, header ✕, backdrop and the screen contexts', async () => {
    await renderSheet();
    expect(gorhom.__calls.present).toBe(1);
    expect(screen.getByTestId('sheet-handle')).toBeTruthy();
    expect(screen.getByTestId('sheet-header')).toBeTruthy();
    expect(screen.getAllByTestId('sheet-close')).toHaveLength(1);
    expect(screen.getByTestId('gorhom-backdrop')).toBeTruthy();
    expect(screen.getByTestId('pick-scroll')).toBeTruthy();
    expect(screen.getByTestId('league')).toHaveTextContent('lg_1');
    expect(gorhom.__calls.props).toMatchObject({ enablePanDownToClose: true });
    // The app's close rule (pullCloses), not gorhom's "past halfway".
    expect(typeof gorhom.__calls.props?.gestureEventsHandlersHook).toBe('function');
  });

  it('a centred pop-up keeps the Modal dialog, not gorhom', async () => {
    await renderSheet({ presentation: 'center' });
    expect(gorhom.__calls.present).toBe(0);
    expect(screen.queryByTestId('gorhom-modal')).toBeNull();
    expect(screen.queryByTestId('sheet-grabber', { includeHiddenElements: true })).toBeNull();
    expect(screen.getByTestId('sheet-backdrop', { includeHiddenElements: true })).toBeTruthy();
  });

  it('closes by its ✕ after the slide down, once', async () => {
    const { onClose } = await renderSheet();
    await fireEvent.press(screen.getByTestId('sheet-close'));
    expect(onClose).not.toHaveBeenCalled();
    await act(() => jest.runAllTimers());
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  it('closeAnimated and the close inside the content do the same', async () => {
    const { onClose, ref } = await renderSheet();
    await act(() => ref.current?.closeAnimated());
    await act(() => jest.runAllTimers());
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(screen.queryByTestId('league')).toBeNull();
  });

  it('a close by its owner dismisses it without calling onClose', async () => {
    const { onClose, rerender } = await renderSheet();
    await rerender({ visible: false });
    await act(() => jest.runAllTimers());
    expect(gorhom.__calls.dismiss).toBe(1);
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.queryByTestId('gorhom-modal')).toBeNull();
  });

  it('opens again after a close', async () => {
    const { rerender } = await renderSheet();
    await rerender({ visible: false });
    await act(() => jest.runAllTimers());
    await rerender({ visible: true });
    expect(gorhom.__calls.present).toBe(2);
    expect(screen.getByTestId('league')).toBeTruthy();
  });

  it('rests at px snap heights under the status bar, opening at initialSnap', async () => {
    await renderSheet({ snapPoints: ['100%', '50%'], initialSnap: 1 });
    // The room: the window less the status bar and an 8px gap.
    const room = Dimensions.get('window').height - 47 - 8;
    expect(gorhom.__calls.props).toMatchObject({
      snapPoints: [Math.round(room / 2), room],
      index: 1,
      enableDynamicSizing: false,
    });
  });

  it('fits its content without snap points, up to 90% of the screen', async () => {
    await renderSheet();
    expect(gorhom.__calls.props).toMatchObject({ enableDynamicSizing: true });
    expect(gorhom.__calls.props?.maxDynamicContentSize).toBeLessThanOrEqual(
      Dimensions.get('window').height * 0.9,
    );
  });

  it('has no header ✕ when the content draws its own', async () => {
    await renderSheet({ hideHeader: true });
    expect(screen.queryByTestId('sheet-header')).toBeNull();
    expect(screen.queryByTestId('sheet-close')).toBeNull();
    expect(screen.getByTestId('sheet-handle')).toBeTruthy();
  });
});

describe('NativeSheet unmounted while open', () => {
  it("gorhom's dismiss on unmount does not close the owner's next sheet", async () => {
    const onClose = jest.fn();
    const view = await render(
      <SafeAreaProvider initialMetrics={metrics}>
        <Sheet visible onClose={onClose} title="Pick" testID="pick">
          <LeagueLabel />
        </Sheet>
      </SafeAreaProvider>,
    );
    await act(async () => undefined);
    const onDismiss = gorhom.__calls.props?.onDismiss as () => void;
    // A width change swaps the sheet out; gorhom then dismisses the old one.
    await view.unmount();
    await act(async () => onDismiss());
    expect(onClose).not.toHaveBeenCalled();
  });
});
