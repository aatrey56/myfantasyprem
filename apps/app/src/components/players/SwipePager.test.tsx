import { isSideways, swipeTarget, SWIPE_DISTANCE } from './SwipePager';

const pages = ['overview', 'log', 'fixtures'] as const;

describe('swipeTarget', () => {
  it('goes to the next page on a left swipe and back on a right one', () => {
    expect(swipeTarget(pages, 'overview', -SWIPE_DISTANCE, 0)).toBe('log');
    expect(swipeTarget(pages, 'log', -120, 10)).toBe('fixtures');
    expect(swipeTarget(pages, 'fixtures', 120, 10)).toBe('log');
  });

  it('stays put at the ends, on a short swipe and on a mostly vertical drag', () => {
    expect(swipeTarget(pages, 'overview', 200, 0)).toBe('overview');
    expect(swipeTarget(pages, 'fixtures', -200, 0)).toBe('fixtures');
    expect(swipeTarget(pages, 'log', -SWIPE_DISTANCE + 1, 0)).toBe('log');
    expect(swipeTarget(pages, 'log', -100, 90)).toBe('log');
  });
});

describe('isSideways', () => {
  it('needs a clear sideways movement', () => {
    expect(isSideways(20, 2)).toBe(true);
    expect(isSideways(8, 0)).toBe(false);
    expect(isSideways(20, 20)).toBe(false);
  });
});
