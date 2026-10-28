import type { Schemas } from '@myfantasyprem/api-client';

import { TAGS, tagMeta, tagsByPlayer } from './tags';

const entry = (id: string, tag: string) =>
  ({
    player: { id },
    tag,
    updatedAt: '2026-10-16T09:00:00Z',
  }) as unknown as Schemas['PlayerTagEntry'];

describe('tags', () => {
  it('is the fixed set of five, in order', () => {
    expect(TAGS.map((t) => `${t.emoji} ${t.tag}`)).toEqual([
      '🔥 hot',
      '🧊 cold',
      '🎯 target',
      '💰 sell',
      '🛑 avoid',
    ]);
  });

  it('knows each tag and ignores unknown ones (open enum)', () => {
    expect(tagMeta('sell')).toEqual({ tag: 'sell', emoji: '💰', label: 'Sell' });
    expect(tagMeta('sleeper')).toBeNull();
    expect(tagMeta(null)).toBeNull();
    const m = tagsByPlayer([entry('a', 'hot'), entry('b', 'meh'), entry('c', 'avoid')]);
    expect([...m.entries()].map(([id, t]) => `${id}=${t.tag}`)).toEqual(['a=hot', 'c=avoid']);
    expect(tagsByPlayer(undefined).size).toBe(0);
  });
});
