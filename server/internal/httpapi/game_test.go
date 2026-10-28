package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// demoFixture is a server with the demo world seeded: a nine-team league
// at gameweek 7 (live), the server clock on Saturday 17 October 2026, after
// the gameweek 8 deadline and before gameweek 9's.
type demoFixture struct {
	srv    *Server
	clock  *fakeClock
	h      http.Handler
	league string
	token  string // the demo commissioner
	other  string // another manager in the league
	mine   league.Team
	theirs league.Team
	game   *league.Game
	users  []string // every manager, the demo commissioner first
}

func newDemoFixture(t *testing.T) demoFixture { return newDemoFixtureOn(t, nil) }

// newDemoFixtureOn is newDemoFixture on Postgres stores when pool is set.
func newDemoFixtureOn(t *testing.T, pool *pgxpool.Pool) demoFixture {
	t.Helper()
	srv, clock := newTestServerOn(pool)
	ctx := context.Background()
	res, err := demo.Seed(ctx, demo.Deps{Auth: srv.auth, Leagues: srv.leagues, Stats: srv.stats, History: srv.history,
		Now: srv.now, Log: testOptions().Logger})
	if err != nil {
		t.Fatal(err)
	}
	srv.historyProvider = demo.ProviderName
	tok := func(uid string) string {
		tk, err := srv.auth.Issue(ctx, uid)
		if err != nil {
			t.Fatal(err)
		}
		return tk.AccessToken
	}
	l, err := srv.leagues.Get(ctx, res.UserIDs[0], res.LeagueID)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := srv.leagues.Repo().Game(ctx, res.LeagueID)
	mine, _ := l.TeamOf(res.UserIDs[0])
	theirs, _ := l.TeamOf(res.UserIDs[1])
	return demoFixture{
		srv: srv, clock: clock, h: newHandler(testOptions(), newRoutes(testOptions(), srv)), league: res.LeagueID,
		token: tok(res.UserIDs[0]), other: tok(res.UserIDs[1]), mine: mine, theirs: theirs, game: g, users: res.UserIDs,
	}
}

// eachDemo runs test on the demo world with the memory stores and, when
// TEST_DATABASE_URL is set, the Postgres ones.
func eachDemo(t *testing.T, test func(t *testing.T, f demoFixture)) {
	t.Run("memory", func(t *testing.T) { test(t, newDemoFixtureOn(t, nil)) })
	t.Run("postgres", func(t *testing.T) { test(t, newDemoFixtureOn(t, storetest.New(t))) })
}

// tokenOf signs in a user.
func (f demoFixture) tokenOf(t *testing.T, userID string) string {
	t.Helper()
	tk, err := f.srv.auth.Issue(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	return tk.AccessToken
}

func (f demoFixture) get(t *testing.T, token, target string, out any) *httptest.ResponseRecorder {
	t.Helper()
	rec := doAs(f.h, token, http.MethodGet, target, "")
	if out != nil && rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s: %v", target, err)
		}
	}
	return rec
}

func TestLeagueHomeStandingsAndMatchups(t *testing.T) {
	f := newDemoFixture(t)
	var home LeagueHomeResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league, &home); rec.Code != 200 {
		t.Fatalf("home = %d %s", rec.Code, rec.Body)
	}
	d := home.Data
	if d.CurrentGameweek.Number != 7 || len(d.Standings) != 9 || len(d.Matchups) != 5 || d.Viewer.Team == nil ||
		d.Viewer.Role != LeagueRoleCommissioner || d.League.Status != LeagueStatus(league.StatusInSeason) {
		t.Fatalf("home = %+v", d)
	}
	if home.Meta.ScoreState == nil || *home.Meta.ScoreState != ScoreStateLive {
		t.Errorf("meta = %+v", home.Meta)
	}
	for i, r := range d.Standings {
		if r.Rank != i+1 || r.Played != 6 || r.Wins+r.Draws+r.Losses != 6 || len(r.Form) != 5 ||
			r.LeaguePoints != 3*r.Wins+r.Draws {
			t.Errorf("standing %d = %+v", i, r)
		}
		if i > 0 && d.Standings[i-1].LeaguePoints < r.LeaguePoints {
			t.Errorf("standings out of order at %d", i)
		}
	}
	avg := 0
	for _, m := range d.Matchups {
		if m.State != ScoreStateLive {
			t.Errorf("matchup state = %s", m.State)
		}
		if a, err := m.Away.AsAverageOpponent(); err == nil && a.Kind == AverageOpponentKindAverage {
			avg++
		}
	}
	if avg != 1 {
		t.Errorf("%d matchups against the average, want 1", avg)
	}
}

func TestMatchupDetailMatchesSummary(t *testing.T) {
	f := newDemoFixture(t)
	var list MatchupListResponse
	f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/6/matchups", &list)
	for _, m := range list.Data {
		var detail MatchupDetailResponse
		if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/6/matchups/"+m.Id, &detail); rec.Code != 200 {
			t.Fatalf("detail = %d %s", rec.Code, rec.Body)
		}
		home, _ := m.Home.AsTeamScore()
		side, err := detail.Data.Home.AsTeamLineup()
		if err != nil || side.TotalPoints != home.Points || len(side.Starters) != 11 || len(side.Bench) != 4 {
			t.Fatalf("home side = %+v (summary %d)", side, home.Points)
		}
		// The total is the starters' points with auto-subs applied.
		total := 0
		out, in := map[int]bool{}, map[int]bool{}
		for _, s := range side.AutoSubs {
			out[s.SlotOut], in[s.SlotIn] = true, true
			if s.Status != Confirmed {
				t.Errorf("gameweek 6 is final; sub %+v is not confirmed", s)
			}
		}
		for _, s := range side.Starters {
			if !out[s.Slot] {
				total += s.Points
			}
			sum := 0
			for _, fx := range s.Fixtures {
				for _, l := range fx.Breakdown {
					sum += l.Points
				}
			}
			if sum != s.Points {
				t.Errorf("slot %d: breakdown %d, points %d", s.Slot, sum, s.Points)
			}
		}
		for _, s := range side.Bench {
			if in[s.Slot] {
				total += s.Points
			}
		}
		if total != side.TotalPoints {
			t.Errorf("recomputed total %d, side says %d", total, side.TotalPoints)
		}
	}
	if detail := f.get(t, f.token, "/v1/leagues/"+f.league+"/gameweeks/12/matchups/"+f.game.MatchupsIn(12)[0].ID, nil); detail.Code != 404 {
		t.Errorf("future matchup = %d", detail.Code)
	}
}

func TestLineupPrivacyAndPoints(t *testing.T) {
	f := newDemoFixture(t)
	base := "/v1/leagues/" + f.league + "/teams/"
	// Gameweek 8's deadline (17 October 12:30) is after the clock (12:00):
	// only the owner sees that lineup.
	var own LineupResponse
	if rec := f.get(t, f.token, base+f.mine.ID+"/lineup", &own); rec.Code != 200 {
		t.Fatalf("own = %d %s", rec.Code, rec.Body)
	}
	if own.Data.Gameweek != 8 || own.Data.Locked || own.Data.TotalPoints != nil || len(own.Data.Starters) != 11 ||
		own.Data.Starters[0].Points != nil {
		t.Errorf("own upcoming lineup = %+v", own.Data)
	}
	p := decodeProblem(t, f.get(t, f.token, base+f.theirs.ID+"/lineup?gameweek=8", nil), http.StatusForbidden)
	if problemType(p) != problemTypeBase+"lineup-private" {
		t.Errorf("type = %s", problemType(p))
	}
	// After the deadline anyone in the league sees it, with points.
	var live LineupResponse
	if rec := f.get(t, f.token, base+f.theirs.ID+"/lineup?gameweek=7", &live); rec.Code != 200 {
		t.Fatalf("live = %d %s", rec.Code, rec.Body)
	}
	if !live.Data.Locked || live.Data.TotalPoints == nil || live.Data.ScoreState == nil || *live.Data.ScoreState != ScoreStateLive ||
		live.Data.Starters[0].Points == nil || live.Data.Starters[0].AutoSub == nil {
		t.Errorf("live lineup = %+v", live.Data)
	}
	var team TeamDetailResponse
	if rec := f.get(t, f.token, base+f.mine.ID, &team); rec.Code != 200 {
		t.Fatalf("team = %d", rec.Code)
	}
	if len(team.Data.Roster) != 15 || len(team.Data.Results) != 6 || team.Data.NextOpponent == nil ||
		team.Data.NextOpponent.Gameweek != 8 || team.Data.Record.Played != 6 {
		t.Errorf("team = %+v", team.Data)
	}
}

func TestLineupEntriesCarryTheSeason(t *testing.T) {
	f := newDemoFixture(t)
	var lineup LineupResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/teams/"+f.mine.ID+"/lineup", &lineup); rec.Code != 200 {
		t.Fatalf("lineup = %d %s", rec.Code, rec.Body)
	}
	// The same season figures as the player list.
	var list PlayerListResponse
	f.get(t, f.token, "/v1/leagues/"+f.league+"/players?teamId="+f.mine.ID+"&limit=100", &list)
	byID := map[string]PlayerListItem{}
	for _, it := range list.Data {
		byID[it.Player.Id] = it
	}
	played := 0
	for _, e := range append(lineup.Data.Starters, lineup.Data.Bench...) {
		s, it := e.Season, byID[e.Player.Id]
		if s == nil {
			t.Fatalf("%s has no season", e.Player.DisplayName)
		}
		if s.TotalPoints != it.TotalPoints || (s.Form == nil) != (it.Form == nil) || (s.Form != nil && *s.Form != *it.Form) {
			t.Errorf("%s season = %+v, list = %+v", e.Player.DisplayName, *s, it)
		}
		// Six gameweeks finished and gameweek 7 live: at most seven matches, more in a double.
		if s.ClubMatches < 6 || s.Appearances > s.ClubMatches || (it.Minutes > 0) != (s.Appearances > 0) {
			t.Errorf("%s appearances = %d of %d (minutes %d)", e.Player.DisplayName, s.Appearances, s.ClubMatches, it.Minutes)
		}
		played += s.Appearances
		switch e.Player.Availability {
		case PlayerAvailabilityAvailable:
			if e.ChanceOfPlaying != nil {
				t.Errorf("%s available with a chance of %d", e.Player.DisplayName, *e.ChanceOfPlaying)
			}
		case PlayerAvailabilityInjured, PlayerAvailabilitySuspended, PlayerAvailabilityUnavailable:
			if e.ChanceOfPlaying == nil || *e.ChanceOfPlaying != 0 {
				t.Errorf("%s out with chance %v", e.Player.DisplayName, e.ChanceOfPlaying)
			}
		}
	}
	if played == 0 {
		t.Error("no appearances in the whole squad")
	}
}

func TestPlayersFiltersSortAndPages(t *testing.T) {
	f := newDemoFixture(t)
	base := "/v1/leagues/" + f.league + "/players"
	count := func(query string) int {
		t.Helper()
		n, cursor := 0, ""
		for {
			q := query + "&limit=100"
			if cursor != "" {
				q += "&cursor=" + url.QueryEscape(cursor)
			}
			var page PlayerListResponse
			if rec := f.get(t, f.token, base+"?"+q, &page); rec.Code != 200 {
				t.Fatalf("%s = %d %s", q, rec.Code, rec.Body)
			}
			n += len(page.Data)
			if page.Meta.NextCursor == nil {
				return n
			}
			cursor = *page.Meta.NextCursor
		}
	}
	all, rostered, available := count("status=all"), count("status=rostered"), count("status=available")
	if all != 300 || rostered != 135 || available != 165 {
		t.Errorf("all %d rostered %d available %d", all, rostered, available)
	}
	if n := count("teamId=" + f.mine.ID); n != 15 {
		t.Errorf("team filter = %d", n)
	}
	// Sorted by draft rank, paged by 10 without overlap.
	var p1, p2 PlayerListResponse
	f.get(t, f.token, base+"?status=all&sort=draft_rank&limit=10", &p1)
	f.get(t, f.token, base+"?status=all&sort=draft_rank&limit=10&cursor="+url.QueryEscape(*p1.Meta.NextCursor), &p2)
	if p1.Data[0].DraftRank != 1 || p1.Data[9].DraftRank != 10 || p2.Data[0].DraftRank != 11 {
		t.Errorf("ranks = %d %d %d", p1.Data[0].DraftRank, p1.Data[9].DraftRank, p2.Data[0].DraftRank)
	}
	// Rostered players carry their owner; totals are consistent.
	var top PlayerListResponse
	f.get(t, f.token, base+"?status=rostered&sort=total_points&limit=5", &top)
	for i, it := range top.Data {
		if it.Owner == nil || it.RosterStatus != RosterStatusRostered {
			t.Errorf("rostered item = %+v", it)
		}
		if i > 0 && top.Data[i-1].TotalPoints < it.TotalPoints {
			t.Error("not sorted by total points")
		}
	}
	var detail PlayerDetailResponse
	if rec := f.get(t, f.token, base+"/"+top.Data[0].Player.Id, &detail); rec.Code != 200 {
		t.Fatalf("detail = %d", rec.Code)
	}
	sum := 0
	for _, g := range detail.Data.Gameweeks {
		sum += g.Points
	}
	if sum != detail.Data.TotalPoints || detail.Data.TotalPoints != top.Data[0].TotalPoints ||
		detail.Data.Owner == nil || len(detail.Data.Upcoming) == 0 || detail.Data.Gameweeks[0].Gameweek != 7 {
		t.Errorf("detail = total %d, gameweeks sum %d, %+v", detail.Data.TotalPoints, sum, detail.Data)
	}
	decodeProblem(t, f.get(t, f.token, base+"?position=GK", nil), http.StatusBadRequest)
	decodeProblem(t, f.get(t, f.token, base+"?limit=101", nil), http.StatusBadRequest)
}

func TestStatsMetaGoesStale(t *testing.T) {
	f := newDemoFixture(t)
	// The demo's gameweek 7 is live; record a good poll now.
	ps, _ := f.srv.stats.PollState(context.Background())
	ps.LastGoodAt = f.clock.Now()
	_ = f.srv.stats.SavePollState(context.Background(), ps)

	var gw GameweekStatusResponse
	f.get(t, "", "/v1/gameweeks/current", &gw)
	if gw.Meta.Stale || gw.Data.Current.Number != 7 || gw.Data.Next == nil || gw.Data.Next.Number != 8 ||
		!gw.Meta.DataAsOf.Equal(f.clock.Now()) {
		t.Fatalf("fresh = %+v", gw)
	}
	f.clock.Advance(3*time.Minute + time.Second)
	f.get(t, "", "/v1/gameweeks/current", &gw)
	if !gw.Meta.Stale || !gw.Meta.DataAsOf.Equal(ps.LastGoodAt) || !gw.Meta.ServerTime.Equal(f.clock.Now()) {
		t.Errorf("after 3 minutes without a good poll: %+v", gw.Meta)
	}
}

// Between match days the poller sleeps for hours: the data stays fresh,
// and meta.staleAt tells the client when it would not be.
func TestStatsMetaIdleNotStaleUntilThePollIsOverdue(t *testing.T) {
	f := newDemoFixture(t)
	ps, _ := f.srv.stats.PollState(context.Background())
	ps.Mode, ps.LastGoodAt, ps.ConsecutiveFailures = stats.ModeIdle, f.clock.Now(), 0
	ps.NextPollAt = ps.LastGoodAt.Add(16 * time.Hour)
	_ = f.srv.stats.SavePollState(context.Background(), ps)

	var gw GameweekStatusResponse
	f.clock.Advance(4 * time.Hour)
	f.get(t, "", "/v1/gameweeks/current", &gw)
	want := ps.NextPollAt.Add(stats.StaleAfter)
	if gw.Meta.Stale || gw.Meta.StaleAt == nil || !gw.Meta.StaleAt.Equal(want) {
		t.Fatalf("idle, 4 h on: %+v", gw.Meta)
	}
	f.clock.Set(want.Add(time.Second))
	f.get(t, "", "/v1/gameweeks/current", &gw)
	if !gw.Meta.Stale {
		t.Errorf("poll overdue past its grace: %+v", gw.Meta)
	}
}

func TestNoSeasonDataIs503(t *testing.T) {
	h := newTestHandler()
	p := decodeProblem(t, doAnon(h, http.MethodGet, "/v1/gameweeks/current", ""), http.StatusServiceUnavailable)
	if problemType(p) != problemTypeBase+"season-data-unavailable" {
		t.Errorf("type = %s", problemType(p))
	}
}

// The league list carries stats-derived scores (record, current matchup),
// so its meta follows the poller's freshness like every other score read.
func TestMyLeaguesMetaGoesStaleWithScores(t *testing.T) {
	f := newDemoFixture(t)
	ps, _ := f.srv.stats.PollState(context.Background())
	ps.LastGoodAt = f.clock.Now()
	_ = f.srv.stats.SavePollState(context.Background(), ps)
	f.clock.Advance(3*time.Minute + time.Second)

	var list MyLeagueListResponse
	if rec := f.get(t, f.token, "/v1/me/leagues", &list); rec.Code != 200 {
		t.Fatalf("list = %d %s", rec.Code, rec.Body)
	}
	if len(list.Data) == 0 || list.Data[0].CurrentMatchup == nil {
		t.Fatalf("no scored league in %+v", list.Data)
	}
	if !list.Meta.Stale || !list.Meta.DataAsOf.Equal(ps.LastGoodAt) {
		t.Errorf("league list with live scores after 3 minutes without a good poll: %+v", list.Meta)
	}
}

// Another manager's team page must not reveal their unlocked lineup for
// the next gameweek (GetLineup answers 403 lineup-private for it): the
// roster is ordered by the last locked lineup instead.
func TestTeamPageHidesUnlockedLineup(t *testing.T) {
	f := newDemoFixture(t)
	base := "/v1/leagues/" + f.league + "/teams/" + f.theirs.ID
	roster := func(token string) []string {
		t.Helper()
		var team TeamDetailResponse
		if rec := f.get(t, token, base, &team); rec.Code != 200 {
			t.Fatalf("team = %d %s", rec.Code, rec.Body)
		}
		var out []string
		for i, e := range team.Data.Roster {
			if e.Slot != i+1 {
				t.Fatalf("slot %d at index %d", e.Slot, i)
			}
			out = append(out, e.Player.Id)
		}
		return out
	}
	// Gameweek 8's deadline (Saturday 17 October 12:30) has passed;
	// gameweek 9's has not.
	f.clock.Set(time.Date(2026, 10, 17, 12, 45, 0, 0, time.UTC))
	before := roster(f.token)

	// The owner swaps their goalkeepers for gameweek 9 (not locked yet).
	var own LineupResponse
	if rec := f.get(t, f.other, base+"/lineup?gameweek=9", &own); rec.Code != 200 {
		t.Fatalf("own lineup = %d %s", rec.Code, rec.Body)
	}
	starters, bench := []string{}, []string{}
	for _, e := range own.Data.Starters {
		starters = append(starters, e.Player.Id)
	}
	for _, e := range own.Data.Bench {
		bench = append(bench, e.Player.Id)
	}
	starters[0], bench[0] = bench[0], starters[0]
	body, _ := json.Marshal(map[string]any{"gameweek": 9, "starters": starters, "bench": bench})
	if rec := doAs(f.h, f.other, http.MethodPut, base+"/lineup", string(body)); rec.Code != 200 {
		t.Fatalf("set lineup = %d %s", rec.Code, rec.Body)
	}

	// The owner sees the new order on their own team page...
	if got := roster(f.other); got[0] != starters[0] {
		t.Errorf("owner's slot 1 = %s, want %s", got[0], starters[0])
	}
	// ...everyone else sees exactly what they saw before: the locked
	// gameweek 8 lineup.
	after := roster(f.token)
	if len(after) != len(before) {
		t.Fatalf("roster size %d -> %d", len(before), len(after))
	}
	for i := range before {
		if before[i] != after[i] {
			t.Fatalf("another manager's team page changed with an unlocked lineup save: slot %d %s -> %s", i+1, before[i], after[i])
		}
	}
	w, err := league.SeasonWorld(context.Background(), f.srv.stats)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := f.srv.leagues.Repo().Game(context.Background(), f.league)
	for i, id := range league.EffectiveLineup(g, w, f.theirs.ID, 8).Slots {
		if after[i] != id {
			t.Fatalf("slot %d = %s, want the locked gameweek 8 lineup's %s", i+1, after[i], id)
		}
	}
}

// The list and roster rows carry every remaining fixture of the club, not
// just the next gameweek's: in kickoff order, unplayed only, TBC last.
func TestRowsCarryEveryRemainingFixture(t *testing.T) {
	f := newDemoFixture(t)
	check := func(who string, fs []RowFixture) {
		t.Helper()
		if len(fs) <= 3 {
			t.Errorf("%s: %d fixtures, want every remaining one (more than 3)", who, len(fs))
		}
		tbc := false
		for i, x := range fs {
			if x.KickoffAt == nil {
				tbc = true
				continue
			}
			if tbc {
				t.Errorf("%s: dated fixture %s after a TBC one", who, x.FixtureId)
			}
			if x.Status != nil && (*x.Status == FixtureStatusLive || *x.Status == FixtureStatusFinished) {
				t.Errorf("%s: fixture %s already kicked off", who, x.FixtureId)
			}
			if i > 0 && fs[i-1].KickoffAt != nil && x.KickoffAt.Before(*fs[i-1].KickoffAt) {
				t.Errorf("%s: fixture %s before the one above it", who, x.FixtureId)
			}
		}
	}
	w, err := league.SeasonWorld(context.Background(), f.srv.stats)
	if err != nil {
		t.Fatal(err)
	}
	remaining := map[string]int{} // by club code
	for _, x := range w.Fixtures {
		if !x.Started() {
			remaining[w.Clubs[x.HomeClubID].Code]++
			remaining[w.Clubs[x.AwayClubID].Code]++
		}
	}
	var list PlayerListResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/players?limit=5", &list); rec.Code != 200 || len(list.Data) == 0 {
		t.Fatalf("players = %d %s", rec.Code, rec.Body)
	}
	for _, it := range list.Data {
		check(it.Player.DisplayName, it.NextFixtures)
		for _, x := range it.NextFixtures {
			if _, ok := w.Clubs[clubIDByCode(w, x.Opponent)]; !ok || x.Opponent == it.Player.Club.Code {
				t.Errorf("%s: opponent %q is not another club's code", it.Player.DisplayName, x.Opponent)
			}
		}
		if want := remaining[it.Player.Club.Code]; len(it.NextFixtures) != want {
			t.Errorf("%s: %d fixtures, want %d", it.Player.DisplayName, len(it.NextFixtures), want)
		}
	}
	var team TeamDetailResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/teams/"+f.mine.ID, &team); rec.Code != 200 {
		t.Fatalf("team = %d %s", rec.Code, rec.Body)
	}
	for _, e := range team.Data.Roster {
		check(e.Player.DisplayName, e.NextFixtures)
	}
}

func clubIDByCode(w *league.World, code string) string {
	for id, c := range w.Clubs {
		if c.Code == code {
			return id
		}
	}
	return ""
}

// A row's fixture is compact: the opponent is a club code (no club object)
// and a scheduled fixture leaves out its status.
func TestRowFixtureIsCompact(t *testing.T) {
	f := newDemoFixture(t)
	var raw struct {
		Data []struct {
			NextFixtures []map[string]json.RawMessage `json:"nextFixtures"`
		} `json:"data"`
	}
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/players?limit=1&status=all", &raw); rec.Code != 200 || len(raw.Data) == 0 {
		t.Fatalf("players = %d %s", rec.Code, rec.Body)
	}
	fx := raw.Data[0].NextFixtures[0]
	var code string
	if err := json.Unmarshal(fx["opponent"], &code); err != nil || code == "" {
		t.Errorf("opponent = %s, want a club code", fx["opponent"])
	}
	if st, ok := fx["status"]; ok {
		t.Errorf("scheduled fixture carries status %s", st)
	}
}

// Remaining fixtures are built for the rows on the page only, once per
// club: the work follows the page, not the ~700-player pool.
func TestRowFixturesFollowThePage(t *testing.T) {
	f := newDemoFixture(t)
	remainingScans.Store(0)
	var list PlayerListResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/players?limit=5&status=all", &list); rec.Code != 200 {
		t.Fatalf("players = %d %s", rec.Code, rec.Body)
	}
	clubs := map[string]bool{}
	for _, it := range list.Data {
		clubs[it.Player.Club.Code] = true
	}
	if n := remainingScans.Load(); n != int64(len(clubs)) {
		t.Errorf("list of %d rows over %d clubs scanned fixtures %d times, want once per club", len(list.Data), len(clubs), n)
	}
	remainingScans.Store(0)
	var team TeamDetailResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+f.league+"/teams/"+f.mine.ID, &team); rec.Code != 200 {
		t.Fatalf("team = %d %s", rec.Code, rec.Body)
	}
	clubs = map[string]bool{}
	for _, e := range team.Data.Roster {
		clubs[e.Player.Club.Code] = true
	}
	if n := remainingScans.Load(); n != int64(len(clubs)) {
		t.Errorf("roster over %d clubs scanned fixtures %d times, want once per club", len(clubs), n)
	}
}
