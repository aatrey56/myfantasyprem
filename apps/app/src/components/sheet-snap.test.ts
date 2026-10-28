import {
  DISMISS_DISTANCE,
  backdropOpacity,
  dragOffset,
  pullCloses,
  settle,
  snapHeights,
  snapOffset,
} from './sheet-snap';

describe('snapHeights', () => {
  it('resolves px and shares of the room, lowest first, without repeats', () => {
    expect(snapHeights(['100%', 300, '50%'], 800)).toEqual([300, 400, 800]);
    expect(snapHeights(['50%', 400], 800)).toEqual([400]);
  });

  it('keeps every snap inside the room and drops empty ones', () => {
    expect(snapHeights([1200, 0, '150%'], 700)).toEqual([700]);
  });
});

describe('dragOffset', () => {
  it('follows the finger between open and closed', () => {
    expect(dragOffset(120, 600)).toBe(120);
  });

  it('resists above the top and stops at the bottom', () => {
    expect(dragOffset(-90, 600)).toBe(-30);
    expect(dragOffset(900, 600)).toBe(600);
  });
});

describe('settle', () => {
  const panel = 800;
  // Half and full height.
  const heights = [400, 800];
  const half = snapOffset(400, panel);

  it('springs back after a short pull', () => {
    expect(settle(60, 0, heights, panel)).toBe(1);
    expect(settle(half + 40, 0, heights, panel)).toBe(0);
  });

  it('snaps to the nearest point', () => {
    expect(settle(half - 100, 0, heights, panel)).toBe(0);
    expect(settle(150, 0, heights, panel)).toBe(1);
  });

  it('lets a fling carry the sheet to the next snap', () => {
    expect(settle(150, 1000, heights, panel)).toBe(0);
    expect(settle(half - 50, -1500, heights, panel)).toBe(1);
  });

  it('closes when pulled well below the lowest snap', () => {
    expect(settle(half + DISMISS_DISTANCE + 1, 0, heights, panel)).toBe(-1);
  });

  it('closes on a fast fling down from the lowest snap, not from a higher one', () => {
    expect(settle(half + 10, 1500, heights, panel)).toBe(-1);
    expect(settle(10, 1500, heights, panel)).toBe(0);
  });

  it('closes a short sheet after half its height', () => {
    expect(settle(101, 0, [200], 200)).toBe(-1);
    expect(settle(90, 0, [200], 200)).toBe(0);
  });

  it('closes when there is nothing to rest on', () => {
    expect(settle(0, 0, [], 0)).toBe(-1);
  });
});

describe('pullCloses (the gorhom sheet)', () => {
  it('closes a full-height card after a moderate pull, not only past halfway', () => {
    expect(pullCloses(121, 0, 760)).toBe(true);
    expect(pullCloses(260, 650, 760)).toBe(true);
  });

  it('springs back from a short slow pull', () => {
    expect(pullCloses(60, 300, 760)).toBe(false);
  });

  it('closes on a fling down once the sheet moved', () => {
    expect(pullCloses(20, 1500, 760)).toBe(true);
  });

  it('never closes when the sheet did not move down (the content scrolled, or a pull up)', () => {
    expect(pullCloses(0, 3000, 760)).toBe(false);
    expect(pullCloses(-40, 3000, 760)).toBe(false);
  });

  it('closes a short sheet after half its height', () => {
    expect(pullCloses(81, 0, 160)).toBe(true);
    expect(pullCloses(79, 0, 160)).toBe(false);
  });
});

describe('backdropOpacity', () => {
  it('is full at or above the lowest snap and fades as the sheet closes', () => {
    expect(backdropOpacity(0, 400, 800)).toBe(1);
    expect(backdropOpacity(400, 400, 800)).toBe(1);
    expect(backdropOpacity(600, 400, 800)).toBe(0.5);
    expect(backdropOpacity(800, 400, 800)).toBe(0);
  });

  it('handles a sheet that rests at its own height', () => {
    expect(backdropOpacity(0, 0, 0)).toBe(1);
    expect(backdropOpacity(150, 0, 300)).toBe(0.5);
  });
});
