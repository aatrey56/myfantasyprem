import {
  COMPACT_POINTS,
  compactPointsBadge,
  DESIGN,
  fitsFull,
  lineTops,
  minPitchHeight,
  PITCH_FONT_SCALE_MAX,
  PITCH_HEIGHT,
  PITCH_MIN_HEIGHT,
  pitchMarkings,
  placeLines,
  SLOT,
  slotCentres,
  slotWidth,
  tokenHeight,
} from './pitchGeometry';

describe('half pitch markings', () => {
  it.each([
    [300, 196],
    [343, 220],
    [358, 420],
    [520, 420],
  ])('halfway line along the top, our box and goal at the bottom at %ix%i', (w, h) => {
    const m = pitchMarkings(w, h);
    // Half a pitch: the halfway line is the top edge, half the circle under it.
    expect(m.halfway).toBe(m.inset);
    expect(m.circleR).toBeGreaterThan(0);
    expect(m.halfway + m.circleR).toBeLessThan(m.box.y);
    // The boxes are centred and nested, the six-yard box on the goal line.
    expect(m.box.x + m.box.w / 2).toBeCloseTo(w / 2, 5);
    expect(m.six.y + m.six.h).toBeCloseTo(m.goalLine, 5);
    expect(m.box.y + m.box.h).toBeCloseTo(m.goalLine, 5);
    expect(m.six.y).toBeGreaterThan(m.box.y);
    expect(m.six.w).toBeLessThan(m.box.w);
    expect(m.box.x).toBeGreaterThanOrEqual(m.inset);
    expect(m.goal.y).toBe(m.goalLine);
    expect(m.penaltySpot).toBeGreaterThan(m.box.y);
    expect(m.penaltySpot).toBeLessThan(m.six.y);
    // The D's chord fits on the box's top edge.
    expect(m.arc.half).toBeLessThan(m.arc.r);
    expect(m.arc.half * 2).toBeLessThan(m.box.w);
  });

  it('is shorter than the old full pitch', () => {
    expect(DESIGN.h).toBe(PITCH_HEIGHT);
    expect(PITCH_MIN_HEIGHT).toBeLessThan(PITCH_HEIGHT);
  });
});

/** Every valid FPL formation: DEF 3–5, MID 2–5, FWD 1–3, ten outfielders. */
const FORMATIONS: [number, number, number][] = [];
for (let d = 3; d <= 5; d++)
  for (let m = 2; m <= 5; m++)
    for (let f = 1; f <= 3; f++) if (d + m + f === 10) FORMATIONS.push([d, m, f]);

describe('line placement', () => {
  it('lists the eight FPL formations', () => {
    expect(FORMATIONS.map((f) => f.join('-')).sort()).toEqual(
      ['3-4-3', '3-5-2', '4-3-3', '4-4-2', '4-5-1', '5-2-3', '5-3-2', '5-4-1'].sort(),
    );
  });

  it.each([1, 2, 3, 4, 5])('spaces a line of %i equally across the width', (n) => {
    for (const w of [343, 358, 520]) {
      const xs = slotCentres(w, n);
      expect(xs).toHaveLength(n);
      // Equal margins at both ends (half a gap) and equal gaps between.
      const step = w / n;
      xs.forEach((x, i) => expect(x).toBeCloseTo((i + 0.5) * step, 9));
      expect(xs[0]!).toBeCloseTo(w - xs[n - 1]!, 9);
      for (let i = 1; i < n; i++) expect(xs[i]! - xs[i - 1]!).toBeCloseTo(step, 9);
    }
    expect(slotCentres(358, 1)).toEqual([179]);
  });

  it('keeper at the bottom, forwards touching the top edge, lines evenly spaced', () => {
    for (const h of [PITCH_MIN_HEIGHT, 230, PITCH_HEIGHT]) {
      for (const compact of [true, false]) {
        const t = tokenHeight('lineup', compact);
        const [gk, def, mid, fwd] = lineTops(h, 4, t) as [number, number, number, number];
        expect(fwd).toBe(SLOT.pad);
        expect(gk + t).toBe(h - SLOT.pad);
        // Inside the goal line.
        expect(gk + t).toBeLessThanOrEqual(pitchMarkings(343, h).goalLine);
        expect(gk).toBeGreaterThan(def);
        expect(def).toBeGreaterThan(mid);
        expect(mid).toBeGreaterThan(fwd);
        expect(gk - def).toBeCloseTo(def - mid, 9);
        expect(def - mid).toBeCloseTo(mid - fwd, 9);
      }
    }
    expect(lineTops(300, 0, 44)).toEqual([]);
    expect(lineTops(300, 1, 44)).toEqual([300 - SLOT.pad - 44]);
  });

  it('turns compact only when full boxes would not fit', () => {
    expect(fitsFull(PITCH_HEIGHT, 4, 'lineup')).toBe(true);
    expect(fitsFull(PITCH_HEIGHT, 4, 'points')).toBe(true);
    expect(fitsFull(PITCH_MIN_HEIGHT, 4, 'lineup')).toBe(false);
    // Compact boxes stay full tap targets and fit the shortest pitch.
    expect(tokenHeight('lineup', true)).toBeGreaterThanOrEqual(44);
    const t = tokenHeight('lineup', true);
    expect(4 * t + 2 * SLOT.pad).toBeLessThanOrEqual(PITCH_MIN_HEIGHT);
  });

  it('splits a line into boxes no wider than the design', () => {
    expect(slotWidth(358, 4)).toBe(84);
    expect(slotWidth(358, 5)).toBe(67);
    expect(slotWidth(343, 5)).toBe(64);
  });

  // 343 = a 375px phone less the screen padding.
  it.each(FORMATIONS.flatMap((f) => [343, 358, 520].map((w) => [w, f] as const)))(
    'keeps every box on a %ipx pitch and apart from its neighbours (%j)',
    (w, [d, m, f]) => {
      const counts = [1, d, m, f];
      const lines = counts.map((n, l) => Array.from({ length: n }, (_, i) => `${l}-${i}`));
      for (const h of [PITCH_MIN_HEIGHT, PITCH_HEIGHT]) {
        const t = tokenHeight('lineup', !fitsFull(h, 4, 'lineup'));
        const placed = placeLines(lines, w, h, t);
        expect(placed).toHaveLength(11);
        for (const p of placed) {
          expect(p.left).toBeGreaterThanOrEqual(0);
          expect(p.left + p.width).toBeLessThanOrEqual(w);
          expect(p.top).toBeGreaterThanOrEqual(0);
          expect(p.top + p.height).toBeLessThanOrEqual(h);
          expect(p.x).toBeCloseTo(p.left + p.width / 2, 9);
        }
        for (let l = 0; l < counts.length; l++) {
          const row = placed.filter((p) => p.line === l);
          for (let i = 1; i < row.length; i++) {
            expect(row[i]!.left - (row[i - 1]!.left + row[i - 1]!.width)).toBeGreaterThanOrEqual(
              SLOT.gap,
            );
          }
          // Lines never overlap vertically.
          if (l > 0) {
            const below = placed.find((p) => p.line === l - 1)!;
            expect(row[0]!.top + t).toBeLessThanOrEqual(below.top);
          }
        }
        const gk = placed.find((p) => p.line === 0)!;
        expect(gk.x).toBe(w / 2);
        expect(Math.max(...placed.map((p) => p.top))).toBe(gk.top);
        expect(Math.min(...placed.map((p) => p.top))).toBe(SLOT.pad);
      }
    },
  );
});

describe('compact points badge', () => {
  // Pitch width on a 320px and a 375px phone (16px gutters).
  it.each([288, 343])('stays inside its column and the pitch, 5 in a line on a %ipx pitch', (w) => {
    const lines = [['g'], ['a', 'b', 'c', 'd', 'e']];
    const placed = placeLines(lines, w, PITCH_MIN_HEIGHT, tokenHeight('points', true));
    for (const withInfo of [false, true]) {
      for (const p of placed) {
        const badge = compactPointsBadge(p.width, withInfo);
        const n = lines[p.line]!.length;
        const col = (p.x / w) * n - 0.5;
        // Widest badge: maxWidth (a 3-digit score is cut to it, never spills).
        const right = p.left + p.width - badge.right;
        const left = right - badge.maxWidth;
        expect(badge.maxWidth).toBeGreaterThanOrEqual(COMPACT_POINTS.width);
        expect(left).toBeGreaterThanOrEqual((col * w) / n - 0.01);
        expect(right).toBeLessThanOrEqual(((col + 1) * w) / n + 0.01);
        expect(left).toBeGreaterThanOrEqual(0);
        expect(right).toBeLessThanOrEqual(w);
        expect(badge.top).toBeGreaterThanOrEqual(0);
        expect(badge.top + COMPACT_POINTS.height).toBeLessThanOrEqual(p.height);
      }
    }
  });
});

describe('larger text (font scale)', () => {
  it.each([1, 1.35, 2])('no two lines of players overlap at font scale %f', (scale) => {
    const lines = 4;
    for (const variant of ['lineup', 'points'] as const) {
      for (let h = minPitchHeight(lines, scale); h <= PITCH_HEIGHT; h += 7) {
        const compact = !fitsFull(h, lines, variant, scale);
        const t = tokenHeight(variant, compact, scale);
        const tops = lineTops(h, lines, t);
        for (let i = 1; i < tops.length; i++)
          expect(tops[i - 1]! - tops[i]!).toBeGreaterThanOrEqual(t);
        expect(tops[lines - 1]).toBeGreaterThanOrEqual(SLOT.pad);
      }
    }
  });

  it('boxes grow with the (capped) scale; the minimum pitch grows to hold them', () => {
    expect(tokenHeight('lineup', false, 1)).toBe(tokenHeight('lineup', false));
    expect(tokenHeight('lineup', false, 1.35)).toBeGreaterThan(tokenHeight('lineup', false));
    expect(tokenHeight('points', true, 3)).toBe(tokenHeight('points', true, PITCH_FONT_SCALE_MAX));
    expect(minPitchHeight(4, 1)).toBe(PITCH_MIN_HEIGHT);
    expect(minPitchHeight(4, 1.35)).toBeGreaterThan(PITCH_MIN_HEIGHT);
  });
});
