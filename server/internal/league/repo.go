package league

import (
	"context"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Repo stores leagues (the League aggregate) and their game state. Memory
// and Postgres implement it; the same test suite runs against both.
type Repo interface {
	// Tx runs fn atomically. Leagues loaded through tx are locked until fn
	// returns (SELECT ... FOR UPDATE in Postgres, one mutex in memory), so
	// every change to a league is a load, a change in Go and a save.
	Tx(ctx context.Context, fn func(tx Tx) error) error

	// Get returns the league or nil when it does not exist.
	Get(ctx context.Context, leagueID string) (*League, error)
	// ForUser returns the leagues where userID owns a team, in any order.
	ForUser(ctx context.Context, userID string) ([]*League, error)
	// ByInviteCode returns the league with that invite code, or nil.
	ByInviteCode(ctx context.Context, code string) (*League, error)

	// Game returns the league's game state (empty before the draft).
	Game(ctx context.Context, leagueID string) (*Game, error)
	// InSeasonLeagues returns the ids of every league whose status is
	// in_season: the ones the poller rescores.
	InSeasonLeagues(ctx context.Context) ([]string, error)
	// SaveScores stores the recomputed scores of one gameweek: team scores
	// (rev goes up only when a value changed) and matchup points and state.
	// It returns the teams whose score changed.
	SaveScores(ctx context.Context, leagueID string, gameweek int, scores []TeamScore, matchups []Matchup) (changed []string, err error)
	// LockScores runs fn holding gameweek gw's scoring lock (across
	// processes on Postgres). Rescores of a gameweek take turns, so each
	// reads the stats after the one before it saved, and one that read
	// older stats never overwrites a newer result.
	LockScores(ctx context.Context, gw int, fn func() error) error

	// State reads a league's State without locking it (nil when the league
	// does not exist). Use Tx.LoadState to change it.
	State(ctx context.Context, leagueID string) (*State, error)
	// LeaguesWithStatus lists the ids of the leagues in that status: the
	// ones the scheduled jobs look at.
	LeaguesWithStatus(ctx context.Context, status string) ([]string, error)
	// Claims returns every claim of a team, pending or decided.
	Claims(ctx context.Context, leagueID, teamID string) ([]Claim, error)
	// Trades returns every trade of a league, open or closed, newest first.
	Trades(ctx context.Context, leagueID string) ([]Trade, error)
	// Transactions returns the league's activity feed, newest first.
	Transactions(ctx context.Context, leagueID string) ([]Txn, error)
}

// Tx is a Repo transaction.
type Tx interface {
	// Load returns the league locked for update, or nil.
	Load(ctx context.Context, leagueID string) (*League, error)
	// LoadByInviteCode returns the league with that invite code locked for
	// update, or nil.
	LoadByInviteCode(ctx context.Context, code string) (*League, error)
	// LeagueIDsOfUser lists the leagues where userID owns a team.
	LeagueIDsOfUser(ctx context.Context, userID string) ([]string, error)
	// CodeTaken reports whether an invite code is in use anywhere.
	CodeTaken(ctx context.Context, code string) (bool, error)
	Insert(ctx context.Context, l *League) error
	// Save writes the league row, settings, teams and invites; teams
	// missing from l.Teams are deleted.
	Save(ctx context.Context, l *League) error
	Delete(ctx context.Context, leagueID string) error
	// StartSeason stores the drafted rosters, first lineups and schedule and
	// sets the league in_season (cmd/seed; the draft saves a State instead).
	StartSeason(ctx context.Context, leagueID string, g *Game) error
	// LoadState returns the league's State with the league locked for
	// update, or nil when it does not exist.
	LoadState(ctx context.Context, leagueID string) (*State, error)
	// SaveState writes what changed since LoadState.
	SaveState(ctx context.Context, s *State) error
	// Schedule runs a job at a time, committed with this transaction on
	// Postgres. Without a Scheduler (SetScheduler) it does nothing and the
	// periodic tick picks the work up instead.
	Schedule(ctx context.Context, args JobArgs, at time.Time) error
}

// Game is a league's game state: who owns whom in which gameweeks, the
// lineups, the schedule and the materialised scores.
type Game struct {
	Rosters  []RosterSlot
	Lineups  []Lineup
	Matchups []Matchup
	Scores   []TeamScore
}

// RosterSlot is one player on one team for gameweeks [FromGW, ToGW); ToGW
// 0 means still on the roster.
type RosterSlot struct {
	ID          int64 // 0 until stored
	TeamID      string
	PlayerID    string
	FromGW      int
	ToGW        int
	AcquiredVia string // draft | waiver | free_agent | trade
}

// OnRosterAt reports whether the slot covers gameweek gw.
func (r RosterSlot) OnRosterAt(gw int) bool {
	return r.FromGW <= gw && (r.ToGW == 0 || gw < r.ToGW)
}

// Lineup is a team's 15 slots (1-11 starters, 12-15 bench in order) from
// gameweek FromGW until the team's next lineup.
type Lineup struct {
	TeamID    string
	FromGW    int
	Slots     []string // player ids
	UpdatedAt time.Time
}

// Matchup is one H2H pairing. An empty team id is the league-average side.
type Matchup struct {
	ID         string
	Gameweek   int
	HomeTeamID string
	AwayTeamID string
	HomePoints int
	AwayPoints int
	State      scoring.ScoreState
}

// TeamScore is a team's materialised gameweek score.
type TeamScore struct {
	TeamID      string
	Gameweek    int
	Points      int
	BenchPoints int
	AutoSubs    []scoring.Sub
	State       scoring.ScoreState
	Rev         int
}

// RosterAt returns the team's player ids on its roster in gameweek gw.
func (g *Game) RosterAt(teamID string, gw int) []string {
	var out []string
	for _, r := range g.Rosters {
		if r.TeamID == teamID && r.OnRosterAt(gw) {
			out = append(out, r.PlayerID)
		}
	}
	return out
}

// OwnerAt maps player id to team id for gameweek gw.
func (g *Game) OwnerAt(gw int) map[string]string {
	out := map[string]string{}
	for _, r := range g.Rosters {
		if r.OnRosterAt(gw) {
			out[r.PlayerID] = r.TeamID
		}
	}
	return out
}

// LineupAt returns the team's lineup in force for gameweek gw: the latest
// one starting at or before gw. ok is false when the team has none.
func (g *Game) LineupAt(teamID string, gw int) (Lineup, bool) {
	var best Lineup
	found := false
	for _, l := range g.Lineups {
		if l.TeamID == teamID && l.FromGW <= gw && (!found || l.FromGW > best.FromGW) {
			best, found = l, true
		}
	}
	return best, found
}

// MatchupsIn returns the gameweek's matchups.
func (g *Game) MatchupsIn(gw int) []Matchup {
	var out []Matchup
	for _, m := range g.Matchups {
		if m.Gameweek == gw {
			out = append(out, m)
		}
	}
	return out
}

// Score returns the team's materialised score for gw.
func (g *Game) Score(teamID string, gw int) (TeamScore, bool) {
	for _, s := range g.Scores {
		if s.TeamID == teamID && s.Gameweek == gw {
			return s, true
		}
	}
	return TeamScore{}, false
}
