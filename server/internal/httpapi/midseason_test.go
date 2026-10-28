package httpapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// midSeasonLeague creates a four-team league of the demo managers and
// drafts it at the fixture's clock (after gameweek 8's deadline, while the
// season's current gameweek is 7), all on autopick. It returns the league
// id and the gameweek the league starts scoring in.
func midSeasonLeague(t *testing.T, f demoFixture) (string, int) {
	t.Helper()
	ctx := context.Background()
	commish := f.users[0]
	l, err := f.srv.leagues.Create(ctx, commish, "Late Kickoff", 4, "Late Runners", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := f.srv.leagues.CreateInvite(ctx, commish, l.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range f.users[1:4] {
		if _, _, err := f.srv.leagues.AcceptInvite(ctx, u, inv.Code, fmt.Sprintf("Late %d", i+2)); err != nil {
			t.Fatal(err)
		}
	}
	d := f.srv.drafts()
	if _, err := d.Start(ctx, commish, l.ID); err != nil {
		t.Fatal(err)
	}
	for _, u := range f.users[:4] {
		if _, err := d.SetAutopick(ctx, u, l.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	w, err := f.srv.world(ctx)
	if err != nil {
		t.Fatal(err)
	}
	first := w.UpcomingGW(f.srv.now())
	if w.Status.Current >= first {
		t.Fatalf("fixture is not mid-season: current %d, upcoming %d", w.Status.Current, first)
	}
	return l.ID, first
}

// Every view of a league drafted mid-season starts at its first gameweek:
// the home's current gameweek and matchups, the hub's current matchup, the
// schedule, the settings and the Status page. Nothing shows a gameweek from
// before the league existed (ADR 0010).
func TestMidSeasonLeagueViewsStartAtFirstGameweek(t *testing.T) {
	f := newDemoFixture(t)
	id, first := midSeasonLeague(t, f)

	var home LeagueHomeResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+id, &home); rec.Code != 200 {
		t.Fatalf("home = %d %s", rec.Code, rec.Body)
	}
	if got := home.Data.CurrentGameweek.Number; got != first {
		t.Errorf("home currentGameweek = %d, want %d", got, first)
	}
	if got := home.Data.Settings.StartGameweek; got != first {
		t.Errorf("settings.startGameweek = %d, want %d", got, first)
	}
	if len(home.Data.Matchups) != 2 {
		t.Errorf("home has %d matchups, want 2", len(home.Data.Matchups))
	}
	for _, m := range home.Data.Matchups {
		if m.Gameweek != first || m.State != ScoreStateUpcoming {
			t.Errorf("home matchup gw %d state %s", m.Gameweek, m.State)
		}
	}

	var mine MyLeagueListResponse
	if rec := f.get(t, f.token, "/v1/me/leagues", &mine); rec.Code != 200 {
		t.Fatalf("my leagues = %d %s", rec.Code, rec.Body)
	}
	found := false
	for _, ml := range mine.Data {
		if ml.League.Id != id {
			continue
		}
		found = true
		if ml.CurrentMatchup == nil || ml.CurrentMatchup.Gameweek != first {
			t.Errorf("hub current matchup = %+v, want gameweek %d", ml.CurrentMatchup, first)
		}
	}
	if !found {
		t.Fatal("league missing from the hub")
	}

	var sched ScheduleResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+id+"/schedule", &sched); rec.Code != 200 {
		t.Fatalf("schedule = %d %s", rec.Code, rec.Body)
	}
	if len(sched.Data) == 0 || sched.Data[0].Gameweek != first {
		t.Errorf("schedule starts at %v, want %d", sched.Data, first)
	}

	var status LeagueStatusPageResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+id+"/status", &status); rec.Code != 200 {
		t.Fatalf("status = %d %s", rec.Code, rec.Body)
	}
	if status.Data.Gameweek != first {
		t.Errorf("status gameweek = %d, want %d", status.Data.Gameweek, first)
	}
}

// After the draft, the queue reads as empty (no 409): the draft room may
// still ask for it as the last pick lands.
func TestDraftQueueAfterCompletionIsEmpty(t *testing.T) {
	f := newDemoFixture(t)
	id, _ := midSeasonLeague(t, f)
	var q DraftQueueResponse
	rec := f.get(t, f.token, "/v1/leagues/"+id+"/draft/queue", &q)
	if rec.Code != 200 {
		t.Fatalf("queue after the draft = %d %s", rec.Code, rec.Body)
	}
	if len(q.Data.Players) != 0 || q.Data.Team.Name != "Late Runners" {
		t.Errorf("queue = %+v", q.Data)
	}
	// The demo league drafted long ago (no draft row until now): the same.
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/draft/queue", &q); rec.Code != 200 || len(q.Data.Players) != 0 {
		t.Errorf("demo league queue = %d %s", rec.Code, rec.Body)
	}
}
