/* global jest */
// Reanimated and its worklets runtime are native modules: Jest runs their mocks.
jest.mock('react-native-worklets', () => require('react-native-worklets/src/mock'));
jest.mock('react-native-reanimated', () => require('react-native-reanimated/mock'));
require('react-native-gesture-handler/jestSetup');
// FlashList measures its window natively and Jest has no layout: give it a
// window tall enough that the lists under test draw every row, as the
// screens' tests expect (its own jestSetup also remaps an export 2.0 lacks).
jest.mock('@shopify/flash-list/dist/recyclerview/utils/measureLayout', () => {
  const actual = jest.requireActual('@shopify/flash-list/dist/recyclerview/utils/measureLayout');
  const box = (width, height) => jest.fn(() => ({ x: 0, y: 0, width, height }));
  return {
    ...actual,
    measureParentSize: box(400, 20000),
    measureFirstChildLayout: box(400, 20000),
    measureItemLayout: box(400, 60),
  };
});
// Jest has no layout pass: a sheet must not close itself for want of one
// (Sheet.safety.test.tsx turns the safety back on).
require('./src/components/sheet-snap').PANEL_LAYOUT_TIMEOUT.ms = Infinity;
// The sheet suites drive the hand-written (web and wide) sheet; the native
// gorhom sheet has its own suite (NativeSheet.test.tsx).
require('./src/components/sheet-snap').NATIVE_SHEET.enabled = false;
