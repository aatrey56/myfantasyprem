import {
  formatAgo,
  formatAsOf,
  formatClock,
  formatCountdown,
  formatDay,
  formatDateTime,
  formatKickoff,
  formatMonthYear,
  formatRelative,
} from './time';

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;
const at = (iso: string) => Date.parse(iso);

// Tue 13 Oct 2026, 09:00 in London (BST, UTC+1).
const NOW = at('2026-10-13T08:00:00Z');
const LONDON = 'Europe/London';

describe('formatDateTime: a scheduled moment', () => {
  it('reads "Sat 17 Oct, 11:00" in the zone it is shown in', () => {
    const deadline = at('2026-10-17T10:00:00Z');
    expect(formatDateTime(deadline, NOW, LONDON)).toBe('Sat 17 Oct, 11:00');
    expect(formatDateTime(deadline, NOW, 'America/New_York')).toBe('Sat 17 Oct, 06:00');
    expect(formatDateTime(deadline, NOW, 'Asia/Kolkata')).toBe('Sat 17 Oct, 15:30');
    // Across midnight, the day changes with the zone.
    expect(formatDateTime(deadline, NOW, 'Pacific/Auckland')).toBe('Sat 17 Oct, 23:00');
    expect(formatDateTime(at('2026-10-17T14:00:00Z'), NOW, 'Pacific/Auckland')).toBe(
      'Sun 18 Oct, 03:00',
    );
  });

  it('uses a 24-hour clock with padded hours and minutes', () => {
    expect(formatDateTime(at('2026-10-17T23:05:00Z'), NOW, LONDON)).toBe('Sun 18 Oct, 00:05');
    expect(formatDateTime(at('2026-10-17T06:07:00Z'), NOW, LONDON)).toBe('Sat 17 Oct, 07:07');
  });

  it('adds the year only beyond six months of now', () => {
    expect(formatDateTime(at('2027-01-02T15:00:00Z'), NOW, LONDON)).toBe('Sat 2 Jan, 15:00');
    expect(formatDateTime(at('2027-05-23T15:00:00Z'), NOW, LONDON)).toBe('Sun 23 May 2027, 16:00');
    expect(formatDateTime(at('2026-03-01T12:00:00Z'), NOW, LONDON)).toBe('Sun 1 Mar 2026, 12:00');
  });

  it('shows wall-clock time on both sides of a DST change', () => {
    // UK clocks go back at 02:00 BST on Sun 25 Oct 2026.
    expect(formatDateTime(at('2026-10-24T10:00:00Z'), NOW, LONDON)).toBe('Sat 24 Oct, 11:00');
    expect(formatDateTime(at('2026-10-31T11:00:00Z'), NOW, LONDON)).toBe('Sat 31 Oct, 11:00');
    // The repeated hour: 01:30 happens twice.
    expect(formatDateTime(at('2026-10-25T00:30:00Z'), NOW, LONDON)).toBe('Sun 25 Oct, 01:30');
    expect(formatDateTime(at('2026-10-25T01:30:00Z'), NOW, LONDON)).toBe('Sun 25 Oct, 01:30');
    // Spring forward: 01:00 GMT on 28 Mar 2027 is 02:00 BST; 01:xx never shows.
    const spring = at('2027-03-28T00:59:00Z');
    expect(formatDateTime(spring, spring, LONDON)).toBe('Sun 28 Mar, 00:59');
    expect(formatDateTime(spring + MIN, spring, LONDON)).toBe('Sun 28 Mar, 02:00');
    // New York falls back a week later than London.
    expect(formatDateTime(at('2026-11-01T05:30:00Z'), NOW, 'America/New_York')).toBe(
      'Sun 1 Nov, 01:30',
    );
    expect(formatDateTime(at('2026-11-01T06:30:00Z'), NOW, 'America/New_York')).toBe(
      'Sun 1 Nov, 01:30',
    );
    // Lord Howe Island moves by half an hour (UTC+11 to +10:30 on Sun 4 Apr 2027).
    const howe = 'Australia/Lord_Howe';
    expect(formatDateTime(at('2027-04-03T14:00:00Z'), at('2027-04-01T00:00:00Z'), howe)).toBe(
      'Sun 4 Apr, 01:00',
    );
    expect(formatDateTime(at('2027-04-04T14:00:00Z'), at('2027-04-01T00:00:00Z'), howe)).toBe(
      'Mon 5 Apr, 00:30',
    );
  });
});

describe('formatKickoff', () => {
  it('is "Sat 15:00" within six days, the full date beyond', () => {
    expect(formatKickoff(at('2026-10-17T14:00:00Z'), NOW, LONDON)).toBe('Sat 15:00');
    expect(formatKickoff(NOW + 6 * DAY - MIN, NOW, LONDON)).toBe('Mon 08:59');
    // Two Saturdays out, the weekday alone would be ambiguous.
    expect(formatKickoff(at('2026-10-24T14:00:00Z'), NOW, LONDON)).toBe('Sat 24 Oct, 15:00');
    // A UK Saturday 15:00 kickoff is early Sunday in New Zealand.
    expect(formatKickoff(at('2026-10-17T14:00:00Z'), NOW, 'Pacific/Auckland')).toBe('Sun 03:00');
  });
});

describe('formatDay and formatClock: kickoffs under a day heading', () => {
  it('heads a day "Sat 17 Oct" and lists "15:00" under it', () => {
    expect(formatDay(at('2026-10-17T14:00:00Z'), NOW, LONDON)).toBe('Sat 17 Oct');
    expect(formatClock(at('2026-10-17T14:00:00Z'), LONDON)).toBe('15:00');
    // 23:30 UTC on Saturday is Sunday 00:30 in London: the day follows the zone.
    expect(formatDay(at('2026-10-17T23:30:00Z'), NOW, LONDON)).toBe('Sun 18 Oct');
    expect(formatClock(at('2026-10-17T23:30:00Z'), LONDON)).toBe('00:30');
    // More than six months away: the year.
    expect(formatDay(at('2027-05-23T15:00:00Z'), NOW, LONDON)).toBe('Sun 23 May 2027');
  });
});

describe('formatCountdown: a clock that ticks every second', () => {
  it('is m:ss under an hour, then hours and minutes, then days and hours', () => {
    expect(formatCountdown(65_000)).toBe('1:05');
    expect(formatCountdown(59 * MIN + 59_999)).toBe('59:59');
    expect(formatCountdown(HOUR)).toBe('1h 0m');
    expect(formatCountdown(3 * HOUR + 20 * MIN)).toBe('3h 20m');
    expect(formatCountdown(52 * HOUR)).toBe('2d 4h');
  });

  it('never goes negative', () => {
    expect(formatCountdown(-5)).toBe('0:00');
    expect(formatCountdown(999)).toBe('0:00');
  });

  it('counts elapsed time, so a DST change does not stretch it', () => {
    // Sat 11:00 BST to Sun 11:00 GMT is 25 hours.
    expect(formatCountdown(at('2026-10-25T11:00:00Z') - at('2026-10-24T10:00:00Z'))).toBe('1d 1h');
  });
});

describe('formatRelative: time left as text', () => {
  it('reads "in 2d 4h", "in 3h 20m", "in 45m", "in <1m"', () => {
    expect(formatRelative(NOW + 52 * HOUR + 30 * MIN, NOW)).toBe('in 2d 4h');
    expect(formatRelative(NOW + 3 * HOUR + 20 * MIN + 59_000, NOW)).toBe('in 3h 20m');
    expect(formatRelative(NOW + 45 * MIN + 30_000, NOW)).toBe('in 45m');
    expect(formatRelative(NOW + 59_000, NOW)).toBe('in <1m');
  });

  it('never shows seconds: it refreshes every 30 seconds or more', () => {
    expect(formatRelative(NOW + 65_000, NOW)).toBe('in 1m');
    expect(formatRelative(NOW + 65_000, NOW)).not.toMatch(/:/);
  });

  it('reads as a past event once the moment has passed', () => {
    expect(formatRelative(NOW, NOW)).toBe('just now');
    expect(formatRelative(NOW - 2 * HOUR, NOW)).toBe('2h ago');
  });
});

describe('formatAgo: a past event', () => {
  it('is "just now", then minutes, then hours, for the last day', () => {
    expect(formatAgo(NOW - 30_000, NOW, LONDON)).toBe('just now');
    expect(formatAgo(NOW - 5 * MIN - 59_000, NOW, LONDON)).toBe('5m ago');
    expect(formatAgo(NOW - 2 * HOUR - 59 * MIN, NOW, LONDON)).toBe('2h ago');
    expect(formatAgo(NOW - 23 * HOUR - 59 * MIN, NOW, LONDON)).toBe('23h ago');
  });

  it('is the day after that: "Mon 12 Oct"', () => {
    expect(formatAgo(NOW - DAY, NOW, LONDON)).toBe('Mon 12 Oct');
    expect(formatAgo(at('2026-10-01T12:00:00Z'), NOW, LONDON)).toBe('Thu 1 Oct');
    expect(formatAgo(at('2025-10-01T12:00:00Z'), NOW, LONDON)).toBe('Wed 1 Oct 2025');
  });

  it("uses the zone's calendar day", () => {
    const event = at('2026-10-11T23:30:00Z');
    expect(formatAgo(event, NOW, LONDON)).toBe('Mon 12 Oct');
    expect(formatAgo(event, NOW, 'America/Los_Angeles')).toBe('Sun 11 Oct');
  });

  it('treats a moment ahead of the device clock (skew) as just now', () => {
    expect(formatAgo(NOW + 20_000, NOW, LONDON)).toBe('just now');
  });
});

describe('formatAsOf: when data was fetched', () => {
  it('is the time within 12 hours, then the day and time', () => {
    expect(formatAsOf(at('2026-10-13T07:05:00Z'), NOW, LONDON)).toBe('08:05');
    expect(formatAsOf(at('2026-10-12T20:00:01Z'), NOW, LONDON)).toBe('21:00');
    expect(formatAsOf(at('2026-10-12T20:00:00Z'), NOW, LONDON)).toBe('Mon 12 Oct, 21:00');
    expect(formatAsOf(at('2026-10-12T22:59:00Z'), NOW, 'Asia/Kolkata')).toBe('04:29');
  });

  it('stays a time just after midnight', () => {
    // Ten minutes ago, yesterday by the calendar.
    const now = at('2026-10-13T23:05:00Z');
    expect(formatAsOf(now - 10 * MIN, now, LONDON)).toBe('23:55');
    expect(formatAsOf(now - 10 * MIN, now, 'Asia/Kolkata')).toBe('04:25');
  });
});

describe('formatMonthYear', () => {
  it('is "Jul 2026" in the zone', () => {
    expect(formatMonthYear(at('2026-07-15T12:00:00Z'), LONDON)).toBe('Jul 2026');
    expect(formatMonthYear(at('2026-07-31T23:30:00Z'), LONDON)).toBe('Aug 2026');
    expect(formatMonthYear(at('2026-07-31T23:30:00Z'), 'America/New_York')).toBe('Jul 2026');
  });
});

describe('the device time zone', () => {
  const device = Intl.DateTimeFormat().resolvedOptions().timeZone;

  it('is what every formatter uses without a zone', () => {
    // A year of instants every 7h 13m: every hour of the day, both DST changes.
    for (let t = at('2026-01-01T00:00:00Z'); t < at('2027-01-01T00:00:00Z'); t += 433 * MIN) {
      const now = t - 3 * DAY;
      expect(formatDateTime(t, now)).toBe(formatDateTime(t, now, device));
      expect(formatKickoff(t, now)).toBe(formatKickoff(t, now, device));
      expect(formatAgo(t, t + 2 * DAY)).toBe(formatAgo(t, t + 2 * DAY, device));
      expect(formatAsOf(t, t + 5 * HOUR)).toBe(formatAsOf(t, t + 5 * HOUR, device));
      expect(formatMonthYear(t)).toBe(formatMonthYear(t, device));
    }
  });

  it('matches the local Date fields', () => {
    const t = at('2026-10-17T10:00:00Z');
    const d = new Date(t);
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    expect(formatDateTime(t, NOW)).toMatch(new RegExp(` ${d.getDate()} \\w{3}, ${hh}:${mm}$`));
  });
});
