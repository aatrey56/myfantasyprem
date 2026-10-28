import { darkColors } from '@/theme/tokens';

/**
 * Pitch colours. Text on the pitch sits on PITCH.chip, never on the grass.
 */
export const PITCH = {
  grass: darkColors.turf,
  stripe: darkColors.turfStripe,
  line: 'rgba(255,255,255,0.55)',
  spot: 'rgba(255,255,255,0.7)',
  /** Name chips and the info button on the grass. */
  chip: 'rgba(8,10,12,0.82)',
  /** The chip as an opaque colour over the lighter stripe, for contrast checks. */
  chipOpaque: '#0D2018',
  text: '#F2F4F7',
  textMuted: '#C7CDD6',
  /** The selected player's ring. */
  ring: '#FFB020',
} as const;

/** The design's half pitch: our half, the halfway line along the top edge. */
export const DESIGN = { w: 358, h: 420 } as const;

/** Default (and tallest) pitch height; the width follows the container. */
export const PITCH_HEIGHT = DESIGN.h;
/** Shortest pitch drawn when fitting it to the screen. */
export const PITCH_MIN_HEIGHT = 196;

/** A player's box on the pitch: shirt ring on top, chip below. */
export const SLOT = {
  /** Widest a player box gets (the design's 84px). */
  maxWidth: 84,
  /** The shirt's ring box (full size). */
  shirt: 40,
  /** The shirt's ring box on a compact pitch. */
  compactShirt: 28,
  /** Gap kept between neighbouring boxes when a line is crowded. */
  gap: 4,
  /** Space kept above the top line and below the keeper's box. */
  pad: 6,
} as const;

/**
 * Largest text scale the pitch's labels follow (Dynamic Type / font size):
 * their maxFontSizeMultiplier, so the boxes below can allow for it.
 */
export const PITCH_FONT_SCALE_MAX = 1.35;

/** The system font scale as the pitch's labels render it: 1 to PITCH_FONT_SCALE_MAX. */
export function pitchFontScale(fontScale: number): number {
  return Math.min(PITCH_FONT_SCALE_MAX, Math.max(1, Number.isFinite(fontScale) ? fontScale : 1));
}

/**
 * Height of one player box: shirt, name chip (with the fixture line) and,
 * for points, the points box; the text's line heights grow with the font
 * scale. Compact boxes are shirt and name only (44px at scale 1, still a
 * full tap target); points sit on the shirt.
 */
export function tokenHeight(
  variant: 'lineup' | 'points',
  compact: boolean,
  fontScale: number = 1,
): number {
  const s = pitchFontScale(fontScale);
  const text = (lineHeight: number) => Math.ceil(lineHeight * s);
  if (compact) return SLOT.compactShirt + 2 + text(16);
  return variant === 'points'
    ? SLOT.shirt + 4 + (4 + text(16)) + 4 + (3 + text(18))
    : SLOT.shirt + 4 + (4 + text(16 + 15));
}

/** Shortest pitch that holds n lines of compact boxes without overlap at this font scale. */
export function minPitchHeight(lines: number, fontScale: number = 1): number {
  return Math.max(PITCH_MIN_HEIGHT, lines * tokenHeight('lineup', true, fontScale) + 2 * SLOT.pad);
}

/** The points badge on a compact player's shirt: its usual size. */
export const COMPACT_POINTS = { width: 24, height: 18 } as const;

/**
 * Where a compact player's points badge sits in their box (width wide):
 * over the shirt's top right, pulled in so it never leaves the box (and so
 * never the player's column or the pitch). Below the ⓘ when there is one.
 */
export function compactPointsBadge(
  width: number,
  withInfo: boolean,
): { right: number; top: number; maxWidth: number } {
  const ring = SLOT.compactShirt;
  // Its left edge 6px into the shirt's ring, as far right as the box allows.
  const left = width / 2 + ring / 2 - 6;
  const right = Math.max(0, width - left - COMPACT_POINTS.width);
  return { right, top: withInfo ? ring - COMPACT_POINTS.height + 2 : 2, maxWidth: width - right };
}

/** Whether n lines of full-size boxes fit a pitch h tall with a little air between lines. */
export function fitsFull(
  h: number,
  lines: number,
  variant: 'lineup' | 'points',
  fontScale: number = 1,
): boolean {
  const t = tokenHeight(variant, false, fontScale);
  return lines * t + (lines - 1) * 6 + 2 * SLOT.pad <= h;
}

export interface PitchMarkings {
  /** Inset of the touchlines and goal line. */
  inset: number;
  /** Mown stripes: y and height of each lighter band. */
  stripes: { y: number; h: number }[];
  /** The halfway line: the pitch's top edge. */
  halfway: number;
  circleR: number;
  goalLine: number;
  box: { x: number; y: number; w: number; h: number };
  six: { x: number; y: number; w: number; h: number };
  goal: { x: number; y: number; w: number; h: number };
  penaltySpot: number;
  /** The "D": an arc on the box's top edge from mid - half to mid + half. */
  arc: { half: number; r: number };
}

/**
 * The markings for a w x h half pitch: the halfway line along the top
 * with half the centre circle under it, our penalty box, six-yard box and
 * goal at the bottom. Depths follow a real half (52.5m), widths a real
 * pitch's 68m. The XI attacks upward.
 */
export function pitchMarkings(w: number, h: number): PitchMarkings {
  // The same as SLOT.pad: the keeper's box ends on the goal line.
  const inset = SLOT.pad;
  const mid = w / 2;
  const goalLine = h - inset;
  const halfway = inset;
  const len = goalLine - halfway;
  const wide = w - 2 * inset;
  const boxW = (40.3 / 68) * wide;
  const boxH = (16.5 / 52.5) * len;
  const sixW = (18.3 / 68) * wide;
  const sixH = (5.5 / 52.5) * len;
  const goalW = (7.32 / 68) * wide * 1.4;
  const circleR = Math.min((9.15 / 68) * wide, (9.15 / 52.5) * len * 1.3);
  const arcR = Math.min(circleR, boxH * 0.9);
  const band = len / 7;
  const stripes: { y: number; h: number }[] = [];
  for (let y = halfway; y < goalLine; y += 2 * band) {
    stripes.push({ y, h: Math.min(band, goalLine - y) });
  }
  return {
    inset,
    stripes,
    halfway,
    circleR,
    goalLine,
    box: { x: mid - boxW / 2, y: goalLine - boxH, w: boxW, h: boxH },
    six: { x: mid - sixW / 2, y: goalLine - sixH, w: sixW, h: sixH },
    goal: { x: mid - goalW / 2, y: goalLine, w: goalW, h: inset - 2 },
    penaltySpot: goalLine - (11 / 52.5) * len,
    arc: { half: arcR * 0.8, r: arcR },
  };
}

/**
 * Top of each line's player boxes, keeper's line first: the keeper's box
 * ends just above the goal line, the attacking line touches the top edge,
 * and the lines between are evenly spaced.
 */
export function lineTops(h: number, lines: number, tokenH: number): number[] {
  const bottom = h - SLOT.pad - tokenH;
  const top = SLOT.pad;
  if (lines <= 1) return lines === 1 ? [bottom] : [];
  const step = (bottom - top) / (lines - 1);
  return Array.from({ length: lines }, (_, i) => bottom - i * step);
}

/** Width of each player box in a line of n across a w-wide pitch. */
export function slotWidth(w: number, n: number): number {
  return Math.max(0, Math.min(SLOT.maxWidth, Math.floor(w / Math.max(1, n)) - SLOT.gap));
}

/** Centre x of each player in a line of n: the pitch split into n equal columns. */
export function slotCentres(w: number, n: number): number[] {
  return Array.from({ length: n }, (_, i) => ((i + 0.5) * w) / n);
}

export interface Placed<T> {
  item: T;
  /** Which line (0 = the keeper's). */
  line: number;
  /** Centre x of the player box. */
  x: number;
  /** The player box. */
  left: number;
  top: number;
  width: number;
  height: number;
}

/** Places lines of players (keeper's line first) on a w x h pitch, boxes tokenH tall. */
export function placeLines<T>(
  lines: readonly (readonly T[])[],
  w: number,
  h: number,
  tokenH: number = tokenHeight('lineup', false),
): Placed<T>[] {
  const tops = lineTops(h, lines.length, tokenH);
  return lines.flatMap((items, line) => {
    const width = slotWidth(w, items.length);
    return slotCentres(w, items.length).map((x, i) => ({
      item: items[i]!,
      line,
      x,
      left: x - width / 2,
      top: tops[line]!,
      width,
      height: tokenH,
    }));
  });
}
