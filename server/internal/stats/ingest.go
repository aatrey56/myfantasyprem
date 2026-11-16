package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"time"
	_ "time/tzdata" // Europe/London for FPL's matchdays and score lock

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// ErrUnmapped means live data referenced a player the store has never
// seen, or a fixture neither the store nor the live data itself knows:
// ingest bootstrap first.
var ErrUnmapped = errors.New("stats: live data references an unknown player or fixture")

// ErrIDTaken means another ingest mapped one of a batch's new upstream ids
// first (two pollers minting ids for the same new player at once). Nothing
// of the batch was applied: resolve the ids again and rebuild it.
var ErrIDTaken = errors.New("stats: an upstream id was mapped by another ingest")

// ErrIncomplete means a live snapshot is missing data the stored one has:
// no players although the gameweek has kicked off, or far fewer rows than
// are stored. It is rejected so bad data never zeroes a score (ADR 0005);
// it always comes with provider.ErrInvalid.
var ErrIncomplete = errors.New("stats: incomplete live snapshot")

// minRowsForDropCheck: below this many stored rows a gameweek is too
// young for a drop in row count to mean anything.
const minRowsForDropCheck = 10

// Ingester writes provider data into a Repo.
type Ingester struct {
	Repo Repo
	// Provider names the source in the id maps, for example "fpl".
	Provider string
	// Rules score every row. Season replaces them with the table upstream
	// publishes (bootstrap settings); StandardRules until then.
	Rules scoring.Rules
	Now   func() time.Time
	// Log hears a published table that differs from StandardRules; nil
	// means slog.Default().
	Log *slog.Logger
}

// NewIngester returns an Ingester with FPL's standard rules; Season
// replaces them with the table upstream publishes.
func NewIngester(repo Repo, providerName string, now func() time.Time) *Ingester {
	return &Ingester{Repo: repo, Provider: providerName, Rules: scoring.StandardRules(), Now: now}
}

// idMap resolves external ids of one entity, minting ours for new ones.
type idMap struct {
	entity, prefix string
	known          map[string]string
	added          []Mapping
}

func (in *Ingester) idMap(ctx context.Context, entity, prefix string) (*idMap, error) {
	known, err := in.Repo.ExternalIDs(ctx, in.Provider, entity)
	if err != nil {
		return nil, err
	}
	return &idMap{entity: entity, prefix: prefix, known: known}, nil
}

// resolve returns our id for ext, minting one (exact-key match, ADR 0006)
// if it is new.
func (m *idMap) resolve(ext string) string {
	if id, ok := m.known[ext]; ok {
		return id
	}
	id := ids.New(m.prefix)
	m.known[ext] = id
	m.added = append(m.added, Mapping{Entity: m.entity, ExternalID: ext, ID: id})
	return id
}

// Season applies bootstrap, fixtures and the gameweek status in one batch:
// clubs, players, gameweeks with their flags, fixtures and the current
// gameweek. It is idempotent. When another ingest maps a new upstream id
// first (ErrIDTaken) it starts over with that ingest's ids.
func (in *Ingester) Season(ctx context.Context, b *provider.Bootstrap, fixtures []provider.Fixture, st *provider.GameweekStatus) error {
	for attempt := 1; ; attempt++ {
		err := in.season(ctx, b, fixtures, st)
		if !errors.Is(err, ErrIDTaken) || attempt == 3 {
			return err
		}
	}
}

func (in *Ingester) season(ctx context.Context, b *provider.Bootstrap, fixtures []provider.Fixture, st *provider.GameweekStatus) error {
	clubs, err := in.idMap(ctx, "club", "clb")
	if err != nil {
		return err
	}
	players, err := in.idMap(ctx, "player", "plr")
	if err != nil {
		return err
	}
	fxs, err := in.idMap(ctx, "fixture", "fx")
	if err != nil {
		return err
	}
	in.useRules(ctx, RulesFromTable(b.Settings.Scoring))
	batch := Batch{Provider: in.Provider}
	for _, c := range b.Clubs {
		batch.Clubs = append(batch.Clubs, Club{ID: clubs.resolve(c.ExternalID), Code: c.ShortName, Name: c.Name})
	}
	ranked := rankFallback(b.Players)
	for _, p := range b.Players {
		club, ok := clubs.known[p.ClubExternalID]
		if !ok {
			return fmt.Errorf("stats: player %s: unknown club %s", p.ExternalID, p.ClubExternalID)
		}
		batch.Players = append(batch.Players, Player{
			ID:           players.resolve(p.ExternalID),
			FirstName:    p.FirstName,
			LastName:     p.LastName,
			DisplayName:  p.DisplayName,
			Position:     scoring.Position(p.Position),
			ClubID:       club,
			Availability: string(p.Availability),
			News:         p.News,
			NewsAt:       utc(p.NewsAt),
			ChanceNext:   p.ChanceNext,
			DraftRank:    ranked[p.ExternalID],
		})
	}
	for _, g := range b.Gameweeks {
		gw := Gameweek{
			Number: g.Number, Name: g.Name,
			DeadlineAt: g.DeadlineAt, WaiverDeadlineAt: g.WaiverDeadlineAt, TradeDeadlineAt: g.TradeDeadlineAt,
			Finished: g.Finished, DataChecked: g.DataChecked,
			// A finished gameweek has its bonus; the current one says so itself.
			BonusAdded: g.Finished || g.DataChecked,
		}
		if st != nil && g.Number == st.Current {
			gw.BonusAdded = gw.BonusAdded || st.BonusAdded
			gw.DataChecked = gw.DataChecked || st.DataChecked
		}
		batch.Gameweeks = append(batch.Gameweeks, gw)
	}
	for _, f := range fixtures {
		fx, err := in.fixture(f, clubs, fxs)
		if err != nil {
			return err
		}
		batch.Fixtures = append(batch.Fixtures, fx)
	}
	if st != nil {
		s := Status{Current: st.Current, WaiversProcessed: st.WaiversProcessed}
		if st.Next != nil {
			s.Next = *st.Next
		}
		batch.Status = &s
	}
	batch.Mappings = slices.Concat(clubs.added, players.added, fxs.added)
	if len(b.Payload) > 0 {
		batch.Snapshots = append(batch.Snapshots, Snapshot{Kind: "bootstrap", FetchedAt: in.Now().UTC(), Payload: b.Payload})
	}
	return in.Repo.Apply(ctx, batch)
}

// RulesFromTable maps upstream's published scoring table to scoring
// rules. Upstream gives goals conceded per 2 and saves per 3; the
// appearance threshold (60 minutes) and the clean-sheet minimum are fixed
// by the rules, not the table.
func RulesFromTable(t provider.ScoringTable) scoring.Rules {
	pp := func(v provider.PositionPoints) map[scoring.Position]int {
		return map[scoring.Position]int{scoring.GKP: v.GKP, scoring.DEF: v.DEF, scoring.MID: v.MID, scoring.FWD: v.FWD}
	}
	r := scoring.StandardRules()
	r.Goal = pp(t.Goal)
	r.Assist = t.Assist
	r.CleanSheet = pp(t.CleanSheet)
	r.ConcededPenalty = pp(t.GoalsConcededPerTwo)
	r.SavesPoints = t.SavesPerThree
	r.PenaltySaved = t.PenaltySave
	r.PenaltyMissed = t.PenaltyMiss
	r.YellowCard = t.YellowCard
	r.RedCard = t.RedCard
	r.OwnGoal = t.OwnGoal
	r.ShortAppearance = t.MinutesUnder60
	r.FullAppearance = t.Minutes60Plus
	r.DefensiveThreshold = pp(t.DefensiveContributionThreshold)
	r.DefensiveContribution = t.DefensiveContribution
	return r
}

// useRules switches to a published table, warning when it is not FPL's
// standard 2026-27 one (the scoring docs and golden tests assume it). An
// empty table (a provider without one) keeps the current rules.
func (in *Ingester) useRules(ctx context.Context, r scoring.Rules) {
	if r.FullAppearance == 0 && r.Assist == 0 {
		return
	}
	if !reflect.DeepEqual(r, scoring.StandardRules()) && !reflect.DeepEqual(r, in.Rules) {
		log := in.Log
		if log == nil {
			log = slog.Default()
		}
		log.WarnContext(ctx, "stats: upstream scoring table differs from the standard 2026-27 rules; using upstream's",
			"provider", in.Provider, "rules", fmt.Sprintf("%+v", r))
	}
	in.Rules = r
}

// Status stores a gameweek status between bootstrap refreshes: the season
// position and the current gameweek's bonus and checked flags.
func (in *Ingester) Status(ctx context.Context, st *provider.GameweekStatus) error {
	gws, err := in.Repo.Gameweeks(ctx)
	if err != nil {
		return err
	}
	s := Status{Current: st.Current, WaiversProcessed: st.WaiversProcessed}
	if st.Next != nil {
		s.Next = *st.Next
	}
	batch := Batch{Provider: in.Provider, Status: &s}
	for _, g := range gws {
		if g.Number == st.Current {
			g.BonusAdded = g.Finished || st.BonusAdded
			g.DataChecked = g.DataChecked || st.DataChecked
			batch.Gameweeks = append(batch.Gameweeks, g)
		}
	}
	return in.Repo.Apply(ctx, batch)
}

// rankFallback gives every player a draft rank >= 1: upstream's when it
// has one, otherwise after every ranked player in id order.
func rankFallback(ps []provider.Player) map[string]int {
	out := map[string]int{}
	maxRank := 0
	for _, p := range ps {
		maxRank = max(maxRank, p.DraftRank)
	}
	next := maxRank + 1
	for _, p := range ps {
		if p.DraftRank > 0 {
			out[p.ExternalID] = p.DraftRank
		} else {
			out[p.ExternalID] = next
			next++
		}
	}
	return out
}

func (in *Ingester) fixture(f provider.Fixture, clubs, fxs *idMap) (Fixture, error) {
	home, okH := clubs.known[f.HomeClubExternalID]
	away, okA := clubs.known[f.AwayClubExternalID]
	if !okH || !okA {
		return Fixture{}, fmt.Errorf("stats: fixture %s: unknown club", f.ExternalID)
	}
	return Fixture{
		ID: fxs.resolve(f.ExternalID), Gameweek: f.Gameweek, HomeClubID: home, AwayClubID: away,
		KickoffAt: f.KickoffAt, Status: string(f.Status), HomeScore: f.HomeScore, AwayScore: f.AwayScore,
	}, nil
}

// Live applies one gameweek's live stats: the fixtures' scores and status
// and every player-fixture row, scored from scratch. It returns the ids of
// players whose rows changed, plus every player of a club whose fixture
// changed status (so the caller rescores only what moved, including
// auto-subs that a finished match unlocks). A repeated poll with the same
// data changes nothing.
//
// The live payload's fixtures are stored as well as updated: upstream's
// bootstrap lists only upcoming fixtures, so a past gameweek's are known
// only from its live data. A fixture seen first here gets its id minted;
// when another ingest maps it first (ErrIDTaken) Live starts over with
// that ingest's ids.
func (in *Ingester) Live(ctx context.Context, live *provider.EventLive, st *provider.GameweekStatus) ([]string, error) {
	for attempt := 1; ; attempt++ {
		changed, err := in.live(ctx, live, st)
		if !errors.Is(err, ErrIDTaken) || attempt == 3 {
			return changed, err
		}
	}
}

func (in *Ingester) live(ctx context.Context, live *provider.EventLive, st *provider.GameweekStatus) ([]string, error) {
	clubs, err := in.idMap(ctx, "club", "clb")
	if err != nil {
		return nil, err
	}
	fxs, err := in.idMap(ctx, "fixture", "fx")
	if err != nil {
		return nil, err
	}
	playerIDs, err := in.Repo.ExternalIDs(ctx, in.Provider, "player")
	if err != nil {
		return nil, err
	}
	allPlayers, err := in.Repo.Players(ctx)
	if err != nil {
		return nil, err
	}
	allFixtures, err := in.Repo.Fixtures(ctx)
	if err != nil {
		return nil, err
	}

	// Fixtures of this gameweek: stored ones, updated by the live payload.
	byID := map[string]Fixture{}
	for _, f := range allFixtures {
		if f.Gameweek == live.Gameweek {
			byID[f.ID] = f
		}
	}
	var updated []Fixture
	for _, f := range live.Fixtures {
		fx, err := in.fixture(f, clubs, fxs)
		if err != nil {
			return nil, err
		}
		byID[fx.ID] = fx
		updated = append(updated, fx)
	}
	clubFixtures := map[string][]Fixture{}
	for _, f := range byID {
		if f.Status == FixturePostponed {
			continue
		}
		clubFixtures[f.HomeClubID] = append(clubFixtures[f.HomeClubID], f)
		clubFixtures[f.AwayClubID] = append(clubFixtures[f.AwayClubID], f)
	}
	position := map[string]scoring.Position{}
	clubOf := map[string]string{}
	for _, p := range allPlayers {
		position[p.ID] = p.Position
		clubOf[p.ID] = p.ClubID
	}

	bonusConfirmed := st != nil && st.Current == live.Gameweek && st.BonusAdded
	if st != nil && st.Current != live.Gameweek {
		st = nil // its per-day flags are about another gameweek
	}
	var rows []PlayerFixture
	gameweekStats := make([]PlayerGameweek, 0, len(live.Players))
	for _, pl := range live.Players {
		pid, ok := playerIDs[pl.PlayerExternalID]
		if !ok {
			return nil, fmt.Errorf("%w: player %s", ErrUnmapped, pl.PlayerExternalID)
		}
		gameweekStats = append(gameweekStats, playerGameweek(pid, live.Gameweek, pl))
		split, err := splitByFixture(pl, fxs.known, clubFixtures[clubOf[pid]])
		if err != nil {
			return nil, err
		}
		for fid, s := range split {
			st := s.stats
			st.Bonus = s.upstreamBonus
			rows = append(rows, PlayerFixture{PlayerID: pid, FixtureID: fid, Gameweek: live.Gameweek, Stats: st,
				BPS: s.bps, BPSUnknown: s.bpsUnknown})
		}
	}
	in.awardBonus(rows, byID, bonusConfirmed, st)
	for i := range rows {
		rows[i].Points, rows[i].Breakdown = in.Rules.Points(position[rows[i].PlayerID], rows[i].Stats)
		if rows[i].Breakdown == nil {
			rows[i].Breakdown = []scoring.Line{}
		}
	}
	slices.SortFunc(rows, func(a, b PlayerFixture) int {
		if a.PlayerID != b.PlayerID {
			return compareStrings(a.PlayerID, b.PlayerID)
		}
		return compareStrings(a.FixtureID, b.FixtureID)
	})

	before, err := in.Repo.PlayerFixtures(ctx, live.Gameweek)
	if err != nil {
		return nil, err
	}
	if err := complete(live, byID, before, rows); err != nil {
		return nil, err
	}
	changed := changedPlayers(before, rows)
	// A fixture whose status moved touches every player of both clubs,
	// even those with no row: a 0-minute starter becomes auto-sub
	// eligible when his match ends.
	stored := map[string]string{}
	for _, f := range allFixtures {
		stored[f.ID] = f.Status
	}
	moved := map[string]bool{}
	for _, f := range updated {
		if stored[f.ID] != f.Status {
			moved[f.HomeClubID], moved[f.AwayClubID] = true, true
		}
	}
	if len(moved) > 0 {
		for _, p := range allPlayers {
			if moved[p.ClubID] && !slices.Contains(changed, p.ID) {
				changed = append(changed, p.ID)
			}
		}
		slices.Sort(changed)
	}

	batch := Batch{
		Provider: in.Provider,
		Fixtures: updated,
		Live:     &LiveRows{Gameweek: live.Gameweek, Rows: rows, Players: gameweekStats},
		Mappings: fxs.added,
	}
	if len(changed) > 0 {
		// Durable until the caller's rescore succeeds.
		batch.Rescore = []int{live.Gameweek}
	}
	if len(live.Payload) > 0 {
		batch.Snapshots = []Snapshot{{Kind: "live", Gameweek: live.Gameweek, FetchedAt: live.FetchedAt, Payload: live.Payload}}
	}
	if err := in.Repo.Apply(ctx, batch); err != nil {
		return nil, err
	}
	return changed, nil
}

// playerGameweek is a player's gameweek line as upstream gives it.
func playerGameweek(playerID string, gw int, pl provider.PlayerLive) PlayerGameweek {
	return PlayerGameweek{
		PlayerID: playerID, Gameweek: gw,
		Minutes: pl.Minutes, GoalsScored: pl.GoalsScored, Assists: pl.Assists, CleanSheets: pl.CleanSheets,
		GoalsConceded: pl.GoalsConceded, OwnGoals: pl.OwnGoals, PenaltiesSaved: pl.PenaltiesSaved,
		PenaltiesMissed: pl.PenaltiesMissed, YellowCards: pl.YellowCards, RedCards: pl.RedCards, Saves: pl.Saves,
		Bonus: pl.Bonus, BPS: pl.BPS,
		ClearancesBlocksInterceptions: pl.ClearancesBlocksInterceptions, Tackles: pl.Tackles, Recoveries: pl.Recoveries,
		DefensiveContribution: pl.DefensiveContribution,
		Influence:             pl.Influence, Creativity: pl.Creativity, Threat: pl.Threat, ICTIndex: pl.ICTIndex,
		ExpectedGoals: pl.ExpectedGoals, ExpectedAssists: pl.ExpectedAssists,
		ExpectedGoalInvolvements: pl.ExpectedGoalInvolvements, ExpectedGoalsConceded: pl.ExpectedGoalsConceded,
		Starts: pl.Starts,
	}
}

// complete rejects a snapshot that would wipe out stored data: no players
// once any fixture of the gameweek has started, or fewer than half the
// stored rows (upstream lists every player once a gameweek is under way,
// and rows only disappear one at a time, through corrections).
func complete(live *provider.EventLive, fixtures map[string]Fixture, before, rows []PlayerFixture) error {
	started := false
	for _, f := range fixtures {
		started = started || f.Started()
	}
	switch {
	case started && len(live.Players) == 0:
		return fmt.Errorf("%w: %w: gameweek %d has started but lists no players",
			ErrIncomplete, provider.ErrInvalid, live.Gameweek)
	case len(before) >= minRowsForDropCheck && 2*len(rows) < len(before):
		return fmt.Errorf("%w: %w: gameweek %d has %d rows, %d stored",
			ErrIncomplete, provider.ErrInvalid, live.Gameweek, len(rows), len(before))
	}
	return nil
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// changedPlayers lists players whose rows differ between before and after.
func changedPlayers(before, after []PlayerFixture) []string {
	key := func(r PlayerFixture) [2]string { return [2]string{r.PlayerID, r.FixtureID} }
	old := map[[2]string]PlayerFixture{}
	for _, r := range before {
		old[key(r)] = r
	}
	seen := map[string]bool{}
	var out []string
	mark := func(id string) {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	for _, r := range after {
		o, ok := old[key(r)]
		delete(old, key(r))
		if !ok || !sameRow(o, r) {
			mark(r.PlayerID)
		}
	}
	for _, r := range old {
		mark(r.PlayerID)
	}
	slices.Sort(out)
	return out
}

type fixtureStats struct {
	stats scoring.FixtureStats
	bps   int
	// bpsUnknown: upstream has no BPS for this fixture.
	bpsUnknown bool
	// upstreamBonus is the bonus upstream reports for this fixture.
	upstreamBonus int
}

// splitByFixture turns a player's gameweek line into per-fixture stats.
//
//   - One fixture (the usual case): the gameweek totals belong to it.
//   - A double gameweek: upstream's explain breakdown splits the scoring
//     stats per fixture, and the fixture's own stats give the BPS in it.
//     A fixture without one is 0 if he did not appear in it, the rest of
//     the gameweek BPS if it is the only one left unknown, and otherwise
//     unknown (a card-only appearance has BPS of its own), which
//     BonusFromBPS never ranks.
//   - No explain, no minutes and no card: the player did not play; no
//     row. A card without minutes (booked on the bench) is playing in
//     FPL's sense and keeps its row (scoring.FixtureStats.Played).
func splitByFixture(pl provider.PlayerLive, fixtureIDs map[string]string, clubFixtures []Fixture) (map[string]fixtureStats, error) {
	out := map[string]fixtureStats{}
	agg := scoring.FixtureStats{
		Minutes: pl.Minutes, Goals: pl.GoalsScored, Assists: pl.Assists, CleanSheet: pl.CleanSheets > 0,
		GoalsConceded: pl.GoalsConceded, OwnGoals: pl.OwnGoals, PenaltiesSaved: pl.PenaltiesSaved,
		PenaltiesMissed: pl.PenaltiesMissed, YellowCards: pl.YellowCards, RedCards: pl.RedCards, Saves: pl.Saves,
		ClearancesBlocksInterceptions: pl.ClearancesBlocksInterceptions, Tackles: pl.Tackles, Recoveries: pl.Recoveries,
	}
	switch {
	case len(pl.Explain) == 1:
		fid, ok := fixtureIDs[pl.Explain[0].FixtureExternalID]
		if !ok {
			return nil, fmt.Errorf("%w: fixture %s", ErrUnmapped, pl.Explain[0].FixtureExternalID)
		}
		out[fid] = fixtureStats{stats: agg, bps: pl.BPS, upstreamBonus: pl.Bonus}
	case len(pl.Explain) > 1:
		known := 0 // sum of the known fixture BPS
		var unknown []string
		for _, fe := range pl.Explain {
			fid, ok := fixtureIDs[fe.FixtureExternalID]
			if !ok {
				return nil, fmt.Errorf("%w: fixture %s", ErrUnmapped, fe.FixtureExternalID)
			}
			fs := fixtureStats{stats: fromExplain(fe.Lines)}
			fs.upstreamBonus = fs.stats.Bonus
			fs.stats.Bonus = 0
			switch {
			case fe.BPS != nil:
				fs.bps = *fe.BPS
				known += fs.bps
			case !fs.stats.Played():
				// No appearance, no BPS. A card-only appearance has BPS
				// of its own, so it stays unknown.
			default:
				fs.bpsUnknown = true
				unknown = append(unknown, fid)
			}
			out[fid] = fs
		}
		// The gameweek BPS is the sum over its fixtures, so one missing
		// value is the rest of the total.
		if len(unknown) == 1 {
			fs := out[unknown[0]]
			fs.bps, fs.bpsUnknown = pl.BPS-known, false
			out[unknown[0]] = fs
		}
	case agg.Played():
		var candidates []Fixture
		for _, f := range clubFixtures {
			if f.Started() {
				candidates = append(candidates, f)
			}
		}
		if len(candidates) != 1 {
			return nil, fmt.Errorf("stats: player %s played (%d minutes) without a breakdown and %d possible fixtures",
				pl.PlayerExternalID, pl.Minutes, len(candidates))
		}
		out[candidates[0].ID] = fixtureStats{stats: agg, bps: pl.BPS, upstreamBonus: pl.Bonus}
	}
	for fid, fs := range out {
		if !fs.stats.Played() && fs.upstreamBonus == 0 {
			delete(out, fid)
		}
	}
	return out, nil
}

// fromExplain rebuilds the scoring stats of one fixture from upstream's
// breakdown. Lines that score nothing are not in it, so a value that
// cannot change the points (say, one goal conceded) may be missing; the
// points are exact.
func fromExplain(lines []provider.ExplainLine) scoring.FixtureStats {
	var s scoring.FixtureStats
	for _, l := range lines {
		switch l.Stat {
		case provider.StatMinutes:
			s.Minutes = l.Value
		case provider.StatGoalsScored:
			s.Goals = l.Value
		case provider.StatAssists:
			s.Assists = l.Value
		case provider.StatCleanSheets:
			s.CleanSheet = l.Value > 0
		case provider.StatGoalsConceded:
			s.GoalsConceded = l.Value
		case provider.StatOwnGoals:
			s.OwnGoals = l.Value
		case provider.StatPenaltiesSaved:
			s.PenaltiesSaved = l.Value
		case provider.StatPenaltiesMissed:
			s.PenaltiesMissed = l.Value
		case provider.StatYellowCards:
			s.YellowCards = l.Value
		case provider.StatRedCards:
			s.RedCards = l.Value
		case provider.StatSaves:
			s.Saves = l.Value
		case provider.StatDefensiveContribution:
			// The value is the action count that reached the threshold.
			s.ClearancesBlocksInterceptions = l.Value
		case provider.StatBonus:
			s.Bonus = l.Value
		}
	}
	return s
}

// ProjectBonusAfter is how far into a match FPL starts showing projected
// bonus (2026-27: "after 20 minutes of each fixture"). The match minute is
// taken as the most minutes any player has in it.
const ProjectBonusAfter = 20

// awardBonus sets each row's bonus. A fixture's bonus is confirmed when
// the gameweek's bonus is added, when FPL marks the fixture finished (it
// does once that match's bonus is in), or when the match is over and
// event-status says its matchday's bonus is added. A bonus value upstream
// is not confirmation: from 2026-27 FPL shows its own projection in live
// points and revises it until confirmation. Otherwise the bonus is
// projected from BPS with FPL's tie rules among the players who played,
// from ProjectBonusAfter minutes into the match, and marked provisional.
// When a player who played has no known BPS in the match, the ranking
// would be incomplete, so the match keeps upstream's projection instead
// (still provisional).
func (in *Ingester) awardBonus(rows []PlayerFixture, fixtures map[string]Fixture, gameweekConfirmed bool, st *provider.GameweekStatus) {
	byFixture := map[string][]int{}
	for i, r := range rows {
		byFixture[r.FixtureID] = append(byFixture[r.FixtureID], i)
	}
	for fid, idx := range byFixture {
		f := fixtures[fid]
		if gameweekConfirmed || f.Status == FixtureFinished || (f.Over() && st != nil && st.BonusAddedOn[MatchDay(f)]) {
			continue // confirmed: keep upstream's bonus
		}
		if !f.Started() {
			continue // nothing to project
		}
		minute := 0
		for _, i := range idx {
			minute = max(minute, rows[i].Stats.Minutes)
		}
		var scores []scoring.BPS
		complete := true
		for _, i := range idx {
			if rows[i].Stats.Minutes > 0 {
				scores = append(scores, scoring.BPS{PlayerID: rows[i].PlayerID, Score: rows[i].BPS})
				complete = complete && !rows[i].BPSUnknown
			}
		}
		for _, i := range idx {
			rows[i].BonusProvisional = true
		}
		if !complete && minute >= ProjectBonusAfter {
			// A player's BPS in this match is unknown (a double without
			// per-fixture data), so no ranking of it is right: keep
			// upstream's projection for every row, provisional.
			continue
		}
		var projected map[string]int
		if minute >= ProjectBonusAfter {
			projected = scoring.BonusFromBPS(scores)
		}
		for _, i := range idx {
			rows[i].Stats.Bonus = projected[rows[i].PlayerID]
		}
	}
}

// london is FPL's time zone: event-status dates, the 09:00 lock.
var london = func() *time.Location {
	loc, err := time.LoadLocation("Europe/London")
	if err != nil {
		panic(err) // time/tzdata is embedded
	}
	return loc
}()

// London returns t in UK time.
func London(t time.Time) time.Time { return t.In(london) }

// ScoreLock is when FPL locks a gameweek's scores (2026-27): 09:00 UK
// time on the day after its last match. ok is false without a kickoff.
func ScoreLock(fixtures []Fixture) (lock time.Time, ok bool) {
	var last time.Time
	for _, f := range fixtures {
		if f.KickoffAt != nil && f.KickoffAt.After(last) {
			last = *f.KickoffAt
		}
	}
	if last.IsZero() {
		return time.Time{}, false
	}
	d := London(last)
	return time.Date(d.Year(), d.Month(), d.Day()+1, 9, 0, 0, 0, london), true
}

// MatchDay is the UK date of a fixture's kickoff ("2006-01-02"), the key
// of FPL's per-day bonus flags; empty without a kickoff.
func MatchDay(f Fixture) string {
	if f.KickoffAt == nil {
		return ""
	}
	return London(*f.KickoffAt).Format("2006-01-02")
}
