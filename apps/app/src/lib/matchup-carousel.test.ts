import {
  accuracyLabel,
  averageLabel,
  carouselOrder,
  matchupAt,
  neighbourPages,
  pageOf,
  seasonLines,
} from './matchup-carousel';

const team = (id: string) =>
  ({ kind: 'team', team: { id, name: id, managerName: id } }) as unknown as {
    kind: 'team';
    team: { id: string; name: string; managerName: string };
  };
const m = (id: string, home: string, away: string) => ({ id, home: team(home), away: team(away) });
const list = [m('a', 't1', 't2'), m('b', 't3', 't4'), m('c', 't5', 't6')];

describe('carouselOrder', () => {
  it('puts my matchup first and keeps the rest in order', () => {
    expect(carouselOrder(list, 't6').map((x) => x.id)).toEqual(['c', 'a', 'b']);
    expect(carouselOrder(list, 't3').map((x) => x.id)).toEqual(['b', 'a', 'c']);
  });
  it('keeps the server order when I am not in the gameweek', () => {
    expect(carouselOrder(list, undefined).map((x) => x.id)).toEqual(['a', 'b', 'c']);
    expect(carouselOrder(list, 'tx').map((x) => x.id)).toEqual(['a', 'b', 'c']);
  });
});

describe('page and matchup mapping', () => {
  const order = carouselOrder(list, 't4'); // b, a, c
  it('maps a matchup to its page and back', () => {
    for (const [i, x] of order.entries()) {
      expect(pageOf(order, x.id)).toBe(i);
      expect(matchupAt(order, i)?.id).toBe(x.id);
    }
  });
  it('falls back to my page for an unknown id', () => {
    expect(pageOf(order, 'zz')).toBe(0);
    expect(pageOf(order, undefined)).toBe(0);
  });
  it('clamps and rounds pages', () => {
    expect(matchupAt(order, -1)?.id).toBe('b');
    expect(matchupAt(order, 9)?.id).toBe('c');
    expect(matchupAt(order, 1.4)?.id).toBe('a');
    expect(matchupAt([], 0)).toBeUndefined();
  });
  it('lists the neighbours to prefetch', () => {
    expect(neighbourPages(3, 0)).toEqual([1]);
    expect(neighbourPages(3, 1)).toEqual([0, 2]);
    expect(neighbourPages(3, 2)).toEqual([1]);
    expect(neighbourPages(1, 0)).toEqual([]);
  });
});

describe('seasonLines', () => {
  it('averages points over gameweeks played and takes accuracy from start/sit', () => {
    const lines = seasonLines(
      [
        { team: { id: 't1' } as never, played: 3, pointsFor: 160, pointsAgainst: 171 },
        { team: { id: 't2' } as never, played: 0, pointsFor: 0, pointsAgainst: 0 },
      ],
      [{ team: { id: 't1' } as never, seasonAccuracy: 0.876 }],
    );
    expect(lines.get('t1')).toEqual({ against: 57, accuracy: 0.876, average: 53.3 });
    expect(lines.get('t2')).toEqual({ against: null, accuracy: null, average: null });
    expect(accuracyLabel(0.876)).toBe('88%');
    expect(accuracyLabel(null)).toBe('–');
    expect(averageLabel(53.3)).toBe('53.3');
    expect(averageLabel(undefined)).toBe('–');
  });
});
