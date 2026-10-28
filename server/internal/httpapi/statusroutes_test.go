package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// The Status tab's routes (league status, gameweek awards, players of the
// week) and the player sheet get the same guards as every other league
// read: a live session, league membership, the per-IP rate limit and id
// validation (the last is in the contract cases).

// statusRoutes are the routes under test for the demo league; player is a
// player in the game.
func statusRoutes(leagueID, player string) []string {
	return []string{
		"/v1/leagues/" + leagueID + "/status",
		"/v1/leagues/" + leagueID + "/players/" + player,
		"/v1/gameweeks/6/awards",
		"/v1/awards/players-of-the-week",
	}
}

func anyPlayer(t *testing.T, f demoFixture) string {
	t.Helper()
	w, err := league.SeasonWorld(context.Background(), f.srv.stats)
	if err != nil {
		t.Fatal(err)
	}
	for id := range w.Players {
		return id
	}
	t.Fatal("no players")
	return ""
}

func TestStatusRoutesNeedALiveSession(t *testing.T) {
	f := newDemoFixture(t)
	routes := statusRoutes(f.league, anyPlayer(t, f))
	for _, r := range routes {
		decodeProblem(t, doAnon(f.h, http.MethodGet, r, ""), http.StatusUnauthorized)
		if rec := doAs(f.h, f.other, http.MethodGet, r, ""); rec.Code == http.StatusUnauthorized {
			t.Fatalf("%s before logout = %d", r, rec.Code)
		}
	}
	if rec := doAs(f.h, f.other, http.MethodPost, "/v1/auth/logout", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", rec.Code, rec.Body)
	}
	for _, r := range routes {
		decodeProblem(t, doAs(f.h, f.other, http.MethodGet, r, ""), http.StatusUnauthorized)
	}
}

// A manager who leaves the league loses its status page and player sheets
// at once, like every other league read.
func TestStatusRoutesNeedMembership(t *testing.T) {
	f := newDemoFixture(t)
	player := anyPlayer(t, f)
	status := "/v1/leagues/" + f.league + "/status"
	sheet := "/v1/leagues/" + f.league + "/players/" + player
	if rec := doAs(f.h, f.other, http.MethodGet, status, ""); rec.Code != http.StatusOK {
		t.Fatalf("member's status = %d %s", rec.Code, rec.Body)
	}
	if rec := doAs(f.h, f.other, http.MethodGet, sheet, ""); rec.Code != http.StatusOK {
		t.Fatalf("member's player sheet = %d %s", rec.Code, rec.Body)
	}
	if rec := doAs(f.h, f.other, http.MethodPost, "/v1/leagues/"+f.league+"/leave", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("leave = %d %s", rec.Code, rec.Body)
	}
	for _, r := range []string{status, sheet} {
		p := decodeProblem(t, doAs(f.h, f.other, http.MethodGet, r, ""), http.StatusForbidden)
		if problemType(p) != problemTypeBase+"not-a-member" {
			t.Errorf("%s after leaving: type = %s", r, problemType(p))
		}
	}
	// The awards are the whole game's, not a league's: any signed-in user.
	for _, r := range statusRoutes(f.league, player)[2:] {
		if rec := doAs(f.h, f.other, http.MethodGet, r, ""); rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
			t.Errorf("%s after leaving = %d", r, rec.Code)
		}
	}
}

// Every route counts against the per-IP limit and answers a contract-valid
// 429 over it.
func TestStatusRoutesRateLimited(t *testing.T) {
	f := newDemoFixture(t)
	l := DefaultRateLimits()
	l.Global = Limit{PerMinute: 60, Burst: 2}
	opts := testOptions()
	opts.RateLimits = &l
	h := newHandler(opts, newRoutes(opts, f.srv))
	for i, r := range statusRoutes(f.league, anyPlayer(t, f)) {
		ip := "203.0.113." + strconv.Itoa(10+i)
		ok, rec := hitUntil429(t, 10, func(int) *httptest.ResponseRecorder {
			return from(h, ip, f.token, "", http.MethodGet, r, "")
		})
		if ok != 2 {
			t.Errorf("%s: %d requests went through, want 2", r, ok)
		}
		check429(t, rec, httptest.NewRequest(http.MethodGet, r, nil))
	}
}
