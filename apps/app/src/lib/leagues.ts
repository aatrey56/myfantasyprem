import type { Schemas } from '@myfantasyprem/api-client';

import type { PillTone } from '@/components/Chip';

/** Label and tone for a league status. Unknown values (open enum) render neutrally. */
export function leagueStatus(status: string): { label: string; tone: PillTone } {
  switch (status) {
    case 'pre_draft':
      return { label: 'Pre-draft', tone: 'neutral' };
    case 'drafting':
      return { label: 'Drafting', tone: 'live' };
    case 'in_season':
      return { label: 'In season', tone: 'accent' };
    case 'complete':
      return { label: 'Complete', tone: 'neutral' };
    default:
      return { label: 'League', tone: 'neutral' };
  }
}

/**
 * A league whose draft finished after the season's last deadline: complete
 * with no gameweek played, so there is no table or matchup to show.
 */
export function seasonOverUnplayed(home: Schemas['LeagueHome']): boolean {
  return (
    home.league.status === 'complete' &&
    home.matchups.length === 0 &&
    home.standings.every((r) => r.played === 0)
  );
}

/** 1st, 2nd, 3rd, 4th, 11th, 12th, 13th, 21st. */
export function ordinal(n: number): string {
  const mod100 = n % 100;
  if (mod100 >= 11 && mod100 <= 13) return `${n}th`;
  switch (n % 10) {
    case 1:
      return `${n}st`;
    case 2:
      return `${n}nd`;
    case 3:
      return `${n}rd`;
    default:
      return `${n}th`;
  }
}

/** `4-1-2` (W-D-L). */
export function recordText(r: Schemas['Record']): string {
  return `${r.wins}-${r.draws}-${r.losses}`;
}

export interface MyMatchupLine {
  myPoints: number;
  opponentName: string;
  opponentPoints: number;
  state: Schemas['ScoreState'];
  matchupId: string;
  gameweek: number;
}

function sideName(side: Schemas['MatchupSide']): string {
  if (side.kind === 'team') return side.team.name;
  if (side.kind === 'average') return 'League average';
  return 'Opponent';
}

/** A matchup summary from my team's side; null if my team is not in it. */
export function myMatchup(m: Schemas['MatchupSummary'], myTeamId: string): MyMatchupLine | null {
  const isMe = (s: Schemas['MatchupSide']) => s.kind === 'team' && s.team.id === myTeamId;
  const [mine, theirs] = isMe(m.home) ? [m.home, m.away] : isMe(m.away) ? [m.away, m.home] : [];
  if (!mine || !theirs) return null;
  return {
    myPoints: mine.points,
    opponentName: sideName(theirs),
    opponentPoints: theirs.points,
    state: m.state,
    matchupId: m.id,
    gameweek: m.gameweek,
  };
}

/** Where tapping a league goes: the draft room while drafting, else the league. */
export function leagueHref(row: { league: Schemas['LeagueSummary'] }): string {
  return row.league.status === 'drafting'
    ? `/leagues/${row.league.id}/draft`
    : `/leagues/${row.league.id}`;
}

/** Hub order: live drafts first, then in-season, pre-draft, complete; stable within each. */
export function sortMyLeagues(rows: readonly Schemas['MyLeague'][]): Schemas['MyLeague'][] {
  const rank = (s: string) => ({ drafting: 0, in_season: 1, pre_draft: 2, complete: 3 })[s] ?? 4;
  return rows
    .map((row, i) => ({ row, i }))
    .sort((a, b) => rank(a.row.league.status) - rank(b.row.league.status) || a.i - b.i)
    .map((x) => x.row);
}

/**
 * Invite codes: 10 characters from the contract's alphabet (8 for codes made
 * before codes grew), case-insensitive on input.
 */
export function normaliseInviteCode(input: string): string | null {
  const trimmed = input.trim();
  // Accept a pasted link: take the last path segment.
  const fromLink = trimmed.match(/\/join\/([^/?#\s]+)/i)?.[1] ?? trimmed;
  const code = fromLink.replace(/[\s-]/g, '').toUpperCase();
  return /^[2-9A-HJKMNP-TV-Z]{8}([2-9A-HJKMNP-TV-Z]{2})?$/.test(code) ? code : null;
}
