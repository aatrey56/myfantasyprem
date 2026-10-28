// Package tradeblock runs a league's trade block: each team marks players
// of its own it would trade, with an optional short note and the positions
// it wants back. The block is public to the league. Entries live in the
// league's State (ADR 0010), so a mark and a roster move queue up on the
// league row, and SaveState drops an entry once its team no longer holds
// the player: a free-agent drop, a waiver run or a processed trade clears
// it with no code here. Depends on league and stats, never on net/http.
package tradeblock

import (
	"context"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// MaxNote is the longest note, in characters.
const MaxNote = 140

// Positions are the positions a team can ask for, in display order.
var Positions = []string{"GKP", "DEF", "MID", "FWD"}

// Service marks and unmarks players on the block.
type Service struct {
	repo   league.Repo
	now    func() time.Time
	notify league.Notifier
}

// New returns a Service. notify may be nil.
func New(repo league.Repo, now func() time.Time, notify league.Notifier) *Service {
	if notify == nil {
		notify = league.NopNotifier{}
	}
	return &Service{repo: repo, notify: notify,
		now: func() time.Time { return now().UTC().Truncate(time.Microsecond) }}
}

// List returns the league's State (its Block, oldest first, and the teams)
// for a member.
func (s *Service) List(ctx context.Context, actor, leagueID string) (*league.State, error) {
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
	return st, nil
}

// Clean checks a note and wanted positions: the note trimmed and at most
// MaxNote characters, the positions known, each once, in Positions order.
func Clean(note string, wanted []string) (string, []string, error) {
	note = strings.TrimSpace(note)
	var errs []league.FieldError
	if utf8.RuneCountInString(note) > MaxNote {
		errs = append(errs, league.FieldError{Code: "too_long", Field: "/note",
			Message: "A note is at most 140 characters."})
	}
	out := []string{}
	for _, p := range wanted {
		if !slices.Contains(Positions, p) {
			errs = append(errs, league.FieldError{Code: "unknown_position", Field: "/wanted",
				Message: "Wanted positions are GKP, DEF, MID and FWD."})
			continue
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	if errs != nil {
		return "", nil, league.Errorf(league.KindInvalid, "validation-failed", "Check the trade block entry",
			errs[0].Message, errs...)
	}
	slices.SortFunc(out, func(a, b string) int { return slices.Index(Positions, a) - slices.Index(Positions, b) })
	return note, out, nil
}

// Set puts one of the actor's players on the block, or changes the note
// and wanted positions of one already there (keeping when it went on).
func (s *Service) Set(ctx context.Context, actor, leagueID, playerID, note string, wanted []string) (*league.State, league.BlockEntry, error) {
	note, wanted, err := Clean(note, wanted)
	if err != nil {
		return nil, league.BlockEntry{}, err
	}
	var out league.BlockEntry
	st, err := s.change(ctx, leagueID, func(st *league.State, now time.Time) (string, error) {
		t, err := own(st, actor)
		if err != nil {
			return "", err
		}
		if !st.Game.Holds(t.ID, playerID) {
			return "", league.Errorf(league.KindConflict, "not-on-roster", "Not on your roster",
				"Only players on your roster can go on your trade block.")
		}
		if e := st.BlockOf(playerID); e != nil {
			e.Note, e.Wanted = note, wanted
			out = *e
		} else {
			out = league.BlockEntry{TeamID: t.ID, PlayerID: playerID, Note: note, Wanted: wanted, AddedAt: now}
			st.Block = append(st.Block, out)
		}
		return t.ID, nil
	})
	return st, out, err
}

// Remove takes one of the actor's players off the block. A player not on
// the block is no error; another team's player is 403.
func (s *Service) Remove(ctx context.Context, actor, leagueID, playerID string) error {
	_, err := s.change(ctx, leagueID, func(st *league.State, _ time.Time) (string, error) {
		t, err := own(st, actor)
		if err != nil {
			return "", err
		}
		e := st.BlockOf(playerID)
		if e == nil {
			return "", nil
		}
		if e.TeamID != t.ID {
			return "", league.Errorf(league.KindForbidden, "not-your-player", "Not your player",
				"Only his team can take a player off the trade block.")
		}
		st.Block = slices.DeleteFunc(st.Block, func(b league.BlockEntry) bool { return b.PlayerID == playerID })
		return t.ID, nil
	})
	return err
}

// own checks the actor's team and that the league plays.
func own(st *league.State, actor string) (league.Team, error) {
	t, err := league.OwnTeam(st.League, actor)
	if err != nil {
		return t, err
	}
	if st.League.Status != league.StatusInSeason {
		return t, league.ErrSeasonNotStarted
	}
	return t, nil
}

// change runs fn in one transaction on the league's State and, when fn
// names a team, announces the change after commit.
func (s *Service) change(ctx context.Context, leagueID string, fn func(st *league.State, now time.Time) (string, error)) (*league.State, error) {
	var (
		out  *league.State
		team string
	)
	err := s.repo.Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return league.ErrLeagueNotFound
		}
		if team, err = fn(st, s.now()); err != nil {
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
	if team != "" {
		s.notify.Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeTradeBlock, TeamID: team})
	}
	return out, nil
}
