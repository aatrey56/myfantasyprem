// Tests for anonymise-fpl-league.mjs on a tiny synthetic capture. Run
// with `npm run test:scripts`.
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { after, test } from 'node:test';

const dir = mkdtempSync(join(tmpdir(), 'anon-'));
after(() => rmSync(dir, { recursive: true, force: true }));

const details = {
  league: { id: 7, name: 'Real Mates League', scoring: 'h' },
  league_entries: [
    {
      id: 11,
      entry_id: 101,
      entry_name: 'Realteam One',
      player_first_name: 'Realfirst',
      player_last_name: 'Reallast',
      short_name: 'RR',
      waiver_pick: 2,
    },
    {
      id: 12,
      entry_id: 102,
      entry_name: 'Secondreal',
      player_first_name: 'Otherfirst',
      player_last_name: 'Otherlast',
      short_name: 'OO',
      waiver_pick: 1,
    },
    {
      id: 13,
      entry_id: null,
      entry_name: 'Average',
      player_first_name: 'Average',
      player_last_name: '',
      short_name: 'AV',
      waiver_pick: 0,
    },
  ],
  matches: [
    {
      event: 1,
      league_entry_1: 11,
      league_entry_1_points: 40,
      league_entry_2: 12,
      league_entry_2_points: 38,
    },
  ],
  standings: [],
};

function capture() {
  const src = join(dir, 'src');
  mkdirSync(join(src, 'entry'), { recursive: true });
  const w = (f, v) => writeFileSync(join(src, f), JSON.stringify(v));
  w('league_7_details.json', details);
  w('draft_7_choices.json', {
    choices: [
      {
        id: 1,
        entry: 101,
        element: 5,
        entry_name: 'Realteam One',
        player_first_name: 'Realfirst',
        player_last_name: 'Reallast',
        index: 1,
      },
    ],
    idle: [],
    element_status: [],
  });
  w('draft_league_7_transactions.json', {
    transactions: [{ id: 9, entry: 102, element_in: 1, element_out: 2 }],
  });
  w('draft_league_7_trades.json', { trades: [] });
  w('league_7_element-status.json', { element_status: [{ element: 5, owner: 101, status: 'o' }] });
  w('entry/101_public.json', {
    entry: {
      id: 101,
      name: 'Realteam One',
      player_first_name: 'Realfirst',
      player_last_name: 'Reallast',
      overall_points: 40,
    },
  });
  w('entry/101_event_1.json', {
    picks: [{ element: 5, position: 1 }],
    subs: [],
    entry_history: {},
  });
  w('entry/101_event_2.json', 'No pick history');
  return src;
}

test('replaces every name and keeps every id and number', () => {
  const out = join(dir, 'out');
  const res = spawnSync(process.execPath, ['scripts/anonymise-fpl-league.mjs', capture(), out], {
    encoding: 'utf8',
  });
  assert.equal(res.status, 0, res.stderr);
  const read = (p) => readFileSync(join(out, p), 'utf8');
  for (const p of ['league/7/details.json', 'draft/7/choices.json', 'entry/101/public.json']) {
    assert.doesNotMatch(read(p), /Real|Other|Secondreal/);
  }
  const d = JSON.parse(read('league/7/details.json'));
  assert.deepEqual(
    d.league_entries.map((e) => [e.id, e.entry_id, e.waiver_pick]),
    [
      [11, 101, 2],
      [12, 102, 1],
      [13, null, 0],
    ],
  );
  assert.equal(d.league_entries[2].short_name, 'AV');
  assert.deepEqual(d.matches, details.matches);
  assert.equal(JSON.parse(read('entry/101/public.json')).entry.overall_points, 40);
  assert.equal(JSON.parse(read('entry/101/event/2.json')), 'No pick history');
  assert.deepEqual(JSON.parse(read('league/7/element-status.json')).element_status[0], {
    element: 5,
    owner: 101,
    status: 'o',
  });
  const c = JSON.parse(read('draft/7/choices.json')).choices[0];
  assert.equal(c.entry_name, d.league_entries[0].entry_name);
});
