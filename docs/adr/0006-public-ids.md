# 0006. Opaque public ids and the player identity map

Status: accepted

## Context

The first contract keyed players by FPL's `code` and used FPL integers for
leagues and entries. That ties every URL, cache key and stored reference to
one provider. A licensed feed (ADR 0004) has its own ids, native leagues will
have no FPL id at all, and FPL's own ids are not uniform:

- A player's `code` is permanent across seasons and is the same in Draft and
  classic FPL. It equals Opta's player id: `opta_code` is `"p"` + `code`.
- The element `id` is per season, and differs between Draft and classic for
  the same player. It must never be used as identity.
- Draft and classic league ids are separate number spaces that overlap, so a
  bare league number is ambiguous.

## Decision

Every public id is our own opaque string: a type prefix, `_`, and a
lowercase ULID (Crockford base32, 26 characters). The contract gives each a
`pattern`:

| Prefix | Resource         |
| ------ | ---------------- |
| `lg_`  | league           |
| `tm_`  | team (FPL entry) |
| `mu_`  | matchup          |
| `plr_` | player           |
| `fx_`  | fixture          |
| `txn_` | transaction      |
| `imp_` | import job       |

`server/internal/ids` mints and validates them; handlers reject a malformed
id with 400. FPL's league id survives only as the `fplLeagueId` attribute.

Providers never mint our ids. They return upstream ids as `ExternalID`
strings and the league/store layer maps them. For players the map is a
table:

```sql
CREATE TABLE player_external_id (
  player_id    text        NOT NULL REFERENCES players(id),  -- plr_...
  provider     text        NOT NULL,                         -- 'fpl', 'opta', 'sportmonks', ...
  external_id  text        NOT NULL,                         -- FPL: code; Opta: p<code>
  season       text        NULL,                             -- NULL when the id is permanent
  match_method text        NOT NULL,                         -- exact_key | dob_name | manual
  confidence   real        NOT NULL,                         -- 1.0 for exact and manual
  verified_at  timestamptz NULL,                             -- set by a human review
  UNIQUE (provider, external_id, season)
);
```

PostgreSQL treats NULLs as distinct in a unique constraint, so the real
constraint is `UNIQUE NULLS NOT DISTINCT (provider, external_id, season)`
(Postgres 15+).

Matching a provider's player to an existing `plr_` id, in order:

1. **Exact key.** A known cross-provider key: FPL `code` ↔ Opta id
   (`"p"` + code), or an id already in the table.
2. **Date of birth plus accent-stripped name**, only when exactly one
   candidate matches.
3. **Club and position** as tie-breakers for step 2.
4. Otherwise a **manual review queue**; nothing is merged automatically.

Teams, leagues and transactions follow the same pattern with simpler maps
(one provider today); the columns live on their own tables
(`teams.fpl_entry_id`, `leagues.fpl_league_id`,
`transactions.fpl_txn_id UNIQUE`).

## Consequences

- URLs and caches survive a provider switch and a season rollover.
- Ids carry no meaning: a client cannot build one, and nothing breaks when a
  provider reuses a number.
- Imports need an id-mapping step, and fuzzy player matches need review
  tooling before a second provider is added.
- The provider interfaces (`StatsSource`, `LeagueMirrorSource`) speak only
  external ids and our own enums; single-letter FPL codes stay inside the
  FPL implementation except for the debug-only `sourceState` fields.
