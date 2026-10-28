package notifications

import (
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

const (
	owner = "usr_01k7m2xq8z00000000000000o1"
	opp   = "usr_01k7m2xq8z00000000000000p1"
	plr   = "plr_01k7m2xq8v0000000000000001"
	fx    = "fx_01k7m2xq8v00000000000000f1"
)

var aud = []Audience{{LeagueID: league1, OwnerID: owner, OpponentID: opp}}

func types(ns []Notification, user string) []Type {
	var out []Type
	for _, n := range ns {
		if n.UserID == user {
			out = append(out, n.Type)
		}
	}
	return out
}

// sendAll files events through a Notifier on s and returns the new ones.
func sendAll(t *testing.T, n *Notifier, ns []Notification) []Notification {
	t.Helper()
	out, err := n.Send(ctx, ns...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLiveGoalsDedupeAcrossRepollsAndCorrections(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	n := NewNotifier(NewMemory(c.now))
	p := PlayerMatch{PlayerID: plr, FixtureID: fx, Name: "Saka", Position: "MID", Minutes: 30, Goals: 1}

	got := sendAll(t, n, LiveEvents(p, aud))
	if !slices.Equal(types(got, owner), []Type{PlayerGoal}) || !slices.Equal(types(got, opp), []Type{OpponentGoal}) {
		t.Fatalf("first goal: owner %v opponent %v", types(got, owner), types(got, opp))
	}
	// Re-poll with the same stats: nothing new.
	if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 0 {
		t.Fatalf("re-poll sent %v", got)
	}
	// Correction takes the goal away: no "goal removed" push.
	p.Goals = 0
	if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 0 {
		t.Fatalf("correction sent %v", got)
	}
	// It is given back: still the same goal, nothing new.
	p.Goals = 1
	if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 0 {
		t.Fatalf("restored goal sent again: %v", got)
	}
	// A second goal and an assist arrive in one poll.
	p.Goals, p.Assists = 2, 1
	got = sendAll(t, n, LiveEvents(p, aud))
	if !slices.Equal(types(got, owner), []Type{PlayerGoal, PlayerAssist}) {
		t.Fatalf("brace + assist: %v", types(got, owner))
	}
}

func TestLiveDefensiveContributionOncePerMatch(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	n := NewNotifier(NewMemory(c.now))
	// A defender at 9 CBIT has no DC points yet: nothing.
	p := PlayerMatch{PlayerID: plr, FixtureID: fx, Name: "Gabriel", Position: "DEF", Minutes: 50}
	if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 0 {
		t.Fatalf("below threshold: %v", got)
	}
	// The 10th action gives 2 points: one push each.
	p.DCPoints = 2
	if got := sendAll(t, n, LiveEvents(p, aud)); !slices.Equal(types(got, owner), []Type{PlayerDC}) ||
		!slices.Equal(types(got, opp), []Type{OpponentDC}) {
		t.Fatalf("threshold: %v", got)
	}
	// More actions (14, still 2 points) and later polls: nothing more.
	for range 3 {
		if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 0 {
			t.Fatalf("DC again: %v", got)
		}
	}
	// Another match (a double gameweek) is a new once.
	p.FixtureID = "fx_01k7m2xq8v00000000000000f2"
	if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 2 {
		t.Fatalf("second fixture: %v", got)
	}
}

func TestCleanSheetRules(t *testing.T) {
	cases := []struct {
		name string
		p    PlayerMatch
		want bool
	}{
		{"live at 70 min, still on", PlayerMatch{Position: "DEF", Minutes: 70}, false},
		{"full time, 90 min, none conceded", PlayerMatch{Position: "DEF", Minutes: 90, Finished: true}, true},
		{"full time, conceded one", PlayerMatch{Position: "GKP", Minutes: 90, GoalsConceded: 1, Finished: true}, false},
		{"subbed off at 65 with it intact", PlayerMatch{Position: "MID", Minutes: 65, Off: true}, true},
		{"subbed off at 59", PlayerMatch{Position: "DEF", Minutes: 59, Off: true}, false},
		{"full time, 59 min", PlayerMatch{Position: "DEF", Minutes: 59, Finished: true}, false},
		{"forward gets no clean sheet", PlayerMatch{Position: "FWD", Minutes: 90, Finished: true}, false},
	}
	for _, c := range cases {
		if got := CleanSheet(c.p); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCleanSheetAfterSubOffSurvivesLaterGoal(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	n := NewNotifier(NewMemory(c.now))
	off := &OffTracker{Still: 5 * time.Minute}
	kick := c.t
	f := stats.Fixture{ID: fx, Status: stats.FixtureLive, KickoffAt: &kick}
	pl := stats.Player{ID: plr, DisplayName: "Saliba", Position: scoring.DEF}
	row := func(min, gc int) stats.PlayerFixture {
		return stats.PlayerFixture{PlayerID: plr, FixtureID: fx, Stats: scoring.FixtureStats{Minutes: min, GoalsConceded: gc}}
	}
	poll := func(r stats.PlayerFixture) []Notification {
		return sendAll(t, n, LiveEvents(Match(pl, r, f, off, c.t), aud))
	}
	// 70th minute, on the pitch.
	c.t = kick.Add(75 * time.Minute)
	if got := poll(row(70, 0)); len(got) != 0 {
		t.Fatalf("still on: %v", got)
	}
	// Minutes stuck at 72 for five minutes of play: he went off at 72.
	c.t = c.t.Add(2 * time.Minute)
	poll(row(72, 0))
	c.t = c.t.Add(5 * time.Minute)
	got := poll(row(72, 0))
	if !slices.Equal(types(got, owner), []Type{PlayerCleanSheet}) || !slices.Equal(types(got, opp), []Type{OpponentCleanSheet}) {
		t.Fatalf("subbed off after 60: %v", got)
	}
	// His team concedes after he went off: his goals conceded stay 0 (FPL
	// counts only goals while he was on), so nothing is taken back...
	c.t = c.t.Add(10 * time.Minute)
	if got := poll(row(72, 0)); len(got) != 0 {
		t.Fatalf("after the goal: %v", got)
	}
	// ...and full time sends nothing twice.
	f.Status = stats.FixtureFinished
	if got := poll(row(72, 0)); len(got) != 0 {
		t.Fatalf("full time: %v", got)
	}
}

func TestOffTrackerIgnoresStoppageTimeAndRedCards(t *testing.T) {
	t0 := time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name         string
		minutes, red int
		live         bool
		want         bool
	}{
		{"minutes stuck at 72", 72, 0, true, true},
		{"stoppage time at 90", 90, 0, true, false},
		{"sent off at 70", 70, 1, true, false},
		{"match over", 72, 0, false, false},
	} {
		o := &OffTracker{Still: 5 * time.Minute}
		o.Observe(plr, fx, c.minutes, c.red, c.live, t0)
		if got := o.Observe(plr, fx, c.minutes, c.red, c.live, t0.Add(6*time.Minute)); got != c.want {
			t.Errorf("%s: off %v, want %v", c.name, got, c.want)
		}
	}
	// Minutes that keep moving are never off.
	o := &OffTracker{Still: 5 * time.Minute}
	for i := range 10 {
		if o.Observe(plr, fx, 60+i, 0, true, t0.Add(time.Duration(i)*time.Minute)) {
			t.Fatalf("on the pitch at %d counted as off", 60+i)
		}
	}
}

func TestRedAndOwnGoalOnce(t *testing.T) {
	c := &clock{t: time.Date(2026, 10, 17, 15, 0, 0, 0, time.UTC)}
	n := NewNotifier(NewMemory(c.now))
	p := PlayerMatch{PlayerID: plr, FixtureID: fx, Name: "X", Position: "DEF", Minutes: 40, RedCards: 1, OwnGoals: 1}
	got := sendAll(t, n, LiveEvents(p, aud))
	if !slices.Equal(types(got, owner), []Type{PlayerOwnGoal, PlayerRedCard}) ||
		!slices.Equal(types(got, opp), []Type{OpponentOwnGoal, OpponentRedCard}) {
		t.Fatalf("got %v", got)
	}
	if got := sendAll(t, n, LiveEvents(p, aud)); len(got) != 0 {
		t.Fatalf("again: %v", got)
	}
}

func TestAudiencesResolveOpponentPerGameweek(t *testing.T) {
	l := &league.League{ID: league1, Teams: []league.Team{
		{ID: "tmA", OwnerID: "uA"}, {ID: "tmB", OwnerID: "uB"}, {ID: "tmC", OwnerID: "uC"}, {ID: "tmD"},
	}}
	g := &league.Game{
		// The player is A's in GW1-2 and C's from GW3 (a trade).
		Rosters: []league.RosterSlot{{TeamID: "tmA", PlayerID: plr, FromGW: 1, ToGW: 3}, {TeamID: "tmC", PlayerID: plr, FromGW: 3}},
		Matchups: []league.Matchup{
			{Gameweek: 1, HomeTeamID: "tmA", AwayTeamID: "tmB"},
			{Gameweek: 2, HomeTeamID: "tmC", AwayTeamID: "tmA"},
			{Gameweek: 3, HomeTeamID: "tmC", AwayTeamID: "tmD"}, // D is an open seat
			{Gameweek: 4, HomeTeamID: "tmC", AwayTeamID: ""},    // league average
		},
	}
	for _, c := range []struct {
		gw         int
		owner, opp string
	}{{1, "uA", "uB"}, {2, "uA", "uC"}, {3, "uC", ""}, {4, "uC", ""}} {
		a := Audiences(l, g, c.gw, plr)
		if len(a) != 1 || a[0].OwnerID != c.owner || a[0].OpponentID != c.opp {
			t.Errorf("GW%d: %+v, want owner %s opponent %q", c.gw, a, c.owner, c.opp)
		}
	}
	if a := Audiences(l, g, 1, "plr_other"); a != nil {
		t.Errorf("free agent has an audience: %+v", a)
	}
}

func TestPreferencesFilterPushNotInbox(t *testing.T) {
	impls(t, func(t *testing.T, s Store, c *clock) {
		n := NewNotifier(s)
		if _, err := s.Subscribe(ctx, Subscription{UserID: userA, Kind: KindWebPush, Endpoint: "https://push.example/a"}); err != nil {
			t.Fatal(err)
		}
		_ = s.SetPreferences(ctx, userA, league1, nil, map[Type]bool{PlayerGoal: false})
		ev := func(typ Type, key, lg string) Notification {
			return Notification{UserID: userA, LeagueID: lg, Type: typ, Title: "t", Key: key}
		}
		sendAll(t, n, []Notification{ev(PlayerGoal, "g1", league1), ev(PlayerAssist, "a1", league1), ev(PlayerGoal, "g2", league2)})
		due, _ := s.Due(ctx, c.t, time.Minute, 10)
		var pushed []string
		for _, d := range due {
			pushed = append(pushed, d.Notification.Key)
		}
		slices.Sort(pushed)
		if !slices.Equal(pushed, []string{"a1", "g2"}) {
			t.Fatalf("pushed %v, want a1 and g2 (goal off in league 1 only)", pushed)
		}
		if _, unread, _ := s.List(ctx, userA, 50, false); unread != 3 {
			t.Fatalf("inbox has %d, want all 3", unread)
		}
		// All off: inbox still fills, nothing pushes.
		_ = s.SetPreferences(ctx, userA, league1, ptr(false), nil)
		sendAll(t, n, []Notification{ev(TradeOffered, "t1", league1), ev(LeagueTradeProcessed, "t2", league1)})
		if due, _ := s.Due(ctx, c.t.Add(time.Hour), time.Minute, 10); len(due) != 2 { // the two leased above come back
			t.Fatalf("all off pushed: %d due", len(due))
		}
		if _, unread, _ := s.List(ctx, userA, 50, false); unread != 5 {
			t.Fatalf("inbox has %d, want 5", unread)
		}
	})
}
