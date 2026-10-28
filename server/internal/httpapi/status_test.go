package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// The Status tab's endpoints against the demo world (server/internal/demo:
// gameweeks 1-6 final, 7 live, 8 with a double and a blank), on memory and
// Postgres. The concrete values were read off the demo data and are
// cross-checked against independent computations where one exists.

// beforeGW8 is Wednesday 14 October 2026: gameweek 7 live, gameweek 8's
// deadline (Saturday 17 October 12:30) and waiver deadline (Friday 16
// October 12:30) still ahead.
var beforeGW8 = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

func TestLeagueStatusOnTheDemoWorld(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		f.clock.t = beforeGW8
		target := "/v1/leagues/" + f.league + "/status"
		var res LeagueStatusPageResponse
		if rec := f.get(t, f.token, target, &res); rec.Code != 200 {
			t.Fatalf("status = %d %s", rec.Code, rec.Body)
		}
		page := res.Data
		if page.Gameweek != 7 || res.Meta.ScoreState == nil || *res.Meta.ScoreState != ScoreStateLive {
			t.Errorf("gameweek %d, score state %v", page.Gameweek, res.Meta.ScoreState)
		}

		// Attention: four of the demo team's starters play for the two
		// clubs whose gameweek 8 match was postponed.
		want := []StatusAttentionItem{{
			Kind: "starters_without_fixture", Severity: "critical", Message: "4 starters have no game in GW 8",
			Count: 4, Gameweek: ptr(8),
		}}
		if !equalJSON(t, page.Attention, want) {
			t.Errorf("attention = %s", mustJSON(t, page.Attention))
		}
		blank := 0
		for _, e := range teamOf(t, f, f.token, f.mine.ID).Roster {
			if e.Slot <= 11 && len(f.srv.mustWorld(t).ClubFixtures(f.srv.mustWorld(t).Players[e.Player.Id].ClubID, 8)) == 0 {
				blank++
			}
		}
		if blank != 4 {
			t.Errorf("%d starters in blank clubs, the strip says 4", blank)
		}

		// Injuries: every flagged player in the game, out before doubtful,
		// then by name, with the chance from FPL's news and the owner here.
		type inj struct {
			Name, Availability string
			Chance             any
			Owner              string
		}
		var injuries []inj
		for _, i := range page.Injuries {
			in := inj{Name: i.Player.DisplayName, Availability: string(i.Player.Availability), Chance: nil}
			if i.ChanceOfPlaying != nil {
				in.Chance = *i.ChanceOfPlaying
			}
			if i.Owner != nil {
				in.Owner = i.Owner.Name
			}
			injuries = append(injuries, in)
		}
		wantInj := []inj{
			{"B. Fairbairn", "injured", 0, ""},
			{"C. Osei", "suspended", 0, "Sunday League Heroes"},
			{"E. Thorne", "unavailable", 0, ""},
			{"A. Tanaka", "doubtful", 75, "Park the Bus"},
			{"D. Castellan", "doubtful", 50, ""},
		}
		if fmt.Sprint(injuries) != fmt.Sprint(wantInj) {
			t.Errorf("injuries = %v\nwant       %v", injuries, wantInj)
		}

		// Top available: ten per position by form, nobody rostered, the
		// next three gameweeks of fixtures.
		if len(page.TopAvailable) != 40 {
			t.Fatalf("top available has %d players", len(page.TopAvailable))
		}
		owned := f.game.OwnerAt(8)
		perPos := map[Position]int{}
		for i, a := range page.TopAvailable {
			perPos[a.Player.Position]++
			if _, ok := owned[a.Player.Id]; ok || a.RosterStatus == RosterStatusRostered {
				t.Errorf("%s is rostered", a.Player.DisplayName)
			}
			if a.Form == nil {
				t.Fatalf("%s has no form in gameweek 7", a.Player.DisplayName)
			}
			if i > 0 && *a.Form > *page.TopAvailable[i-1].Form {
				t.Errorf("not best first at %d: %v after %v", i, *a.Form, *page.TopAvailable[i-1].Form)
			}
			var gws []int
			for _, u := range a.Upcoming {
				gws = append(gws, u.Gameweek)
			}
			if !slices.Equal(gws, []int{8, 9, 10}) {
				t.Errorf("%s upcoming gameweeks %v", a.Player.DisplayName, gws)
			}
		}
		for pos, n := range perPos {
			if n != 10 {
				t.Errorf("%s: %d players", pos, n)
			}
		}
		top := page.TopAvailable[0]
		// Gameweek 7 is live and gameweek 8's waivers have not run, so
		// unowned players are locked until that run (16 Oct 12:30).
		clears := time.Date(2026, 10, 16, 12, 30, 0, 0, time.UTC)
		// Castellan's card: gameweeks 5 and 6 sum to 21, so form (21) / 2 = 10.5.
		if top.Player.DisplayName != "C. Castellan" || top.Player.Position != DEF || *top.Form != 10.5 ||
			top.RosterStatus != RosterStatusLocked || top.WaiverClearsAt == nil || !top.WaiverClearsAt.Equal(clears) {
			t.Errorf("top available = %s %s form %v %s %v", top.Player.DisplayName, top.Player.Position, *top.Form, top.RosterStatus, top.WaiverClearsAt)
		}
		// Form is the mean of gameweeks 5 and 6 (7 is live), which the
		// player card lists: a gameweek he did not play counts as 0, so
		// the divisor is 2.
		for _, a := range page.TopAvailable[:12] {
			var card PlayerDetailResponse
			f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+a.Player.Id, &card)
			sum := 0
			for _, g := range card.Data.Gameweeks {
				if g.Gameweek >= 5 && g.Gameweek <= 6 {
					sum += g.Points
				}
			}
			want := math.Round(float64(sum)/2*10) / 10
			if math.Abs(float64(*a.Form)-want) > 1e-6 || card.Data.Form == nil || *card.Data.Form != *a.Form {
				t.Errorf("%s form %v, card form %v, gameweeks 5-6 sum to %d", a.Player.DisplayName, *a.Form, card.Data.Form, sum)
			}
		}

		// A trade offer to the demo team and one of its waiver claims join
		// the strip.
		theirs, mine := samePosition(t, f, f.theirs.ID, f.mine.ID, owned)
		body := `{"receivingTeamId":"` + f.mine.ID + `","offeredPlayerIds":["` + theirs + `"],"requestedPlayerIds":["` + mine + `"]}`
		if rec := doAs(f.h, f.other, "POST", "/v1/leagues/"+f.league+"/trades", body, "Idempotency-Key", "status-trade-1"); rec.Code != 201 {
			t.Fatalf("propose = %d %s", rec.Code, rec.Body)
		}
		add, drop := waiverPair(t, f, owned)
		body = `{"addPlayerId":"` + add + `","dropPlayerId":"` + drop + `"}`
		if rec := doAs(f.h, f.token, "POST", "/v1/leagues/"+f.league+"/waivers/claims", body, "Idempotency-Key", "status-claim-1"); rec.Code != 201 {
			t.Fatalf("claim = %d %s", rec.Code, rec.Body)
		}
		f.get(t, f.token, target, &res)
		want = append(want,
			StatusAttentionItem{Kind: "trade_offers", Severity: "warning", Message: "1 trade offer waiting for you", Count: 1},
			StatusAttentionItem{Kind: "pending_waivers", Severity: "info", Message: "1 waiver claim pending", Count: 1, Gameweek: ptr(8)})
		if !equalJSON(t, res.Data.Attention, want) {
			t.Errorf("attention = %s", mustJSON(t, res.Data.Attention))
		}
		// The offering manager sees no offer to accept.
		var theirsPage LeagueStatusPageResponse
		f.get(t, f.other, target, &theirsPage)
		for _, a := range theirsPage.Data.Attention {
			if a.Kind == "trade_offers" || a.Kind == "pending_waivers" {
				t.Errorf("the other manager sees %s", a.Message)
			}
		}

		// After the deadline the strip is about gameweek 9.
		f.clock.t = time.Date(2026, 10, 17, 12, 45, 0, 0, time.UTC)
		f.get(t, f.token, target, &res)
		for _, a := range res.Data.Attention {
			if a.Gameweek != nil && *a.Gameweek != 9 {
				t.Errorf("after the deadline: %s (gameweek %d)", a.Message, *a.Gameweek)
			}
		}
		if g := res.Data.TopAvailable[0].Upcoming[0].Gameweek; g != 9 {
			t.Errorf("after the deadline upcoming starts at %d", g)
		}
	})
}

// rankChange is each manager's move in the table with gameweek 6, the
// last completed one, worked out here from the matchups (win 3, draw 1,
// then points for).
func TestLeagueStatusRankChange(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		g, err := f.srv.leagues.Repo().Game(context.Background(), f.league)
		if err != nil {
			t.Fatal(err)
		}
		ranks := func(last int) map[string]int {
			type row struct {
				id     string
				lp, pf int
			}
			rows := map[string]*row{}
			for _, m := range g.Matchups {
				if m.Gameweek > last {
					continue
				}
				for _, side := range []struct {
					id      string
					for_, v int
				}{{m.HomeTeamID, m.HomePoints, m.AwayPoints}, {m.AwayTeamID, m.AwayPoints, m.HomePoints}} {
					if side.id == "" {
						continue
					}
					r := rows[side.id]
					if r == nil {
						r = &row{id: side.id}
						rows[side.id] = r
					}
					r.pf += side.for_
					switch scoring.H2H(side.for_, side.v) {
					case scoring.Win:
						r.lp += 3
					case scoring.Draw:
						r.lp++
					}
				}
			}
			var list []*row
			for _, r := range rows {
				list = append(list, r)
			}
			slices.SortFunc(list, func(a, b *row) int {
				return cmp.Or(cmp.Compare(b.lp, a.lp), cmp.Compare(b.pf, a.pf), cmp.Compare(a.id, b.id))
			})
			out := map[string]int{}
			for i, r := range list {
				out[r.id] = i + 1
			}
			return out
		}
		before, after := ranks(5), ranks(6)
		l, err := f.srv.leagues.Get(context.Background(), f.users[0], f.league)
		if err != nil {
			t.Fatal(err)
		}
		moved := 0
		got := map[string]int{}
		for _, uid := range f.users {
			var res LeagueStatusPageResponse
			if rec := f.get(t, f.tokenOf(t, uid), "/v1/leagues/"+f.league+"/status", &res); rec.Code != 200 {
				t.Fatalf("status = %d %s", rec.Code, rec.Body)
			}
			team, _ := l.TeamOf(uid)
			got[team.Name] = res.Data.RankChange
			if want := before[team.ID] - after[team.ID]; res.Data.RankChange != want {
				t.Errorf("%s: rank change %d, want %d (%d -> %d)", team.Name, res.Data.RankChange, want, before[team.ID], after[team.ID])
			}
			if res.Data.RankChange != 0 {
				moved++
			}
		}
		if moved == 0 {
			t.Errorf("nobody moved in gameweek 6: %v", got)
		}
		want := map[string]int{"Bench Warmers": 1, "Clean Sheet Club": -2, "Expected Toulouse": 0, "Gaffer's Army": -2,
			"Late Winners": -1, "Net Busters": 3, "Park the Bus": -1, "Sunday League Heroes": 0, "Top Bins": 2}
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Errorf("rank changes = %v\nwant %v", got, want)
		}
	})
}

func TestLeagueStatusBeforeTheDraft(t *testing.T) {
	f := newDemoFixture(t)
	var lr LeagueResponse
	rec := doAs(f.h, f.token, "POST", "/v1/leagues", `{"name":"Later","teamCount":4,"teamName":"Mine"}`)
	if rec.Code != 201 || json.Unmarshal(rec.Body.Bytes(), &lr) != nil {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	}
	rec = doAs(f.h, f.token, "GET", "/v1/leagues/"+lr.Data.League.Id+"/status", "")
	if rec.Code != 409 || !json.Valid(rec.Body.Bytes()) {
		t.Fatalf("pre-draft status = %d %s", rec.Code, rec.Body)
	}
	if rec := doAs(f.h, f.other, "GET", "/v1/leagues/"+lr.Data.League.Id+"/status", ""); rec.Code != 403 {
		t.Fatalf("outsider = %d", rec.Code)
	}
}

func TestGameweekAwardsOnTheDemoWorld(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		var a GameweekAwardsResponse
		if rec := f.get(t, f.token, "/v1/gameweeks/6/awards", &a); rec.Code != 200 {
			t.Fatalf("awards = %d %s", rec.Code, rec.Body)
		}
		d := a.Data
		if d.Gameweek != 6 || d.State != ScoreStateFinal || a.Meta.ScoreState == nil || *a.Meta.ScoreState != ScoreStateFinal {
			t.Errorf("gameweek %d state %s", d.Gameweek, d.State)
		}
		if d.PlayerOfTheWeek == nil || d.PlayerOfTheWeek.Player.DisplayName != "B. Moreau" ||
			d.PlayerOfTheWeek.Stats != (AwardStats{Points: 20, Bonus: 3, Bps: 58, Goals: 2, Assists: 1, DefensiveContribution: 12, CleanSheets: 0, Minutes: 90}) {
			t.Errorf("player of the week = %s", mustJSON(t, d.PlayerOfTheWeek))
		}
		var team []string
		for _, p := range d.TeamOfTheWeek {
			team = append(team, fmt.Sprintf("%s %s %d", p.Player.Position, p.Player.DisplayName, p.Stats.Points))
		}
		wantTeam := []string{
			"GKP E. Calder 9",
			"DEF C. Castellan 15", "DEF C. Delacroix 10", "DEF E. Vasquez 10",
			"MID B. Moreau 20", "MID D. Moreau 15", "MID B. Ketteridge 12", "MID C. Sutcliffe 12", "MID A. Kovacs 12",
			"FWD D. Lockhart 12", "FWD B. Sorensen 12",
		}
		if !slices.Equal(team, wantTeam) {
			t.Errorf("team of the week =\n%q\nwant\n%q", team, wantTeam)
		}

		// The same rules over the whole pool, rebuilt from the player cards
		// of everyone who played: nobody outranks the Player of the Week.
		w := f.srv.mustWorld(t)
		for id, rows := range w.Rows[6] {
			pts := 0
			for _, r := range rows {
				pts += r.Points
			}
			if pts > d.PlayerOfTheWeek.Stats.Points {
				t.Errorf("%s scored %d, more than the Player of the Week", w.Players[id].DisplayName, pts)
			}
		}

		// The live gameweek, and gameweeks that have not started.
		if rec := f.get(t, f.token, "/v1/gameweeks/7/awards", &a); rec.Code != 200 || a.Data.State != ScoreStateLive ||
			a.Data.PlayerOfTheWeek.Player.DisplayName != "A. Rask" || len(a.Data.TeamOfTheWeek) != 11 {
			t.Errorf("gameweek 7 = %d %s", rec.Code, mustJSON(t, a.Data.PlayerOfTheWeek))
		}
		for _, gw := range []string{"8", "9", "38"} {
			if rec := f.get(t, f.token, "/v1/gameweeks/"+gw+"/awards", nil); rec.Code != 404 {
				t.Errorf("gameweek %s = %d, want 404", gw, rec.Code)
			}
		}
		if rec := doAnon(f.h, "GET", "/v1/gameweeks/6/awards", ""); rec.Code != 401 {
			t.Errorf("anonymous = %d", rec.Code)
		}

		var list PlayerOfTheWeekListResponse
		if rec := f.get(t, f.token, "/v1/awards/players-of-the-week", &list); rec.Code != 200 {
			t.Fatalf("players of the week = %d %s", rec.Code, rec.Body)
		}
		var got []string
		for _, p := range list.Data {
			got = append(got, fmt.Sprintf("%d %s %s %d", p.Gameweek, p.State, p.Award.Player.DisplayName, p.Award.Stats.Points))
		}
		wantList := []string{
			"1 final A. Ferreira 20", "2 final C. Verhoeven 15", "3 final A. Ibarra 13", "4 final D. Verhoeven 16",
			"5 final C. Mazur 15", "6 final B. Moreau 20", "7 live A. Rask 17",
		}
		if !slices.Equal(got, wantList) {
			t.Errorf("players of the week =\n%q\nwant\n%q", got, wantList)
		}
	})
}

// The player card's per-game stats are the demo feed's own numbers: every
// field, for players who played gameweek 6, compared with the raw
// event/6/live.json; bonus and DC points with the points breakdown.
func TestPlayerGameStatsOnTheDemoWorld(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		ctx := context.Background()
		var boot struct {
			Elements []struct {
				ID   int `json:"id"`
				Code int `json:"code"`
			} `json:"elements"`
		}
		var live struct {
			Elements map[string]struct {
				Stats map[string]any `json:"stats"`
			} `json:"elements"`
		}
		readJSON(t, "bootstrap-static.json", &boot)
		readJSON(t, "event/6/live.json", &live)
		ids, err := f.srv.stats.ExternalIDs(ctx, demo.ProviderName, "player")
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for _, e := range boot.Elements {
			raw := live.Elements[strconv.Itoa(e.ID)].Stats
			if raw["minutes"].(float64) == 0 || e.ID%7 != 0 { // a sample, to keep the test quick
				continue
			}
			pid := ids[strconv.Itoa(e.Code)]
			var card PlayerDetailResponse
			if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+pid, &card); rec.Code != 200 {
				t.Fatalf("player = %d %s", rec.Code, rec.Body)
			}
			var gw PlayerGameweek
			for _, g := range card.Data.Gameweeks {
				if g.Gameweek == 6 {
					gw = g
				}
			}
			s := gw.Stats
			num := func(k string) float64 { v, _ := raw[k].(float64); return v }
			dec := func(k string) float64 { v, _ := strconv.ParseFloat(raw[k].(string), 64); return v }
			ints := map[string]*int{
				"goals_scored": s.GoalsScored, "assists": s.Assists, "clean_sheets": s.CleanSheets,
				"goals_conceded": s.GoalsConceded, "own_goals": s.OwnGoals, "penalties_saved": s.PenaltiesSaved,
				"penalties_missed": s.PenaltiesMissed, "yellow_cards": s.YellowCards, "red_cards": s.RedCards,
				"saves": s.Saves, "bps": s.Bps, "recoveries": s.Recoveries, "tackles": s.Tackles,
			}
			for k, v := range ints {
				if v == nil || float64(*v) != num(k) {
					t.Errorf("element %d %s = %v, feed %v", e.ID, k, valueOf(v), num(k))
				}
			}
			if s.Cbit == nil || float64(*s.Cbit) != num("clearances_blocks_interceptions")+num("tackles") {
				t.Errorf("element %d cbit = %v", e.ID, valueOf(s.Cbit))
			}
			floats := map[string]*float32{
				"influence": s.Influence, "creativity": s.Creativity, "threat": s.Threat, "ict_index": s.IctIndex,
				"expected_goals": s.ExpectedGoals, "expected_assists": s.ExpectedAssists,
				"expected_goal_involvements": s.ExpectedGoalInvolvements, "expected_goals_conceded": s.ExpectedGoalsConceded,
			}
			for k, v := range floats {
				if v == nil || *v != float32(dec(k)) {
					t.Errorf("element %d %s = %v, feed %s", e.ID, k, valueOf(v), raw[k])
				}
			}
			bonus, dc := 0, 0
			for _, fx := range gw.Fixtures {
				for _, l := range fx.Breakdown {
					switch l.Stat {
					case "bonus":
						bonus += l.Value
					case "defensive_contribution":
						dc += l.Points
					}
				}
			}
			if s.Bonus == nil || *s.Bonus != bonus || float64(bonus) != num("bonus") || s.DefensiveContributionPoints == nil ||
				*s.DefensiveContributionPoints != dc {
				t.Errorf("element %d bonus %v DC points %v, breakdown %d and %d", e.ID, valueOf(s.Bonus), valueOf(s.DefensiveContributionPoints), bonus, dc)
			}
			if card.Data.PositionRank == nil || *card.Data.PositionRank < 1 {
				t.Errorf("element %d position rank %v", e.ID, card.Data.PositionRank)
			}
			checked++
		}
		if checked < 20 {
			t.Fatalf("checked only %d players", checked)
		}

		// B. Moreau, gameweek 6's Player of the Week: the best midfielder
		// of the season so far is ranked against midfielders only.
		w := f.srv.mustWorld(t)
		totals := seasonTotals(w)
		for id, p := range w.Players {
			if p.DisplayName != "B. Moreau" {
				continue
			}
			var card PlayerDetailResponse
			f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+id, &card)
			want := 1
			for o, q := range w.Players {
				if q.Position == p.Position && totals[o].points > totals[id].points {
					want++
				}
			}
			if card.Data.PositionRank == nil || *card.Data.PositionRank != want {
				t.Errorf("B. Moreau position rank %v, want %d", card.Data.PositionRank, want)
			}
		}
	})
}

// The players list sorted by form: best first, each form the mean of
// gameweeks 5 and 6 on the player card (gameweek 7 is live).
func TestPlayersByFormOnTheDemoWorld(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		f.clock.t = beforeGW8
		var res PlayerListResponse
		if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/players?sort=form&limit=100", &res); rec.Code != 200 {
			t.Fatalf("players = %d %s", rec.Code, rec.Body)
		}
		if len(res.Data) != 100 {
			t.Fatalf("%d players", len(res.Data))
		}
		for i, p := range res.Data {
			if p.Form == nil {
				t.Fatalf("%s has no form", p.Player.DisplayName)
			}
			if i > 0 && *p.Form > *res.Data[i-1].Form {
				t.Errorf("not best first at %d: %v after %v", i, *p.Form, *res.Data[i-1].Form)
			}
		}
		for _, p := range res.Data[:8] {
			var card PlayerDetailResponse
			f.get(t, f.token, "/v1/leagues/"+f.league+"/players/"+p.Player.Id, &card)
			sum := 0
			for _, g := range card.Data.Gameweeks {
				if g.Gameweek >= 5 && g.Gameweek <= 6 {
					sum += g.Points
				}
			}
			if want := math.Round(float64(sum)/2*10) / 10; math.Abs(float64(*p.Form)-want) > 1e-6 {
				t.Errorf("%s form %v, gameweeks 5-6 sum to %d", p.Player.DisplayName, *p.Form, sum)
			}
		}
	})
}

// seasonTotals takes the form gameweek from the season's current gameweek
// (statuspage.FormGameweek): the live one, or the next once it finished.
func TestSeasonTotalsForm(t *testing.T) {
	row := func(gw, pts, mins int) stats.PlayerFixture {
		return stats.PlayerFixture{Gameweek: gw, Points: pts, Stats: scoring.FixtureStats{Minutes: mins}}
	}
	// Gameweeks 1-6: 3, 8, 4, 0 without minutes, 9 and a double (2 + 5);
	// gameweek 7 (live or next) scores 20, which form never counts.
	rows := map[int]map[string][]stats.PlayerFixture{
		1: {"p": {row(1, 3, 90)}}, 2: {"p": {row(2, 8, 90)}}, 3: {"p": {row(3, 4, 90)}},
		4: {"p": {row(4, 0, 0)}}, 5: {"p": {row(5, 9, 90)}}, 6: {"p": {row(6, 2, 90), row(6, 5, 90)}},
		7: {"p": {row(7, 20, 90)}},
	}
	for _, tc := range []struct {
		name     string
		current  int
		finished bool
		want     *float32
	}{
		{"gameweek 7 live: (9 + 2 + 5) / 2", 7, false, ptr(float32(8))},
		{"gameweek 6 finished: the same, for gameweek 7", 6, true, ptr(float32(8))},
		{"gameweek 6 live: (0 + 9) / 2", 6, false, ptr(float32(4.5))},
		{"gameweek 3: (3 + 8) / 2", 3, false, ptr(float32(5.5))},
		{"gameweek 2: gameweek 1's points", 2, false, ptr(float32(3))},
		{"gameweek 1 finished: still gameweek 1's points, for gameweek 2", 1, true, ptr(float32(3))},
		{"gameweek 1 live: no form", 1, false, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := &league.World{
				Status:    stats.Status{Current: tc.current},
				Gameweeks: map[int]stats.Gameweek{tc.current: {Number: tc.current, Finished: tc.finished}},
				Rows:      rows,
			}
			got := seasonTotals(w)["p"].contractForm()
			if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
				t.Errorf("form = %v, want %v", deref32(got), deref32(tc.want))
			}
		})
	}
}

func deref32(f *float32) any {
	if f == nil {
		return nil
	}
	return *f
}

// ------------------------------------------------------------ helpers

func (s *Server) mustWorld(t *testing.T) *league.World {
	t.Helper()
	w, err := s.world(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func teamOf(t *testing.T, f demoFixture, token, teamID string) TeamDetail {
	t.Helper()
	var res TeamDetailResponse
	if rec := f.get(t, token, "/v1/leagues/"+f.league+"/teams/"+teamID, &res); rec.Code != 200 {
		t.Fatalf("team = %d %s", rec.Code, rec.Body)
	}
	return res.Data
}

// samePosition finds a player of each team at the same position.
func samePosition(t *testing.T, f demoFixture, from, to string, owned map[string]string) (string, string) {
	t.Helper()
	w := f.srv.mustWorld(t)
	var a, b []string
	for id, team := range owned {
		switch team {
		case from:
			a = append(a, id)
		case to:
			b = append(b, id)
		}
	}
	slices.Sort(a)
	slices.Sort(b)
	for _, x := range a {
		for _, y := range b {
			if w.Players[x].Position == w.Players[y].Position && w.Players[x].Position == scoring.MID {
				return x, y
			}
		}
	}
	t.Fatal("no pair")
	return "", ""
}

// waiverPair is an unowned defender to add and one of the demo team's to
// drop.
func waiverPair(t *testing.T, f demoFixture, owned map[string]string) (add, drop string) {
	t.Helper()
	w := f.srv.mustWorld(t)
	var ids []string
	for id := range w.Players {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		p := w.Players[id]
		if p.Position != scoring.DEF || p.Availability != "available" {
			continue
		}
		if _, ok := owned[id]; !ok && add == "" {
			add = id
		}
		if owned[id] == f.mine.ID && drop == "" {
			drop = id
		}
	}
	return add, drop
}

func readJSON(t *testing.T, name string, out any) {
	t.Helper()
	b, err := fs.ReadFile(demo.FS(), name)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func equalJSON(t *testing.T, a, b any) bool { return mustJSON(t, a) == mustJSON(t, b) }

func valueOf[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}
