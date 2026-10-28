package league

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Change kinds: what realtime clients hear about (docs/realtime.md). The
// rule packages and the jobs report them after their transaction commits;
// httpapi renders them into the contract's Realtime* events.
const (
	ChangeDraft          = "draft.updated"
	ChangePick           = "draft.pick_made"
	ChangeMatchups       = "matchups.updated"
	ChangeScoreCorrected = "score.corrected"
	ChangeTrade          = "trade.updated"
	ChangeWaivers        = "waivers.processed"
	ChangeLineupLock     = "lineup.locked"
	ChangeLeague         = "league.updated"
	ChangeTradeBlock     = "trade_block.updated"
)

// Change is one thing that happened to a league.
type Change struct {
	LeagueID string
	Kind     string
	Overall  int    // ChangePick: the pick
	TradeID  string // ChangeTrade
	Gameweek int    // matchups, corrections, waivers, lineup lock
	TeamID   string // ChangeScoreCorrected; ChangeTradeBlock: the team, empty if several
	Previous int    // ChangeScoreCorrected: the settled points before
	Points   int    // ChangeScoreCorrected: the points now
}

// Notifier hears about changes after they are committed. Delivery is best
// effort: clients that miss an event refetch (REST is the source of truth).
type Notifier interface {
	Notify(ctx context.Context, changes ...Change)
}

// NopNotifier drops every change.
type NopNotifier struct{}

func (NopNotifier) Notify(context.Context, ...Change) {}

// JobArgs are a scheduled job's arguments. They match river.JobArgs, so the
// rule packages declare jobs without importing River.
type JobArgs interface{ Kind() string }

// Scheduler runs a job at a time. ScheduleTx receives the repository's
// transaction (a pgx.Tx on Postgres, nil in memory), so on Postgres the job
// commits or rolls back with the change that scheduled it (ADR 0010).
type Scheduler interface {
	ScheduleTx(ctx context.Context, tx any, args JobArgs, at time.Time) error
}

// ErrNoSeasonData: nothing has been ingested yet, so there are no players or
// gameweeks to work with. httpapi answers 503.
var ErrNoSeasonData = errors.New("league: no season data")

// SeasonWorld loads players, clubs, gameweeks and fixtures (no stat rows),
// or returns ErrNoSeasonData.
func SeasonWorld(ctx context.Context, repo stats.Repo) (*World, error) {
	if _, ok, err := repo.Status(ctx); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrNoSeasonData
	}
	return LoadWorld(ctx, repo, -1)
}

// ErrSeasonNotStarted answers lineup, waiver and trade requests in a league
// that has not drafted.
var ErrSeasonNotStarted = Errorf(KindConflict, "season-not-started", "Season not started", "This league has not drafted yet.")

// ErrSeasonOver answers roster moves after the last deadline.
var ErrSeasonOver = Errorf(KindConflict, "season-over", "Season over", "The last gameweek's deadline has passed.")

// OwnTeam returns the actor's team in l: 403 not-a-member when the actor
// is not in the league or has no team.
func OwnTeam(l *League, actor string) (Team, error) {
	if err := member(l, actor); err != nil {
		return Team{}, err
	}
	t, ok := l.TeamOf(actor)
	if !ok {
		return Team{}, Errorf(KindForbidden, "not-a-member", "Not a member", "You have no team in this league.")
	}
	return t, nil
}

// InTradeReview reports whether the player is in an accepted trade that
// has not executed yet (accepted or in review).
func (s *State) InTradeReview(playerID string) bool {
	for _, t := range s.Trades {
		if t.Status == TradeAccepted || t.Status == TradeInReview {
			if slices.Contains(t.Offered, playerID) || slices.Contains(t.Requested, playerID) {
				return true
			}
		}
	}
	return false
}

// UpcomingGW is the first gameweek whose deadline is after now: the one
// lineups and roster moves are for. It is 39 after the last deadline.
func (w *World) UpcomingGW(now time.Time) int {
	for gw := 1; gw <= 38; gw++ {
		if g, ok := w.Gameweeks[gw]; ok && now.Before(g.DeadlineAt) {
			return gw
		}
	}
	return 39
}
