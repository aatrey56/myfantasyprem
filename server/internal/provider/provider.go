package provider

import (
	"context"
	"errors"
)

var (
	// ErrNotFound means upstream does not know the requested resource.
	ErrNotFound = errors.New("provider: not found")
	// ErrNotYetAvailable means upstream has the resource but is not
	// publishing it yet, for example entry picks before the deadline.
	ErrNotYetAvailable = errors.New("provider: not yet available")
	// ErrUpstream wraps transport errors, bad status codes and undecodable
	// responses. Callers keep serving the last good snapshot.
	ErrUpstream = errors.New("provider: upstream error")
	// ErrInvalid marks a response that decoded but broke the documented
	// shape or its own references (always together with ErrUpstream). The
	// caller must not store any of it.
	ErrInvalid = errors.New("provider: invalid response")
)

// StatsSource supplies global football data: players, clubs, fixtures and
// live points. It is not tied to any league, so one cached fetch serves every
// league, and a licensed feed can implement it on its own. All methods are
// read-only.
type StatsSource interface {
	// Bootstrap returns players, clubs, gameweeks and the scoring and squad
	// settings (FPL: /bootstrap-static).
	Bootstrap(ctx context.Context) (*Bootstrap, error)

	// Fixtures returns every fixture of the season (FPL: fixtures in
	// /bootstrap-static or /event/{gw}/live).
	Fixtures(ctx context.Context) ([]Fixture, error)

	// GameweekStatus returns the current and next gameweek and processing
	// flags: bonus added, data checked, waivers processed (FPL: /game and
	// /pl/event-status).
	GameweekStatus(ctx context.Context) (*GameweekStatus, error)

	// EventLive returns every player's stats and points for a gameweek,
	// including the per-fixture explain breakdown (FPL: /event/{gw}/live).
	EventLive(ctx context.Context, gameweek int) (*EventLive, error)
}

// LeagueMirrorSource is what importing and syncing an FPL Draft league
// needs (docs/adr/0012); package mirror uses it.
//
// LeagueMirrorSource supplies the state of one league: teams, schedule,
// ownership, drafts, transactions and lineups. It only exists inside FPL
// Draft; a stats feed cannot provide it. Leagues, league entries and FPL
// entries are identified by upstream ids as strings (for FPL, the decimal
// league id, league-entry id and entry id). All methods are read-only and
// need no FPL login.
type LeagueMirrorSource interface {
	// LeagueDetails returns league settings, teams, the full head-to-head
	// schedule with results, and standings (FPL: /league/{id}/details).
	LeagueDetails(ctx context.Context, leagueID string) (*LeagueDetails, error)

	// ElementStatus returns who owns each player in the league
	// (FPL: /league/{id}/element-status).
	ElementStatus(ctx context.Context, leagueID string) ([]ElementStatus, error)

	// DraftChoices returns every draft pick (FPL: /draft/{id}/choices).
	DraftChoices(ctx context.Context, leagueID string) ([]DraftChoice, error)

	// Transactions returns every waiver and free-agent move, including failed
	// claims (FPL: /draft/league/{id}/transactions).
	Transactions(ctx context.Context, leagueID string) ([]Transaction, error)

	// Trades returns the trades the public feed exposes: processed,
	// vetoed and accepted but not yet processed. Open offers need a member
	// login and are not available (FPL: /draft/league/{id}/trades).
	Trades(ctx context.Context, leagueID string) ([]Trade, error)

	// EntryEventPicks returns a team's lineup for a gameweek as upstream
	// shows it, after auto-subs, with the subs (FPL: /entry/{id}/event/{gw}).
	// It returns ErrNotYetAvailable before that gameweek's deadline.
	EntryEventPicks(ctx context.Context, entryID string, gameweek int) (*EntryPicks, error)
}
