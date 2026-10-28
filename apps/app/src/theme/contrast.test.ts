import { darkColors, TEXT_PAIRS, TINT_PAIRS, tint, type ColorTokens } from './tokens';
import { PITCH } from '@/components/pitch/pitchGeometry';

/** WCAG 2.x relative luminance of a #RRGGBB colour. */
function luminance(hex: string): number {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const [r, g, b] = channels.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
  return 0.2126 * r! + 0.7152 * g! + 0.0722 * b!;
}

export function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi! + 0.05) / (lo! + 0.05);
}

describe('colour contrast (WCAG AA, 4.5:1 for text)', () => {
  it('mixes a tint', () => {
    expect(tint('#FFFFFF', '#000000', 0.5)).toBe('#808080');
    expect(tint('#FFB020', '#171A1F', 0)).toBe('#171a1f');
  });

  it('keeps pitch names readable: light text on the dark chip, never on the grass', () => {
    expect(contrast(PITCH.text, PITCH.chipOpaque)).toBeGreaterThanOrEqual(4.5);
    expect(contrast(PITCH.textMuted, PITCH.chipOpaque)).toBeGreaterThanOrEqual(4.5);
    // Pitch markings stand out from the grass (non-text, 3:1).
    expect(contrast('#FFFFFF', PITCH.grass)).toBeGreaterThanOrEqual(3);
  });

  it('matches the 2026 design board', () => {
    expect(darkColors).toMatchObject({
      background: '#0B0D10',
      surface: '#14171C',
      surfaceSelected: '#2A303A',
      text: '#F2F4F7',
      textMuted: '#A6ADBA',
      textSubtle: '#7C8594',
      accent: '#FFB020',
      accentBright: '#FFC857',
      danger: '#DA291C',
      turf: '#1E7A46',
      turfStripe: '#22854D',
    });
  });

  it('keeps pitch lines and chips visible on both turf stripes (non-text, 3:1)', () => {
    for (const grass of [darkColors.turf, darkColors.turfStripe]) {
      expect(contrast('#FFFFFF', grass)).toBeGreaterThanOrEqual(3);
    }
  });

  it('computes known ratios', () => {
    expect(contrast('#000000', '#FFFFFF')).toBeCloseTo(21, 5);
    expect(contrast('#777777', '#FFFFFF')).toBeCloseTo(4.48, 2);
  });

  describe('dark theme', () => {
    const c: ColorTokens = darkColors;

    it.each(TEXT_PAIRS.map(([fg, bg]) => [fg, bg]))('%s on %s', (fg, bg) => {
      expect(
        contrast(c[fg as keyof ColorTokens], c[bg as keyof ColorTokens]),
      ).toBeGreaterThanOrEqual(4.5);
    });

    it.each(TINT_PAIRS.map(([fg, fill, base]) => [fg, fill, base]))(
      '%s on a 16%% %s tint over %s',
      (fg, fill, base) => {
        const bg = tint(c[fill as keyof ColorTokens], c[base as keyof ColorTokens]);
        expect(contrast(c[fg as keyof ColorTokens], bg)).toBeGreaterThanOrEqual(4.5);
      },
    );
  });
});
