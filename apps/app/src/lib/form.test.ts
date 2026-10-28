import { compareForm, formText } from './form';

describe('formText', () => {
  it('shows one decimal, and a dash before any gameweek is completed', () => {
    expect(formText(4.333)).toBe('4.3');
    expect(formText(3)).toBe('3.0');
    expect(formText(null)).toBe('–');
  });
});

describe('compareForm', () => {
  it('sorts best first, no form as 0', () => {
    expect([1.5, null, 4.3, 0, -0.3].sort(compareForm)).toEqual([4.3, 1.5, null, 0, -0.3]);
  });
});
