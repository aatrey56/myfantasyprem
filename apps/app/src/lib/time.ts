/**
 * Every date and time the app shows is formatted here. The rules:
 *
 * - The device's time zone, a 24-hour clock, English day and month names.
 *   `timeZone` (an IANA name) exists for tests; the app never passes it.
 * - A scheduled moment (deadline, draft start, expiry, waiver run):
 *   {@link formatDateTime}, `Sat 17 Oct, 11:00`.
 * - A kickoff: {@link formatKickoff}, `Sat 15:00` within six days, otherwise
 *   as a scheduled moment. The team header's one-line lock time uses it too
 *   (`Locks Sat 11:00`), with the full moment as its accessible name.
 * - Kickoffs listed under a day heading (SCORES): the heading
 *   {@link formatDay}, `Sat 17 Oct`, and each kickoff {@link formatClock},
 *   `15:00`.
 * - Time left as text that refreshes every 30 s or more:
 *   {@link formatRelative}, `in 2d 4h`, `in 3h 20m`, `in 45m`, `in <1m`.
 * - A clock that ticks every second (the pick timer, the next deadline):
 *   {@link formatCountdown}, `2d 4h`, `3h 20m`, `1:05`.
 * - A past event (activity, a trade offer): {@link formatAgo}, `just now`,
 *   `5m ago`, `2h ago` for a day, then the day: `Mon 12 Oct`.
 * - When data was fetched: {@link formatAsOf}, `14:05` within 12 hours,
 *   otherwise `Mon 12 Oct, 14:05`.
 * - A month: {@link formatMonthYear}, `Jul 2026`.
 * - The zone itself, for "times in your time zone" notes: {@link deviceTimeZone}.
 *
 * Dates more than six months from now add the year (`Sun 23 May 2027, 16:00`).
 * Durations are elapsed time, so a DST change never stretches a countdown;
 * wall-clock fields follow the zone's offset on the day shown.
 */

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;
const SIX_MONTHS = 183 * DAY;

const pad = (n: number) => String(n).padStart(2, '0');
const DAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;
const MONTHS = [
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
] as const;

/** Wall-clock fields of an instant: month 0-11, weekday 0 (Sun) to 6. */
interface Fields {
  year: number;
  month: number;
  day: number;
  weekday: number;
  hour: number;
  minute: number;
}

const zoneFormats = new Map<string, Intl.DateTimeFormat>();

/** The instant's fields on the device clock, or in `timeZone` when given. */
function fields(ms: number, timeZone?: string): Fields {
  if (!timeZone) {
    const d = new Date(ms);
    return {
      year: d.getFullYear(),
      month: d.getMonth(),
      day: d.getDate(),
      weekday: d.getDay(),
      hour: d.getHours(),
      minute: d.getMinutes(),
    };
  }
  let f = zoneFormats.get(timeZone);
  if (!f) {
    f = new Intl.DateTimeFormat('en-US', {
      timeZone,
      hourCycle: 'h23',
      weekday: 'short',
      year: 'numeric',
      month: 'numeric',
      day: 'numeric',
      hour: 'numeric',
      minute: 'numeric',
    });
    zoneFormats.set(timeZone, f);
  }
  const p: Partial<Record<Intl.DateTimeFormatPartTypes, string>> = {};
  for (const part of f.formatToParts(ms)) p[part.type] = part.value;
  return {
    year: Number(p.year),
    month: Number(p.month) - 1,
    day: Number(p.day),
    weekday: DAYS.indexOf(p.weekday as (typeof DAYS)[number]),
    // Some engines write midnight as 24 even on a 23-hour cycle.
    hour: Number(p.hour) % 24,
    minute: Number(p.minute),
  };
}

const clock = (f: Fields) => `${pad(f.hour)}:${pad(f.minute)}`;

/** `Mon 12 Oct`, with the year when far from now. */
function day(ms: number, now: number, timeZone?: string): string {
  const f = fields(ms, timeZone);
  const year = Math.abs(ms - now) > SIX_MONTHS ? ` ${f.year}` : '';
  return `${DAYS[f.weekday]} ${f.day} ${MONTHS[f.month]}${year}`;
}

/** A calendar day, for a heading over that day's kickoffs: `Sat 17 Oct`. */
export function formatDay(ms: number, now = Date.now(), timeZone?: string): string {
  return day(ms, now, timeZone);
}

/** The time of day alone, under a day heading: `15:00`. */
export function formatClock(ms: number, timeZone?: string): string {
  return clock(fields(ms, timeZone));
}

/** A scheduled moment: `Sat 17 Oct, 11:00`. */
export function formatDateTime(ms: number, now = Date.now(), timeZone?: string): string {
  return `${day(ms, now, timeZone)}, ${clock(fields(ms, timeZone))}`;
}

/** A kickoff: `Sat 15:00` within six days of now, otherwise `Sat 24 Oct, 15:00`. */
export function formatKickoff(ms: number, now = Date.now(), timeZone?: string): string {
  if (Math.abs(ms - now) >= 6 * DAY) return formatDateTime(ms, now, timeZone);
  const f = fields(ms, timeZone);
  return `${DAYS[f.weekday]} ${clock(f)}`;
}

/** Days and hours, or hours and minutes under a day. */
function span(ms: number): string {
  const h = Math.floor(ms / HOUR);
  if (h < 24) return `${h}h ${Math.floor((ms % HOUR) / MINUTE)}m`;
  return `${Math.floor(h / 24)}d ${h % 24}h`;
}

/**
 * A clock that ticks every second: `1:05` under an hour (m:ss), `3h 20m`
 * under a day, `2d 4h` beyond. Never negative.
 */
export function formatCountdown(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000));
  if (s < 3600) return `${Math.floor(s / 60)}:${pad(s % 60)}`;
  return span(s * 1000);
}

/**
 * Time left as text: `in 2d 4h`, `in 3h 20m`, `in 45m`, `in <1m`. Never
 * seconds, so it may refresh slowly. Once passed, as {@link formatAgo}.
 */
export function formatRelative(target: number, now = Date.now(), timeZone?: string): string {
  const left = target - now;
  if (left <= 0) return formatAgo(target, now, timeZone);
  if (left < MINUTE) return 'in <1m';
  if (left < HOUR) return `in ${Math.floor(left / MINUTE)}m`;
  return `in ${span(left)}`;
}

/**
 * A past event: `just now`, `5m ago`, `2h ago` within a day, then the day
 * it happened, `Mon 12 Oct`. A moment ahead of the device clock is just now.
 */
export function formatAgo(ms: number, now = Date.now(), timeZone?: string): string {
  const ago = now - ms;
  if (ago < MINUTE) return 'just now';
  if (ago < HOUR) return `${Math.floor(ago / MINUTE)}m ago`;
  if (ago < DAY) return `${Math.floor(ago / HOUR)}h ago`;
  return day(ms, now, timeZone);
}

/**
 * When data was fetched: `14:05` within 12 hours (so ten minutes ago reads
 * as a time just after midnight too), otherwise `Mon 12 Oct, 14:05`.
 */
export function formatAsOf(ms: number, now = Date.now(), timeZone?: string): string {
  if (Math.abs(now - ms) < 12 * HOUR) return clock(fields(ms, timeZone));
  return formatDateTime(ms, now, timeZone);
}

/** The device's IANA zone, e.g. `Europe/London`; empty when the runtime hides it. */
export function deviceTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? '';
  } catch {
    return '';
  }
}

/** A month: `Jul 2026`. */
export function formatMonthYear(ms: number, timeZone?: string): string {
  const f = fields(ms, timeZone);
  return `${MONTHS[f.month]} ${f.year}`;
}
