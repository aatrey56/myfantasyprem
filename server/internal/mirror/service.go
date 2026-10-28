// Package mirror imports an FPL Draft league that is already in its
// season, re-syncs it from FPL when its commissioner asks (resetting it
// to FPL's state), and checks it against FPL with our own engines
// (docs/adr/0012). An imported league is playable here from the moment
// it is imported; nothing syncs it automatically.
//
// The work is split so the parts that decide are pure: Build turns what
// upstream says (a Snapshot) into a Plan, Reset clears what was played
// here and Apply makes a league State match a Plan, Verify recomputes the
// league with the league and scoring packages and diffs it against
// upstream. Service fetches, runs those in the league's transaction,
// rescores and stores the Report; the jobs package runs its work as River
// jobs.
//
// It depends on provider (LeagueMirrorSource), league, scoring, stats,
// store and ids, never on net/http.
package mirror

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// ImportArgs is the import job.
type ImportArgs struct {
	ImportID string `json:"importId"`
}

// Kind names the job.
func (ImportArgs) Kind() string { return "fpl_import" }

// ResyncArgs is a re-sync of one league, requested at At (part of the
// arguments so each request is its own unique job).
type ResyncArgs struct {
	LeagueID string    `json:"leagueId"`
	At       time.Time `json:"at"`
}

// Kind names the job.
func (ResyncArgs) Kind() string { return "fpl_resync" }

// CheckArgs is a check of one league against FPL, requested at At.
type CheckArgs struct {
	LeagueID string    `json:"leagueId"`
	At       time.Time `json:"at"`
}

// Kind names the job.
func (CheckArgs) Kind() string { return "fpl_check" }

// Service imports, re-syncs and checks leagues.
type Service struct {
	Source  provider.LeagueMirrorSource
	Leagues league.Repo
	Stats   stats.Repo
	Store   Store
	// Sched runs the import, re-sync and check jobs; nil leaves running
	// them to the caller (tests).
	Sched  league.Scheduler
	Now    func() time.Time
	Notify league.Notifier
	Log    *slog.Logger
	// Alert files the commissioner's shadow check notifications; nil:
	// none.
	Alert Alerter
	// ShadowUntil ends the scheduled shadow checks (the switch-over);
	// zero: no end.
	ShadowUntil time.Time

	// planned remembers the shadow checks this process scheduled, so the
	// hourly planner does not schedule one twice on the in-memory
	// scheduler (River also refuses a duplicate by its arguments).
	planned sync.Map
}

func (s *Service) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC().Truncate(time.Microsecond)
	}
	return s.Now().UTC().Truncate(time.Microsecond)
}

func (s *Service) log() *slog.Logger {
	if s.Log == nil {
		return slog.Default()
	}
	return s.Log
}

// Errors.
var (
	errNotFound      = league.Errorf(league.KindNotFound, "not-found", "Not found", "No FPL Draft league with that id.")
	errImportMissing = league.Errorf(league.KindNotFound, "not-found", "Not found", "Import not found.")
	errNotImported   = league.Errorf(league.KindConflict, "not-imported", "Not an imported league",
		"This league was created here, not imported from FPL Draft.")
	// ErrImportUnavailable: our player data comes from another source (the
	// demo world), so no FPL league can be imported, now or later.
	ErrImportUnavailable = league.Errorf(league.KindConflict, "import-unavailable", "Import unavailable",
		"This server isn't connected to live player data, so FPL Draft leagues can't be imported here.")
	// ErrStatsNotReady: FPL's player data has not been ingested yet.
	ErrStatsNotReady = league.Errorf(league.KindConflict, "stats-not-ready", "Player data not ready",
		"Player data is still loading. Try again in a few minutes.")
)

// statsProblem says why our player data cannot back an import, or nil when
// it can: every import maps FPL's player ids to ours. Season data with no
// FPL players came from another source (POLLER_SOURCE=demo) and never
// will; no season data at all is FPL's, still loading.
func (s *Service) statsProblem(ctx context.Context) (*league.Error, error) {
	players, err := s.Stats.ExternalIDs(ctx, Provider, "player")
	if err != nil || len(players) > 0 {
		return nil, err
	}
	if _, ok, err := s.Stats.Status(ctx); err != nil {
		return nil, err
	} else if ok {
		return ErrImportUnavailable, nil
	}
	return ErrStatsNotReady, nil
}

// ErrUpstream wraps a failure to reach FPL (httpapi answers 502).
var ErrUpstream = provider.ErrUpstream

func fplID(n int64) string { return strconv.FormatInt(n, 10) }

func upstream(err error) error {
	if errors.Is(err, provider.ErrNotFound) {
		return errNotFound
	}
	return err
}

// PreviewSeat is one team of a league being previewed.
type PreviewSeat struct {
	Seat        int
	TeamName    string
	ManagerName string
}

// Preview is enough to answer "Is this your league?".
type Preview struct {
	FPLLeagueID     int64
	Name            string
	Seats           []PreviewSeat
	CurrentGameweek int
	H2H             bool
	DraftComplete   bool
	// Problem is why it cannot be imported; nil when it can.
	Problem *league.Error
	// ImportedLeagueID is the caller's league from an earlier import.
	ImportedLeagueID string
}

// Preview looks a league up (one upstream request).
func (s *Service) Preview(ctx context.Context, actor string, fplLeagueID int64) (*Preview, error) {
	d, err := s.Source.LeagueDetails(ctx, fplID(fplLeagueID))
	if err != nil {
		return nil, upstream(err)
	}
	st, _, err := s.Stats.Status(ctx)
	if err != nil {
		return nil, err
	}
	cal := &league.World{Status: st, Gameweeks: map[int]stats.Gameweek{}}
	if gws, err := s.Stats.Gameweeks(ctx); err == nil {
		for _, g := range gws {
			cal.Gameweeks[g.Number] = g
		}
	}
	cal.SyncCalendar(s.now())
	p := &Preview{FPLLeagueID: fplLeagueID, Name: d.Name, CurrentGameweek: cal.Status.Current, H2H: d.Scoring == provider.ScoringH2H,
		DraftComplete: d.DraftCompleted, Problem: Unsupported(d)}
	// Said before the user picks a team: no import can work on this server yet.
	if sp, err := s.statsProblem(ctx); err != nil {
		return nil, err
	} else if sp != nil {
		p.Problem = sp
	}
	for i, e := range realSeats(d) {
		p.Seats = append(p.Seats, PreviewSeat{Seat: i + 1, TeamName: e.TeamName, ManagerName: e.ManagerName})
	}
	if imp, err := s.Store.LatestImport(ctx, actor, fplLeagueID); err != nil {
		return nil, err
	} else if imp != nil && imp.Status == ImportSucceeded {
		p.ImportedLeagueID = imp.LeagueID
	}
	return p, nil
}

// StartImport queues an import of the FPL league with the actor in seat
// (1-based, as in the preview). existing is true when the actor already
// has an active or successful import of it: that one is returned.
func (s *Service) StartImport(ctx context.Context, actor string, fplLeagueID int64, seat int) (Import, bool, error) {
	if imp, err := s.Store.LatestImport(ctx, actor, fplLeagueID); err != nil {
		return Import{}, false, err
	} else if imp != nil && (imp.Active() || imp.Status == ImportSucceeded) {
		return *imp, true, nil
	}
	d, err := s.Source.LeagueDetails(ctx, fplID(fplLeagueID))
	if err != nil {
		return Import{}, false, upstream(err)
	}
	if e := Unsupported(d); e != nil {
		return Import{}, false, e
	}
	if seat < 1 || seat > len(realSeats(d)) {
		return Import{}, false, league.Errorf(league.KindInvalid, "invalid-request", "Invalid request",
			"Pick your team: seat must be 1 to "+strconv.Itoa(len(realSeats(d)))+".",
			league.FieldError{Code: "out_of_range", Message: "No such team in this league.", Field: "/seat"})
	}
	// Refused only when it can never work; player data still loading may
	// be there by the time the job runs (else the job fails saying so).
	if e, err := s.statsProblem(ctx); err != nil {
		return Import{}, false, err
	} else if e == ErrImportUnavailable {
		return Import{}, false, e
	}
	now := s.now()
	imp := Import{ID: ids.New("imp"), UserID: actor, FPLLeagueID: fplLeagueID, Seat: seat, Status: ImportQueued,
		LeagueID: ids.New("lg"), CreatedAt: now, UpdatedAt: now}
	out, err := s.Store.CreateImport(ctx, imp, s.Sched, ImportArgs{ImportID: imp.ID})
	if errors.Is(err, ErrImportExists) {
		return out, true, nil
	}
	return out, false, err
}

// Import returns the actor's import.
func (s *Service) Import(ctx context.Context, actor, id string) (Import, error) {
	imp, err := s.Store.Import(ctx, id)
	if err != nil {
		return Import{}, err
	}
	if imp == nil || imp.UserID != actor {
		return Import{}, errImportMissing
	}
	return *imp, nil
}

// RunImport is the import job: fetch, plan, apply, rescore, verify. It is
// safe to run again: a finished import does nothing, a retried one
// converges on the same league.
func (s *Service) RunImport(ctx context.Context, id string) error {
	imp, err := s.Store.Import(ctx, id)
	if err != nil || imp == nil || imp.Status == ImportSucceeded || imp.Status == ImportFailed {
		return err
	}
	imp.Status, imp.UpdatedAt = ImportRunning, s.now()
	if err := s.Store.SaveImport(ctx, *imp); err != nil {
		return err
	}
	progress := func(done, total int) {
		imp.Done, imp.Total, imp.UpdatedAt = done, total, s.now()
		if err := s.Store.SaveImport(ctx, *imp); err != nil {
			s.log().WarnContext(ctx, "import progress not saved", "import", id, "err", err)
		}
	}
	_, err = s.refresh(ctx, imp.LeagueID, imp, progress)
	imp.UpdatedAt = s.now()
	if err != nil {
		if ctx.Err() != nil {
			return err // shutting down: River retries the job
		}
		imp.Status, imp.Error = ImportFailed, problemOf(err)
		s.log().ErrorContext(ctx, "import failed", "import", id, "fpl_league", imp.FPLLeagueID, "err", err)
		return s.Store.SaveImport(ctx, *imp)
	}
	imp.Status, imp.Done = ImportSucceeded, imp.Total
	return s.Store.SaveImport(ctx, *imp)
}

func problemOf(err error) *Problem {
	var le *league.Error
	if errors.As(err, &le) {
		return &Problem{Slug: le.Slug, Title: le.Title, Detail: le.Detail}
	}
	if errors.Is(err, provider.ErrUpstream) || errors.Is(err, provider.ErrNotYetAvailable) {
		return &Problem{Slug: "upstream-error", Title: "FPL unavailable",
			Detail: "FPL Draft could not be reached or sent something unexpected. Try again in a few minutes."}
	}
	if strings.HasPrefix(err.Error(), "mirror: player") {
		// One player FPL knows and we do not yet (a new signing).
		return problemOf(ErrStatsNotReady)
	}
	return &Problem{Slug: "import-failed", Title: "Import failed", Detail: "The import could not finish. Try again."}
}

// Resync is the re-sync job: the league is reset to FPL's current state
// (Reset, then Apply), rescored and verified. A failure is recorded on the
// league's sync record, not returned: the league keeps its state.
func (s *Service) Resync(ctx context.Context, leagueID string) error {
	l, err := s.Leagues.Get(ctx, leagueID)
	if err != nil || l == nil || l.Source != league.SourceFPL {
		return err
	}
	if _, err = s.refresh(ctx, leagueID, nil, nil); err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err() // shutting down: River retries the job
	}
	s.log().WarnContext(ctx, "re-sync failed", "league", leagueID, "err", err)
	return s.finish(ctx, leagueID, err)
}

// Check is the check job: FPL is fetched and the league compared with it
// (Verify) without changing anything; the report is stored.
func (s *Service) Check(ctx context.Context, leagueID string) error {
	l, err := s.Leagues.Get(ctx, leagueID)
	if err != nil || l == nil || l.Source != league.SourceFPL {
		return err
	}
	err = func() error {
		_, snap, plan, err := s.plan(ctx, l.FPLLeagueID, s.now(), nil)
		if err != nil {
			return err
		}
		// Verify against the stored scores' world, as after a re-sync.
		w, err := league.LoadWorld(ctx, s.Stats)
		if err != nil {
			return err
		}
		st, err := s.Leagues.State(ctx, leagueID)
		if err != nil {
			return err
		}
		return s.Store.SaveReport(ctx, leagueID, Verify(snap, plan, st, w, s.now()))
	}()
	if err != nil && ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		s.log().WarnContext(ctx, "check failed", "league", leagueID, "err", err)
	}
	if ferr := s.finish(ctx, leagueID, err); ferr != nil {
		return ferr
	}
	if s.Notify != nil {
		s.Notify.Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeLeague})
	}
	return nil
}

// finish records a re-sync's or check's outcome (err nil: it worked) and
// clears the pending request.
func (s *Service) finish(ctx context.Context, leagueID string, err error) error {
	rec, rerr := s.Store.Sync(ctx, leagueID)
	if rerr != nil {
		return rerr
	}
	if rec == nil {
		rec = &SyncRecord{LeagueID: leagueID}
	}
	now := s.now()
	rec.LastAttemptAt, rec.LastError, rec.Pending, rec.RequestedAt = &now, "", "", nil
	if err != nil {
		rec.LastError = problemOf(err).Detail
	}
	return s.Store.SaveSync(ctx, *rec)
}

// fetch reads everything about the league from upstream: five league
// endpoints and each seat's picks for every gameweek whose deadline has
// passed (cached once the gameweek is finished).
func (s *Service) fetch(ctx context.Context, fplLeagueID int64, w *league.World, now time.Time, progress func(done, total int)) (*Snapshot, error) {
	id := fplID(fplLeagueID)
	snap := &Snapshot{Picks: map[string]map[int]*provider.EntryPicks{}}
	var err error
	if snap.Details, err = s.Source.LeagueDetails(ctx, id); err != nil {
		return nil, upstream(err)
	}
	if e := Unsupported(snap.Details); e != nil {
		return nil, e
	}
	seats := realSeats(snap.Details)
	last := min(w.UpcomingGW(now)-1, snap.Details.EndGameweek)
	var gws []int
	for gw := snap.Details.StartGameweek; gw <= last; gw++ {
		gws = append(gws, gw)
	}
	total, done := 5+len(seats)*len(gws), 1
	report := func() {
		if progress != nil {
			progress(done, total)
		}
	}
	report()
	steps := []func() error{
		func() (err error) { snap.Status, err = s.Source.ElementStatus(ctx, id); return },
		func() (err error) { snap.Choices, err = s.Source.DraftChoices(ctx, id); return },
		func() (err error) { snap.Transactions, err = s.Source.Transactions(ctx, id); return },
		func() (err error) { snap.Trades, err = s.Source.Trades(ctx, id); return },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, upstream(err)
		}
		done++
		report()
	}
	for _, e := range seats {
		snap.Picks[e.ExternalID] = map[int]*provider.EntryPicks{}
		for _, gw := range gws {
			p, err := s.Store.Picks(ctx, Provider, e.EntryID, gw)
			if err != nil {
				return nil, err
			}
			if p == nil {
				p, err = s.Source.EntryEventPicks(ctx, e.EntryID, gw)
				switch {
				case errors.Is(err, provider.ErrNotYetAvailable), errors.Is(err, provider.ErrNotFound):
					p, err = nil, nil // not public yet, or the entry had no team then
				case err != nil:
					return nil, err
				case w.Gameweeks[gw].Finished:
					if err := s.Store.SavePicks(ctx, Provider, p); err != nil {
						return nil, err
					}
				}
			}
			if p != nil {
				snap.Picks[e.ExternalID][gw] = p
			}
			done++
			report()
		}
	}
	return snap, nil
}

// plan fetches the FPL league and builds what it says the league should be.
func (s *Service) plan(ctx context.Context, fplLeague int64, now time.Time, progress func(done, total int)) (*league.World, *Snapshot, *Plan, error) {
	if e, err := s.statsProblem(ctx); err != nil {
		return nil, nil, nil, err
	} else if e != nil {
		return nil, nil, nil, e
	}
	w, err := league.LoadWorld(ctx, s.Stats)
	if err != nil {
		return nil, nil, nil, err
	}
	w.SyncCalendar(now)
	if len(w.Gameweeks) == 0 {
		return nil, nil, nil, league.ErrNoSeasonData
	}
	snap, err := s.fetch(ctx, fplLeague, w, now, progress)
	if err != nil {
		return nil, nil, nil, err
	}
	players, err := s.Stats.ExternalIDs(ctx, Provider, "player")
	if err != nil {
		return nil, nil, nil, err
	}
	plan, err := Build(snap, players, func(id string) scoring.Position { return w.Players[id].Position })
	if err != nil {
		return nil, nil, nil, err
	}
	return w, snap, plan, nil
}

// refresh is an import's or re-sync's work on one league: fetch, plan,
// then in one transaction clear what was played here (Reset) and apply
// FPL's state (Apply); rescore, verify, and store the report and the sync
// record (with the re-sync's notice). create is the import that makes the
// league on its first run.
func (s *Service) refresh(ctx context.Context, leagueID string, create *Import, progress func(done, total int)) (*Report, error) {
	now := s.now()
	var fplLeague int64
	if create != nil {
		fplLeague = create.FPLLeagueID
	} else {
		l, err := s.Leagues.Get(ctx, leagueID)
		if err != nil {
			return nil, err
		}
		if l == nil {
			return nil, league.ErrLeagueNotFound
		}
		fplLeague = l.FPLLeagueID
	}
	w, snap, plan, err := s.plan(ctx, fplLeague, now, progress)
	if err != nil {
		return nil, err
	}

	var cancelled Cancelled
	err = s.Leagues.Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			if create == nil {
				return league.ErrLeagueNotFound
			}
			l := &league.League{ID: leagueID, Name: plan.Name, Season: league.Season, Status: league.StatusInSeason,
				Settings: plan.Settings, CommissionerID: create.UserID, CreatedAt: now, Source: league.SourceFPL,
				FPLLeagueID: create.FPLLeagueID}
			if err := tx.Insert(ctx, l); err != nil {
				return err
			}
			if st, err = tx.LoadState(ctx, leagueID); err != nil {
				return err
			}
		}
		if st.League.Source != league.SourceFPL {
			return errNotImported
		}
		cancelled = Reset(st, plan, now)
		Apply(st, plan, w, now)
		if create != nil {
			if _, owns := st.League.TeamOf(create.UserID); !owns {
				mine := Seats(st)[plan.Seats[create.Seat-1].ExternalID]
				for i := range st.League.Teams {
					if t := &st.League.Teams[i]; t.ID == mine && t.OwnerID == "" {
						t.OwnerID = create.UserID
					}
				}
			}
		}
		for _, t := range st.League.Teams {
			if _, ok := st.League.ClaimLink(t.ID); !ok && t.OwnerID == "" {
				if _, err := league.IssueClaimInvite(ctx, tx, st.League, t.ID, now); err != nil {
					return err
				}
			}
		}
		return tx.SaveState(ctx, st)
	})
	if err != nil {
		return nil, err
	}

	scorer := &league.Scorer{Leagues: s.Leagues, Stats: s.Stats, Rules: scoring.StandardLineup(), Log: s.Log, Notify: s.Notify}
	for gw := plan.Settings.StartGameweek; gw <= min(w.UpcomingGW(now)-1, plan.Settings.EndGameweek); gw++ {
		if _, _, err := scorer.RescoreLeague(ctx, leagueID, gw); err != nil {
			return nil, err
		}
	}
	// Verify against the world the scores were made from (rescored rows
	// included).
	if w, err = league.LoadWorld(ctx, s.Stats); err != nil {
		return nil, err
	}
	st, err := s.Leagues.State(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	rep := Verify(snap, plan, st, w, now)
	if err := s.Store.SaveReport(ctx, leagueID, rep); err != nil {
		return nil, err
	}
	rec, err := s.Store.Sync(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		rec = &SyncRecord{LeagueID: leagueID}
	}
	done := s.now()
	rec.LastAttemptAt, rec.LastSyncedAt, rec.LastError, rec.Pending, rec.RequestedAt = &done, &now, "", "", nil
	rec.Note = ""
	if create == nil {
		rec.Note = cancelled.Note()
	}
	if err := s.Store.SaveSync(ctx, *rec); err != nil {
		return nil, err
	}
	if s.Notify != nil {
		s.Notify.Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeLeague})
	}
	return &rep, nil
}

// Report returns the league's latest verification report, or nil.
func (s *Service) Report(ctx context.Context, leagueID string) (*Report, error) {
	return s.Store.LatestReport(ctx, leagueID)
}
