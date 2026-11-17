import type { ScrollView, View } from 'react-native';

import { scrollNodeIntoView } from './sheet-context';

describe('scrollNodeIntoView', () => {
  it('measures against the inner view ref, not the numeric node tag', () => {
    const innerRef = {} as View;
    const scrollTo = jest.fn();
    const scroller = {
      getInnerViewNode: () => 42,
      getInnerViewRef: () => innerRef,
      scrollTo,
    } as unknown as ScrollView;
    const measureLayout = jest.fn((_rel: unknown, ok: (x: number, y: number) => void) =>
      ok(0, 400),
    );
    const node = { measureLayout } as unknown as View;

    scrollNodeIntoView(scroller, node);

    expect(measureLayout.mock.calls[0]?.[0]).toBe(innerRef);
    expect(scrollTo).toHaveBeenCalledWith({ y: 240, animated: true });
  });

  it('does nothing without a scroller, a node or an inner view', () => {
    const measureLayout = jest.fn();
    const node = { measureLayout } as unknown as View;
    scrollNodeIntoView(null, node);
    scrollNodeIntoView({ getInnerViewRef: () => null } as unknown as ScrollView, node);
    expect(measureLayout).not.toHaveBeenCalled();
  });
});
