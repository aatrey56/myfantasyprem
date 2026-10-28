package notifications

import (
	"regexp"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// appRoute is every path a notification may open: each is a screen in the
// app (apps/app/src/app/(app)/leagues/[leagueId]/...; the app's
// notification-links test checks the same shapes resolve to route files).
// The old "/league/..." form was not a route: a tap opened "Page not found".
var appRoute = regexp.MustCompile(`^/leagues/[^/]+/(matchup|draft|team|players|players/trades|players/waivers|players/[^/]+)$`)

func TestEveryNotificationLinksToAnAppRoute(t *testing.T) {
	l := fourTeams()
	var all []Notification
	for _, status := range []string{league.TradeOffered, league.TradeDeclined, league.TradeProcessed} {
		tr := league.Trade{ID: "trd_1", OfferingTeamID: "tmA", ReceivingTeamID: "tmB", Status: status, ReviewMode: "commissioner"}
		all = append(all, TradeEvents(l, tr, name)...)
	}
	all = append(all, WaiverEvents(l, []league.Claim{
		{TeamID: "tmA", AddPlayerID: "1", Gameweek: 8, Status: league.ClaimWon},
		{TeamID: "tmB", AddPlayerID: "1", Gameweek: 8, Status: league.ClaimLost},
	}, 8, name)...)
	all = append(all, PickEvent(l, &league.Draft{Status: league.DraftInProgress, Order: []string{"tmA", "tmB"},
		Picks: make([]league.Pick, 1), Autopick: map[string]bool{}})...)

	waiverAt := time.Date(2026, 10, 23, 17, 30, 0, 0, time.UTC)
	deadline := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	w := &league.World{Gameweeks: map[int]stats.Gameweek{8: {Number: 8, WaiverDeadlineAt: waiverAt, DeadlineAt: deadline}}}
	st := &league.State{League: l, Game: &league.Game{}, Runs: map[league.RunKey]time.Time{}}
	all = append(all, DeadlineEvents(st, w, waiverAt.Add(-time.Hour))...)
	st.Runs[league.RunKey{Kind: league.RunWaivers, Gameweek: 8}] = waiverAt
	all = append(all, DeadlineEvents(st, w, deadline.Add(-30*time.Minute))...)
	starts := time.Date(2026, 8, 20, 19, 0, 0, 0, time.UTC)
	pre := &league.League{ID: league1, Status: league.StatusPreDraft, Teams: l.Teams, Settings: league.Settings{DraftStartsAt: &starts}}
	all = append(all, DeadlineEvents(&league.State{League: pre}, w, starts.Add(-10*time.Minute))...)

	g := &league.Game{
		Rosters:  []league.RosterSlot{{TeamID: "tmA", PlayerID: plr, FromGW: 1}},
		Matchups: []league.Matchup{{Gameweek: 8, HomeTeamID: "tmB", AwayTeamID: "tmA"}},
	}
	all = append(all, NewsEvents(l, g, 8, []stats.Player{{ID: plr, DisplayName: "Saka", Availability: "doubtful", News: "Knock"}})...)
	all = append(all, LiveEvents(PlayerMatch{PlayerID: plr, FixtureID: fx, Name: "Saka", Position: "MID", Minutes: 90,
		Goals: 1, Assists: 1, OwnGoals: 1, RedCards: 1, DCPoints: 2}, aud)...)

	seen := map[Type]bool{}
	for _, n := range all {
		seen[n.Type] = true
		if !appRoute.MatchString(n.URL) {
			t.Errorf("%s links to %q, not an app route", n.Type, n.URL)
		}
	}
	for _, info := range Catalog {
		// Clean sheets come from LiveEvents' same emit (and URL) at full time.
		if !seen[info.Type] && info.Type != PlayerCleanSheet && info.Type != OpponentCleanSheet {
			t.Errorf("no producer case for %s", info.Type)
		}
	}
}
