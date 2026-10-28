# 0004. Provider interfaces: StatsSource and LeagueMirrorSource

Status: accepted. Amended by [0008](0008-native-leagues.md): only `StatsSource` is needed for v1; `LeagueMirrorSource` is a later, optional interface.

## Context

v1 reads FPL Draft's public API. That is fine for a private app for friends. A
public App Store app that depends on it conflicts with FPL terms clause 28(d)
(no automated systems to extract information) and Apple guideline 5.2.2 (an
app that shows another service's content needs that service's permission). UK
database right also covers live match data (RESEARCH.md section 8).

A licensed feed, such as Sportmonks or Opta, can replace FPL as the data
source, with some approximation (bonus, defensive contributions) that must be
labelled provisional.

## Decision

All upstream data goes through two Go interfaces in
`server/internal/provider`, both read-only and context-aware:

- `StatsSource`: players, clubs, gameweeks and scoring settings, fixtures,
  gameweek status, and event live points with the per-fixture explain
  breakdown, BPS and defensive-contribution stats. A licensed stats feed can
  replace this alone.
- `LeagueMirrorSource`: league details, element status (ownership), draft
  choices, transactions, trades and entry event picks. This data exists only
  in FPL Draft.

The first implementation of both will call the FPL Draft API. League and
scoring code depend only on the interfaces and the provider-neutral types.
Implementations translate upstream codes (FPL's single-letter statuses,
`a`/`di`/`do` claim results, `p`/`v` trade states) into our own enums at the
boundary; only debug `SourceState` fields keep the raw code. There are no
untyped `Raw` maps. Providers return external ids; our public ids and the
mapping table are in ADR 0006.

## Consequences

- Swapping to a licensed feed is a new `StatsSource`, not a rewrite of league
  or scoring code.
- The types are still shaped by FPL's data. A non-FPL feed will need
  adapters, for example for bonus and defensive contributions.
- League import (league details, picks, transactions) is FPL-specific by
  nature. A public app must resolve that separately with the Premier League.
- Until the legal position is settled, the app stays private or web only, with
  text-only branding and no money handling.
