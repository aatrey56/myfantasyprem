package notifications

import (
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

func fourTeams() *league.League {
	return &league.League{ID: league1, Name: "Lads", Status: league.StatusInSeason, Teams: []league.Team{
		{ID: "tmA", Name: "A FC", OwnerID: "uA"}, {ID: "tmB", Name: "B FC", OwnerID: "uB"},
		{ID: "tmC", Name: "C FC", OwnerID: "uC"}, {ID: "tmD", Name: "D FC"},
	}}
}

func name(id string) string { return "P" + id }

// who lists "user:type" pairs.
func who(ns []Notification) []string {
	var out []string
	for _, n := range ns {
		out = append(out, n.UserID+":"+string(n.Type))
	}
	return out
}

func TestTradeEventsPerStatus(t *testing.T) {
	l := fourTeams()
	tr := league.Trade{ID: "trd_1", OfferingTeamID: "tmA", ReceivingTeamID: "tmB", Offered: []string{"1"}, Requested: []string{"2"}}
	cases := []struct {
		status, review string
		want           []string
	}{
		{league.TradeOffered, "", []string{"uB:trade_offered"}},
		{league.TradeAccepted, "", []string{"uA:trade_accepted"}},
		{league.TradeInReview, "commissioner", []string{"uA:trade_accepted"}},
		{league.TradeDeclined, "", []string{"uA:trade_declined"}},
		{league.TradeVetoed, "league_vote", []string{"uA:trade_accepted", "uA:trade_reviewed", "uB:trade_reviewed"}},
		{league.TradeWithdrawn, "", nil},
		// Processed: both sides, plus everyone else in the league (D is an open seat).
		{league.TradeProcessed, "", []string{"uA:trade_accepted", "uA:trade_processed", "uB:trade_processed", "uC:league_trade_processed"}},
		{league.TradeProcessed, "commissioner", []string{"uA:trade_accepted", "uA:trade_reviewed", "uB:trade_reviewed",
			"uA:trade_processed", "uB:trade_processed", "uC:league_trade_processed"}},
	}
	for _, c := range cases {
		tr.Status, tr.ReviewMode = c.status, c.review
		got := who(TradeEvents(l, tr, name))
		if !slices.Equal(got, c.want) {
			t.Errorf("%s/%s: %v, want %v", c.status, c.review, got, c.want)
		}
	}
	tr.Status = league.TradeOffered
	if b := TradeEvents(l, tr, name)[0].Body; b != "A FC offers P1 for P2." {
		t.Errorf("body %q", b)
	}
}

func TestTradeHeardTwiceSendsOnce(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	n := NewNotifier(NewMemory(c.now))
	tr := league.Trade{ID: "trd_1", OfferingTeamID: "tmA", ReceivingTeamID: "tmB", Status: league.TradeProcessed}
	// Accepted into review first: the proposer hears it then...
	tr.Status = league.TradeInReview
	if got := sendAll(t, n, TradeEvents(fourTeams(), tr, name)); len(got) != 1 {
		t.Fatalf("in review: %v", who(got))
	}
	// ...and processing does not repeat it.
	tr.Status = league.TradeProcessed
	if got := sendAll(t, n, TradeEvents(fourTeams(), tr, name)); len(got) != 3 {
		t.Fatalf("first: %v", who(got))
	}
	if got := sendAll(t, n, TradeEvents(fourTeams(), tr, name)); len(got) != 0 {
		t.Fatalf("again: %v", who(got))
	}
}

func TestWaiverEventsWonAndLostWithReason(t *testing.T) {
	claims := []league.Claim{
		{ID: "wv_1", TeamID: "tmA", AddPlayerID: "1", DropPlayerID: "9", Gameweek: 8, Status: league.ClaimWon},
		{ID: "wv_2", TeamID: "tmB", AddPlayerID: "1", Gameweek: 8, Status: league.ClaimLost},
		{ID: "wv_3", TeamID: "tmC", AddPlayerID: "3", DropPlayerID: "4", Gameweek: 8, Status: league.ClaimLost, FailureReason: league.ClaimOutUnavailable},
		{ID: "wv_4", TeamID: "tmC", AddPlayerID: "5", Gameweek: 8, Status: league.ClaimPending},
		{ID: "wv_5", TeamID: "tmC", AddPlayerID: "6", Gameweek: 7, Status: league.ClaimWon},
		{ID: "wv_6", TeamID: "tmD", AddPlayerID: "7", Gameweek: 8, Status: league.ClaimWon},
	}
	got := WaiverEvents(fourTeams(), claims, 8, name)
	var bodies []string
	for _, n := range got {
		bodies = append(bodies, n.UserID+": "+n.Body)
	}
	want := []string{
		"uA: You got P1 and dropped P9.",
		"uB: You missed out on P1: a team ahead of you in the waiver order claimed him.",
		"uC: You missed out on P3: the player to drop was no longer on your roster.",
	}
	if !slices.Equal(bodies, want) {
		t.Fatalf("got %q", bodies)
	}
}

func TestPickEventOnTheClock(t *testing.T) {
	l := fourTeams()
	d := &league.Draft{Status: league.DraftInProgress, Order: []string{"tmA", "tmB", "tmC"},
		Picks: make([]league.Pick, 3), Autopick: map[string]bool{}}
	// Pick 4 is round 2's first: the snake gives it to C.
	got := PickEvent(l, d)
	if len(got) != 1 || got[0].UserID != "uC" || got[0].Key != "pick:"+league1+":4" || got[0].Body != "Pick 2.1 in Lads is yours." {
		t.Fatalf("got %+v", got)
	}
	d.Autopick["tmC"] = true
	if got := PickEvent(l, d); got != nil {
		t.Fatalf("autopick team pinged: %+v", got)
	}
	d.Status = league.DraftPaused
	if got := PickEvent(l, d); got != nil {
		t.Fatalf("paused: %+v", got)
	}
}

func TestDeadlineEvents(t *testing.T) {
	waiverAt := time.Date(2026, 10, 23, 17, 30, 0, 0, time.UTC)
	deadline := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	w := &league.World{Gameweeks: map[int]stats.Gameweek{
		8: {Number: 8, Name: "Gameweek 8", WaiverDeadlineAt: waiverAt, DeadlineAt: deadline},
	}}
	l := fourTeams()
	full := func(prefix string) []string {
		var s []string
		for i := range 15 {
			s = append(s, prefix+string(rune('a'+i)))
		}
		return s
	}
	g := &league.Game{}
	for _, tm := range []string{"tmA", "tmB"} {
		for _, p := range full(tm) {
			g.Rosters = append(g.Rosters, league.RosterSlot{TeamID: tm, PlayerID: p, FromGW: 1})
		}
	}
	g.Lineups = []league.Lineup{
		{TeamID: "tmA", FromGW: 1, Slots: full("tmA")},
		// B's XI names a player B no longer has.
		{TeamID: "tmB", FromGW: 1, Slots: append([]string{"gone"}, full("tmB")[1:]...)},
		// C has no lineup at all.
	}
	st := &league.State{League: l, Game: g, Runs: map[league.RunKey]time.Time{}}

	at := func(now time.Time) []string { return who(DeadlineEvents(st, w, now)) }
	if got := at(waiverAt.Add(-61 * time.Minute)); got != nil {
		t.Errorf("61 min before waivers: %v", got)
	}
	if got := at(waiverAt.Add(-time.Hour)); !slices.Equal(got, []string{"uA:waivers_due", "uB:waivers_due", "uC:waivers_due"}) {
		t.Errorf("60 min before waivers: %v", got)
	}
	// The run happened: free agency is open until the deadline.
	st.Runs[league.RunKey{Kind: league.RunWaivers, Gameweek: 8}] = waiverAt
	got := at(deadline.Add(-30 * time.Minute))
	want := []string{"uA:free_agency_closing", "uB:free_agency_closing", "uC:free_agency_closing",
		"uB:lineup_invalid", "uC:lineup_invalid"}
	if !slices.Equal(got, want) {
		t.Errorf("30 min before the deadline: %v, want %v", got, want)
	}
	if got := at(deadline); got != nil {
		t.Errorf("at the deadline (next gameweek unknown): %v", got)
	}
	// A pre-draft league hears about its draft.
	starts := time.Date(2026, 8, 20, 19, 0, 0, 0, time.UTC)
	pre := &league.League{ID: league1, Status: league.StatusPreDraft, Teams: l.Teams,
		Settings: league.Settings{DraftStartsAt: &starts}}
	if got := who(DeadlineEvents(&league.State{League: pre}, w, starts.Add(-10*time.Minute))); len(got) != 3 || got[0] != "uA:draft_starting" {
		t.Errorf("draft starting: %v", got)
	}
}

func TestNewsChangesNotifyOwnerAndOpponentOnce(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	n := NewNotifier(NewMemory(c.now))
	l := fourTeams()
	g := &league.Game{
		Rosters:  []league.RosterSlot{{TeamID: "tmA", PlayerID: plr, FromGW: 1}},
		Matchups: []league.Matchup{{Gameweek: 8, HomeTeamID: "tmB", AwayTeamID: "tmA"}},
	}
	before := map[string]stats.Player{plr: {ID: plr, DisplayName: "Saka", Availability: "available"},
		"other": {ID: "other", Availability: "available"}}
	after := map[string]stats.Player{plr: {ID: plr, DisplayName: "Saka", Availability: "doubtful", News: "Knock - 75% chance"},
		"other": {ID: "other", Availability: "available"}, "new": {ID: "new", Availability: "injured"}}
	changed := NewsChanged(before, after)
	if len(changed) != 1 || changed[0].ID != plr {
		t.Fatalf("changed %+v", changed)
	}
	got := sendAll(t, n, NewsEvents(l, g, 8, changed))
	if !slices.Equal(who(got), []string{"uA:player_news", "uB:opponent_news"}) || got[0].Body != "Knock - 75% chance" {
		t.Fatalf("got %v", who(got))
	}
	if again := sendAll(t, n, NewsEvents(l, g, 8, changed)); len(again) != 0 {
		t.Fatalf("same news twice: %v", who(again))
	}
}
