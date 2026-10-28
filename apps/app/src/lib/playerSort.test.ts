import {
  DEFAULT_SORT,
  filterSortGroups,
  formatSortValue,
  parseSortChoice,
  SORT_GROUPS,
  sortChipA11y,
  sortChipText,
  sortValueA11y,
  sortOption,
} from './playerSort';

const values = (q: string) => filterSortGroups(q).flatMap((g) => g.options.map((x) => x.value));

describe('SORT_GROUPS', () => {
  it('has the six groups in order', () => {
    expect(SORT_GROUPS.map((g) => g.title)).toEqual([
      'Popular',
      'Points & rank',
      'Attacking',
      'Defending',
      'Creativity & threat',
      'Discipline & other',
    ]);
  });

  it('puts exactly the seven popular stats first, in order', () => {
    expect(SORT_GROUPS[0]!.options.map((x) => x.label)).toEqual([
      'Total points',
      'Form',
      'Games started',
      'Goals',
      'Assists',
      'Clean sheets',
      'Defensive contribution',
    ]);
  });

  it('lists each of the 31 options once', () => {
    const all = SORT_GROUPS.flatMap((g) => g.options.map((x) => x.value));
    expect(all).toHaveLength(31);
    expect(new Set(all).size).toBe(31);
  });

  it('keeps the attacking expected stats in order', () => {
    expect(SORT_GROUPS[2]!.options.map((x) => x.short)).toEqual(['xG', 'xA', 'xGI']);
  });

  it('has no parentheses in any label, short or long name', () => {
    for (const x of SORT_GROUPS.flatMap((g) => g.options)) {
      for (const t of [x.label, x.short, x.long, sortChipText(x.value), sortChipA11y(x.value)]) {
        expect(t).not.toMatch(/[()]/);
      }
      expect(sortValueA11y(x.value, 1)).not.toMatch(/[()]/);
    }
  });
});

describe('filterSortGroups', () => {
  it('returns everything for an empty search', () => {
    expect(filterSortGroups('  ')).toBe(SORT_GROUPS);
  });

  it('matches xg case-insensitively: xG, xGI and xGC but not xA', () => {
    expect(values('xg')).toEqual([
      'expected_goals',
      'expected_goal_involvements',
      'expected_goals_conceded',
    ]);
    expect(values('XG')).toEqual(values('xg'));
  });

  it('matches the long name', () => {
    expect(values('clearances')).toEqual(['clearances_blocks_interceptions']);
  });

  it('matches the short label', () => {
    expect(values('cs')).toContain('clean_sheets');
  });

  it('drops groups with no match', () => {
    expect(filterSortGroups('yellow').map((g) => g.title)).toEqual(['Discipline & other']);
    expect(filterSortGroups('zzzz')).toEqual([]);
  });
});

describe('labels', () => {
  it('shows the chip as the short label, with no direction', () => {
    expect(sortChipText('expected_goal_involvements')).toBe('Sort: xGI');
    expect(sortChipText('total_points')).toBe('Sort: PTS');
  });

  it('formats a row value: dash for null, one decimal for fractional stats', () => {
    expect(formatSortValue('expected_goal_involvements', 2.24)).toBe('2.2');
    expect(formatSortValue('expected_goal_involvements', null)).toBe('–');
    expect(formatSortValue('form', 5)).toBe('5.0');
    expect(formatSortValue('goals', 7)).toBe('7');
    expect(formatSortValue('total_points', 0)).toBe('0');
  });

  it('knows no option for name or trend sorts', () => {
    expect(sortOption('name')).toBeUndefined();
    expect(sortOption('form_rise')).toBeUndefined();
  });
});

describe('accessibility labels', () => {
  it('reads the chip as the sort name, not abbreviation, with no direction', () => {
    expect(sortChipA11y('expected_goal_involvements')).toBe('Sort: Expected goal involvements');
    expect(sortChipA11y('goals')).toBe('Sort: Goals');
  });

  it('reads a row value with the stat name, and no value for null', () => {
    expect(sortValueA11y('goals', 3)).toBe('Goals 3');
    expect(sortValueA11y('form', 5)).toBe('Form 5.0');
    expect(sortValueA11y('starts', null)).toBe('Games started, no value');
  });
});

describe('parseSortChoice', () => {
  it('reads a stored choice', () => {
    expect(parseSortChoice('{"sort":"tackles"}')).toEqual({ sort: 'tackles' });
  });

  it('falls back to the default for nothing, junk or an unknown sort', () => {
    expect(parseSortChoice(null)).toEqual(DEFAULT_SORT);
    expect(parseSortChoice('{')).toEqual(DEFAULT_SORT);
    expect(parseSortChoice('{"sort":"nope","order":"asc"}')).toEqual(DEFAULT_SORT);
  });

  it('ignores an order stored by an older build', () => {
    expect(parseSortChoice('{"sort":"goals","order":"asc"}')).toEqual({ sort: 'goals' });
    expect(parseSortChoice('{"sort":"goals","order":"x"}')).toEqual({ sort: 'goals' });
  });
});
