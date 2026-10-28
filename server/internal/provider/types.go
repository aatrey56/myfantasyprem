package provider

import "time"

// Position is a player's position.
type Position string

const (
	GKP Position = "GKP"
	DEF Position = "DEF"
	MID Position = "MID"
	FWD Position = "FWD"
)

// Availability is whether a player is expected to play. FPL's status codes
// (a, d, i, s, u, n) are translated at the provider boundary; an unrecognised
// code becomes AvailabilityUnknown.
type Availability string

const (
	AvailabilityAvailable   Availability = "available"
	AvailabilityDoubtful    Availability = "doubtful"
	AvailabilityInjured     Availability = "injured"
	AvailabilitySuspended   Availability = "suspended"
	AvailabilityUnavailable Availability = "unavailable"
	AvailabilityUnknown     Availability = "unknown"
)

// FixtureStatus is the lifecycle of a match. Derive it from upstream flags
// (FPL: started, finished_provisional, finished, and a missing kickoff for a
// postponed match).
type FixtureStatus string

const (
	FixtureScheduled           FixtureStatus = "scheduled"
	FixtureLive                FixtureStatus = "live"
	FixtureFinishedProvisional FixtureStatus = "finished_provisional" // over, stats not final
	FixtureFinished            FixtureStatus = "finished"
	FixturePostponed           FixtureStatus = "postponed"
)

// TransactionKind is how a player moved. FPL: w (waiver), f (free agent).
type TransactionKind string

const (
	TransactionWaiver    TransactionKind = "waiver"
	TransactionFreeAgent TransactionKind = "free_agent"
)

// ClaimResult is the outcome of a transaction. FPL: a (approved), di (denied,
// the incoming player is no longer available), do (denied, the outgoing player
// is no longer available).
type ClaimResult string

const (
	ClaimApproved                   ClaimResult = "approved"
	ClaimDeniedPlayerInUnavailable  ClaimResult = "denied_player_in_unavailable"
	ClaimDeniedPlayerOutUnavailable ClaimResult = "denied_player_out_unavailable"
	ClaimUnknown                    ClaimResult = "unknown"
)

// TradeState is where a trade stands. FPL Draft documents proposed,
// withdrawn, rejected, accepted (waiting for approval), invalid (a player
// was in another accepted trade), vetoed and processed; offers also
// expire at the waiver deadline. The public feed has shown only three
// codes so far: p (processed), v (vetoed) and a (accepted, waiting for
// the commissioner or the veto window, then processing). Any other code
// is TradeUnknown with the code in Trade.SourceState (docs/adr/0012).
type TradeState string

const (
	TradeProcessed TradeState = "processed"
	TradeVetoed    TradeState = "vetoed"
	TradeAccepted  TradeState = "accepted"
	TradeOffered   TradeState = "offered"
	TradeRejected  TradeState = "rejected"
	TradeWithdrawn TradeState = "withdrawn"
	TradeExpired   TradeState = "expired"
	TradeInvalid   TradeState = "invalid"
	TradeUnknown   TradeState = "unknown"
)

// Ownership is a player's status in a league. FPL element-status: o (owned),
// a (available), w (waived: dropped and locked until waivers run).
type Ownership string

const (
	OwnershipOwned     Ownership = "owned"
	OwnershipFreeAgent Ownership = "free_agent"
	OwnershipWaived    Ownership = "waived"
	OwnershipUnknown   Ownership = "unknown"
)

// Bootstrap is the season reference data.
type Bootstrap struct {
	Players   []Player
	Clubs     []Club
	Gameweeks []Gameweek
	Settings  GameSettings
	// Payload is the upstream response as received, for the audit log
	// (stat_snapshots). Never parse it outside the provider.
	Payload []byte
}

// Player is a season player.
type Player struct {
	// ExternalID is the permanent upstream identity. FPL: "code", which equals
	// Opta's player id (opta_code is "p"+code). The league/store layer maps it
	// to our plr_ id.
	ExternalID string
	// SeasonElementID is FPL's element id. It is per season and differs
	// between Draft and classic, so it is never identity. It exists for
	// provider-internal joins only.
	SeasonElementID int64
	FirstName       string
	LastName        string
	// DisplayName is the short name shown in lists (FPL: web_name).
	DisplayName string
	// BirthDate is nil when upstream does not provide it.
	BirthDate *time.Time
	// ClubExternalID references Club.ExternalID.
	ClubExternalID string
	Position       Position
	Availability   Availability
	// News is the short injury or availability note, empty if none.
	News string
	// NewsAt is when upstream added the news; nil when unknown or no news.
	NewsAt *time.Time
	// ChanceNext is upstream's percent chance of playing next round; nil
	// when upstream gives none (usually: fit).
	ChanceNext *int
	// DraftRank is the pre-draft ranking, 1 = best; 0 when upstream has none.
	DraftRank int
}

// Club is a Premier League club.
type Club struct {
	// ExternalID is the permanent club id (FPL: team "code").
	ExternalID string
	// SeasonTeamID is FPL's per-season team id, for provider-internal joins.
	SeasonTeamID int64
	// ShortName is the three letter code, for example "ARS".
	ShortName string
	Name      string
}

// Gameweek is one of the 38 gameweeks.
type Gameweek struct {
	Number           int
	Name             string
	DeadlineAt       time.Time
	WaiverDeadlineAt time.Time
	TradeDeadlineAt  time.Time
	Finished         bool
	DataChecked      bool
}

// GameSettings carries the squad and scoring rules from upstream. Golden tests
// compare the scoring engine's table against it.
type GameSettings struct {
	// SquadSize is the number of players per team (15).
	SquadSize int
	// StartingSize is the number of starters (11).
	StartingSize int
	Scoring      ScoringTable
}

// PositionPoints is a value that depends on the player's position.
type PositionPoints struct {
	GKP, DEF, MID, FWD int
}

// ScoringTable is the points table. Penalties are negative numbers.
type ScoringTable struct {
	// MinutesUnder60 is the total points for playing 1 to 59 minutes.
	MinutesUnder60 int
	// Minutes60Plus is the total points for playing 60 minutes or more.
	Minutes60Plus int
	Goal          PositionPoints
	Assist        int
	CleanSheet    PositionPoints
	// GoalsConcededPerTwo is awarded for every 2 goals conceded.
	GoalsConcededPerTwo PositionPoints
	// SavesPerThree is awarded for every 3 saves.
	SavesPerThree int
	PenaltySave   int
	PenaltyMiss   int
	OwnGoal       int
	YellowCard    int
	RedCard       int
	// DefensiveContributionThreshold is the stat total (clearances, blocks,
	// interceptions, tackles, plus recoveries for midfielders and forwards)
	// needed for DefensiveContribution points. Zero means the position cannot
	// earn them.
	DefensiveContributionThreshold PositionPoints
	DefensiveContribution          int
}

// Fixture is a single match.
type Fixture struct {
	// ExternalID is the permanent fixture id (FPL: fixture "code").
	ExternalID string
	// SeasonFixtureID is FPL's per-season fixture id, for internal joins.
	SeasonFixtureID int64
	Gameweek        int
	// HomeClubExternalID and AwayClubExternalID reference Club.ExternalID.
	HomeClubExternalID string
	AwayClubExternalID string
	// KickoffAt is nil while a postponed match has no date.
	KickoffAt *time.Time
	// Status: treat a club's gameweek as done only when all of its fixtures
	// are, so double gameweeks work.
	Status    FixtureStatus
	HomeScore *int
	AwayScore *int
}

// GameweekStatus is the current and next gameweek plus processing flags.
type GameweekStatus struct {
	Current int
	Next    *int
	// BonusAdded: bonus is confirmed for every matchday of the current
	// gameweek.
	BonusAdded bool
	// BonusAddedOn lists the current gameweek's matchdays (UK dates,
	// "2006-01-02") whose bonus is confirmed (FPL: pl/event-status
	// bonus_added per date). Until then any bonus upstream shows for that
	// day's matches is FPL's live projection, not a confirmed value.
	BonusAddedOn      map[string]bool
	DataChecked       bool
	WaiversProcessed  bool
	LeaguesUpdatedFor time.Time
}

// Stat identifies a line in a points breakdown. The values are our names; they
// match FPL's explain identifiers where FPL has one.
type Stat string

const (
	StatMinutes               Stat = "minutes"
	StatGoalsScored           Stat = "goals_scored"
	StatAssists               Stat = "assists"
	StatCleanSheets           Stat = "clean_sheets"
	StatGoalsConceded         Stat = "goals_conceded"
	StatOwnGoals              Stat = "own_goals"
	StatPenaltiesSaved        Stat = "penalties_saved"
	StatPenaltiesMissed       Stat = "penalties_missed"
	StatYellowCards           Stat = "yellow_cards"
	StatRedCards              Stat = "red_cards"
	StatSaves                 Stat = "saves"
	StatBonus                 Stat = "bonus"
	StatDefensiveContribution Stat = "defensive_contribution"
)

// ExplainLine is one stat line of a points breakdown.
type ExplainLine struct {
	Stat   Stat
	Value  int
	Points int
}

// FixtureExplain is the explain breakdown of one player in one fixture. Golden
// tests compare the scoring engine against these.
type FixtureExplain struct {
	// FixtureExternalID references Fixture.ExternalID.
	FixtureExternalID string
	Lines             []ExplainLine
	// BPS is the player's bonus points system score in this fixture alone,
	// from the fixture's own stats (FPL: fixtures[].stats, identifier
	// "bps"). Nil when upstream does not give it. In a double gameweek it
	// is the only per-fixture BPS: PlayerLive.BPS is the gameweek total.
	BPS *int
}

// PlayerLive is one player's live stats and points in a gameweek.
type PlayerLive struct {
	// PlayerExternalID references Player.ExternalID.
	PlayerExternalID string
	Minutes          int
	TotalPoints      int
	// Bonus is the current bonus, provisional until GameweekStatus.BonusAdded.
	Bonus int
	// BPS is the raw bonus points system score that bonus is derived from.
	BPS int
	// Defensive contribution inputs.
	ClearancesBlocksInterceptions int
	Recoveries                    int
	Tackles                       int
	// DefensiveContribution is the stat total counted against the threshold
	// for the player's position.
	DefensiveContribution int
	// Gameweek totals of the scoring stats (across fixtures in a double).
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
	// Underlying stats, gameweek totals: the ICT index and its parts, and
	// expected goals, assists, goal involvements and goals conceded. They
	// score nothing; nil when upstream does not publish them.
	Influence                *float64
	Creativity               *float64
	Threat                   *float64
	ICTIndex                 *float64
	ExpectedGoals            *float64
	ExpectedAssists          *float64
	ExpectedGoalInvolvements *float64
	ExpectedGoalsConceded    *float64
	// Starts is the gameweek's starts (2 in a double where the player
	// started both); nil when upstream does not send it.
	Starts *int
	// Explain is upstream's per-fixture points breakdown. It splits a
	// double gameweek's stats by fixture.
	Explain []FixtureExplain
}

// EventLive is a gameweek's live data for all players.
type EventLive struct {
	Gameweek int
	Players  []PlayerLive
	Fixtures []Fixture
	// FetchedAt is when the upstream response was received.
	FetchedAt time.Time
	// Payload is the upstream response as received, for the audit log.
	Payload []byte
}

// LeagueScoring is how a league scores. FPL: h (head-to-head), c (classic).
type LeagueScoring string

const (
	ScoringH2H     LeagueScoring = "h2h"
	ScoringClassic LeagueScoring = "classic"
	ScoringUnknown LeagueScoring = "unknown"
)

// TradeMode is how a league reviews trades. FPL: m (the commissioner
// reviews; the league owner's confirmation, docs/adr/0012), n (no
// trades). Other codes are TradeModeUnknown with the code in
// LeagueDetails.TradeModeSource.
type TradeMode string

const (
	TradeModeCommissioner TradeMode = "commissioner"
	TradeModeNoTrades     TradeMode = "no_trades"
	TradeModeUnknown      TradeMode = "unknown"
)

// TransactionMode is how a league moves players. FPL: waivers, then free
// agents after the waiver run.
type TransactionMode string

const (
	TransactionModeWaivers TransactionMode = "waivers"
	TransactionModeUnknown TransactionMode = "unknown"
)

// LeagueDetails is the league as upstream describes it.
type LeagueDetails struct {
	// ExternalID is the upstream league id (FPL: the decimal Draft league id).
	ExternalID string
	Name       string
	Scoring    LeagueScoring
	TradeMode  TradeMode
	// TradeModeSource and TransactionModeSource are the raw codes (FPL:
	// league.trades and league.transaction_mode), for support and the
	// import report only. Never branch on them.
	TradeModeSource       string
	TransactionMode       TransactionMode
	TransactionModeSource string
	// DraftCompleted is true once the draft has finished. FPL's draft_status
	// is unreliable; read drafts[].draft_completed instead.
	DraftCompleted   bool
	DraftCompletedAt *time.Time
	DraftAt          *time.Time
	// DraftGameweek is the gameweek the draft's squads start in.
	DraftGameweek   int
	PickTimeSeconds int
	// AdminEntry references LeagueEntry.ExternalID; "" when upstream names
	// no admin among the entries.
	AdminEntry string
	Teams      []LeagueEntry
	Matches    []LeagueMatch
	// Standings includes the average pseudo-entry when upstream lists it. The
	// league package filters it out.
	Standings []LeagueStanding
	// StartGameweek and EndGameweek bound the league's scored gameweeks.
	StartGameweek int
	EndGameweek   int
}

// LeagueEntry is one team (seat) in a league.
//
// FPL's league-average pseudo-entry appears in league_entries with a null
// entry_id and short_name "AV", and in the standings. Implementations must set
// IsAverage on it; the league package filters it out of standings. It plays
// one real team per gameweek in odd-sized leagues, and its score is floor(mean
// of all real teams' gameweek points, including its opponent).
type LeagueEntry struct {
	// ExternalID is the league-entry id that matches and standings refer
	// to (FPL: league_entries[].id). Every other league type refers to a
	// team by it too; implementations translate.
	ExternalID string
	// EntryID is the FPL entry id used for EntryEventPicks. Empty for the
	// average entry.
	EntryID     string
	TeamName    string
	ManagerName string
	ShortName   string
	// WaiverPick is the team's place in the next waiver run (1 = first).
	WaiverPick int
	JoinedAt   *time.Time
	IsAverage  bool
}

// LeagueMatch is one head-to-head match. HomeEntry and AwayEntry reference
// LeagueEntry.ExternalID; one of them is the average entry when the league
// has an odd number of teams. FPL: entry 1 is home.
type LeagueMatch struct {
	Gameweek  int
	HomeEntry string
	AwayEntry string
	HomePts   int
	AwayPts   int
	Started   bool
	Finished  bool
}

// LeagueStanding is one row of the standings. LeagueEntry references
// LeagueEntry.ExternalID. FPL's matches_played is not carried: it counts
// every scheduled match, played or not.
type LeagueStanding struct {
	Rank          int
	LeagueEntry   string
	Wins          int
	Draws         int
	Losses        int
	PointsFor     int
	PointsAgainst int
	Total         int
}

// ElementStatus is a player's ownership in a league.
type ElementStatus struct {
	// PlayerExternalID references Player.ExternalID.
	PlayerExternalID string
	// OwnerEntry references LeagueEntry.ExternalID; nil unless owned.
	OwnerEntry *string
	Ownership  Ownership
	// InAcceptedTrade: the player is in a trade that was accepted and is
	// waiting for approval or processing (a TradeAccepted trade).
	InAcceptedTrade bool
}

// DraftChoice is one draft pick.
type DraftChoice struct {
	// Overall is the pick number across rounds, from 1.
	Overall int
	Round   int
	Pick    int
	// LeagueEntry references LeagueEntry.ExternalID.
	LeagueEntry      string
	PlayerExternalID string
	WasAuto          bool
	At               time.Time
}

// Transaction is a waiver or free-agent move.
type Transaction struct {
	ExternalID string
	Kind       TransactionKind
	Result     ClaimResult
	// SourceState is the raw provider code (FPL: kind and result letters
	// joined, for example "w/di"), for support and debug display only. Never
	// branch on it.
	SourceState string
	// LeagueEntry references LeagueEntry.ExternalID.
	LeagueEntry string
	// PlayerIn and PlayerOut reference Player.ExternalID.
	PlayerIn  string
	PlayerOut string
	// Gameweek is the gameweek the move counts from.
	Gameweek    int
	Priority    *int
	Index       int
	ProcessedAt time.Time
}

// Trade is a trade the public feed shows: processed, vetoed, or accepted
// and waiting. Open offers are not visible without a member login (see
// the package documentation).
type Trade struct {
	ExternalID string
	State      TradeState
	// SourceState is the raw provider code (FPL: p, v, a), for support and debug
	// display only. Never branch on it.
	SourceState string
	// OfferEntry and ReceiveEntry reference LeagueEntry.ExternalID.
	OfferEntry   string
	ReceiveEntry string
	// Offered move from the offering team to the receiving one, Requested
	// the other way; both reference Player.ExternalID, in pairs.
	Offered   []string
	Requested []string
	// Gameweek is the gameweek the trade counts from.
	Gameweek   int
	OfferedAt  time.Time
	ResolvedAt *time.Time
}

// EntryPicks is a team's locked lineup for a gameweek as upstream shows
// it: FPL lists the picks after auto-subs (slots 1-11 are the XI that
// scored) and the subs it made.
type EntryPicks struct {
	// EntryID is the FPL entry id (LeagueEntry.EntryID).
	EntryID  string
	Gameweek int
	// Picks are in slot order, 1 to 15.
	Picks    []Pick
	AutoSubs []AutoSub
	// Points is upstream's gameweek total for the team when it publishes
	// one (FPL: entry_history.points, empty in Draft); nil otherwise.
	Points *int
}

// Pick is one lineup slot.
type Pick struct {
	Slot int
	// PlayerExternalID references Player.ExternalID.
	PlayerExternalID string
}

// AutoSub is an applied substitution; both fields reference Player.ExternalID.
type AutoSub struct {
	OutExternalID string
	InExternalID  string
}

// PlayerSeason is a player's totals for one finished Premier League season
// (FPL: element-summary history_past). Seasons link across years by
// PlayerExternalID (FPL `code`), never by name. A stat is nil when FPL did
// not track it that season (FPL sends 0 instead; see the fpl package) or
// did not send it. There is no club: history_past has none.
type PlayerSeason struct {
	PlayerExternalID string
	// Season is FPL's label, "2021/22".
	Season string

	TotalPoints     *int
	Minutes         *int
	GoalsScored     *int
	Assists         *int // FPL's assists, about 20% above Opta's
	CleanSheets     *int
	GoalsConceded   *int
	OwnGoals        *int
	PenaltiesSaved  *int
	PenaltiesMissed *int
	YellowCards     *int
	RedCards        *int
	Saves           *int
	Bonus           *int
	BPS             *int
	// StartCost and EndCost are in tenths of a million (FPL classic).
	StartCost *int
	EndCost   *int

	Influence  *float64
	Creativity *float64
	Threat     *float64
	ICTIndex   *float64

	Starts                   *int
	ExpectedGoals            *float64
	ExpectedAssists          *float64
	ExpectedGoalInvolvements *float64
	ExpectedGoalsConceded    *float64

	ClearancesBlocksInterceptions *int
	Recoveries                    *int
	Tackles                       *int
	DefensiveContribution         *int
}
