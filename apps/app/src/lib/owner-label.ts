import { userColor } from './identity';
import { tint, type ColorTokens } from '@/theme/tokens';

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

/** Titles that come before a name: "Dr Sam Rivers" → "Sam". */
const TITLE = /^(mr|mrs|ms|miss|dr|sir|prof)\.?$/i;

/** One letter, maybe with a dot: "J", "A.". */
const INITIAL = /^\p{L}\.?$/u;

/**
 * A person's first name from a display name: "Sam P." → "Sam". A leading
 * lone initial keeps the next word ("J Doe", "A. Smith"); leading titles
 * and emoji are skipped; hyphenated and possessive names stay whole. A
 * single word is that word. Null when the name is initials only ("A B"),
 * has no letters or stands for no one ("Open seat").
 */
export function firstNameOf(name: string): string | null {
  const trimmed = name.trim();
  if (!trimmed || NO_PERSON.test(trimmed)) return null;
  const words = trimmed
    .split(/\s+/)
    .map((w) => w.replace(/^[^\p{L}\p{N}]+|[^\p{L}\p{N}'’.-]+$/gu, ''))
    .filter((w) => /\p{L}/u.test(w));
  if (words.length === 0) return null;
  if (words.length === 1) return stripDot(words[0]!);
  let i = 0;
  while (i < words.length - 1 && TITLE.test(words[i]!)) i++;
  const rest = words.slice(i);
  if (rest.every((w) => INITIAL.test(w))) return null;
  if (INITIAL.test(rest[0]!)) return `${rest[0]} ${stripDot(rest[1]!)}`;
  return stripDot(rest[0]!);
}

/** A trailing full stop off a word ("P." → "P"); an initial keeps it. */
function stripDot(word: string): string {
  return INITIAL.test(word) ? word : word.replace(/\.+$/, '');
}

/** A team name as shown: the name itself (emoji included), `Team` when blank. */
export function teamLabelOf(name: string): string {
  return name.trim() || 'Team';
}

/** The team a chip names. `avatarColor` when the API sends one. */
export interface OwnerTeam {
  name: string;
  managerName: string;
  avatarColor?: string | null;
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
  team: OwnerTeam,
  mine: boolean,
  mode: OwnerLabelMode,
): OwnerChipLabel {
  const color = userColor(team.name, team.avatarColor).bg;
  if (mine) return { text: 'You', color };
  const text =
    mode === 'team_name'
      ? teamLabelOf(team.name)
      : (firstNameOf(team.managerName) ?? teamLabelOf(team.name));
  return { text, color };
}

/**
 * A chip's fill, border and text: a light tint of the avatar colour over
 * the surface with the body text colour, or solid accent for `You`.
 */
export function ownerChipColors(
  color: string,
  mine: boolean,
  colors: Pick<ColorTokens, 'accent' | 'onAccent' | 'surface' | 'text'>,
): { background: string; border: string; text: string } {
  if (mine) return { background: colors.accent, border: colors.accent, text: colors.onAccent };
  return {
    background: tint(color, colors.surface, 0.18),
    border: tint(color, colors.surface, 0.55),
    text: colors.text,
  };
}
