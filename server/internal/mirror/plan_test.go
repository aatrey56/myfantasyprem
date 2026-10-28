package mirror

import (
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// A synthetic three-team league (so FPL adds its average entry "9"):
// seats 1-3, players p1..p9 three each, then moves.
func tinySnapshot() *Snapshot {
	t0 := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	d := &provider.LeagueDetails{
		ExternalID: "5", Name: "Tiny", Scoring: provider.ScoringH2H, TradeMode: provider.TradeModeCommissioner,
		TradeModeSource: "m", TransactionMode: provider.TransactionModeWaivers, DraftCompleted: true, DraftGameweek: 1,
		PickTimeSeconds: 90, StartGameweek: 1, EndGameweek: 38,
		Teams: []provider.LeagueEntry{
			{ExternalID: "3", EntryID: "103", TeamName: "Three", WaiverPick: 1},
			{ExternalID: "1", EntryID: "101", TeamName: "One", WaiverPick: 3},
			{ExternalID: "9", TeamName: "Average", IsAverage: true},
			{ExternalID: "2", EntryID: "102", TeamName: "Two", WaiverPick: 2},
		},
		Matches: []provider.LeagueMatch{
			{Gameweek: 1, HomeEntry: "1", AwayEntry: "2", HomePts: 10, AwayPts: 8, Started: true, Finished: true},
			{Gameweek: 1, HomeEntry: "9", AwayEntry: "3", HomePts: 9, AwayPts: 7, Started: true, Finished: true},
		},
	}
	s := &Snapshot{Details: d, Picks: map[string]map[int]*provider.EntryPicks{}}
	for i, seat := range []string{"1", "2", "3"} {
		for j := range 3 {
			n := i*3 + j + 1
			s.Choices = append(s.Choices, provider.DraftChoice{Overall: n, Round: j + 1, Pick: i + 1, LeagueEntry: seat,
				PlayerExternalID: "c" + string(rune('0'+n)), At: t0})
		}
	}
	return s
}

func tinyPlayers() map[string]string {
	m := map[string]string{}
	for n := 1; n <= 9+3; n++ {
		c := string(rune('0' + n))
		m["c"+c] = "p" + c
	}
	return m
}

func anyPos(string) scoring.Position { return scoring.MID }

func TestBuildSeatsAverageAndOrder(t *testing.T) {
	p, err := Build(tinySnapshot(), tinyPlayers(), anyPos)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Seats) != 3 || p.Seats[0].ExternalID != "1" || p.Seats[2].ExternalID != "3" || p.Settings.TeamCount != 3 {
		t.Errorf("seats = %+v", p.Seats)
	}
	if m := p.Matches[1]; m.Home != "" || m.Away != "3" || m.HomePts != 9 {
		t.Errorf("average match = %+v", m)
	}
	if strings.Join(p.Priority, ",") != "3,2,1" {
		t.Errorf("priority = %v", p.Priority)
	}
	if len(p.Rosters) != 9 || p.Rosters[0].FromGW != 1 || p.Rosters[0].Via != "draft" {
		t.Errorf("rosters = %+v", p.Rosters)
	}
	if len(p.Problems) != 0 {
		t.Errorf("problems = %+v", p.Problems)
	}
}

func TestBuildReplaysMoves(t *testing.T) {
	s := tinySnapshot()
	at := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	s.Transactions = []provider.Transaction{
		// GW2: seat 1 swaps p1 for pa (waiver), then drops pa for pb (free
		// agent) in the same gameweek: pa never counts.
		{ExternalID: "1", Kind: provider.TransactionWaiver, Result: provider.ClaimApproved, LeagueEntry: "1", PlayerIn: "c:", PlayerOut: "c1", Gameweek: 2, ProcessedAt: at},
		{ExternalID: "2", Kind: provider.TransactionFreeAgent, Result: provider.ClaimApproved, LeagueEntry: "1", PlayerIn: "c;", PlayerOut: "c:", Gameweek: 2, ProcessedAt: at.Add(time.Hour)},
		// A lost claim moves nobody.
		{ExternalID: "3", Kind: provider.TransactionWaiver, Result: provider.ClaimDeniedPlayerInUnavailable, LeagueEntry: "2", PlayerIn: "c:", PlayerOut: "c4", Gameweek: 2, ProcessedAt: at},
	}
	s.Trades = []provider.Trade{{ExternalID: "7", State: provider.TradeProcessed, OfferEntry: "2", ReceiveEntry: "3",
		Offered: []string{"c5"}, Requested: []string{"c8"}, Gameweek: 3, OfferedAt: at}}
	p, err := Build(s, tinyPlayers(), anyPos)
	if err != nil {
		t.Fatal(err)
	}
	on := func(seat, player string, gw int) bool {
		for _, r := range p.Rosters {
			if r.Seat == seat && r.PlayerID == player && r.FromGW <= gw && (r.ToGW == 0 || gw < r.ToGW) {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		seat, player string
		gw           int
		want         bool
	}{
		{"1", "p1", 1, true}, {"1", "p1", 2, false}, {"1", "p:", 2, false}, {"1", "p;", 2, true},
		{"2", "p5", 2, true}, {"2", "p5", 3, false}, {"3", "p5", 3, true}, {"2", "p8", 3, true}, {"2", "p4", 3, true},
	} {
		if got := on(c.seat, c.player, c.gw); got != c.want {
			t.Errorf("%s on %s in gameweek %d: %v", c.player, c.seat, c.gw, got)
		}
	}
	for _, r := range p.Rosters {
		if r.PlayerID == "p:" {
			t.Errorf("a slot that never counted: %+v", r)
		}
	}
	if len(p.Txns) != 3 || p.Txns[2].Approved || p.Txns[2].Reason != league.ClaimInUnavailable || len(p.Trades) != 1 {
		t.Errorf("feed = %+v trades %+v", p.Txns, p.Trades)
	}
}

func TestBuildRefusesWhatDoesNotReplay(t *testing.T) {
	for name, mutate := range map[string]func(s *Snapshot){
		"drop a player the team does not have": func(s *Snapshot) {
			s.Transactions = []provider.Transaction{{ExternalID: "1", Kind: provider.TransactionFreeAgent, Result: provider.ClaimApproved,
				LeagueEntry: "1", PlayerIn: "c:", PlayerOut: "c4", Gameweek: 2}}
		},
		"add an owned player": func(s *Snapshot) {
			s.Transactions = []provider.Transaction{{ExternalID: "1", Kind: provider.TransactionFreeAgent, Result: provider.ClaimApproved,
				LeagueEntry: "1", PlayerIn: "c4", PlayerOut: "c1", Gameweek: 2}}
		},
		"trade a player the offering team does not have": func(s *Snapshot) {
			s.Trades = []provider.Trade{{ExternalID: "7", State: provider.TradeProcessed, OfferEntry: "2", ReceiveEntry: "3",
				Offered: []string{"c1"}, Requested: []string{"c8"}, Gameweek: 3}}
		},
		"unknown player": func(s *Snapshot) { s.Choices[0].PlayerExternalID = "c?" },
	} {
		t.Run(name, func(t *testing.T) {
			s := tinySnapshot()
			mutate(s)
			if _, err := Build(s, tinyPlayers(), anyPos); err == nil {
				t.Error("built")
			}
		})
	}
	s := tinySnapshot()
	s.Details.Scoring = provider.ScoringClassic
	if _, err := Build(s, tinyPlayers(), anyPos); err == nil || !strings.Contains(err.Error(), "unsupported-league") {
		t.Errorf("classic: %v", err)
	}
	s = tinySnapshot()
	s.Details.DraftCompleted = false
	if e := Unsupported(s.Details); e == nil || e.Slug != "draft-not-complete" {
		t.Errorf("undrafted: %v", e)
	}
}

// Settings and trade states without an equivalent become unverified items.
func TestBuildReportsWhatItCannotMap(t *testing.T) {
	s := tinySnapshot()
	s.Details.TradeMode, s.Details.TradeModeSource = provider.TradeModeUnknown, "y"
	s.Trades = []provider.Trade{{ExternalID: "7", State: provider.TradeUnknown, SourceState: "q", OfferEntry: "2", ReceiveEntry: "3",
		Offered: []string{"c5"}, Requested: []string{"c8"}, Gameweek: 3}}
	p, err := Build(s, tinyPlayers(), anyPos)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Problems) != 2 || p.Problems[0].Theirs != "y" || p.Problems[1].Key != "trade-state:7" || len(p.Trades) != 0 {
		t.Errorf("problems = %+v", p.Problems)
	}
}

// FPL's post-sub picks: the XI sorted by position, the bench with each
// sub's slot holding the player he replaced.
func TestLineupUndoesSubs(t *testing.T) {
	pos := map[string]scoring.Position{}
	var picks []provider.Pick
	add := func(id string, p scoring.Position) {
		pos[id] = p
		picks = append(picks, provider.Pick{Slot: len(picks) + 1, PlayerExternalID: id})
	}
	// After subs: Bruno (MID) came on for Robinson (DEF), Davis (DEF) for
	// Hato (DEF), as in the real gameweek 6.
	add("gk", scoring.GKP)
	add("d1", scoring.DEF)
	add("d2", scoring.DEF)
	add("davis", scoring.DEF)
	add("bruno", scoring.MID)
	add("m1", scoring.MID)
	add("m2", scoring.MID)
	add("m3", scoring.MID)
	add("f1", scoring.FWD)
	add("f2", scoring.FWD)
	add("f3", scoring.FWD)
	add("gk2", scoring.GKP)
	add("robinson", scoring.DEF)
	add("sangare", scoring.MID)
	add("hato", scoring.DEF)
	ep := &provider.EntryPicks{Gameweek: 6, Picks: picks, AutoSubs: []provider.AutoSub{
		{InExternalID: "bruno", OutExternalID: "robinson"}, {InExternalID: "davis", OutExternalID: "hato"},
	}}
	same := func(id string) (string, error) { return id, nil }
	lu, err := lineup(ep, same, func(id string) scoring.Position { return pos[id] })
	if err != nil {
		t.Fatal(err)
	}
	want := "gk robinson hato d1 d2 m1 m2 m3 f1 f2 f3 gk2 bruno sangare davis"
	if got := strings.Join(lu.Slots, " "); got != want {
		t.Errorf("lineup\n got %s\nwant %s", got, want)
	}
	if len(lu.Subs) != 2 || lu.Subs[0] != (Sub{Out: "robinson", In: "bruno"}) {
		t.Errorf("subs = %+v", lu.Subs)
	}
}

// The real gameweek 8 (2026-27), ids anonymised: FPL's final picks after
// d1 (DEF) went off for dB (DEF) and d2 (DEF) for fB (FWD). The XI comes
// back sorted by position in FPL's own order (dB ahead of d3), so the
// rebuilt XI puts the subbed-out defenders first, in FPL's sub order:
// taking starters in XI order, our engine then makes FPL's subs.
func TestLineupUndoesSubsRealGW8(t *testing.T) {
	pos := map[string]scoring.Position{}
	var picks []provider.Pick
	for _, x := range []struct {
		id string
		p  scoring.Position
	}{{"g1", scoring.GKP}, {"dB", scoring.DEF}, {"d3", scoring.DEF}, {"d4", scoring.DEF}, {"m3", scoring.MID}, {"m4", scoring.MID},
		{"m1", scoring.MID}, {"m5", scoring.MID}, {"m2", scoring.MID}, {"fB", scoring.FWD}, {"f1", scoring.FWD},
		{"g2", scoring.GKP}, {"d1", scoring.DEF}, {"d2", scoring.DEF}, {"f2", scoring.FWD}} {
		pos[x.id] = x.p
		picks = append(picks, provider.Pick{Slot: len(picks) + 1, PlayerExternalID: x.id})
	}
	ep := &provider.EntryPicks{Gameweek: 8, Picks: picks, AutoSubs: []provider.AutoSub{
		{InExternalID: "dB", OutExternalID: "d1"}, {InExternalID: "fB", OutExternalID: "d2"},
	}}
	same := func(id string) (string, error) { return id, nil }
	lu, err := lineup(ep, same, func(id string) scoring.Position { return pos[id] })
	if err != nil {
		t.Fatal(err)
	}
	want := "g1 d1 d2 d3 d4 m3 m4 m1 m5 m2 f1 g2 dB fB f2"
	if got := strings.Join(lu.Slots, " "); got != want {
		t.Errorf("lineup\n got %s\nwant %s", got, want)
	}
}
