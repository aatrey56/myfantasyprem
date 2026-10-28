package lineup

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Service sets lineups. It is safe for concurrent use.
type Service struct {
	repo   league.Repo
	stats  stats.Repo
	now    func() time.Time
	rules  scoring.LineupRules
	notify league.Notifier
}

// New returns a Service. notify may be nil.
func New(repo league.Repo, st stats.Repo, now func() time.Time, notify league.Notifier) *Service {
	if notify == nil {
		notify = league.NopNotifier{}
	}
	return &Service{repo: repo, stats: st, rules: scoring.StandardLineup(), notify: notify,
		now: func() time.Time { return now().UTC().Truncate(time.Microsecond) }}
}

// LockDue announces the lock of the latest gameweek whose deadline has
// passed, once per gameweek (the jobs): it records the run and reports
// lineup.locked. The lock itself needs no job: Set refuses a gameweek
// whose deadline has passed. It reports whether it announced one.
func (s *Service) LockDue(ctx context.Context, leagueID string) (bool, error) {
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return false, err
	}
	gw := min(w.UpcomingGW(s.now()), 39) - 1
	st, err := s.repo.State(ctx, leagueID)
	if err != nil || st == nil || gw < 1 || st.League.Status != league.StatusInSeason {
		return false, err
	}
	if _, done := st.Ran(league.RunLineupLock, gw); done {
		return false, nil
	}
	announced := false
	err = s.repo.Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil || st == nil {
			return err
		}
		if _, done := st.Ran(league.RunLineupLock, gw); done {
			return nil
		}
		st.Runs[league.RunKey{Kind: league.RunLineupLock, Gameweek: gw}] = s.now()
		announced = true
		return tx.SaveState(ctx, st)
	})
	if err != nil || !announced {
		return false, err
	}
	s.notify.Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeLineupLock, Gameweek: gw})
	return true, nil
}

// Set stores starters (slots 1-11) and bench (12-15, in auto-sub order)
// as the team's lineup from gameweek gw on (owner only).
func (s *Service) Set(ctx context.Context, actor, leagueID, teamID string, gw int, starters, bench []string) error {
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return err
	}
	return s.repo.Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return league.ErrLeagueNotFound
		}
		l := st.League
		if err := league.Member(l, actor); err != nil {
			return err
		}
		team, ok := l.Team(teamID)
		if !ok {
			return league.Errorf(league.KindNotFound, "not-found", "Not found", "Team not found.")
		}
		if team.OwnerID != actor {
			return league.Errorf(league.KindForbidden, "not-team-owner", "Not your team", "Only the team's manager can set its lineup.")
		}
		if l.Status != league.StatusInSeason {
			return league.ErrSeasonNotStarted
		}
		g, ok := w.Gameweeks[gw]
		if !ok {
			return league.Errorf(league.KindNotFound, "not-found", "Not found", "No such gameweek this season.")
		}
		now := s.now()
		if !now.Before(g.DeadlineAt) {
			next := w.UpcomingGW(now)
			msg := "Gameweek " + strconv.Itoa(gw) + " has started."
			if next <= 38 {
				msg += " Changes now apply from gameweek " + strconv.Itoa(next) + "."
			}
			return league.Errorf(league.KindConflict, "lineup-locked", "Lineup locked", msg)
		}
		roster := map[string]scoring.Position{}
		for _, id := range st.Game.RosterAt(teamID, gw) {
			roster[id] = w.Players[id].Position
		}
		if len(roster) == 0 {
			return league.Errorf(league.KindNotFound, "no-roster", "No roster", "This team has no players for that gameweek.")
		}
		if vs := s.rules.ValidateLineup(roster, starters, bench); len(vs) > 0 {
			errs := make([]league.FieldError, 0, len(vs))
			for _, v := range vs {
				errs = append(errs, league.FieldError{Code: v.Code, Message: v.Message, PlayerID: v.PlayerID})
			}
			return league.Errorf(league.KindInvalid, "invalid-lineup", "Invalid lineup", "That lineup breaks the formation rules.", errs...)
		}
		st.Game.SetLineup(teamID, gw, slices.Concat(starters, bench), now)
		return tx.SaveState(ctx, st)
	})
}
