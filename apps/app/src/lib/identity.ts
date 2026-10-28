/**
 * Colours and initials for things without artwork: users (initials
 * avatars) and clubs (the shirt glyph). Derived from a stable hash, so the
 * same name or code always gets the same colour. Our own palette: no real
 * club colours, no Premier League or Sleeper colours.
 */

const AVATAR_PALETTE = [
  '#2F6FED',
  '#8B5CF6',
  '#EC4899',
  '#14B8A6',
  '#F97316',
  '#22C55E',
  '#EAB308',
  '#06B6D4',
  '#EF4444',
  '#A855F7',
  '#84CC16',
  '#64748B',
] as const;

/** [body, trim] pairs for the shirt glyph. */
const SHIRT_PALETTE: readonly (readonly [string, string])[] = [
  ['#C8102E', '#FFFFFF'],
  ['#1D4ED8', '#FFFFFF'],
  ['#0F172A', '#FACC15'],
  ['#F8FAFC', '#1D4ED8'],
  ['#7C2D12', '#7DD3FC'],
  ['#15803D', '#FFFFFF'],
  ['#FACC15', '#111827'],
  ['#6D28D9', '#F8FAFC'],
  ['#0EA5E9', '#0F172A'],
  ['#EA580C', '#111827'],
  ['#BE123C', '#FDE68A'],
  ['#334155', '#F472B6'],
  ['#065F46', '#FDE68A'],
  ['#1E3A8A', '#F87171'],
];

/** FNV-1a: small, stable, good enough to spread names over a palette. */
export function hashString(value: string): number {
  let h = 0x811c9dc5;
  for (let i = 0; i < value.length; i++) {
    h ^= value.charCodeAt(i);
    h = Math.imul(h, 0x01000193);
  }
  return h >>> 0;
}

/**
 * Up to two initials: "Sam P." → "SP", "sunday league heroes" → "SL",
 * "Gaffer's Army" → "GA" (an apostrophe joins its word), "" → "?".
 */
export function initialsOf(name: string): string {
  const words = name
    .replace(/['’]/g, '')
    .replace(/[^\p{L}\p{N}\s]/gu, ' ')
    .trim()
    .split(/\s+/)
    .filter(Boolean);
  if (words.length === 0) return '?';
  const first = words[0]!.charAt(0);
  const second = words.length > 1 ? words[1]!.charAt(0) : words[0]!.charAt(1);
  return (first + second).toUpperCase();
}

function luminance(hex: string): number {
  const [r, g, b] = [1, 3, 5]
    .map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
    .map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}

/** Dark or light text, whichever contrasts more with `bg`. */
export function readableOn(bg: string): '#0E1013' | '#FFFFFF' {
  const l = luminance(bg);
  const onDark = 1.05 / (l + 0.05);
  const onLight = (l + 0.05) / (luminance('#0E1013') + 0.05);
  return onLight >= onDark ? '#0E1013' : '#FFFFFF';
}

const HEX = /^#[0-9a-fA-F]{6}$/;

export function userColor(name: string, color?: string | null): { bg: string; fg: string } {
  const bg =
    color && HEX.test(color) ? color : AVATAR_PALETTE[hashString(name) % AVATAR_PALETTE.length]!;
  return { bg, fg: readableOn(bg) };
}

export function clubColors(code: string): { body: string; trim: string } {
  const [body, trim] = SHIRT_PALETTE[hashString(code.toUpperCase()) % SHIRT_PALETTE.length]!;
  return { body, trim };
}
