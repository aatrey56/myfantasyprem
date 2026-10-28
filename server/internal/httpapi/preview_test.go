package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// A future gameweek's preview: the scheduled pairing, both squads as they
// stand with each player's fixtures in that gameweek, and no points.
func TestMatchupPreviewFutureGameweek(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		const gw = 12
		var mu string
		for _, m := range f.game.MatchupsIn(gw) {
			if m.HomeTeamID == f.mine.ID || m.AwayTeamID == f.mine.ID {
				mu = m.ID
			}
		}
		if mu == "" {
			t.Fatal("no matchup of mine in gameweek 12")
		}
		var got struct {
			Data MatchupPreview `json:"data"`
		}
		rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/12/matchups/"+mu+"/preview", &got)
		if rec.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rec.Code, rec.Body)
		}
		if got.Data.Id != mu || got.Data.Gameweek != gw {
			t.Fatalf("preview %s gw %d, want %s gw %d", got.Data.Id, got.Data.Gameweek, mu, gw)
		}
		sides := []PreviewSide{got.Data.Home, got.Data.Away}
		var mine *PreviewSide
		for i, s := range sides {
			if s.Kind != PreviewSideKindTeam {
				continue
			}
			if s.Team.Id == f.mine.ID {
				mine = &sides[i]
			}
			if len(s.Starters) != 11 || len(s.Bench) != 4 {
				t.Fatalf("%s: %d starters, %d bench", s.Team.Name, len(s.Starters), len(s.Bench))
			}
			if s.Formation == nil || *s.Formation == "" || s.LineupGameweek == nil {
				t.Fatalf("%s: no formation or lineup gameweek", s.Team.Name)
			}
			fixtures := 0
			for _, sl := range append(s.Starters, s.Bench...) {
				fixtures += len(sl.Fixtures)
				for _, fx := range sl.Fixtures {
					if fx.KickoffAt.IsZero() || fx.Opponent.Code == "" {
						t.Fatalf("fixture without kickoff or opponent: %+v", fx)
					}
				}
			}
			if fixtures == 0 {
				t.Fatalf("%s: no fixtures in gameweek %d", s.Team.Name, gw)
			}
		}
		if mine == nil {
			t.Fatal("my team is not in the preview")
		}
		// The other team's lineup for gameweek 12 is private until its deadline.
		for _, s := range sides {
			if s.Kind == PreviewSideKindTeam && s.Team.Id != f.mine.ID && *s.LineupGameweek >= gw {
				t.Fatalf("%s: lineup gameweek %d before its deadline", s.Team.Name, *s.LineupGameweek)
			}
		}
		// My own lineup is the one set for that gameweek.
		if *mine.LineupGameweek != gw {
			t.Fatalf("my lineup gameweek %d, want %d", *mine.LineupGameweek, gw)
		}
		roster := f.rosterOf(t, f.mine.ID)
		if len(roster) != 15 {
			t.Fatalf("roster %d", len(roster))
		}
		// The JSON carries no points at all.
		for _, key := range []string{`"points"`, `"totalPoints"`, `"minutes"`} {
			if strings.Contains(rec.Body.String(), key) {
				t.Fatalf("preview carries %s", key)
			}
		}
	})
}

func TestMatchupPreviewUnknownMatchup(t *testing.T) {
	f := newDemoFixture(t)
	rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/12/matchups/"+tMatchup+"/preview", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

// A league drafted mid-season has no locked lineup before its first
// gameweek: another team's side there is its roster in the default order,
// never a lineup from a gameweek before the league existed, and never its
// unlocked lineup for the first gameweek.
func TestMatchupPreviewMidSeasonFirstGameweek(t *testing.T) {
	f := newDemoFixture(t)
	id, first := midSeasonLeague(t, f)
	var sched ScheduleResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+id+"/schedule", &sched); rec.Code != 200 {
		t.Fatalf("schedule = %d %s", rec.Code, rec.Body)
	}
	if len(sched.Data) == 0 || sched.Data[0].Gameweek != first {
		t.Fatalf("schedule starts at %v, want %d", sched.Data, first)
	}
	others := 0
	for _, m := range sched.Data[0].Matchups {
		var got struct {
			Data MatchupPreview `json:"data"`
		}
		url := fmt.Sprintf("/v1/leagues/%s/gameweeks/%d/matchups/%s/preview", id, first, m.Id)
		if rec := f.get(t, f.token, url, &got); rec.Code != http.StatusOK {
			t.Fatalf("preview = %d %s", rec.Code, rec.Body)
		}
		for _, s := range []PreviewSide{got.Data.Home, got.Data.Away} {
			if s.Kind != PreviewSideKindTeam {
				continue
			}
			if len(s.Starters) != 11 || len(s.Bench) != 4 {
				t.Fatalf("%s: %d starters, %d bench", s.Team.Name, len(s.Starters), len(s.Bench))
			}
			if s.LineupGameweek != nil && *s.LineupGameweek < first {
				t.Errorf("%s: lineup from gameweek %d, before the league's first (%d)", s.Team.Name, *s.LineupGameweek, first)
			}
			if s.LineupGameweek == nil {
				others++
			}
		}
	}
	if others == 0 {
		t.Fatal("no other team's side in the first gameweek's previews")
	}
}

// Before the first deadline nothing has locked: no other team's preview may
// show the lineup it has set for gameweek 1, only the default order.
func TestMatchupPreviewBeforeFirstDeadline(t *testing.T) {
	f := newDemoFixture(t)
	f.clock.t = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	others := 0
	for _, m := range f.game.MatchupsIn(1) {
		var got struct {
			Data MatchupPreview `json:"data"`
		}
		url := "/v1/leagues/" + f.league + "/gameweeks/1/matchups/" + m.ID + "/preview"
		if rec := f.get(t, f.token, url, &got); rec.Code != http.StatusOK {
			t.Fatalf("preview = %d %s", rec.Code, rec.Body)
		}
		for _, s := range []PreviewSide{got.Data.Home, got.Data.Away} {
			if s.Kind != PreviewSideKindTeam || s.Team.Id == f.mine.ID {
				continue
			}
			others++
			if s.LineupGameweek != nil {
				t.Errorf("%s: lineup from gameweek %d shown before the first deadline", s.Team.Name, *s.LineupGameweek)
			}
		}
	}
	if others == 0 {
		t.Fatal("no other team's side in gameweek 1")
	}
}
