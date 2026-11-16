import { nameMinHeight } from './MatchupList';
import { MAX_FONT_SCALE } from '../AppText';
import { lineHeights } from '@/theme';

describe('nameMinHeight (two lines of team name on a matchup card)', () => {
  it('is two body lines at the default text size', () => {
    expect(nameMinHeight(1)).toBe(lineHeights.body * 2);
  });

  it('grows with the system text size, up to the body cap', () => {
    expect(nameMinHeight(1.2)).toBe(Math.ceil(lineHeights.body * 1.2) * 2);
    expect(nameMinHeight(3)).toBe(Math.ceil(lineHeights.body * MAX_FONT_SCALE.body) * 2);
  });

  it('never shrinks below the default, and ignores a bad scale', () => {
    expect(nameMinHeight(0.8)).toBe(lineHeights.body * 2);
    expect(nameMinHeight(Number.NaN)).toBe(lineHeights.body * 2);
  });
});
