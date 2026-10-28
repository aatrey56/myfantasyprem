package httpapi

import (
	"net/http"
	"slices"
	"testing"
	"time"
)

func watchIDs(r WatchlistResponse) []string {
	out := []string{}
	for _, e := range r.Data {
		out = append(out, e.Player.Id)
	}
	return out
}

func listIDs(r PlayerListResponse) []string {
	out := []string{}
	for _, it := range r.Data {
		out = append(out, it.Player.Id)
	}
	return out
}

// The watchlist is per member and league, newest first, idempotent, and
// filters the players list.
func TestWatchlist(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		base := "/v1/leagues/" + f.league
		var top PlayerListResponse
		f.get(t, f.token, base+"/players?status=all&sort=draft_rank&limit=3", &top)
		a, b, c := top.Data[0].Player.Id, top.Data[1].Player.Id, top.Data[2].Player.Id

		for _, id := range []string{a, b, c} {
			rec := doAs(f.h, f.token, http.MethodPut, base+"/watchlist/"+id, "")
			if rec.Code != http.StatusOK {
				t.Fatalf("watch %s = %d %s", id, rec.Code, rec.Body)
			}
			f.clock.t = f.clock.t.Add(time.Minute)
		}
		// Watching again keeps the entry where it was.
		if rec := doAs(f.h, f.token, http.MethodPut, base+"/watchlist/"+a, ""); rec.Code != http.StatusOK {
			t.Fatalf("watch again = %d", rec.Code)
		}
		var mine WatchlistResponse
		f.get(t, f.token, base+"/watchlist", &mine)
		if got, want := watchIDs(mine), []string{c, b, a}; !slices.Equal(got, want) {
			t.Fatalf("watchlist = %v, want newest first %v", got, want)
		}
		// Another manager's list is their own.
		var theirs WatchlistResponse
		f.get(t, f.other, base+"/watchlist", &theirs)
		if len(theirs.Data) != 0 {
			t.Fatalf("other manager sees %v", watchIDs(theirs))
		}
		// The players list filters to it, in the requested order.
		var watched PlayerListResponse
		f.get(t, f.token, base+"/players?status=all&watchlist=true&sort=draft_rank", &watched)
		if got, want := listIDs(watched), []string{a, b, c}; !slices.Equal(got, want) {
			t.Fatalf("players?watchlist = %v, want %v", got, want)
		}
		var none PlayerListResponse
		f.get(t, f.other, base+"/players?watchlist=true", &none)
		if len(none.Data) != 0 {
			t.Fatalf("other manager's watchlist filter = %v", listIDs(none))
		}

		if rec := doAs(f.h, f.token, http.MethodDelete, base+"/watchlist/"+b, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("unwatch = %d", rec.Code)
		}
		if rec := doAs(f.h, f.token, http.MethodDelete, base+"/watchlist/"+b, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("unwatch again = %d", rec.Code)
		}
		f.get(t, f.token, base+"/watchlist", &mine)
		if got, want := watchIDs(mine), []string{c, a}; !slices.Equal(got, want) {
			t.Fatalf("after unwatch = %v, want %v", got, want)
		}
		decodeProblem(t, doAs(f.h, f.token, http.MethodPut, base+"/watchlist/"+tPlayer, ""), http.StatusNotFound)
	})
}

// Risers and fallers order by formChange, which is form minus the two
// gameweeks before; season stats add up from the scored rows.
func TestPlayersFormChangeAndSeasonStats(t *testing.T) {
	f := newDemoFixture(t)
	base := "/v1/leagues/" + f.league + "/players?status=all&limit=100"
	for _, sort := range []string{"form_rise", "form_fall"} {
		var page PlayerListResponse
		if rec := f.get(t, f.token, base+"&sort="+sort, &page); rec.Code != 200 {
			t.Fatalf("%s = %d %s", sort, rec.Code, rec.Body)
		}
		for i, it := range page.Data {
			if it.FormChange == nil {
				t.Fatalf("%s: %s has no form change at gameweek 7", sort, it.Player.Id)
			}
			if i == 0 {
				continue
			}
			prev := *page.Data[i-1].FormChange
			if (sort == "form_rise" && prev < *it.FormChange) || (sort == "form_fall" && prev > *it.FormChange) {
				t.Fatalf("%s out of order at %d: %v then %v", sort, i, prev, *it.FormChange)
			}
		}
	}
	// formChange is form minus the mean of gameweeks 3-4 (form is 5-6).
	var top PlayerListResponse
	f.get(t, f.token, base+"&sort=total_points", &top)
	it := top.Data[0]
	var detail PlayerDetailResponse
	f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+it.Player.Id, &detail)
	pts := map[int]int{}
	goals, assists := 0, 0
	for _, g := range detail.Data.Gameweeks {
		pts[g.Gameweek] = g.Points
		if g.Stats.GoalsScored != nil {
			goals += *g.Stats.GoalsScored
		}
		if g.Stats.Assists != nil {
			assists += *g.Stats.Assists
		}
	}
	prev := float64(pts[3]+pts[4]) / 2
	want := float32(roundTo(float64(*it.Form)-roundTo(prev, 1), 1))
	if *it.FormChange != want {
		t.Errorf("formChange = %v, want %v (form %v, gameweeks 3-4 %v)", *it.FormChange, want, *it.Form, pts)
	}
	if it.SeasonStats.Goals != goals || it.SeasonStats.Assists != assists {
		t.Errorf("season goals/assists = %d/%d, game log %d/%d", it.SeasonStats.Goals, it.SeasonStats.Assists, goals, assists)
	}
}

// roundTo rounds half away from zero to n decimals.
func roundTo(v float64, n int) float64 {
	p := 1.0
	for range n {
		p *= 10
	}
	if v < 0 {
		return -float64(int64(-v*p+0.5)) / p
	}
	return float64(int64(v*p+0.5)) / p
}
