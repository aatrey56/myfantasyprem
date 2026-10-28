package draft

import (
	"cmp"
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Pick clock limits (DraftUpdate.pickTimerSeconds in the contract).
const (
	MinPickTimer = 15
	MaxPickTimer = 86400
)

// MinManagers is how many teams need a manager before the draft can
// start; every other place becomes an open seat.
const MinManagers = 2

// ClockArgs is the job that runs when a pick clock runs out. Deadline is
// part of the arguments so a resumed clock gets its own (unique) job.
type ClockArgs struct {
	LeagueID string    `json:"league_id"`
	Overall  int       `json:"overall"`
	Deadline time.Time `json:"deadline"`
}

// Kind names the job.
func (ClockArgs) Kind() string { return "draft_clock" }

var (
	errDraftStarted  = league.Errorf(league.KindConflict, "draft-started", "Draft started", "The draft has started.")
	errDraftComplete = league.Errorf(league.KindConflict, "draft-complete", "Draft complete", "This league has already drafted.")
	errNotLive       = league.Errorf(league.KindConflict, "draft-not-live", "Draft not live", "The draft is not running.")
	errNotPaused     = league.Errorf(league.KindConflict, "draft-not-paused", "Draft not paused", "The draft is not paused.")
	errNoTeam        = league.Errorf(league.KindForbidden, "not-a-member", "Not a member", "You have no team in this league.")
	errNoGameweek    = league.Errorf(league.KindConflict, "season-over", "Season over",
		"No gameweek is left for this league to play: the deadline of its last gameweek has passed.")
)

// firstGW is the gameweek a draft finishing at now starts the league in
// (ADR 0010): the later of startGameweek and the first gameweek whose
// deadline has not passed. ok is false when that is past the league's
// last gameweek, so there is nothing left to play.
func firstGW(l *league.League, w *league.World, now time.Time) (gw int, ok bool) {
	gw = max(l.Settings.StartGameweek, w.UpcomingGW(now))
	return gw, gw <= min(l.Settings.EndGameweek, 38)
}

// Service runs drafts. It is safe for concurrent use.
type Service struct {
	repo   league.Repo
	stats  stats.Repo
	now    func() time.Time
	notify league.Notifier
	rules  scoring.LineupRules
	// shuffle permutes n items (randomised order); tests replace it.
	shuffle func(n int, swap func(i, j int))
}

// New returns a Service. notify may be nil.
func New(repo league.Repo, st stats.Repo, now func() time.Time, notify league.Notifier) *Service {
	if notify == nil {
		notify = league.NopNotifier{}
	}
	return &Service{
		repo: repo, stats: st, notify: notify, rules: scoring.StandardLineup(), shuffle: rand.Shuffle,
		now: func() time.Time { return now().UTC().Truncate(time.Microsecond) },
	}
}

// Rounds is the number of rounds: one per squad place.
func (s *Service) Rounds() int { return s.rules.SquadSize() }

// View is a draft as one member sees it.
type View struct {
	League *league.League
	Draft  *league.Draft
	World  *league.World
	Rounds int
}

// Total is the number of picks in the draft.
func (v *View) Total() int { return len(v.Draft.Order) * v.Rounds }

// Slot returns the team that makes overall pick n (1-based) in a snake
// draft over order, the round and the pick within the round.
func Slot(order []string, n int) (team string, round, inRound int) {
	t := len(order)
	if t == 0 || n < 1 {
		return "", 0, 0
	}
	round = (n-1)/t + 1
	inRound = (n-1)%t + 1
	i := inRound - 1
	if round%2 == 0 {
		i = t - 1 - i
	}
	return order[i], round, inRound
}

// Clock is the pick on the clock.
type Clock struct {
	Overall, Round, InRound int
	TeamID                  string
	StartedAt               time.Time
	// DeadlineAt is nil while the draft is paused.
	DeadlineAt *time.Time
}

// ClockAt is the clock as it stood for overall pick n: started when pick
// n-1 was made (or the draft started), running for the pick timer. It is
// nil past the last pick or before the draft started.
func ClockAt(d *league.Draft, rounds, n int) *Clock {
	if d.ClockStartedAt == nil || n > len(d.Order)*rounds || n < 1 || n > len(d.Picks)+1 {
		return nil
	}
	team, round, in := Slot(d.Order, n)
	c := &Clock{Overall: n, Round: round, InRound: in, TeamID: team, StartedAt: *d.ClockStartedAt}
	if n <= len(d.Picks) && n >= 2 {
		c.StartedAt = d.Picks[n-2].PickedAt
	}
	if n == len(d.Picks)+1 && d.Status == league.DraftPaused {
		return c
	}
	if n == len(d.Picks)+1 && d.Status != league.DraftInProgress {
		return nil
	}
	dl := c.StartedAt.Add(time.Duration(d.PickTimerS) * time.Second)
	c.DeadlineAt = &dl
	return c
}

// Current is the pick on the clock, or nil.
func (v *View) Current() *Clock {
	if v.Draft.Status != league.DraftInProgress && v.Draft.Status != league.DraftPaused {
		return nil
	}
	return ClockAt(v.Draft, v.Rounds, len(v.Draft.Picks)+1)
}

// deadline of the pick on the clock.
func deadline(d *league.Draft) time.Time {
	return d.ClockStartedAt.Add(time.Duration(d.PickTimerS) * time.Second)
}

// ensure creates the draft row the first time a league's draft is touched:
// scheduled before the draft, complete for a league that is already in
// season without one (seeded leagues).
func ensure(st *league.State, now time.Time) {
	if st.Draft != nil {
		return
	}
	d := &league.Draft{
		ID: ids.New("drf"), Status: league.DraftScheduled, PickTimerS: st.League.Settings.PickTimerSeconds,
		Autopick: map[string]bool{}, StayManual: map[string]bool{}, Queues: map[string][]string{},
	}
	if st.League.Status == league.StatusInSeason || st.League.Status == league.StatusComplete {
		d.Status = league.DraftComplete
		for _, t := range st.League.Teams {
			d.Order = append(d.Order, t.ID)
		}
		at := st.League.CreatedAt
		d.CompletedAt = &at
	}
	st.Draft = d
}

// change runs fn on the league's State in one transaction, then reports
// the changes. The draft row is created first, in its own transaction, so
// its id is stable even when fn fails.
func (s *Service) change(ctx context.Context, leagueID string,
	fn func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error,
) (*View, error) {
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return nil, err
	}
	if err := s.ensureDraft(ctx, leagueID); err != nil {
		return nil, err
	}
	var (
		out     *league.State
		changes []league.Change
	)
	err = s.repo.Tx(ctx, func(tx league.Tx) error {
		changes = nil
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return league.ErrLeagueNotFound
		}
		ensure(st, s.now())
		if err := fn(tx, st, w, s.now(), &changes); err != nil {
			return err
		}
		if err := tx.SaveState(ctx, st); err != nil {
			return err
		}
		out = st
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.notify.Notify(ctx, changes...)
	return &View{League: out.League, Draft: out.Draft, World: w, Rounds: s.Rounds()}, nil
}

// ensureDraft stores the league's draft row if it has none yet.
func (s *Service) ensureDraft(ctx context.Context, leagueID string) error {
	st, err := s.repo.State(ctx, leagueID)
	if err != nil {
		return err
	}
	if st == nil {
		return league.ErrLeagueNotFound
	}
	if st.Draft != nil {
		return nil
	}
	return s.repo.Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil || st == nil || st.Draft != nil {
			return err
		}
		ensure(st, s.now())
		return tx.SaveState(ctx, st)
	})
}

func notDrafting(l *league.League) error {
	if l.Status == league.StatusInSeason || l.Status == league.StatusComplete {
		return errDraftComplete
	}
	return nil
}

// Get returns the draft. A league seen for the first time gets its draft
// row, so the id is stable from the first read.
func (s *Service) Get(ctx context.Context, actor, leagueID string) (*View, error) {
	st, err := s.repo.State(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, league.ErrLeagueNotFound
	}
	if err := league.Member(st.League, actor); err != nil {
		return nil, err
	}
	if st.Draft != nil {
		w, err := league.SeasonWorld(ctx, s.stats)
		if err != nil {
			return nil, err
		}
		return &View{League: st.League, Draft: st.Draft, World: w, Rounds: s.Rounds()}, nil
	}
	return s.change(ctx, leagueID, func(league.Tx, *league.State, *league.World, time.Time, *[]league.Change) error { return nil })
}

// Update is a commissioner's change before the draft starts.
type Update struct {
	StartsAt      *time.Time
	ClearStartsAt bool
	PickTimer     *int
	Order         []string
	Randomize     bool
}

// UpdateDraft schedules or orders the draft (commissioner, before it starts).
func (s *Service) UpdateDraft(ctx context.Context, actor, leagueID string, in Update) (*View, error) {
	return s.change(ctx, leagueID, func(_ league.Tx, st *league.State, _ *league.World, _ time.Time, ch *[]league.Change) error {
		if err := league.Commissioner(st.League, actor); err != nil {
			return err
		}
		if err := notDrafting(st.League); err != nil {
			return err
		}
		if st.Draft.Status != league.DraftScheduled {
			return errDraftStarted
		}
		var errs []league.FieldError
		if in.Order != nil && in.Randomize {
			errs = append(errs, league.FieldError{Code: "order_and_randomize", Message: "Send order or randomizeOrder, not both.", Field: "/order"})
		}
		if in.PickTimer != nil && (*in.PickTimer < MinPickTimer || *in.PickTimer > MaxPickTimer) {
			errs = append(errs, league.FieldError{Code: "out_of_range", Message: "pickTimerSeconds must be 15 to 86400.", Field: "/pickTimerSeconds"})
		}
		if in.Order != nil && !sameTeams(in.Order, st.League.Teams) {
			errs = append(errs, league.FieldError{Code: "invalid_order", Message: "order must list every team once.", Field: "/order"})
		}
		if len(errs) > 0 {
			return league.Errorf(league.KindInvalid, "invalid-request", "Invalid request", errs[0].Message, errs...)
		}
		if in.Order != nil {
			st.Draft.Order = slices.Clone(in.Order)
		}
		if in.Randomize {
			order := teamIDs(st.League)
			s.shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
			st.Draft.Order = order
		}
		if in.PickTimer != nil {
			st.League.Settings.PickTimerSeconds = *in.PickTimer
			st.Draft.PickTimerS = *in.PickTimer
		}
		if in.ClearStartsAt {
			st.League.Settings.DraftStartsAt = nil
		} else if in.StartsAt != nil {
			t := in.StartsAt.UTC()
			st.League.Settings.DraftStartsAt = &t
		}
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeDraft})
		return nil
	})
}

func teamIDs(l *league.League) []string {
	out := make([]string, 0, len(l.Teams))
	for _, t := range l.Teams {
		out = append(out, t.ID)
	}
	return out
}

func sameTeams(order []string, teams []league.Team) bool {
	if len(order) != len(teams) {
		return false
	}
	seen := map[string]bool{}
	for _, id := range order {
		if seen[id] || !slices.ContainsFunc(teams, func(t league.Team) bool { return t.ID == id }) {
			return false
		}
		seen[id] = true
	}
	return true
}

// Start starts the draft now (commissioner).
func (s *Service) Start(ctx context.Context, actor, leagueID string) (*View, error) {
	return s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		if err := league.Commissioner(st.League, actor); err != nil {
			return err
		}
		return s.start(ctx, tx, st, w, now, ch)
	})
}

func (s *Service) start(ctx context.Context, tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
	if err := notDrafting(st.League); err != nil {
		return err
	}
	if st.Draft.Status != league.DraftScheduled {
		return errDraftStarted
	}
	l := st.League
	if err := enoughManagers(l); err != nil {
		return err
	}
	if _, ok := firstGW(l, w, now); !ok {
		return errNoGameweek
	}
	openSeats(l, now)
	// The order: the one set, with teams that joined since added at the
	// end; otherwise join order.
	order := slices.DeleteFunc(slices.Clone(st.Draft.Order), func(id string) bool { _, ok := l.Team(id); return !ok })
	for _, id := range teamIDs(l) {
		if !slices.Contains(order, id) {
			order = append(order, id)
		}
	}
	d := st.Draft
	d.Order, d.Status, d.PickTimerS, d.PausedAt = order, league.DraftInProgress, l.Settings.PickTimerSeconds, nil
	d.ClockStartedAt = &now
	l.Status = league.StatusDrafting
	*ch = append(*ch, league.Change{LeagueID: l.ID, Kind: league.ChangeDraft}, league.Change{LeagueID: l.ID, Kind: league.ChangeLeague})
	return s.advance(ctx, tx, st, w, now, ch)
}

// enoughManagers refuses a league with fewer than MinManagers teams that
// have a manager.
func enoughManagers(l *league.League) error {
	if l.MemberCount() >= MinManagers {
		return nil
	}
	return league.Errorf(league.KindConflict, "not-enough-managers", "Not enough managers",
		fmt.Sprintf("%d of %d teams have a manager: at least %d are needed to start the draft.",
			l.MemberCount(), l.Settings.TeamCount, MinManagers))
}

// openSeats fills every empty team place with an open seat: a team named
// "Open seat N" without a manager. It drafts on autopick without waiting
// for the clock (advance) and whoever joins later takes it over
// (league.AcceptInvite).
func openSeats(l *league.League, now time.Time) {
	for n := len(l.Teams) + 1; n <= l.Settings.TeamCount; n++ {
		l.Teams = append(l.Teams, league.Team{ID: ids.New("tm"), Name: fmt.Sprintf("Open seat %d", n),
			JoinedAt: now.Add(time.Duration(n) * time.Microsecond)})
	}
}

// StartIfDue starts a scheduled draft whose start time has passed and whose
// league has enough managers (the tick job). It reports whether it started one.
func (s *Service) StartIfDue(ctx context.Context, leagueID string) (bool, error) {
	st, err := s.repo.State(ctx, leagueID)
	if err != nil || st == nil {
		return false, err
	}
	if !startDue(st.League, s.now()) {
		return false, nil
	}
	started := false
	_, err = s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		// Again under the lock: the start may have been postponed or
		// cleared, or a member left, since the read above.
		if !startDue(st.League, now) || st.Draft.Status != league.DraftScheduled {
			return nil
		}
		if _, ok := firstGW(st.League, w, now); !ok {
			return nil // too late to play; the commissioner sees why on Start
		}
		started = true
		return s.start(ctx, tx, st, w, now, ch)
	})
	return started, err
}

// startDue reports a pre-draft league with enough managers whose
// scheduled start passed.
func startDue(l *league.League, now time.Time) bool {
	at := l.Settings.DraftStartsAt
	return l.Status == league.StatusPreDraft && at != nil && !now.Before(*at) && enoughManagers(l) == nil
}

// Pause stops the clock (commissioner).
func (s *Service) Pause(ctx context.Context, actor, leagueID string) (*View, error) {
	return s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		if err := league.Commissioner(st.League, actor); err != nil {
			return err
		}
		if err := notDrafting(st.League); err != nil {
			return err
		}
		// A clock that already ran out is settled first.
		if err := s.advance(ctx, tx, st, w, now, ch); err != nil {
			return err
		}
		if st.Draft.Status != league.DraftInProgress {
			return errNotLive
		}
		st.Draft.Status, st.Draft.PausedAt = league.DraftPaused, &now
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeDraft})
		return nil
	})
}

// Resume restarts the current pick with a full clock (commissioner).
func (s *Service) Resume(ctx context.Context, actor, leagueID string) (*View, error) {
	return s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		if err := league.Commissioner(st.League, actor); err != nil {
			return err
		}
		if err := notDrafting(st.League); err != nil {
			return err
		}
		if st.Draft.Status != league.DraftPaused {
			return errNotPaused
		}
		st.Draft.Status, st.Draft.PausedAt, st.Draft.ClockStartedAt = league.DraftInProgress, nil, &now
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeDraft})
		return s.advance(ctx, tx, st, w, now, ch)
	})
}

// Pick makes a pick for the caller's team, or, by the commissioner, for the
// team on the clock. It returns the pick.
func (s *Service) Pick(ctx context.Context, actor, leagueID, playerID string) (*View, league.Pick, error) {
	var made league.Pick
	v, err := s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		l := st.League
		if err := league.Member(l, actor); err != nil {
			return err
		}
		if err := notDrafting(l); err != nil {
			return err
		}
		// Settle a clock that ran out before this request arrived.
		if err := s.advance(ctx, tx, st, w, now, ch); err != nil {
			return err
		}
		d := st.Draft
		if d.Status != league.DraftInProgress {
			if l.Status == league.StatusInSeason {
				return errDraftComplete
			}
			return errNotLive
		}
		team, _, _ := Slot(d.Order, len(d.Picks)+1)
		method := league.PickManual
		if mine, ok := l.TeamOf(actor); !ok || mine.ID != team {
			if l.CommissionerID != actor {
				t, _ := l.Team(team)
				return league.Errorf(league.KindConflict, "not-your-turn", "Not your turn", t.Name+" is on the clock.")
			}
			method = league.PickCommissioner
		}
		p, ok := w.Players[playerID]
		if !ok {
			return league.Errorf(league.KindNotFound, "not-found", "Not found", "Player not found.")
		}
		if slices.ContainsFunc(d.Picks, func(pk league.Pick) bool { return pk.PlayerID == playerID }) {
			return league.Errorf(league.KindConflict, "player-taken", "Player taken", p.DisplayName+" has been drafted.")
		}
		if s.needs(d, w, team)[p.Position] <= 0 {
			msg := "No " + string(p.Position) + " places left."
			return league.Errorf(league.KindInvalid, "position-full", "Position full",
				"The squad already has every "+string(p.Position)+" it can hold.",
				league.FieldError{Code: "position_full", Message: msg, Field: "/playerId", PlayerID: playerID})
		}
		made = s.record(st, team, playerID, method, now, ch)
		return s.advance(ctx, tx, st, w, now, ch)
	})
	return v, made, err
}

// needs is how many more players of each position the team may draft.
func (s *Service) needs(d *league.Draft, w *league.World, team string) scoring.Counts {
	out := scoring.Counts{}
	for pos, n := range s.rules.Squad {
		out[pos] = n
	}
	for _, p := range d.Picks {
		if p.TeamID == team {
			out[w.Players[p.PlayerID].Position]--
		}
	}
	return out
}

// record appends a pick, restarts the clock and drops the player from
// every queue.
func (s *Service) record(st *league.State, team, playerID, method string, now time.Time, ch *[]league.Change) league.Pick {
	d := st.Draft
	pk := league.Pick{ID: ids.New("pk"), Overall: len(d.Picks) + 1, TeamID: team, PlayerID: playerID, Method: method, PickedAt: now}
	d.Picks = append(d.Picks, pk)
	d.ClockStartedAt = &now
	for t, q := range d.Queues {
		if slices.Contains(q, playerID) {
			d.Queues[t] = slices.DeleteFunc(slices.Clone(q), func(id string) bool { return id == playerID })
		}
	}
	*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangePick, Overall: pk.Overall})
	return pk
}

// choose is autopick: the first queued player still available at a
// position the squad needs, else the best available by draft rank.
func (s *Service) choose(d *league.Draft, w *league.World, team string) (string, string, bool) {
	need := s.needs(d, w, team)
	taken := map[string]bool{}
	for _, p := range d.Picks {
		taken[p.PlayerID] = true
	}
	for _, id := range d.Queues[team] {
		if p, ok := w.Players[id]; ok && !taken[id] && need[p.Position] > 0 {
			return id, league.PickQueue, true
		}
	}
	best := ""
	for id, p := range w.Players {
		if taken[id] || need[p.Position] <= 0 {
			continue
		}
		if b, ok := w.Players[best]; best == "" || !ok ||
			cmp.Or(cmp.Compare(p.DraftRank, b.DraftRank), cmp.Compare(id, best)) < 0 {
			best = id
		}
	}
	return best, league.PickRank, best != ""
}

// advance settles the clock: teams on autopick (or without a manager) pick
// at once, a clock that ran out is autopicked and turns that team's
// autopick on, the last pick completes the draft, and otherwise the next
// clock is scheduled.
func (s *Service) advance(ctx context.Context, tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
	d := st.Draft
	total := len(d.Order) * s.Rounds()
	for d.Status == league.DraftInProgress && len(d.Picks) < total {
		team, _, _ := Slot(d.Order, len(d.Picks)+1)
		t, _ := st.League.Team(team)
		auto := d.Autopick[team] || t.OwnerID == ""
		missed := false
		if !auto {
			if now.Before(deadline(d)) {
				break
			}
			// Missed the clock: autopick picks, so the draft never stalls,
			// and stays on unless the team turned that off.
			missed = true
			if !d.StayManual[team] {
				d.Autopick[team] = true
				*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeDraft})
			}
		}
		player, method, ok := s.choose(d, w, team)
		if !ok {
			return league.Errorf(league.KindConflict, "no-players-left", "No players left",
				"There is no player left that this squad can take.")
		}
		pk := s.record(st, team, player, method, now, ch)
		if missed {
			s.announceMissed(st, w, pk, now)
		}
	}
	if d.Status == league.DraftInProgress && len(d.Picks) >= total && total > 0 {
		return s.complete(st, w, now, ch)
	}
	if d.Status == league.DraftInProgress {
		return tx.Schedule(ctx, ClockArgs{LeagueID: st.League.ID, Overall: len(d.Picks) + 1, Deadline: deadline(d)}, deadline(d))
	}
	return nil
}

// announceMissed marks an autopick made because the clock ran out and adds
// it to the activity feed, so it is never silent. The entry's gameweek is
// the league's first.
func (s *Service) announceMissed(st *league.State, w *league.World, pk league.Pick, now time.Time) {
	st.Draft.Picks[pk.Overall-1].MissedClock = true
	first, _ := firstGW(st.League, w, now)
	st.NewTxns = append(st.NewTxns, league.Txn{
		ID: ids.New("txn"), Kind: league.TxnDraftAutopick, Gameweek: min(first, 38), OccurredAt: now,
		TeamID: pk.TeamID, PlayerIn: pk.PlayerID, Overall: pk.Overall, Method: pk.Method,
	})
}

// complete turns the picks into rosters from the first gameweek still open,
// with default lineups, the H2H schedule and the waiver order (the reverse
// of the draft order), records that gameweek as the league's
// startGameweek and puts the league in season. When the last pick
// comes after the league's last deadline there is nothing left to play:
// the rosters start after the season (gameweek 39 at the latest, never a
// played one), no lineups or matchups are made and the league is complete,
// so the draft ends instead of failing (and being retried) forever.
func (s *Service) complete(st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
	d, l := st.Draft, st.League
	first, playable := firstGW(l, w, now)
	d.Status, d.CompletedAt = league.DraftComplete, &now
	rosters := map[string][]string{}
	for _, p := range d.Picks {
		st.Game.Acquire(p.TeamID, p.PlayerID, first, "draft")
		rosters[p.TeamID] = append(rosters[p.TeamID], p.PlayerID)
	}
	st.Priority = slices.Clone(d.Order)
	slices.Reverse(st.Priority)
	*ch = append(*ch, league.Change{LeagueID: l.ID, Kind: league.ChangeDraft}, league.Change{LeagueID: l.ID, Kind: league.ChangeLeague})
	if !playable {
		l.Status = league.StatusComplete
		return nil
	}
	// The league's first scoring gameweek: every view starts there, so a
	// league drafted mid-season never shows a gameweek before it existed.
	l.Settings.StartGameweek = first
	for _, team := range d.Order {
		st.Game.SetLineup(team, first, league.DefaultLineup(rosters[team], w.Players), now)
	}
	st.NewMatchups = append(st.NewMatchups, league.RoundRobin(d.Order, first, l.Settings.EndGameweek)...)
	l.Status = league.StatusInSeason
	return nil
}

// Expire settles a league's pick clock (the clock job and the tick). It
// reports whether anything changed.
func (s *Service) Expire(ctx context.Context, leagueID string) (bool, error) {
	st, err := s.repo.State(ctx, leagueID)
	if err != nil || st == nil || st.Draft == nil || st.Draft.Status != league.DraftInProgress {
		return false, err
	}
	team, _, _ := Slot(st.Draft.Order, len(st.Draft.Picks)+1)
	t, _ := st.League.Team(team)
	if s.now().Before(deadline(st.Draft)) && !st.Draft.Autopick[team] && t.OwnerID != "" {
		return false, nil
	}
	n := len(st.Draft.Picks)
	v, err := s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		if st.Draft.Status != league.DraftInProgress {
			return nil
		}
		return s.advance(ctx, tx, st, w, now, ch)
	})
	if err != nil {
		return false, err
	}
	return len(v.Draft.Picks) != n, nil
}

// Queue is a team's draft queue.
type Queue struct {
	TeamID   string
	Autopick bool
	// OnTimeout is "autopick when my clock runs out" (on by default).
	OnTimeout bool
	Players   []string
	World     *league.World
	League    *league.League
}

func queueOf(st *league.State, w *league.World, team string) *Queue {
	taken := map[string]bool{}
	for _, p := range st.Draft.Picks {
		taken[p.PlayerID] = true
	}
	q := &Queue{TeamID: team, Autopick: st.Draft.Autopick[team], OnTimeout: !st.Draft.StayManual[team], World: w, League: st.League}
	for _, id := range st.Draft.Queues[team] {
		if !taken[id] {
			q.Players = append(q.Players, id)
		}
	}
	return q
}

func ownTeam(l *league.League, actor string) (league.Team, error) {
	if err := league.Member(l, actor); err != nil {
		return league.Team{}, err
	}
	t, ok := l.TeamOf(actor)
	if !ok {
		return league.Team{}, errNoTeam
	}
	return t, nil
}

// GetQueue returns the caller's queue. After the draft it is empty rather
// than an error: a client may still ask as the last pick lands.
func (s *Service) GetQueue(ctx context.Context, actor, leagueID string) (*Queue, error) {
	v, err := s.Get(ctx, actor, leagueID)
	if err != nil {
		return nil, err
	}
	t, err := ownTeam(v.League, actor)
	if err != nil {
		return nil, err
	}
	if notDrafting(v.League) != nil {
		return &Queue{TeamID: t.ID, Autopick: v.Draft.Autopick[t.ID], OnTimeout: !v.Draft.StayManual[t.ID], World: v.World, League: v.League}, nil
	}
	return queueOf(&league.State{League: v.League, Draft: v.Draft}, v.World, t.ID), nil
}

// SetQueue replaces the caller's queue. Drafted players are dropped
// silently; unknown ones are a 422.
func (s *Service) SetQueue(ctx context.Context, actor, leagueID string, players []string) (*Queue, error) {
	var q *Queue
	_, err := s.change(ctx, leagueID, func(_ league.Tx, st *league.State, w *league.World, _ time.Time, _ *[]league.Change) error {
		t, err := ownTeam(st.League, actor)
		if err != nil {
			return err
		}
		if err := notDrafting(st.League); err != nil {
			return err
		}
		var errs []league.FieldError
		seen := map[string]bool{}
		if len(players) > 100 {
			errs = append(errs, league.FieldError{Code: "too_long", Message: "A queue holds at most 100 players.", Field: "/playerIds"})
		}
		for _, id := range players {
			if _, ok := w.Players[id]; !ok {
				errs = append(errs, league.FieldError{Code: "unknown_player", Message: "No such player.", Field: "/playerIds", PlayerID: id})
			} else if seen[id] {
				errs = append(errs, league.FieldError{Code: "duplicate_player", Message: "A player appears twice.", Field: "/playerIds", PlayerID: id})
			}
			seen[id] = true
		}
		if len(errs) > 0 {
			return league.Errorf(league.KindInvalid, "invalid-request", "Invalid request", "The queue is not valid.", errs...)
		}
		st.Draft.Queues[t.ID] = slices.Clone(players)
		q = queueOf(st, w, t.ID)
		st.Draft.Queues[t.ID] = q.Players
		return nil
	})
	return q, err
}

// SetAutopickOnTimeout sets the caller's "autopick when my clock runs
// out" (on by default). Off, a missed clock still picks for the team, so
// the draft never stalls, but does not turn its autopick on.
func (s *Service) SetAutopickOnTimeout(ctx context.Context, actor, leagueID string, on bool) (*Queue, error) {
	var q *Queue
	_, err := s.change(ctx, leagueID, func(_ league.Tx, st *league.State, w *league.World, _ time.Time, ch *[]league.Change) error {
		t, err := ownTeam(st.League, actor)
		if err != nil {
			return err
		}
		if err := notDrafting(st.League); err != nil {
			return err
		}
		if st.Draft.StayManual == nil {
			st.Draft.StayManual = map[string]bool{}
		}
		if st.Draft.StayManual[t.ID] == on {
			if on {
				delete(st.Draft.StayManual, t.ID)
			} else {
				st.Draft.StayManual[t.ID] = true
			}
			*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeDraft})
		}
		q = queueOf(st, w, t.ID)
		return nil
	})
	return q, err
}

// SetAutopick turns autopick on or off for the caller's team. Turned on
// while the team is on the clock, it picks at once.
func (s *Service) SetAutopick(ctx context.Context, actor, leagueID string, on bool) (*Queue, error) {
	var q *Queue
	_, err := s.change(ctx, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change) error {
		t, err := ownTeam(st.League, actor)
		if err != nil {
			return err
		}
		if err := notDrafting(st.League); err != nil {
			return err
		}
		if st.Draft.Autopick[t.ID] != on {
			if on {
				st.Draft.Autopick[t.ID] = true
			} else {
				delete(st.Draft.Autopick, t.ID)
			}
			*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeDraft})
		}
		if err := s.advance(ctx, tx, st, w, now, ch); err != nil {
			return err
		}
		q = queueOf(st, w, t.ID)
		return nil
	})
	return q, err
}
