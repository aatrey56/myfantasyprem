// Package league owns league domain logic: creating native leagues and
// their settings, membership, invites and teams (Service, over a Repo with
// Memory and Postgres implementations), and the game state that scoring
// reads: rosters over half-open gameweek ranges, lineups, the H2H
// schedule (RoundRobin), materialised team scores and matchups, and the
// derived standings.
//
// Scoring is recomputed from scratch (ADR 0005): ScoreTeam turns a team's
// lineup and the tier-1 stats (package stats) into points, auto-subs and
// bench points with the pure scoring package; ScoreGameweek adds the
// matchups and the league-average side; Scorer stores the result for every
// in-season league and is what the poller calls, reporting score moves and
// corrections to a Notifier.
//
// State (state.go) is the per-league aggregate the draft, lineup, waivers
// and trades packages change: loaded with the league row locked, saved as
// a diff, on Postgres and in memory (docs/adr/0010).
//
// It depends on scoring, stats, store and ids, never on net/http.
// Importing an FPL Draft league (provider.LeagueMirrorSource) is a later
// feature (docs/adr/0008).
package league
