// Package stats is tier 1 of the data (docs/ARCHITECTURE.md "Data
// tiers"): the global season data every league shares — clubs, players,
// gameweeks, fixtures and each player's per-fixture stats and points. Only
// the poller (and cmd/seed) writes it, through Ingester; everything else
// reads it through Repo.
//
// Ingester maps provider data to our ids (ADR 0006: players through
// player_external_id, clubs and fixtures through external_ref), splits a
// gameweek's stats per fixture, awards provisional bonus from BPS until
// upstream confirms it (FPL's own live bonus is a projection too), and
// scores every row with the pure scoring package.
// Memory and Postgres implement Repo; one test suite runs against both.
package stats

import (
	"context"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Season is the season the data belongs to.
const Season = "2026-27"

// StaleAfter is how long live data may go without a good poll before
// responses say meta.stale (ADR 0005).
const StaleAfter = 3 * time.Minute

// Club is a Premier League club. ID is internal (clb_); the contract
// identifies clubs by Code.
type Club struct {
	ID   string
	Code string
	Name string
}

// Player is a season player.
type Player struct {
	ID           string
	FirstName    string
	LastName     string
	DisplayName  string
	Position     scoring.Position
	ClubID       string
	Availability string // open enum (provider.Availability values)
	News         string
	NewsAt       *time.Time // when the news was added; nil if unknown
	ChanceNext   *int       // upstream's chance of playing next round; nil if none
	DraftRank    int
}

// Gameweek is one gameweek and its deadlines and processing flags.
type Gameweek struct {
	Number           int
	Name             string
	DeadlineAt       time.Time
	WaiverDeadlineAt time.Time
	TradeDeadlineAt  time.Time
	Finished         bool
	BonusAdded       bool
	DataChecked      bool
}

// Fixture statuses (FixtureStatus in the contract).
const (
	FixtureScheduled           = "scheduled"
	FixtureLive                = "live"
	FixtureFinishedProvisional = "finished_provisional"
	FixtureFinished            = "finished"
	FixturePostponed           = "postponed"
)

// Fixture is one match. Gameweek is 0 while it is postponed.
type Fixture struct {
	ID         string
	Gameweek   int
	HomeClubID string
	AwayClubID string
	KickoffAt  *time.Time
	Status     string
	HomeScore  *int
	AwayScore  *int
}

// Started reports whether the match has kicked off.
func (f Fixture) Started() bool {
	switch f.Status {
	case FixtureLive, FixtureFinishedProvisional, FixtureFinished:
		return true
	}
	return false
}

// Over reports whether the match has been played to the end.
func (f Fixture) Over() bool {
	return f.Status == FixtureFinishedProvisional || f.Status == FixtureFinished
}

// PlayerFixture is one player's stats and points in one fixture.
type PlayerFixture struct {
	PlayerID  string
	FixtureID string
	Gameweek  int
	// Stats includes the bonus counted in Points.
	Stats scoring.FixtureStats
	BPS   int
	// BPSUnknown: upstream gave no BPS for this fixture (a double gameweek
	// without per-fixture data); BPS is then 0 and means nothing.
	BPSUnknown bool
	// BonusProvisional: Stats.Bonus is projected from BPS, not confirmed.
	BonusProvisional bool
	// Points is scoring.Rules.Points for the player's position, and
	// Breakdown its lines, both from the rules the row was scored with
	// (upstream's published table), so the lines always sum to Points.
	Points    int
	Breakdown []scoring.Line
	// Rev counts changes to the row (1 on insert).
	Rev int
}

// PlayerGameweek is one player's match stats for a gameweek as upstream
// publishes them: gameweek totals, summed over a double gameweek's
// fixtures, and never scored (the player detail's per-game stats and the
// awards read them; points come from PlayerFixture). Every listed player
// has one once the gameweek is under way, zeros included.
type PlayerGameweek struct {
	PlayerID string
	Gameweek int

	Minutes         int
	GoalsScored     int
	Assists         int
	CleanSheets     int
	GoalsConceded   int
	OwnGoals        int
	PenaltiesSaved  int
	PenaltiesMissed int
	YellowCards     int
	RedCards        int
	Saves           int
	// Bonus is upstream's figure, which is a projection until confirmed;
	// the bonus that scores is PlayerFixture.Stats.Bonus.
	Bonus int
	BPS   int
	// Defensive actions; DefensiveContribution is upstream's total for the
	// player's position (CBIT for defenders, CBIRT otherwise).
	ClearancesBlocksInterceptions int
	Tackles                       int
	Recoveries                    int
	DefensiveContribution         int
	// Nil when upstream does not publish them.
	Influence                *float64
	Creativity               *float64
	Threat                   *float64
	ICTIndex                 *float64
	ExpectedGoals            *float64
	ExpectedAssists          *float64
	ExpectedGoalInvolvements *float64
	ExpectedGoalsConceded    *float64
	// Starts: nil when upstream does not send it.
	Starts *int
}

// Status is the season's current position.
type Status struct {
	Current          int
	Next             int // 0 after gameweek 38
	WaiversProcessed bool
}

// Poller modes, from the busiest.
const (
	ModeLive      = "live"      // matches on: poll every 30-60 s
	ModeBonus     = "bonus"     // matches over, bonus not added: every 10 min
	ModeUnchecked = "unchecked" // bonus added, data not checked: every 6 h, closely around the 09:00 UK lock
	ModeIdle      = "idle"      // between gameweeks: daily, or at the next kickoff
)

// PollState is the poller's health, shared through the store so every API
// instance can compute meta.stale.
type PollState struct {
	Mode                string
	Gameweek            int
	LastAttemptAt       time.Time
	LastGoodAt          time.Time
	NextPollAt          time.Time
	LastError           string
	ConsecutiveFailures int
}

// Freshness returns meta.dataAsOf and meta.stale for stats-derived data.
//
// While matches are live, dataAsOf is the last good poll and the data is
// stale 3 minutes after it. Between matches nothing upstream moves, so the
// data is current as long as the poller keeps its schedule: dataAsOf is
// now. A poller that missed its schedule by StaleAfter (failing, or not
// running) makes the data stale with dataAsOf at the last good poll.
func Freshness(ps PollState, now time.Time) (dataAsOf time.Time, stale bool) {
	if ps.LastGoodAt.IsZero() {
		return now, false
	}
	if ps.Mode == ModeLive {
		return ps.LastGoodAt, now.Sub(ps.LastGoodAt) > StaleAfter
	}
	if ps.ConsecutiveFailures == 0 && !ps.NextPollAt.IsZero() && now.Before(ps.NextPollAt.Add(StaleAfter)) {
		return now, false
	}
	return ps.LastGoodAt, now.Sub(ps.LastGoodAt) > StaleAfter
}

// StaleAt is when data read under ps turns stale unless a newer poll
// lands (meta.staleAt): StaleAfter past the last good poll while live,
// else StaleAfter past the next scheduled poll, so an idle poller sleeping
// for hours keeps the data fresh until it is due. Zero before any good poll.
func StaleAt(ps PollState) time.Time {
	if ps.LastGoodAt.IsZero() {
		return time.Time{}
	}
	if ps.Mode == ModeLive || ps.ConsecutiveFailures > 0 || ps.NextPollAt.IsZero() {
		return ps.LastGoodAt.Add(StaleAfter)
	}
	return ps.NextPollAt.Add(StaleAfter)
}

// Mapping links an upstream id to ours.
type Mapping struct {
	Entity     string // player | club | fixture
	ExternalID string
	ID         string
}

// Snapshot is a raw upstream payload for the audit log.
type Snapshot struct {
	Kind      string // bootstrap | live
	Gameweek  int    // 0 when not per gameweek
	FetchedAt time.Time
	Payload   []byte
}

// Batch is one ingest, applied atomically. Nil or empty parts are left
// alone; Live replaces every row of its gameweek.
type Batch struct {
	Provider  string
	Mappings  []Mapping
	Clubs     []Club
	Players   []Player
	Gameweeks []Gameweek
	Fixtures  []Fixture
	Status    *Status
	Live      *LiveRows
	Snapshots []Snapshot
	// Rescore marks gameweeks whose league scores must be recomputed
	// (see Repo.RescorePending), atomically with the rest of the batch.
	Rescore []int
}

// LiveRows are every player-fixture row of one gameweek, and every
// player's gameweek stats.
type LiveRows struct {
	Gameweek int
	Rows     []PlayerFixture
	// Players replaces the gameweek's PlayerGameweek rows when not nil.
	Players []PlayerGameweek
}

// Repo stores tier-1 data.
type Repo interface {
	Clubs(ctx context.Context) ([]Club, error)
	Players(ctx context.Context) ([]Player, error)
	Gameweeks(ctx context.Context) ([]Gameweek, error)
	Fixtures(ctx context.Context) ([]Fixture, error)
	// PlayerFixtures returns one gameweek's rows, or every row for gw 0.
	PlayerFixtures(ctx context.Context, gw int) ([]PlayerFixture, error)
	// PlayerGameweeks returns one gameweek's per-player stats, or every
	// gameweek's for gw 0, ordered by gameweek then player.
	PlayerGameweeks(ctx context.Context, gw int) ([]PlayerGameweek, error)
	// PlayerGameweeksOf returns one player's per-gameweek stats, in
	// gameweek order.
	PlayerGameweeksOf(ctx context.Context, playerID string) ([]PlayerGameweek, error)
	// Status returns the season position; ok is false before any ingest.
	Status(ctx context.Context) (st Status, ok bool, err error)
	PollState(ctx context.Context) (PollState, error)

	// ExternalIDs maps upstream ids to ours for one provider and entity.
	ExternalIDs(ctx context.Context, provider, entity string) (map[string]string, error)
	// Apply writes a batch in one transaction, or nothing (ErrIDTaken)
	// when an upstream id of its Mappings already maps to another id.
	// Rows of Live get Rev 1 on insert and Rev+1 when a value changed.
	Apply(ctx context.Context, b Batch) error
	SavePollState(ctx context.Context, ps PollState) error
	// RescorePending lists the gameweeks marked by Batch.Rescore and not yet
	// cleared, in ascending order.
	RescorePending(ctx context.Context) ([]RescoreMark, error)
	// ClearRescore removes a gameweek's mark after a successful rescore,
	// unless the gameweek was marked again since m was read.
	ClearRescore(ctx context.Context, m RescoreMark) error
	// StartsChecked lists the gameweeks marked by MarkStartsChecked.
	StartsChecked(ctx context.Context) (map[int]bool, error)
	// MarkStartsChecked records that a gameweek was re-read for starts and
	// the source sent none, so it is not re-read for them again.
	MarkStartsChecked(ctx context.Context, gw int) error
}

// RescoreMark is a gameweek pending a rescore. Version changes every time
// the gameweek is marked, so a rescore clears only the mark it started from.
type RescoreMark struct {
	Gameweek int
	Version  int64
}
