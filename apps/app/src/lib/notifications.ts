import type { Schemas } from '@myfantasyprem/api-client';

/** Groups in display order; an unknown group (open enum) goes last under its own name. */
const GROUP_TITLES: Record<string, string> = { personal: 'Personal', league: 'League' };

/** The settings' types by group: Personal, then League, then any group this build doesn't know. */
export function groupTypes(
  types: readonly Schemas['NotificationTypeInfo'][],
): { group: string; title: string; types: Schemas['NotificationTypeInfo'][] }[] {
  const out: { group: string; title: string; types: Schemas['NotificationTypeInfo'][] }[] = [];
  for (const t of types) {
    let g = out.find((x) => x.group === t.group);
    if (!g) {
      g = { group: t.group, title: GROUP_TITLES[t.group] ?? t.group, types: [] };
      out.push(g);
    }
    g.types.push(t);
  }
  const rank = (g: string) => (g in GROUP_TITLES ? Object.keys(GROUP_TITLES).indexOf(g) : 99);
  return out.sort((a, b) => rank(a.group) - rank(b.group));
}
