/**
 * View logic for an FPL Draft league imported into the app (ADR 0012): the
 * import screen, the "Imported from FPL" status, the commissioner's re-sync
 * and check against FPL, and the seats. An imported league is playable at
 * once; nothing here gates a move.
 */
import type { Schemas } from '@myfantasyprem/api-client';

import type { PillTone } from '@/components/Chip';
import { joinSentences } from '@/lib/errors';
import { formatAgo, formatDateTime } from '@/lib/time';

type Preview = Schemas['ImportPreview'];
type Import = Schemas['Import'];

/** `12 teams · gameweek 7`, or `before gameweek 1` before the season. */
export function previewFacts(p: Preview): string {
  const teams = `${p.teamCount} ${p.teamCount === 1 ? 'team' : 'teams'}`;
  const gw = p.currentGameweek > 0 ? `gameweek ${p.currentGameweek}` : 'before gameweek 1';
  return `${teams} · ${gw}`;
}

/** Import problems no retry can fix here, so the status line does not suggest one. */
const FINAL_PROBLEMS = new Set(['import-unavailable', 'unsupported-league']);

/** The slug at the end of a problem's `type` URI. */
function problemSlugOf(p: Schemas['Problem'] | null | undefined): string {
  return p?.type.split('/').pop() ?? '';
}

/** One line for an import job. Unknown states (open enum) never look final. */
export function importStatusText(job: Import): string {
  switch (job.status) {
    case 'queued':
      return 'Waiting to start…';
    case 'running':
      return job.progress.total > 0
        ? `Importing… ${job.progress.done} of ${job.progress.total} steps`
        : 'Importing…';
    case 'succeeded':
      return 'Imported. Opening your league…';
    case 'failed':
      return joinSentences(
        `Import failed: ${job.error?.detail ?? job.error?.title ?? 'unknown error'}`,
        FINAL_PROBLEMS.has(problemSlugOf(job.error)) ? null : 'Try again.',
      );
    default:
      return `This version of the app does not know the import state "${job.status}". Try again.`;
  }
}

type Sync = Schemas['LeagueSync'];
type Verification = Schemas['SyncVerification'];
type SyncItem = Schemas['SyncItem'];

/**
 * The league header line: `Imported from FPL · last synced 5m ago`. FPL's
 * state was last applied by the import or the last re-sync.
 */
export function syncLine(sync: Sync, now = Date.now()): string {
  const last = sync.lastSyncedAt
    ? `last synced ${formatAgo(Date.parse(sync.lastSyncedAt), now)}`
    : 'not synced yet';
  return `Imported from FPL · ${last}`;
}

/** What is running now, or null. Unknown kinds (open enum) still say something is. */
export function syncPendingText(sync: Sync): string | null {
  if (!sync.pending) return null;
  switch (sync.pending.kind) {
    case 'resync':
      return 'Re-sync running…';
    case 'check':
      return 'Checking against FPL…';
    default:
      return 'Working with FPL…';
  }
}

/** A failed re-sync or check; the league kept its state. */
export function syncErrorText(sync: Sync): string | null {
  return sync.lastError
    ? joinSentences(`The last re-sync or check failed: ${sync.lastError}`, 'Nothing changed.')
    : null;
}

/** The confirmation before a re-sync: what it replaces, since when. */
export function resyncWarning(sync: Sync, now = Date.now(), timeZone?: string): string {
  const since = sync.lastSyncedAt
    ? ` since ${formatDateTime(Date.parse(sync.lastSyncedAt), now, timeZone)}`
    : '';
  return `This replaces all moves made here${since} with FPL's current state: squads, waiver order, trades, waiver claims and lineups (team names and settings too). Open trades made here and pending claims are cancelled. Scores and standings are recomputed.`;
}

/**
 * The check against FPL at a glance: `Matches FPL`, `2 mismatches`,
 * `Matches FPL · 1 trade pending on FPL`. Informational.
 */
export function verificationSummary(v: Verification): { label: string; tone: PillTone } {
  const { mismatch, unverified, pending } = v.counts;
  if (mismatch > 0)
    return { label: `${mismatch} ${mismatch === 1 ? 'mismatch' : 'mismatches'}`, tone: 'loss' };
  if (unverified > 0) return { label: `${unverified} not checked yet`, tone: 'warning' };
  if (pending > 0)
    return {
      label: `Matches FPL · ${pending} ${pending === 1 ? 'trade' : 'trades'} pending on FPL`,
      tone: 'warning',
    };
  return { label: 'Matches FPL', tone: 'win' };
}

/** The automatic check's pill: mismatches first, then what is unchecked or FPL is still updating. */
export function shadowSummary(s: Schemas['ShadowCheck']): { label: string; tone: PillTone } {
  const { mismatch, unverified, pending } = s.counts;
  if (mismatch > 0)
    return { label: `${mismatch} ${mismatch === 1 ? 'mismatch' : 'mismatches'}`, tone: 'loss' };
  if (unverified > 0) return { label: `${unverified} not checked yet`, tone: 'warning' };
  if (pending > 0) return { label: `Matches so far · ${pending} updating`, tone: 'warning' };
  return { label: 'Matches FPL', tone: 'win' };
}

const SHADOW_REASON: Record<string, string> = {
  after_waivers: 'after the waiver run',
  before_deadline: 'before the deadline',
  after_deadline: 'after the deadline',
  kickoff: 'at kickoff',
  half_time: 'at half time',
  full_time: 'at full time',
  bonus: 'after bonus',
  data_checked: 'after FPL checked the data',
  admin: 'by an admin',
};

/** `Last checked after bonus, GW9, 5m ago.` */
export function shadowLine(s: Schemas['ShadowCheck'], now = Date.now()): string {
  const why = SHADOW_REASON[s.reason];
  const gw = s.gameweek !== null ? `, GW${s.gameweek}` : '';
  return `Last checked${why ? ` ${why}` : ''}${gw}, ${formatAgo(Date.parse(s.checkedAt), now)}.`;
}

const KIND_LABEL: Record<string, string> = {
  points: 'Points',
  subs: 'Auto-subs',
  result: 'Result',
  standing: 'Table',
  table: 'Table logic',
  squad: 'Squad',
  owner: 'Player owners',
  waiver_order: 'Waiver order',
  pending_trade: 'Pending FPL trade',
  setting: 'Setting',
  waiver: 'Waiver claim',
  trade: 'Trade',
  lineup: 'Lineup',
  player_points: 'Player points',
};

/** `GW5 · Corner Flag Crew · Points` for a failing item; unknown kinds keep their code. */
export function syncItemTitle(item: SyncItem): string {
  return [
    item.gameweek !== null ? `GW${item.gameweek}` : null,
    item.team?.name ?? null,
    KIND_LABEL[item.kind] ?? item.kind,
  ]
    .filter(Boolean)
    .join(' · ');
}

/** `Ours 61 · FPL 63`. */
export function syncItemValues(item: SyncItem): string {
  return `Ours ${item.ours || '—'} · FPL ${item.theirs || '—'}`;
}

/** A seat row's subtitle: who it is in FPL and whether it is claimed here. */
export function seatSubtitle(seat: Schemas['Seat']): string {
  if (seat.claimed)
    return `FPL: ${seat.fplManagerName} · claimed by ${seat.owner?.displayName ?? 'a manager'}`;
  return `FPL: ${seat.fplManagerName} · not claimed yet`;
}
