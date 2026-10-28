package mirror

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// View is an imported league's FPL status. Report, Explanations,
// Unexplained and Pending are the commissioner's only (nil for members).
type View struct {
	League       *league.League
	Record       *SyncRecord // nil before the first import finished
	Commissioner bool
	Report       *Report
	Explanations []Explanation
	// Unexplained are the report's failing items without an explanation.
	// Informational: nothing waits on them.
	Unexplained []Item
	// Pending are the trades FPL accepted but has not processed or vetoed
	// yet; not failing: the next re-sync applies what FPL decides.
	Pending []Item
	// Shadow is the latest shadow check (nil before the first).
	Shadow *ShadowReport
}

// staleRequest is when a pending re-sync or check no longer blocks a new
// request: well past the job's timeout, so a lost job cannot block the
// league for ever.
const staleRequest = 30 * time.Minute

var (
	errSyncPending = league.Errorf(league.KindConflict, "sync-pending", "Already running",
		"A re-sync or check of this league is already queued or running.")
	errNotConfirmed = league.Errorf(league.KindInvalid, "invalid-request", "Invalid request",
		"Confirm the re-sync: it replaces every move made here since the last one.",
		league.FieldError{Code: "required", Message: "Must be true.", Field: "/confirm"})
)

// View returns the league's FPL status for a member.
func (s *Service) View(ctx context.Context, actor, leagueID string) (*View, error) {
	l, err := s.Leagues.Get(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, league.ErrLeagueNotFound
	}
	if err := league.Member(l, actor); err != nil {
		return nil, err
	}
	if l.Source != league.SourceFPL {
		return nil, errNotImported
	}
	v := &View{League: l, Commissioner: l.CommissionerID == actor}
	if v.Record, err = s.Store.Sync(ctx, leagueID); err != nil {
		return nil, err
	}
	if !v.Commissioner {
		return v, nil
	}
	if v.Report, err = s.Store.LatestReport(ctx, leagueID); err != nil {
		return nil, err
	}
	if v.Explanations, err = s.Store.Explanations(ctx, leagueID); err != nil {
		return nil, err
	}
	if v.Shadow, err = s.Store.LatestShadow(ctx, leagueID); err != nil {
		return nil, err
	}
	v.Unexplained, v.Pending = []Item{}, []Item{}
	if v.Report != nil {
		v.Unexplained = append(v.Unexplained, v.Report.Blocking(v.Explanations)...)
		v.Pending = append(v.Pending, v.Report.Pending()...)
	}
	return v, nil
}

// commissionerOf loads an imported league the actor is commissioner of.
func (s *Service) commissionerOf(ctx context.Context, actor, leagueID string) (*league.League, error) {
	l, err := s.Leagues.Get(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, league.ErrLeagueNotFound
	}
	if err := league.Commissioner(l, actor); err != nil {
		return nil, err
	}
	if l.Source != league.SourceFPL {
		return nil, errNotImported
	}
	return l, nil
}

// AuthorizeCommissioner returns the error a commissioner-only request would
// get (not found, not a member, not the commissioner, not imported), or nil.
func (s *Service) AuthorizeCommissioner(ctx context.Context, actor, leagueID string) error {
	_, err := s.commissionerOf(ctx, actor, leagueID)
	return err
}

// RequestResync queues a re-sync of the league from FPL (commissioner
// only; confirm must be true): Resync resets it to FPL's current state.
func (s *Service) RequestResync(ctx context.Context, actor, leagueID string, confirm bool) (*View, error) {
	if !confirm {
		return nil, errNotConfirmed
	}
	return s.request(ctx, actor, leagueID, PendingResync, func(at time.Time) league.JobArgs {
		return ResyncArgs{LeagueID: leagueID, At: at}
	})
}

// RequestCheck queues a check of the league against FPL (commissioner
// only); it changes nothing but the report.
func (s *Service) RequestCheck(ctx context.Context, actor, leagueID string) (*View, error) {
	return s.request(ctx, actor, leagueID, PendingCheck, func(at time.Time) league.JobArgs {
		return CheckArgs{LeagueID: leagueID, At: at}
	})
}

// request marks the work pending, one at a time per league, and schedules
// its job.
func (s *Service) request(ctx context.Context, actor, leagueID, kind string, job func(time.Time) league.JobArgs) (*View, error) {
	if _, err := s.commissionerOf(ctx, actor, leagueID); err != nil {
		return nil, err
	}
	now := s.now()
	ok, err := s.Store.Request(ctx, leagueID, kind, now, now.Add(-staleRequest))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errSyncPending
	}
	s.log().InfoContext(ctx, "fpl "+kind+" requested", "league", leagueID, "by", actor)
	if s.Sched != nil {
		if err := s.Sched.ScheduleTx(ctx, nil, job(now), now); err != nil {
			return nil, errors.Join(err, s.finish(ctx, leagueID, err))
		}
	}
	if s.Notify != nil {
		s.Notify.Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeLeague})
	}
	return s.View(ctx, actor, leagueID)
}

// MaxNote is the longest explanation.
const MaxNote = 500

// Explain records the commissioner's note on a failing item of the latest
// report.
func (s *Service) Explain(ctx context.Context, actor, leagueID, key, note string) (*View, error) {
	if _, err := s.commissionerOf(ctx, actor, leagueID); err != nil {
		return nil, err
	}
	note = strings.TrimSpace(note)
	if n := len([]rune(note)); n < 1 || n > MaxNote {
		return nil, league.Errorf(league.KindInvalid, "invalid-request", "Invalid request",
			"The note must be 1 to "+strconv.Itoa(MaxNote)+" characters.",
			league.FieldError{Code: "out_of_range", Message: "Say why this is fine.", Field: "/note"})
	}
	rep, err := s.Store.LatestReport(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	var item *Item
	if rep != nil {
		for i := range rep.Items {
			if rep.Items[i].Key == key && rep.Items[i].Failing() {
				item = &rep.Items[i]
			}
		}
	}
	if item == nil {
		return nil, league.Errorf(league.KindNotFound, "not-found", "Not found", "No failing item with that key in the latest report.")
	}
	if err := s.Store.SaveExplanation(ctx, leagueID, Explanation{Key: key, Ours: item.Ours, Theirs: item.Theirs, Note: note,
		ExplainedBy: actor, ExplainedAt: s.now()}); err != nil {
		return nil, err
	}
	return s.View(ctx, actor, leagueID)
}
