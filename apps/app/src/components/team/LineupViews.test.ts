import { fitPitchHeight, pitchTopInContent } from './LineupViews';
import { PITCH_HEIGHT, PITCH_MIN_HEIGHT } from '../pitch/pitchGeometry';

describe('fitted pitch height', () => {
  const window = 667;
  const bench = 74;
  const bottom = 61;
  // Pitch, gaps, bench and bar end on the viewport's bottom.
  const fits = (top: number, h: number) => top + h + 8 + bench + 8 + bottom <= window + 0.5;

  it('is the same after the list was scrolled: the top is in content, not on screen', () => {
    const atRest = fitPitchHeight(window, pitchTopInContent(180, 0), bench, bottom);
    // Scrolled 400px down the list, then switched: the pitch measures at -220 on screen.
    const scrolled = fitPitchHeight(window, pitchTopInContent(-220, 400), bench, bottom);
    expect(scrolled).toBe(atRest);
    expect(fits(180, atRest)).toBe(true);
    expect(atRest).toBeLessThan(PITCH_HEIGHT);
  });

  it('shrinks when something above the pitch grows (an errors banner), so the bench stays on screen', () => {
    const before = fitPitchHeight(window, pitchTopInContent(180, 0), bench, bottom);
    const after = fitPitchHeight(window, pitchTopInContent(180 + 96, 0), bench, bottom);
    expect(after).toBe(before - 96);
    expect(fits(276, after)).toBe(true);
  });

  it('follows the viewport (rotation) and clamps between the minimum and the design height', () => {
    expect(fitPitchHeight(1200, 180, bench, bottom)).toBe(PITCH_HEIGHT);
    expect(fitPitchHeight(390, 180, bench, bottom)).toBe(PITCH_MIN_HEIGHT);
    // Larger text: a taller minimum.
    expect(fitPitchHeight(390, 180, bench, bottom, 230)).toBe(230);
  });
});
