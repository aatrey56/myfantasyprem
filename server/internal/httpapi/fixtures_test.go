package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

func TestGameweekFixtures(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		// Public: no token. The demo's gameweek 7 is live.
		var live GameweekFixtureListResponse
		if rec := f.get(t, "", "/v1/gameweeks/7/fixtures", &live); rec.Code != 200 {
			t.Fatalf("gameweek 7 = %d %s", rec.Code, rec.Body)
		}
		if len(live.Data) != 10 || live.Meta.ScoreState == nil || *live.Meta.ScoreState != ScoreStateLive {
			t.Fatalf("gameweek 7 = %d fixtures, meta %+v", len(live.Data), live.Meta)
		}
		statuses := map[FixtureStatus]int{}
		for i, fx := range live.Data {
			statuses[fx.Status]++
			if fx.Gameweek != 7 || !strings.HasPrefix(fx.FixtureId, "fx_") || fx.KickoffAt == nil ||
				fx.Home.Code == "" || fx.Away.Code == "" || fx.Home.Code == fx.Away.Code {
				t.Errorf("fixture %d = %+v", i, fx)
			}
			if fx.HomeScore == nil || fx.AwayScore == nil {
				t.Errorf("fixture %d (%s) has no score", i, fx.Status)
			}
			if i > 0 {
				prev := live.Data[i-1]
				if c := prev.KickoffAt.Compare(*fx.KickoffAt); c > 0 || (c == 0 && prev.FixtureId > fx.FixtureId) {
					t.Errorf("fixtures out of order at %d", i)
				}
			}
		}
		if statuses[FixtureStatusLive] == 0 || statuses[FixtureStatusFinished] == 0 {
			t.Errorf("statuses = %v", statuses)
		}

		var next GameweekFixtureListResponse
		if rec := f.get(t, f.token, "/v1/gameweeks/9/fixtures", &next); rec.Code != 200 {
			t.Fatalf("gameweek 9 = %d %s", rec.Code, rec.Body)
		}
		if len(next.Data) != 10 || next.Meta.ScoreState == nil || *next.Meta.ScoreState != ScoreStateUpcoming {
			t.Fatalf("gameweek 9 = %d fixtures, meta %+v", len(next.Data), next.Meta)
		}
		for i, fx := range next.Data {
			if fx.Status != FixtureStatusScheduled || fx.HomeScore != nil || fx.AwayScore != nil {
				t.Errorf("upcoming fixture %d = %+v", i, fx)
			}
		}

		decodeProblem(t, f.get(t, "", "/v1/gameweeks/39/fixtures", nil), http.StatusBadRequest)
		decodeProblem(t, f.get(t, "", "/v1/gameweeks/0/fixtures", nil), http.StatusBadRequest)
	})
}

func TestGameweekFixturesNoSeasonDataIs503(t *testing.T) {
	h := newTestHandler()
	p := decodeProblem(t, doAnon(h, http.MethodGet, "/v1/gameweeks/7/fixtures", ""), http.StatusServiceUnavailable)
	if problemType(p) != problemTypeBase+"season-data-unavailable" {
		t.Errorf("type = %s", problemType(p))
	}
}

// One gameweek only, postponed fixtures kept, by kickoff then id, and a
// fixture without a kickoff last.
func TestGameweekFixturesOrder(t *testing.T) {
	at := func(h int) *time.Time { v := time.Date(2026, 10, 17, h, 0, 0, 0, time.UTC); return &v }
	all := []stats.Fixture{
		{ID: "fx_e", Gameweek: 7},
		{ID: "fx_d", Gameweek: 7, KickoffAt: at(16)},
		{ID: "fx_c", Gameweek: 7, KickoffAt: at(14), Status: stats.FixturePostponed},
		{ID: "fx_b", Gameweek: 7, KickoffAt: at(14)},
		{ID: "fx_a", Gameweek: 8, KickoffAt: at(12)},
	}
	var ids []string
	for _, f := range gameweekFixtures(all, 7) {
		ids = append(ids, f.ID)
	}
	if want := []string{"fx_b", "fx_c", "fx_d", "fx_e"}; !slices.Equal(ids, want) {
		t.Errorf("order = %v, want %v", ids, want)
	}
}

// A live fixture carries its minute and period; every other one has none.
func TestGameweekFixturesClock(t *testing.T) {
	f := newDemoFixture(t)
	var live GameweekFixtureListResponse
	if rec := f.get(t, "", "/v1/gameweeks/7/fixtures", &live); rec.Code != 200 {
		t.Fatalf("gameweek 7 = %d %s", rec.Code, rec.Body)
	}
	clocks := 0
	for _, fx := range live.Data {
		if fx.Status != FixtureStatusLive {
			if fx.Minute != nil || fx.Period != nil {
				t.Errorf("%s fixture %s has a clock: %v %v", fx.Status, fx.FixtureId, fx.Minute, fx.Period)
			}
			continue
		}
		if fx.Minute != nil {
			clocks++
			if *fx.Minute < 1 || fx.Period == nil || !fx.Period.Valid() {
				t.Errorf("live fixture %s clock = %d %v", fx.FixtureId, *fx.Minute, fx.Period)
			}
		}
	}
	if clocks == 0 {
		t.Error("no live fixture has a minute")
	}
}

func TestMatchClock(t *testing.T) {
	kickoff := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return kickoff.Add(time.Duration(m) * time.Minute) }
	for _, c := range []struct {
		minute int
		now    time.Time
		want   string // "" for no clock
	}{
		{0, at(3), ""},
		{12, at(13), "12 first_half"},
		{45, at(47), "45 first_half"}, // stoppage time
		{45, at(50), "45 first_half"},
		{45, at(51), "45 half_time"},
		{46, at(62), "46 second_half"},
		{90, at(110), "90 second_half"},
	} {
		m, p := matchClock(c.minute, &kickoff, c.now)
		got := ""
		if m != nil {
			got = fmt.Sprintf("%d %s", *m, *p)
		}
		if got != c.want {
			t.Errorf("matchClock(%d, +%v) = %q, want %q", c.minute, c.now.Sub(kickoff), got, c.want)
		}
	}
	if m, p := matchClock(45, nil, at(70)); *m != 45 || *p != FirstHalf {
		t.Errorf("no kickoff = %d %s", *m, *p)
	}
}

func TestFixtureDetail(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		fixtures, err := f.srv.stats.Fixtures(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		byStatus := map[string]stats.Fixture{}
		for _, fx := range fixtures {
			if _, ok := byStatus[fx.Status]; !ok && fx.Gameweek > 0 {
				byStatus[fx.Status] = fx
			}
		}
		owned := f.game.OwnerAt(7)

		for _, status := range []string{stats.FixtureLive, stats.FixtureFinished} {
			fx := byStatus[status]
			var got FixtureDetailResponse
			if rec := f.get(t, f.token, "/v1/fixtures/"+fx.ID, &got); rec.Code != 200 {
				t.Fatalf("%s fixture = %d %s", status, rec.Code, rec.Body)
			}
			d := got.Data
			if d.Fixture.FixtureId != fx.ID || string(d.Fixture.Status) != status || got.Meta.ScoreState == nil {
				t.Fatalf("%s fixture = %+v meta %+v", status, d.Fixture, got.Meta)
			}
			if d.Home.Club != d.Fixture.Home || d.Away.Club != d.Fixture.Away {
				t.Errorf("sides %v %v, fixture %v %v", d.Home.Club, d.Away.Club, d.Fixture.Home, d.Fixture.Away)
			}
			if len(d.Home.Players) == 0 || len(d.Away.Players) == 0 {
				t.Fatalf("%s fixture players %d + %d", status, len(d.Home.Players), len(d.Away.Players))
			}
			goals := map[string]int{"home": 0, "away": 0}
			for name, side := range map[string]FixtureSide{"home": d.Home, "away": d.Away} {
				for i, p := range side.Players {
					if p.Player.Club != side.Club {
						t.Errorf("%s: %s of %s listed for %s", status, p.Player.DisplayName, p.Player.Club.Code, side.Club.Code)
					}
					if p.Minutes == 0 && p.Stats.YellowCards == 0 && p.Stats.RedCards == 0 && len(p.Owners) == 0 {
						t.Errorf("%s: %s has not played and is not owned", status, p.Player.Id)
					}
					sum := 0
					for _, l := range p.Breakdown {
						sum += l.Points
						if l.Stat == PointsStatGoalsScored && l.Value != p.Stats.GoalsScored {
							t.Errorf("%s: goals line %d, stats %d", p.Player.Id, l.Value, p.Stats.GoalsScored)
						}
						if l.Stat == PointsStatBonus && l.Value != p.Stats.Bonus {
							t.Errorf("%s: bonus line %d, stats %d", p.Player.Id, l.Value, p.Stats.Bonus)
						}
					}
					if sum != p.Points {
						t.Errorf("%s: breakdown sums to %d, points %d", p.Player.Id, sum, p.Points)
					}
					if i > 0 && side.Players[i-1].Points < p.Points {
						t.Errorf("%s side out of order at %d", name, i)
					}
					// The demo user is in one league: one owner at most, and
					// "mine" exactly for their own team.
					if team, ok := owned[p.Player.Id]; ok {
						if len(p.Owners) != 1 || p.Owners[0].LeagueId != f.league || p.Owners[0].Team.Id != team ||
							p.Owners[0].Mine != (team == f.mine.ID) || p.Owners[0].LeagueName == "" {
							t.Errorf("%s owners = %+v, want team %s", p.Player.Id, p.Owners, team)
						}
					} else if len(p.Owners) != 0 {
						t.Errorf("unowned %s has owners %+v", p.Player.Id, p.Owners)
					}
					goals[name] += p.Stats.GoalsScored
				}
			}
			// Own goals aside, the scorers add up to the score.
			if goals["home"] > *d.Fixture.HomeScore || goals["away"] > *d.Fixture.AwayScore {
				t.Errorf("%s: scorers %v, score %d-%d", status, goals, *d.Fixture.HomeScore, *d.Fixture.AwayScore)
			}
		}

		// Before kickoff: no stats, only owned players of the two clubs.
		next := byStatus[stats.FixtureScheduled]
		var up FixtureDetailResponse
		if rec := f.get(t, f.token, "/v1/fixtures/"+next.ID, &up); rec.Code != 200 {
			t.Fatalf("scheduled fixture = %d %s", rec.Code, rec.Body)
		}
		for _, p := range append(up.Data.Home.Players, up.Data.Away.Players...) {
			if p.Minutes != 0 || p.Points != 0 || len(p.Owners) == 0 || len(p.Breakdown) != 0 {
				t.Errorf("scheduled fixture player %+v", p)
			}
		}
		if up.Data.Fixture.HomeScore != nil || up.Data.Fixture.Minute != nil {
			t.Errorf("scheduled fixture = %+v", up.Data.Fixture)
		}

		// Someone in no league sees the box score without owners.
		outsider := signIn(f.srv, "Nobody", "nobody@example.com")
		var bare FixtureDetailResponse
		if rec := f.get(t, outsider, "/v1/fixtures/"+byStatus[stats.FixtureLive].ID, &bare); rec.Code != 200 {
			t.Fatalf("outsider = %d %s", rec.Code, rec.Body)
		}
		for _, p := range append(bare.Data.Home.Players, bare.Data.Away.Players...) {
			if len(p.Owners) != 0 || p.Minutes == 0 && p.Stats.YellowCards == 0 && p.Stats.RedCards == 0 {
				t.Errorf("outsider sees %+v", p)
			}
		}

		decodeProblem(t, f.get(t, "", "/v1/fixtures/"+next.ID, nil), http.StatusUnauthorized)
		decodeProblem(t, f.get(t, f.token, "/v1/fixtures/fx_01k7m2xq8w0000000000000046", nil), http.StatusNotFound)
		decodeProblem(t, f.get(t, f.token, "/v1/fixtures/lg_01k7m2xq8w0000000000000046", nil), http.StatusBadRequest)
	})
}
