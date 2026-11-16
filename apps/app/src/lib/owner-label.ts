import { userColor } from './identity';

/** How an owner chip names a team's manager: `Sam` or `Sunday League Heroes`. */
export type OwnerLabelMode = 'first_name' | 'team_name';

export const DEFAULT_OWNER_LABEL: OwnerLabelMode = 'first_name';

/** The device's stored choice (AsyncStorage; localStorage on web). */
export const OWNER_LABEL_KEY = 'mfp.ownerLabel';

export function parseOwnerLabel(raw: string | null): OwnerLabelMode {
  try {
    const v: unknown = raw === null ? null : JSON.parse(raw);
    return v === 'team_name' || v === 'first_name' ? v : DEFAULT_OWNER_LABEL;
  } catch {
    return DEFAULT_OWNER_LABEL;
  }
}

/** Manager names that stand for no person. */
const NO_PERSON = /^(open seat|unclaimed|league average)$/i;

/**
 * A person's first name from a display name: "Sam P." → "Sam",
 * "A. Smith" → "Smith" (a lone initial is skipped), "Ö" → "Ö". Null when the
 * name has no letters or stands for no one ("Open seat").
 */
export function firstNameOf(name: string): string | null {
  const trimmed = name.trim();
  if (!trimmed || NO_PERSON.test(trimmed)) return null;
  const words = trimmed
    .split(/\s+/)
    .map((w) => w.replace(/^[^\p{L}\p{N}]+|[^\p{L}\p{N}'’-]+$/gu, ''))
    .filter((w) => /\p{L}/u.test(w));
  if (words.length === 0) return null;
  return words.find((w) => [...w].length > 1) ?? words[0]!;
}

/** A team name as shown: the name itself (emoji included), `Team` when blank. */
export function teamLabelOf(name: string): string {
  return name.trim() || 'Team';
}

export interface OwnerChipLabel {
  /** `You`, `Sam` or the team name. */
  text: string;
  /** The team's avatar colour: the chip's tint. */
  color: string;
}

/**
 * What a player's owner chip says: `You` on my own team; otherwise the
 * manager's first name (the team name when there is no person) or, in
 * team-name mode, the team name.
 */
export function ownerChipLabel(
  team: { name: string; managerName: string },
  mine: boolean,
  mode: OwnerLabelMode,
): OwnerChipLabel {
  const color = userColor(team.name).bg;
  if (mine) return { text: 'You', color };
  const text =
    mode === 'team_name'
      ? teamLabelOf(team.name)
      : (firstNameOf(team.managerName) ?? teamLabelOf(team.name));
  return { text, color };
}
