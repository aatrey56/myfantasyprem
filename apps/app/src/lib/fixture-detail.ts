import type { Schemas } from '@myfantasyprem/api-client';

import { initialsOf } from './identity';

type Stats = Schemas['FixturePlayerStats'];
type Owner = Schemas['FixtureOwner'];

/** One thing a player did in a match, as a short tag and its spoken form. */
export interface MatchEvent {
  key: string;
  /** `2G`, `A`, `YC`, `3 SV`, `B3`. */
  label: string;
  /** `2 goals`, `1 assist`, `yellow card`, `3 saves`, `3 bonus`. */
  a11y: string;
  tone: 'good' | 'bad' | 'neutral';
}

const plural = (n: number, one: string, many = `${one}s`) => `${n} ${n === 1 ? one : many}`;
const counted = (n: number, tag: string) => (n === 1 ? tag : `${n}${tag}`);

/**
 * A player's match as tags, in the order a box score reads: goals,
 * assists, own goals, penalties saved and missed, cards, saves, bonus.
 * Nothing for a stat at zero.
 */
export function matchEvents(s: Stats): MatchEvent[] {
  const out: MatchEvent[] = [];
  const add = (n: number, e: Omit<MatchEvent, 'key'> & { key: string }) => {
    if (n > 0) out.push(e);
  };
  add(s.goalsScored, {
    key: 'goals',
    label: counted(s.goalsScored, 'G'),
    a11y: plural(s.goalsScored, 'goal'),
    tone: 'good',
  });
  add(s.assists, {
    key: 'assists',
    label: counted(s.assists, 'A'),
    a11y: plural(s.assists, 'assist'),
    tone: 'good',
  });
  add(s.ownGoals, {
    key: 'own-goals',
    label: counted(s.ownGoals, 'OG'),
    a11y: plural(s.ownGoals, 'own goal'),
    tone: 'bad',
  });
  add(s.penaltiesSaved, {
    key: 'pen-saved',
    label: counted(s.penaltiesSaved, 'PS'),
    a11y: plural(s.penaltiesSaved, 'penalty saved', 'penalties saved'),
    tone: 'good',
  });
  add(s.penaltiesMissed, {
    key: 'pen-missed',
    label: counted(s.penaltiesMissed, 'PM'),
    a11y: plural(s.penaltiesMissed, 'penalty missed', 'penalties missed'),
    tone: 'bad',
  });
  add(s.yellowCards, {
    key: 'yellow',
    label: counted(s.yellowCards, 'YC'),
    a11y: s.yellowCards === 1 ? 'yellow card' : plural(s.yellowCards, 'yellow card'),
    tone: 'bad',
  });
  add(s.redCards, {
    key: 'red',
    label: counted(s.redCards, 'RC'),
    a11y: s.redCards === 1 ? 'red card' : plural(s.redCards, 'red card'),
    tone: 'bad',
  });
  add(s.saves, {
    key: 'saves',
    label: `${s.saves} SV`,
    a11y: plural(s.saves, 'save'),
    tone: 'neutral',
  });
  add(s.bonus, { key: 'bonus', label: `B${s.bonus}`, a11y: `${s.bonus} bonus`, tone: 'good' });
  return out;
}

export interface OwnerBadge {
  key: string;
  /** The owning team's initials: `SL` for Sunday League Heroes. */
  initials: string;
  mine: boolean;
  /** `Sunday League Heroes in The Sunday Draft (your team)`. */
  a11y: string;
}

/** A player's owners as badges: my teams first, then the server's (league) order. */
export function ownerBadges(owners: readonly Owner[]): OwnerBadge[] {
  return owners
    .map((o, i) => ({ o, i }))
    .sort((a, b) => Number(b.o.mine) - Number(a.o.mine) || a.i - b.i)
    .map(({ o }) => ({
      key: `${o.leagueId}/${o.team.id}`,
      initials: initialsOf(o.team.name),
      mine: o.mine,
      a11y: `${o.team.name} in ${o.leagueName}${o.mine ? ' (your team)' : ''}`,
    }));
}

/**
 * The league whose player card a tap in the box score opens: where the
 * player is mine, else the first league that has him, else `fallback` (the
 * user's first league), or null with no league to open it in.
 */
export function cardLeagueFor(owners: readonly Owner[], fallback: string | null): string | null {
  return owners.find((o) => o.mine)?.leagueId ?? owners[0]?.leagueId ?? fallback;
}

/** How many players in the box score are on one of my teams. */
export function mineCount(detail: Schemas['FixtureDetail']): number {
  return [...detail.home.players, ...detail.away.players].filter((p) =>
    p.owners.some((o) => o.mine),
  ).length;
}

/** `90'` played, `–` before a player has come on. */
export const minutesText = (minutes: number) => (minutes > 0 ? `${minutes}'` : '–');
