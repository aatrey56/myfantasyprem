package lineup

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

func problem(err error) *league.Error {
	var e *league.Error
	if errors.As(err, &e) {
		return e
	}
	return nil
}

func slug(err error) string {
	if e := problem(err); e != nil {
		return e.Slug
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// threeFiveTwo reorders a default 4-4-2 lineup (GK, 4 DEF, 4 MID, 2 FWD,
// then GK, DEF, MID, FWD on the bench) into a 3-5-2 by swapping the fourth
// defender with the bench midfielder.
func threeFiveTwo(t *testing.T, e *leaguetest.Env, slots []string) ([]string, []string) {
	t.Helper()
	w := e.World(t)
	starters, bench := slices.Clone(slots[:11]), slices.Clone(slots[11:])
	for i, id := range bench {
		if w.Players[id].Position == scoring.MID {
			starters[4], bench[i] = bench[i], starters[4]
			return starters, bench
		}
	}
	t.Fatal("no midfielder on the bench")
	return nil, nil
}

func TestSetLineup(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.InSeason(t, 4, league.SettingsInput{})
		team := l.Teams[1].ID
		cur, _ := e.State(t, l.ID).Game.LineupAt(team, 8)
		starters, bench := threeFiveTwo(t, e, cur.Slots)

		if err := s.Set(e.Ctx, users[1], l.ID, team, 8, starters, bench); err != nil {
			t.Fatal(err)
		}
		g := e.State(t, l.ID).Game
		for _, gw := range []int{8, 9, 20} {
			lu, ok := g.LineupAt(team, gw)
			if !ok || !slices.Equal(lu.Slots, slices.Concat(starters, bench)) {
				t.Fatalf("gameweek %d lineup = %v", gw, lu.Slots)
			}
		}

		// A later gameweek can differ; setting an earlier one leaves it
		// alone and only fills the gameweeks before it.
		if err := s.Set(e.Ctx, users[1], l.ID, team, 10, cur.Slots[:11], cur.Slots[11:]); err != nil {
			t.Fatal(err)
		}
		if lu, _ := e.State(t, l.ID).Game.LineupAt(team, 10); !slices.Equal(lu.Slots, cur.Slots) {
			t.Fatalf("gameweek 10 = %v", lu.Slots)
		}
		if err := s.Set(e.Ctx, users[1], l.ID, team, 9, starters, bench); err != nil {
			t.Fatal(err)
		}
		if lu, _ := e.State(t, l.ID).Game.LineupAt(team, 10); !slices.Equal(lu.Slots, cur.Slots) {
			t.Fatalf("gameweek 10 after setting 9 = %v", lu.Slots)
		}

		// Every broken rule is listed.
		bad := slices.Clone(starters)
		bad[0], bad[10] = bench[0], bench[0] // two keepers... and a duplicate
		err := s.Set(e.Ctx, users[1], l.ID, team, 8, bad, bench)
		p := problem(err)
		if p == nil || p.Slug != "invalid-lineup" || p.Kind != league.KindInvalid {
			t.Fatalf("invalid lineup: %v", err)
		}
		var codes []string
		for _, fe := range p.Errors {
			codes = append(codes, fe.Code)
		}
		for _, want := range []string{"duplicate_player", "player_missing", "too_many_goalkeepers"} {
			if !slices.Contains(codes, want) {
				t.Errorf("errors %v lack %s", codes, want)
			}
		}

		if err := s.Set(e.Ctx, users[2], l.ID, team, 8, starters, bench); slug(err) != "not-team-owner" {
			t.Fatalf("other manager: %v", err)
		}
		if err := s.Set(e.Ctx, e.User(t), l.ID, team, 8, starters, bench); slug(err) != "not-a-member" {
			t.Fatalf("outsider: %v", err)
		}

		// The deadline locks the gameweek; the next one stays open.
		w := e.World(t)
		e.Clock.Set(w.Gameweeks[8].DeadlineAt)
		if err := s.Set(e.Ctx, users[1], l.ID, team, 8, cur.Slots[:11], cur.Slots[11:]); slug(err) != "lineup-locked" {
			t.Fatalf("after the deadline: %v", err)
		}
		e.Clock.Advance(-time.Second)
		if err := s.Set(e.Ctx, users[1], l.ID, team, 8, cur.Slots[:11], cur.Slots[11:]); err != nil {
			t.Fatalf("a second before the deadline: %v", err)
		}
		e.Clock.Advance(time.Hour)
		if err := s.Set(e.Ctx, users[1], l.ID, team, 9, cur.Slots[:11], cur.Slots[11:]); err != nil {
			t.Fatalf("next gameweek: %v", err)
		}
		// The locked gameweek kept the lineup in force at its deadline.
		if lu, _ := e.State(t, l.ID).Game.LineupAt(team, 8); !slices.Equal(lu.Slots, cur.Slots) {
			t.Fatalf("locked gameweek changed: %v", lu.Slots)
		}
	})
}

// Saving gameweek 9 and then gameweek 8 keeps both: each save replaces
// only its own gameweek.
func TestSaveLaterThenEarlier(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.InSeason(t, 4, league.SettingsInput{})
		team := l.Teams[1].ID
		cur, _ := e.State(t, l.ID).Game.LineupAt(team, 8)
		starters, bench := threeFiveTwo(t, e, cur.Slots)
		nine := slices.Concat(starters, bench)
		eight := slices.Clone(cur.Slots)
		eight[12], eight[13] = eight[13], eight[12]

		if err := s.Set(e.Ctx, users[1], l.ID, team, 9, starters, bench); err != nil {
			t.Fatal(err)
		}
		if err := s.Set(e.Ctx, users[1], l.ID, team, 8, eight[:11], eight[11:]); err != nil {
			t.Fatal(err)
		}
		g := e.State(t, l.ID).Game
		for gw, want := range map[int][]string{8: eight, 9: nine, 15: nine} {
			if lu, _ := g.LineupAt(team, gw); !slices.Equal(lu.Slots, want) {
				t.Errorf("gameweek %d = %v, want %v", gw, lu.Slots, want)
			}
		}
	})
}

func TestSetLineupBeforeTheDraft(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 4, league.SettingsInput{})
		if err := s.Set(e.Ctx, users[0], l.ID, l.Teams[0].ID, 8, nil, nil); slug(err) != "season-not-started" {
			t.Fatalf("before the draft: %v", err)
		}
	})
}
