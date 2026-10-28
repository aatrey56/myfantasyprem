import { newsTime } from '../players/CardExtras';
import { statusTag } from '../RosterRow';

describe('news', () => {
  it('says how long ago the news was added, nothing without a time', () => {
    const now = Date.parse('2026-10-16T12:00:00Z');
    expect(newsTime('2026-10-16T09:30:00Z', now)).toBe('2h ago');
    expect(newsTime('2026-10-16T11:55:00Z', now)).toBe('5m ago');
    expect(newsTime(null, now)).toBe('');
    expect(newsTime('not a date', now)).toBe('');
  });
});

describe('statusTag', () => {
  it('shows the chance, OUT or SUSP, coloured by how bad', () => {
    expect(statusTag('available', null)).toBeNull();
    expect(statusTag('doubtful', 75)).toEqual({ label: '75%', tone: 'doubt' });
    expect(statusTag('doubtful', null)).toEqual({ label: 'DOUBT', tone: 'doubt' });
    expect(statusTag('injured', 0)).toEqual({ label: 'OUT', tone: 'out' });
    expect(statusTag('unavailable')).toEqual({ label: 'OUT', tone: 'out' });
    expect(statusTag('suspended', 0)).toEqual({ label: 'SUSP', tone: 'out' });
    // Open enum: an unknown status still shows something.
    expect(statusTag('on_loan')).toEqual({ label: '?', tone: 'doubt' });
  });
});
