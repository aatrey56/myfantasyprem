#!/usr/bin/env node
// Turns a capture of an FPL Draft league's public endpoints into test
// fixtures without anyone's real name (docs/adr/0012): every manager,
// team and the league get invented names; every id, number, time and code
// stays as captured, so the shapes and the maths stay real. It reads only
// local files and never calls FPL.
//
//   node scripts/anonymise-fpl-league.mjs <capture dir> <out dir>
//
// The capture dir holds league_<id>_details.json, draft_<id>_choices.json,
// draft_league_<id>_transactions.json, draft_league_<id>_trades.json,
// league_<id>_element-status.json and entry/<entry>_public.json and
// entry/<entry>_event_<gw>.json. The out dir gets the API's own paths
// (league/<id>/details.json, entry/<entry>/event/<gw>.json, ...), which is
// what provider/fpl's DirTransport serves.
import { existsSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const FIRST = [
  'Alex',
  'Billie',
  'Casey',
  'Dani',
  'Eden',
  'Frankie',
  'Gray',
  'Harper',
  'Indy',
  'Jules',
  'Kit',
  'Lou',
  'Morgan',
  'Noor',
  'Oakley',
  'Pip',
];
const LAST = [
  'Applegate',
  'Brookfield',
  'Cobble',
  'Dunmore',
  'Elmswood',
  'Fernhill',
  'Gorse',
  'Hawthorn',
  'Ivybridge',
  'Juniper',
  'Kestrel',
  'Larchmont',
  'Millbrook',
  'Nettlebed',
  'Orchard',
  'Pebblestone',
];
const TEAMS = [
  'Allotment Athletic',
  'Bramble Rovers',
  'Compost United',
  'Dandelion Town',
  'Elderflower FC',
  'Foxglove Wanderers',
  'Gooseberry City',
  'Hedgerow Albion',
  'Ivy League XI',
  'Jam Jar Juniors',
  'Kale Harriers',
  'Leek Lane',
  'Marrow Park',
  'Nettle Rangers',
  'Onion Bag Orient',
  'Parsnip Vale',
];
export const LEAGUE_NAME = 'Fixture Draft League';

// names returns the invented names for the seats, by entry id, in the
// order of league_entries (so the same capture always gives the same names).
export function names(details) {
  const out = new Map();
  details.league_entries.forEach((e, i) => {
    if (e.entry_id == null) return; // the league-average entry keeps "AV"
    const first = FIRST[i % FIRST.length];
    const last = LAST[i % LAST.length];
    out.set(e.entry_id, {
      first,
      last,
      team: TEAMS[i % TEAMS.length],
      short: (first[0] + last[0]).toUpperCase(),
    });
  });
  return out;
}

export function anonymiseDetails(details, n) {
  return {
    ...details,
    league: { ...details.league, name: LEAGUE_NAME },
    league_entries: details.league_entries.map((e) => {
      const x = n.get(e.entry_id);
      if (!x) return e;
      return {
        ...e,
        entry_name: x.team,
        player_first_name: x.first,
        player_last_name: x.last,
        short_name: x.short,
      };
    }),
  };
}

export function anonymiseChoices(choices, n) {
  return {
    ...choices,
    choices: choices.choices.map((c) => {
      const x = n.get(c.entry);
      if (!x) throw new Error(`choice ${c.id}: unknown entry ${c.entry}`);
      return { ...c, entry_name: x.team, player_first_name: x.first, player_last_name: x.last };
    }),
  };
}

export function anonymisePublic(pub, n) {
  const x = n.get(pub.entry.id);
  if (!x) throw new Error(`entry ${pub.entry.id} is not in the league`);
  return {
    ...pub,
    entry: { ...pub.entry, name: x.team, player_first_name: x.first, player_last_name: x.last },
  };
}

// realNames lists every name string in the capture, to check none survives.
export function realNames(details) {
  const out = [details.league.name];
  for (const e of details.league_entries) {
    if (e.entry_id == null) continue;
    out.push(e.entry_name, e.player_first_name, e.player_last_name);
  }
  return out.filter((s) => s && s.length >= 3);
}

function read(path) {
  return JSON.parse(readFileSync(path, 'utf8'));
}

function write(path, value) {
  mkdirSync(dirname(path), { recursive: true });
  writeFileSync(path, typeof value === 'string' ? value : JSON.stringify(value));
}

function main(src, out) {
  const file = (re) => {
    const f = readdirSync(src).find((x) => re.test(x));
    if (!f) throw new Error(`no file matching ${re} in ${src}`);
    return join(src, f);
  };
  const details = read(file(/^league_\d+_details\.json$/));
  const id = details.league.id;
  const n = names(details);
  write(join(out, `league/${id}/details.json`), anonymiseDetails(details, n));
  write(join(out, `league/${id}/element-status.json`), read(file(/_element-status\.json$/)));
  write(
    join(out, `draft/${id}/choices.json`),
    anonymiseChoices(read(file(/^draft_\d+_choices\.json$/)), n),
  );
  write(join(out, `draft/league/${id}/transactions.json`), read(file(/_transactions\.json$/)));
  write(join(out, `draft/league/${id}/trades.json`), read(file(/_trades\.json$/)));
  for (const f of readdirSync(join(src, 'entry'))) {
    let m = /^(\d+)_public\.json$/.exec(f);
    if (m) {
      write(
        join(out, `entry/${m[1]}/public.json`),
        anonymisePublic(read(join(src, 'entry', f)), n),
      );
      continue;
    }
    m = /^(\d+)_event_(\d+)\.json$/.exec(f);
    if (m) {
      // Picks carry no names; "No pick history" stays a JSON string.
      write(
        join(out, `entry/${m[1]}/event/${m[2]}.json`),
        readFileSync(join(src, 'entry', f), 'utf8'),
      );
    }
  }
  // Nothing written may still hold a real name.
  const leaked = [];
  const walk = (dir) => {
    for (const f of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, f.name);
      if (f.isDirectory()) walk(p);
      else {
        const body = readFileSync(p, 'utf8');
        for (const s of realNames(details))
          if (body.includes(JSON.stringify(s).slice(1, -1))) leaked.push(`${p}: ${s}`);
      }
    }
  };
  for (const d of ['league', 'draft', 'entry']) if (existsSync(join(out, d))) walk(join(out, d));
  if (leaked.length) throw new Error(`real names left:\n${leaked.join('\n')}`);
  console.log(`anonymised league ${id}: ${n.size} entries into ${out}`);
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
  const [src, out] = process.argv.slice(2);
  if (!src || !out) {
    console.error('usage: node scripts/anonymise-fpl-league.mjs <capture dir> <out dir>');
    process.exit(2);
  }
  main(src, out);
}
