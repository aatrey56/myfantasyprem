/**
 * The Players list's View (FPL Draft's View dropdown): one choice at a
 * time, all players, a position or a club. Plus the Available toggle
 * (unowned only). Pure: options, labels, the query filter and the stored
 * choice.
 */

export type ViewPosition = 'GKP' | 'DEF' | 'MID' | 'FWD';

export type PlayerViewChoice =
  { kind: 'all' } | { kind: 'position'; position: ViewPosition } | { kind: 'club'; code: string };

export interface ClubLike {
  code: string;
  name: string;
}

export interface ViewOption {
  /** Stable id: `all`, `pos-DEF`, `club-ARS` (also the test id suffix). */
  id: string;
  label: string;
  choice: PlayerViewChoice;
  /** The heading it falls under in the list. */
  group: 'all' | 'position' | 'club';
}

export const VIEW_POSITIONS: { position: ViewPosition; label: string }[] = [
  { position: 'GKP', label: 'Goalkeepers' },
  { position: 'DEF', label: 'Defenders' },
  { position: 'MID', label: 'Midfielders' },
  { position: 'FWD', label: 'Forwards' },
];

export const ALL_VIEW: PlayerViewChoice = { kind: 'all' };

export function viewId(v: PlayerViewChoice): string {
  return v.kind === 'all' ? 'all' : v.kind === 'position' ? `pos-${v.position}` : `club-${v.code}`;
}

/** The list in order: All players, the four positions, then every club A–Z by name. */
export function viewOptions(clubs: readonly ClubLike[]): ViewOption[] {
  const sorted = [...clubs].sort((a, b) => a.name.localeCompare(b.name));
  return [
    { id: 'all', label: 'All players', choice: ALL_VIEW, group: 'all' },
    ...VIEW_POSITIONS.map((p) => {
      const choice: PlayerViewChoice = { kind: 'position', position: p.position };
      return { id: viewId(choice), label: p.label, choice, group: 'position' as const };
    }),
    ...sorted.map((c) => {
      const choice: PlayerViewChoice = { kind: 'club', code: c.code };
      return { id: viewId(choice), label: c.name, choice, group: 'club' as const };
    }),
  ];
}

/** The View button text: `View: Arsenal`, `View: Defenders`, `View: All players`. */
export function viewButtonText(
  v: PlayerViewChoice,
  clubs: readonly ClubLike[] | undefined,
): string {
  if (v.kind === 'all') return 'View: All players';
  if (v.kind === 'position') {
    return `View: ${VIEW_POSITIONS.find((p) => p.position === v.position)!.label}`;
  }
  return `View: ${clubs?.find((c) => c.code === v.code)?.name ?? v.code}`;
}

/** The query's position and club for a view. */
export function viewWhere(v: PlayerViewChoice): { position?: ViewPosition; club?: string } {
  if (v.kind === 'position') return { position: v.position };
  if (v.kind === 'club') return { club: v.code };
  return {};
}

/**
 * A stored club the club list no longer has is All players (once that
 * list is known); anything else stands.
 */
export function knownView(
  v: PlayerViewChoice,
  clubs: readonly ClubLike[] | undefined,
): PlayerViewChoice {
  if (v.kind === 'club' && clubs && !clubs.some((c) => c.code === v.code)) return ALL_VIEW;
  return v;
}

export interface ViewSettings {
  view: PlayerViewChoice;
  /** Only players that can be signed or claimed (status `available`). */
  available: boolean;
}

export const DEFAULT_VIEW: ViewSettings = { view: ALL_VIEW, available: true };

/** The league's stored View and Available key. */
export const viewStorageKey = (leagueId: string) => `players-view:${leagueId}`;

const POSITION_SET = new Set<string>(VIEW_POSITIONS.map((p) => p.position));

/** Reads stored settings; each malformed or unknown part falls back to its default. */
export function parseViewSettings(raw: string | null): ViewSettings {
  if (!raw) return DEFAULT_VIEW;
  let v: unknown;
  try {
    v = JSON.parse(raw);
  } catch {
    return DEFAULT_VIEW;
  }
  if (!v || typeof v !== 'object') return DEFAULT_VIEW;
  const o = v as { view?: unknown; available?: unknown };
  const available = typeof o.available === 'boolean' ? o.available : DEFAULT_VIEW.available;
  return { view: parseChoice(o.view), available };
}

function parseChoice(v: unknown): PlayerViewChoice {
  if (!v || typeof v !== 'object') return ALL_VIEW;
  const o = v as { kind?: unknown; position?: unknown; code?: unknown };
  if (o.kind === 'position' && typeof o.position === 'string' && POSITION_SET.has(o.position)) {
    return { kind: 'position', position: o.position as ViewPosition };
  }
  if (o.kind === 'club' && typeof o.code === 'string' && /^[A-Za-z]{2,4}$/.test(o.code)) {
    return { kind: 'club', code: o.code };
  }
  return ALL_VIEW;
}
