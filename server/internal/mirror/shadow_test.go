package mirror_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// A hand-built two-team league in gameweek 9 (Sunday 25 October 2026):
// one match, Arsenal v Burnley, Saturday 14:00. Every player plays 90
// minutes for 2 points except a1, who has 10: Ash 30, Birch 22.
type shadowCase struct {
	fixture             string // the match's status
	provisional         bool   // bonus still projected
	bonusAdded, checked bool
	fplFinished         bool // FPL's H2H match is finished
	fplHome, fplAway    int  // FPL's match points
	ourClaim            string
	fplApproved         bool
	live                map[string]int // FPL live points that differ from 2/10
}

var shadowNow = time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)

func positions() []scoring.Position {
	return []scoring.Position{scoring.GKP, scoring.DEF, scoring.DEF, scoring.DEF, scoring.DEF, scoring.MID, scoring.MID,
		scoring.MID, scoring.MID, scoring.FWD, scoring.FWD, scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD}
}

func (c shadowCase) input(t *testing.T) mirror.ShadowInput {
	t.Helper()
	ko := time.Date(2026, 10, 24, 14, 0, 0, 0, time.UTC)
	w := &league.World{
		Clubs:   map[string]stats.Club{"c1": {ID: "c1"}, "c2": {ID: "c2"}},
		Players: map[string]stats.Player{},
		Gameweeks: map[int]stats.Gameweek{
			9:  {Number: 9, DeadlineAt: time.Date(2026, 10, 23, 18, 30, 0, 0, time.UTC), Finished: c.fixture == stats.FixtureFinished, BonusAdded: c.bonusAdded, DataChecked: c.checked},
			10: {Number: 10, DeadlineAt: time.Date(2026, 10, 30, 18, 30, 0, 0, time.UTC)},
		},
		Fixtures: []stats.Fixture{{ID: "fx1", Gameweek: 9, HomeClubID: "c1", AwayClubID: "c2", KickoffAt: &ko, Status: c.fixture}},
		Rows:     map[int]map[string][]stats.PlayerFixture{9: {}},
	}
	w.IndexFixtures()
	l := &league.League{ID: "lg_1", Name: "Shadow", Settings: league.DefaultSettings(2), Source: league.SourceFPL, FPLLeagueID: 1,
		Teams: []league.Team{{ID: "tA", Name: "Ash"}, {ID: "tB", Name: "Birch"}}}
	l.Settings.WaiverOrder = league.WaiverRolling
	g := &league.Game{}
	plan := &mirror.Plan{Settings: l.Settings, Seats: []mirror.Seat{{ExternalID: "1"}, {ExternalID: "2"}}}
	plan.Settings.StartGameweek = 9
	for _, side := range []struct{ team, seat, club, prefix string }{{"tA", "1", "c1", "a"}, {"tB", "2", "c2", "b"}} {
		var slots []string
		for i, pos := range positions() {
			id := fmt.Sprintf("%s%d", side.prefix, i+1)
			slots = append(slots, id)
			w.Players[id] = stats.Player{ID: id, DisplayName: strings.ToUpper(id), Position: pos, ClubID: side.club}
			pts := 2
			if id == "a1" {
				pts = 10
			}
			w.Rows[9][id] = []stats.PlayerFixture{{PlayerID: id, FixtureID: "fx1", Gameweek: 9, Points: pts,
				Stats: scoring.FixtureStats{Minutes: 90}, BonusProvisional: c.provisional}}
			g.Rosters = append(g.Rosters, league.RosterSlot{TeamID: side.team, PlayerID: id, FromGW: 1, AcquiredVia: "draft"})
		}
		g.Lineups = append(g.Lineups, league.Lineup{TeamID: side.team, FromGW: 9, Slots: slots})
		plan.Lineups = append(plan.Lineups, mirror.PlanLineup{Seat: side.seat, Gameweek: 9, Slots: slots})
	}
	// Our stored score is ours once the match is over (the rescore wrote it).
	mu := league.Matchup{ID: "mu1", Gameweek: 9, HomeTeamID: "tA", AwayTeamID: "tB"}
	if c.fixture == stats.FixtureFinished {
		mu.HomePoints, mu.AwayPoints = 30, 22
	}
	g.Matchups = []league.Matchup{mu}
	plan.Matches = []mirror.PlanMatch{{Key: "m9", Gameweek: 9, Home: "1", Away: "2", HomePts: c.fplHome, AwayPts: c.fplAway, Finished: c.fplFinished}}

	st := &league.State{League: l, Game: g, Priority: []string{"tA", "tB"}, Refs: map[league.RefKey]string{
		{Entity: league.RefTeam, Provider: mirror.Provider, ExternalID: "1"}:   "tA",
		{Entity: league.RefTeam, Provider: mirror.Provider, ExternalID: "2"}:   "tB",
		{Entity: league.RefMatch, Provider: mirror.Provider, ExternalID: "m9"}: "mu1",
	}}
	if c.ourClaim != "" {
		st.Claims = []league.Claim{{ID: "clm_1", TeamID: "tA", AddPlayerID: "fa1", DropPlayerID: "a15", Order: 1, Gameweek: 9, Status: c.ourClaim}}
		plan.Txns = []mirror.PlanTxn{{ExternalID: "77", Kind: league.TxnWaiver, Gameweek: 9, Seat: "1", In: "fa1", Out: "a15",
			Approved: c.fplApproved, Priority: 1}}
	}
	gw, _ := league.NextWaiverRun(st, w, shadowNow)
	for _, id := range league.WaiverOrder(st, w, gw) {
		plan.Priority = append(plan.Priority, map[string]string{"tA": "1", "tB": "2"}[id])
	}

	// FPL's table from its own match points.
	snap := &mirror.Snapshot{Details: &provider.LeagueDetails{}}
	if c.fplFinished {
		h, a := provider.LeagueStanding{LeagueEntry: "1"}, provider.LeagueStanding{LeagueEntry: "2"}
		h.PointsFor, h.PointsAgainst, a.PointsFor, a.PointsAgainst = c.fplHome, c.fplAway, c.fplAway, c.fplHome
		switch {
		case c.fplHome > c.fplAway:
			h.Wins, h.Total, a.Losses, h.Rank, a.Rank = 1, 3, 1, 1, 2
		case c.fplHome < c.fplAway:
			a.Wins, a.Total, h.Losses, a.Rank, h.Rank = 1, 3, 1, 1, 2
		default:
			h.Draws, a.Draws, h.Total, a.Total, h.Rank, a.Rank = 1, 1, 1, 1, 1, 2
		}
		snap.Details.Standings = []provider.LeagueStanding{h, a}
	} else {
		snap.Details.Standings = []provider.LeagueStanding{{LeagueEntry: "1", Rank: 1}, {LeagueEntry: "2", Rank: 2}}
	}

	live := map[string]int{}
	for id := range w.Players {
		live[id] = 2
	}
	live["a1"] = 10
	for id, p := range c.live {
		live[id] = p
	}
	return mirror.ShadowInput{Snap: snap, Plan: plan, State: st, World: w, Live: live, LiveGW: 9, Reason: mirror.ReasonBonus, Now: shadowNow}
}

func byKey(r mirror.ShadowReport) map[string]mirror.Item {
	out := map[string]mirror.Item{}
	for _, it := range r.Items {
		out[it.Key] = it
	}
	return out
}

func notOK(r mirror.ShadowReport) []string {
	var out []string
	for _, it := range r.Items {
		if it.Status != mirror.ItemOK {
			out = append(out, it.Key+"="+it.Status+" ("+it.Ours+" vs "+it.Theirs+")")
		}
	}
	return out
}

var finalCase = shadowCase{fixture: stats.FixtureFinished, bonusAdded: true, checked: true, fplFinished: true, fplHome: 30, fplAway: 22}

func TestShadowIdentical(t *testing.T) {
	c := finalCase
	c.ourClaim, c.fplApproved = league.ClaimWon, true
	r := mirror.Shadow(c.input(t))
	if r.Status != mirror.ReportClean || len(r.Mismatches()) != 0 {
		t.Fatalf("status %s, failing %v", r.Status, notOK(r))
	}
	items := byKey(r)
	for _, k := range []string{"points:gw9:tA", "result:gw9:mu1", "standing:tA", "lineup:gw9:tA", "player_points:gw9:a1", "waiver:gw9:tA:fa1:a15"} {
		if it, ok := items[k]; !ok || it.Status != mirror.ItemOK {
			t.Errorf("%s = %+v", k, it)
		}
	}
	if r.Gameweek != 9 || r.Reason != mirror.ReasonBonus || !r.CheckedAt.Equal(shadowNow) {
		t.Errorf("report = %+v", r)
	}
}

func TestShadowWaiverOutcome(t *testing.T) {
	c := finalCase
	c.ourClaim, c.fplApproved = league.ClaimLost, true // FPL gave Ash fa1; we did not
	r := mirror.Shadow(c.input(t))
	ms := r.Mismatches()
	if r.Status != mirror.ReportMismatch || len(ms) != 1 {
		t.Fatalf("mismatches %v", notOK(r))
	}
	it := ms[0]
	if it.Key != "waiver:gw9:tA:fa1:a15" || it.Kind != mirror.KindWaiver || it.TeamID != "tA" || it.Ours != "lost (order 1)" ||
		it.Theirs != "won (priority 1)" || it.Note != "fa1 for A15, Ash." {
		t.Errorf("waiver item = %+v", it)
	}
}

// While the match is live FPL is a poll behind: 28 for Ash and 1 for a2
// are pending, not mismatches. After full time and bonus FPL agrees and
// the same check is clean.
func TestShadowLiveLagResolves(t *testing.T) {
	live := shadowCase{fixture: stats.FixtureLive, provisional: true, fplHome: 28, fplAway: 22, live: map[string]int{"a2": 1}}
	r := mirror.Shadow(live.input(t))
	if len(r.Mismatches()) != 0 || r.Status != mirror.ReportIncomplete {
		t.Fatalf("live: status %s, failing %v", r.Status, notOK(r))
	}
	items := byKey(r)
	if it := items["points:gw9:tA"]; it.Status != mirror.ItemPending || it.Ours != "30" || it.Theirs != "28" {
		t.Errorf("live points = %+v", it)
	}
	if it := items["player_points:gw9:a2"]; it.Status != mirror.ItemPending || it.Ours != "2" || it.Theirs != "1" {
		t.Errorf("live player = %+v", it)
	}
	if it := items["points:gw9:tB"]; it.Status != mirror.ItemOK {
		t.Errorf("Birch = %+v", it)
	}

	// Full time, bonus not confirmed: still pending.
	ft := live
	ft.fixture = stats.FixtureFinishedProvisional
	if r := mirror.Shadow(ft.input(t)); len(r.Mismatches()) != 0 {
		t.Errorf("full time, provisional bonus: %v", notOK(r))
	}

	r = mirror.Shadow(finalCase.input(t))
	if r.Status != mirror.ReportClean {
		t.Errorf("after bonus: %v", notOK(r))
	}
}

// After bonus FPL still gives a2 3 points and Ash 31: a real mismatch,
// with the player, team and numbers.
func TestShadowFinalMismatch(t *testing.T) {
	c := finalCase
	c.fplHome, c.live = 31, map[string]int{"a2": 3}
	r := mirror.Shadow(c.input(t))
	if r.Status != mirror.ReportMismatch {
		t.Fatalf("status %s", r.Status)
	}
	items := byKey(r)
	if it := items["player_points:gw9:a2"]; it.Status != mirror.ItemMismatch || it.TeamID != "tA" || it.Ours != "2" || it.Theirs != "3" ||
		it.Note != "A2: ours 2, FPL 3." {
		t.Errorf("player = %+v", it)
	}
	if it := items["points:gw9:tA"]; it.Status != mirror.ItemMismatch || it.Ours != "30" || it.Theirs != "31" {
		t.Errorf("team = %+v", it)
	}
	if it := items["result:gw9:mu1"]; it.Status != mirror.ItemMismatch || it.Ours != "30-22" || it.Theirs != "31-22" {
		t.Errorf("result = %+v", it)
	}
	if it := items["standing:tA"]; it.Status != mirror.ItemMismatch {
		t.Errorf("standing = %+v", it)
	}
	if it := items["player_points:gw9:a3"]; it.Status != mirror.ItemOK {
		t.Errorf("a3 = %+v", it)
	}
}
