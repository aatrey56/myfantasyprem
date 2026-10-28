package fpl

import (
	"os"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// testdata/real-gw6 holds real FPL Draft responses for 2026-27, saved
// once by hand (never fetched by a test): bootstrap-static and
// event/6/live as served on 13 October 2026, gameweek 6 finished and 7
// next. game.json and pl/event-status.json are written in their recorded
// shape to match.

func realClient() *Client {
	return New(Options{Transport: DirTransport{FS: os.DirFS("testdata/real-gw6")}, Logger: quiet(), MinInterval: -1})
}

// The real bootstrap only lists the upcoming gameweeks' fixtures.
func TestRealBootstrapDecodes(t *testing.T) {
	b, err := realClient().Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Clubs) != 20 || len(b.Players) != 667 || len(b.Gameweeks) != 38 {
		t.Fatalf("clubs %d players %d gameweeks %d", len(b.Clubs), len(b.Players), len(b.Gameweeks))
	}
	fixtures, err := realClient().Fixtures(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.Gameweek < 7 {
			t.Errorf("bootstrap fixture %s is in gameweek %d", f.ExternalID, f.Gameweek)
		}
	}
	if len(fixtures) != 30 {
		t.Errorf("%d bootstrap fixtures, want 30", len(fixtures))
	}
	// Draft events have no data_checked: finished counts as checked.
	if g := b.Gameweeks[5]; g.Number != 6 || !g.Finished || !g.DataChecked {
		t.Errorf("gameweek 6 = %+v", g)
	}
	if g := b.Gameweeks[6]; g.Number != 7 || g.Finished || g.DataChecked {
		t.Errorf("gameweek 7 = %+v", g)
	}
	if s := b.Settings; s.SquadSize != 15 || s.StartingSize != 11 || s.Scoring.DefensiveContributionThreshold.DEF != 10 ||
		s.Scoring.DefensiveContribution != 2 {
		t.Errorf("settings = %+v", s)
	}
}

// FPL Draft names its settings differently from classic FPL: squad
// "size" and "play", and per-position "defensive_contribution_limit_X"
// and "defensive_contribution_X".
func TestDraftSettingsKeys(t *testing.T) {
	var s rawSettings
	if err := decodeStrict([]byte(`{"squad":{"size":13,"play":10},"scoring":{
	  "defensive_contribution_limit_GKP":0,"defensive_contribution_limit_DEF":9,"defensive_contribution_limit_MID":11,
	  "defensive_contribution_limit_FWD":13,"defensive_contribution_GKP":0,"defensive_contribution_DEF":3,
	  "defensive_contribution_MID":3,"defensive_contribution_FWD":3}}`), &s); err != nil {
		t.Fatal(err)
	}
	g := gameSettings(s)
	want := provider.PositionPoints{GKP: 0, DEF: 9, MID: 11, FWD: 13}
	if g.SquadSize != 13 || g.StartingSize != 10 || g.Scoring.DefensiveContributionThreshold != want ||
		g.Scoring.DefensiveContribution != 3 {
		t.Errorf("settings = %+v", g)
	}
}

// The real live payload carries its own fixtures (bootstrap no longer
// lists them), per-fixture stats keyed "s" rather than "identifier", and
// ICT and expected stats as JSON numbers.
func TestRealEventLiveDecodes(t *testing.T) {
	live, err := realClient().EventLive(ctx, 6)
	if err != nil {
		t.Fatal(err)
	}
	if len(live.Players) != 667 {
		t.Errorf("%d players", len(live.Players))
	}
	codes := map[string]bool{}
	for _, f := range live.Fixtures {
		codes[f.ExternalID] = true
		if f.Gameweek != 6 || f.Status != provider.FixtureFinished || f.HomeScore == nil || f.AwayScore == nil || f.KickoffAt == nil {
			t.Errorf("fixture %+v", f)
		}
	}
	if len(live.Fixtures) != 10 || !codes["2645245"] {
		t.Errorf("fixtures %v", codes)
	}
	// Starts, counted by hand in the recorded payload: 220 players started,
	// the other 447 sent 0 (element 1 started, element 2 did not).
	withMinutes, started := 0, 0
	for _, p := range live.Players {
		if p.Starts == nil {
			t.Fatalf("player %s: no starts", p.PlayerExternalID)
		}
		started += *p.Starts
		if len(p.Explain) != 1 {
			t.Fatalf("player %s: %d explain entries", p.PlayerExternalID, len(p.Explain))
		}
		if p.Minutes == 0 {
			continue
		}
		withMinutes++
		if fe := p.Explain[0]; fe.BPS == nil || *fe.BPS != p.BPS {
			t.Errorf("player %s: fixture BPS %v, gameweek BPS %d", p.PlayerExternalID, fe.BPS, p.BPS)
		}
		for _, v := range []*float64{p.Influence, p.Creativity, p.Threat, p.ICTIndex,
			p.ExpectedGoals, p.ExpectedAssists, p.ExpectedGoalInvolvements, p.ExpectedGoalsConceded} {
			if v == nil {
				t.Errorf("player %s: missing ICT or expected stat", p.PlayerExternalID)
				break
			}
		}
	}
	if withMinutes != 315 {
		t.Errorf("%d players with minutes", withMinutes)
	}
	if started != 220 {
		t.Errorf("%d starts in total, want 220", started)
	}
	byID := map[string]provider.PlayerLive{}
	for _, p := range live.Players {
		byID[p.PlayerExternalID] = p
	}
	if s := byID[live.Players[0].PlayerExternalID].Starts; s == nil || *s != 1 {
		t.Errorf("element 1 starts = %v, want 1", s)
	}
	if s := byID[live.Players[1].PlayerExternalID].Starts; s == nil || *s != 0 {
		t.Errorf("element 2 starts = %v, want 0", s)
	}
}

// A finished Draft gameweek is checked: from bootstrap's event, or from
// /game's current_event_finished before the next bootstrap says so.
func TestDraftFinishedGameweekIsChecked(t *testing.T) {
	st, err := realClient().GameweekStatus(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.Current != 6 || !st.BonusAdded || !st.DataChecked {
		t.Errorf("status = %+v", st)
	}
	yes, no, cur := true, false, 6
	idx := &bootstrapIndex{events: map[int]rawEvent{6: {ID: 6}}}
	for _, c := range []struct {
		name     string
		finished *bool
		event    rawEvent
		want     bool
	}{
		{"nothing", nil, rawEvent{ID: 6}, false},
		{"game not finished", &no, rawEvent{ID: 6}, false},
		{"game finished", &yes, rawEvent{ID: 6}, true},
		{"event finished", &no, rawEvent{ID: 6, Finished: true}, true},
		{"classic data_checked", nil, rawEvent{ID: 6, DataChecked: true}, true},
	} {
		idx.events[6] = c.event
		got := gameweekStatus(&rawGame{CurrentEvent: &cur, CurrentFinished: c.finished}, &rawEventStatus{}, idx).DataChecked
		if got != c.want {
			t.Errorf("%s: DataChecked %v, want %v", c.name, got, c.want)
		}
	}
}

// FPL elements carry the news time (news_added) and the chance of playing
// next round. Values from testdata/real-gw6/bootstrap-static.json by hand:
// Hincapie (code 448104) "Ankle injury - 75% chance of playing", added
// 2026-10-12T15:00:07.550884Z, chance 75; Timber (445122) has no news and
// chance 100 (cleared news: fit again), so no news time.
func TestRealBootstrapNews(t *testing.T) {
	b, err := realClient().Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	byCode := map[string]provider.Player{}
	for _, p := range b.Players {
		byCode[p.ExternalID] = p
	}
	h := byCode["448104"]
	want := time.Date(2026, 10, 12, 15, 0, 7, 550884000, time.UTC)
	if h.News != "Ankle injury - 75% chance of playing" || h.NewsAt == nil || !h.NewsAt.Equal(want) ||
		h.ChanceNext == nil || *h.ChanceNext != 75 || h.Availability != provider.AvailabilityDoubtful {
		t.Errorf("Hincapie = %+v (newsAt %v chance %v)", h, h.NewsAt, h.ChanceNext)
	}
	tb := byCode["445122"]
	if tb.News != "" || tb.NewsAt != nil || tb.ChanceNext == nil || *tb.ChanceNext != 100 {
		t.Errorf("Timber = %+v (newsAt %v chance %v)", tb, tb.NewsAt, tb.ChanceNext)
	}
}
