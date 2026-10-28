package waivers

import (
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// gw6Results gives the fixture's league finished gameweek 6 results:
// team 0 10-90 team 1, team 2 50-40 team 3. Standings: 1 (3 pts, 90),
// 2 (3, 50), 3 (0, 40), 0 (0, 10), so reverse standings is 0, 3, 2, 1,
// while the order the draft left is 3, 2, 1, 0.
func gw6Results(t *testing.T, f *fixture) {
	t.Helper()
	err := f.e.Repo.Tx(f.e.Ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(f.e.Ctx, f.l.ID)
		if err != nil {
			return err
		}
		st.NewMatchups = []league.Matchup{
			{ID: "mu_01k7m2xq8s00000000000000a1", Gameweek: 6, HomeTeamID: f.teams[0], AwayTeamID: f.teams[1],
				HomePoints: 10, AwayPoints: 90, State: scoring.StateFinal},
			{ID: "mu_01k7m2xq8s00000000000000a2", Gameweek: 6, HomeTeamID: f.teams[2], AwayTeamID: f.teams[3],
				HomePoints: 50, AwayPoints: 40, State: scoring.StateFinal},
		}
		return tx.SaveState(f.e.Ctx, st)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func setOrder(f *fixture, actor, order string) error {
	_, err := f.e.Leagues.Update(f.e.Ctx, actor, f.l.ID, nil, league.SettingsInput{WaiverOrder: &order})
	return err
}

// The commissioner switches the rule between gameweeks; it applies from
// the next waiver run, and not while a run is due.
func TestSwitchWaiverOrder(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		f := newFixture(t, e)
		gw6Results(t, f)
		tm := f.teams
		reverse := []string{tm[0], tm[3], tm[2], tm[1]}
		order := func() (string, []string) {
			st := e.State(t, f.l.ID)
			gw, _ := league.NextWaiverRun(st, f.w, e.Clock.Now())
			return st.League.Settings.WaiverRule(), league.WaiverOrder(st, f.w, gw)
		}
		if rule, got := order(); rule != league.WaiverReverseStandings || !slices.Equal(got, reverse) {
			t.Fatalf("new league: %s %v, want reverse standings %v", rule, got, reverse)
		}
		if err := setOrder(f, f.users[1], league.WaiverRolling); slug(err) != "not-commissioner" {
			t.Errorf("member: %v", err)
		}
		if err := setOrder(f, f.users[0], "fifo"); slug(err) != "invalid-request" {
			t.Errorf("unknown rule: %v", err)
		}

		// Gameweek 8's run is due: no change until it has run.
		g8 := f.w.Gameweeks[8]
		e.Clock.Set(g8.WaiverDeadlineAt)
		if err := setOrder(f, f.users[0], league.WaiverRolling); slug(err) != "waivers-processing" {
			t.Errorf("while the run is due: %v", err)
		}
		e.Clock.Set(g8.WaiverDeadlineAt.Add(-time.Hour))

		// To rolling: it starts from the order the next run would have used.
		if err := setOrder(f, f.users[0], league.WaiverRolling); err != nil {
			t.Fatal(err)
		}
		if rule, got := order(); rule != league.WaiverRolling || !slices.Equal(got, reverse) {
			t.Fatalf("rolling: %s %v, want %v", rule, got, reverse)
		}
		// Team 2 wins a claim at gameweek 8's run and stays at the back.
		add := f.free(t, scoring.DEF, 1)[0]
		drop := f.rostered(t, 2, scoring.DEF, 8)[0]
		if _, _, err := f.s.CreateClaim(e.Ctx, f.users[2], f.l.ID, add, drop); err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(g8.WaiverDeadlineAt)
		if ran, err := f.s.RunDue(e.Ctx, f.l.ID); err != nil || !ran {
			t.Fatalf("run: %v %v", ran, err)
		}
		rolled := []string{tm[0], tm[3], tm[1], tm[2]}
		if _, got := order(); !slices.Equal(got, rolled) {
			t.Fatalf("after the run: %v, want %v", got, rolled)
		}

		// Back to reverse standings once the run is done: the standings
		// decide again, whatever the rolling order was.
		if err := setOrder(f, f.users[0], league.WaiverReverseStandings); err != nil {
			t.Fatal(err)
		}
		if rule, got := order(); rule != league.WaiverReverseStandings || !slices.Equal(got, reverse) {
			t.Fatalf("reverse again: %s %v, want %v", rule, got, reverse)
		}
	})
}

// Before the draft the rule changes freely.
func TestWaiverOrderBeforeTheDraft(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		rolling := league.WaiverRolling
		l, users := e.League(t, 2, league.SettingsInput{WaiverOrder: &rolling})
		if l.Settings.WaiverOrder != league.WaiverRolling {
			t.Fatalf("created with %q", l.Settings.WaiverOrder)
		}
		reverse := league.WaiverReverseStandings
		l, err := e.Leagues.Update(e.Ctx, users[0], l.ID, nil, league.SettingsInput{WaiverOrder: &reverse})
		if err != nil || l.Settings.WaiverOrder != league.WaiverReverseStandings {
			t.Fatalf("update: %+v %v", l, err)
		}
		if got, _ := e.Repo.Get(e.Ctx, l.ID); got.Settings.WaiverOrder != league.WaiverReverseStandings {
			t.Errorf("stored %q", got.Settings.WaiverOrder)
		}
	})
}
