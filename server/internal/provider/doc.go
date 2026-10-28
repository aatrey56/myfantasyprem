// Package provider defines the seam between the app and its sources of
// football data: the StatsSource and LeagueMirrorSource interfaces and the
// provider-neutral types they return.
//
// The first implementation, package fpl, reads FPL Draft's public API
// (https://draft.premierleague.com/api). A public App Store build cannot
// depend on that API (FPL terms clause 28(d), Apple guideline 5.2.2), so the
// interfaces exist to let a licensed feed such as Sportmonks or Opta replace
// it without touching league or scoring code. The two interfaces are split
// because a stats feed can replace StatsSource alone, while league data
// (LeagueMirrorSource) only exists inside FPL Draft. See docs/adr/0004.
//
// Since docs/adr/0008 leagues are native and need only StatsSource;
// LeagueMirrorSource serves importing an FPL Draft league, keeping it
// synced and switching it over (docs/adr/0012, package mirror).
//
// # Identity
//
// Providers never mint our ids. They return upstream ids as ExternalID
// strings, and the league/store layer maps them to our opaque public ids
// (plr_, tm_, ...) through the player_external_id table described in
// docs/adr/0006-public-ids.md.
//
//   - A player's ExternalID is FPL's permanent "code". It equals Opta's player
//     id (opta_code is "p" followed by the code), is stable across seasons and
//     is the same in Draft and classic FPL.
//   - FPL's "element id" is per season and differs between Draft and classic.
//     It is carried only in Player.SeasonElementID for provider-internal
//     joins. Never use it as identity and never expose it.
//   - Every reference between types (ownership, picks, transactions, trades,
//     live stats) uses ExternalID strings, never element ids.
//
// # League quirks
//
//   - Odd-sized leagues have a league-average pseudo-entry. FPL lists it in
//     league_entries with a null entry_id and short_name "AV", and in the
//     standings. Implementations must flag it (LeagueEntry.IsAverage) and the
//     league package must filter it out of standings. It plays one real team
//     per gameweek, and its score is floor(mean of all real teams' gameweek
//     points, including its opponent).
//   - FPL's draft_status field is unreliable. Detect a completed draft with
//     drafts[].draft_completed (LeagueDetails.DraftCompleted).
//   - FPL names a team by its league-entry id in matches and standings and
//     by its entry id everywhere else; implementations translate every
//     reference to LeagueEntry.ExternalID.
//   - FPL's public trades feed shows processed (p), vetoed (v) and
//     accepted trades still waiting for approval or processing (a). Open
//     offers are visible only to a logged-in league member, and we never
//     collect FPL logins, so Trades never returns them.
//
// # Rules for implementations
//
//   - Honour context cancellation and deadlines on every call.
//   - Translate upstream codes into the typed enums in this package at the
//     boundary. Single-letter FPL codes never leave an implementation, except
//     in the debug-only SourceState fields.
//   - Return ErrNotFound for unknown ids and ErrNotYetAvailable when upstream
//     has the resource but is not publishing it yet (for example a locked
//     lineup before the deadline). Wrap everything else in ErrUpstream.
//   - Be polite: at most 2 requests per second, a clear User-Agent that names
//     the app and a contact, and back off on errors (honour Retry-After, then
//     exponential delay). Caching is the caller's job, not the provider's.
package provider
