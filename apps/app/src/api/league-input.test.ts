/**
 * The import screen's parser against api/testdata/league-inputs.json, the
 * cases the server's parser is also tested on (leagueurl_test.go).
 */
import { readFileSync } from 'fs';
import { join } from 'path';

import { LEAGUE_INPUT_MESSAGES, parseLeagueInput } from './league-input';

type Case = { input: string; id?: number; error?: string };
const shared = JSON.parse(
  readFileSync(join(__dirname, '../../../../api/testdata/league-inputs.json'), 'utf8'),
) as { messages: Record<string, string>; cases: Case[] };

describe('parseLeagueInput (shared cases)', () => {
  it('has the same messages as the server', () => {
    expect(LEAGUE_INPUT_MESSAGES).toEqual(shared.messages);
  });

  it.each(shared.cases.map((c) => [JSON.stringify(c.input), c] as const))('%s', (_, c) => {
    const got = parseLeagueInput(c.input);
    if (c.error === undefined) expect(got).toEqual({ kind: 'id', fplLeagueId: c.id });
    else if (c.error === 'classic') expect(got).toEqual({ kind: 'classic' });
    else expect(got).toEqual({ kind: 'invalid', reason: shared.messages[c.error] });
  });

  it('never names a request field in a message', () => {
    for (const msg of Object.values(LEAGUE_INPUT_MESSAGES)) {
      expect(msg).not.toMatch(/draftLeagueUrl|fplLeagueId/);
    }
  });
});
