import type { Schemas } from '@myfantasyprem/api-client';

/** A personal tag (the contract's PlayerTag; open, so any string may arrive). */
export type PlayerTag = Schemas['PlayerTag'];

export interface TagMeta {
  tag: PlayerTag;
  emoji: string;
  label: string;
}

/**
 * The fixed set, in display order, like Sleeper's personal tags: private to
 * the manager, no free text.
 */
export const TAGS: readonly TagMeta[] = [
  { tag: 'hot', emoji: '🔥', label: 'Hot' },
  { tag: 'cold', emoji: '🧊', label: 'Cold' },
  { tag: 'target', emoji: '🎯', label: 'Target' },
  { tag: 'sell', emoji: '💰', label: 'Sell' },
  { tag: 'avoid', emoji: '🛑', label: 'Avoid' },
];

/** The tag's emoji and label; null for one this build does not know (open enum). */
export function tagMeta(tag: string | null | undefined): TagMeta | null {
  return TAGS.find((t) => t.tag === tag) ?? null;
}

/** Tags by player id, from the tags list; unknown tags are left out. */
export function tagsByPlayer(
  entries: readonly Schemas['PlayerTagEntry'][] | undefined,
): ReadonlyMap<string, TagMeta> {
  const out = new Map<string, TagMeta>();
  for (const e of entries ?? []) {
    const m = tagMeta(e.tag);
    if (m) out.set(e.player.id, m);
  }
  return out;
}
