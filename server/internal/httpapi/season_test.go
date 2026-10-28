package httpapi

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/jobs"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// The end-to-end mini-season: a league is created and filled over HTTP,
// drafts with manual picks, autopick and a missed clock, sets lineups,
// plays gameweek 1 on ingested demo stats, runs waivers, adds a free
// agent, trades, and plays gameweek 2 — with the jobs (Runner.Tick) and
// the scorer doing what the River jobs and the poller do in production,
// and a realtime client hearing every kind of event on the way.
func TestMiniSeason(t *testing.T) { runSeason(t, nil) }

// TestMiniSeasonPostgres is the same season on Postgres.
func TestMiniSeasonPostgres(t *testing.T) { runSeason(t, storetest.New(t)) }

type season struct {
	t      *testing.T
	srv    *Server
	clock  *fakeClock
	h      http.Handler
	ingest *stats.Ingester
	runner *jobs.Runner
	tokens []string
	users  []string
	lid    string
	// gameweeks are the demo calendar's real flags.
	gameweeks map[int]stats.Gameweek
	// fixtures are the demo calendar's real fixtures, by gameweek.
	fixtures map[int][]stats.Fixture
}

// call sends a request as user i and decodes a 2xx body into out.
func (s *season) call(i int, method, target, body string, want int, out any) {
	s.t.Helper()
	rec := doAs(s.h, s.tokens[i], method, target, body)
	if rec.Code != want {
		s.t.Fatalf("%s %s as %d = %d, want %d: %s", method, target, i, rec.Code, want, rec.Body)
	}
	if out != nil {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			s.t.Fatalf("%s %s: %v", method, target, err)
		}
	}
}

func (s *season) path(p string) string { return "/v1/leagues/" + s.lid + p }

// setClock moves time to at and signs everyone in again (access tokens
// last an hour).
func (s *season) setClock(at time.Time) {
	s.t.Helper()
	s.clock.t = at
	for i, tok := range s.tokens {
		uid := s.users[i]
		if uid == "" {
			uid = userOf(s.t, s.srv, tok)
		}
		fresh, err := s.srv.auth.Issue(context.Background(), uid)
		if err != nil {
			s.t.Fatal(err)
		}
		s.tokens[i] = fresh.AccessToken
	}
}

// refresh issues a new token for the user of tok (after a clock move).
func (s *season) refresh(tok string) string {
	s.t.Helper()
	fresh, err := s.srv.auth.Issue(context.Background(), userOf(s.t, s.srv, tok))
	if err != nil {
		s.t.Fatal(err)
	}
	return fresh.AccessToken
}

// tick runs the due jobs.
func (s *season) tick() jobs.TickResult {
	s.t.Helper()
	res, err := s.runner.Tick(context.Background())
	if err != nil || res.Errors > 0 {
		s.t.Fatalf("tick: %+v %v", res, err)
	}
	return res
}

// play ingests a gameweek's demo stats and rescores, as the poller does.
func (s *season) play(gw int) {
	s.t.Helper()
	ctx := context.Background()
	src := demo.Source(testOptions().Logger)
	st, err := src.GameweekStatus(ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	live, err := src.EventLive(ctx, gw)
	if err != nil {
		s.t.Fatal(err)
	}
	changed, err := s.ingest.Live(ctx, live, st)
	if err != nil {
		s.t.Fatal(err)
	}
	if err := s.srv.stats.Apply(ctx, stats.Batch{Gameweeks: []stats.Gameweek{s.gameweeks[gw]}, Fixtures: s.fixtures[gw]}); err != nil {
		s.t.Fatal(err)
	}
	sc := &league.Scorer{Leagues: s.srv.leagues.Repo(), Stats: s.srv.stats, Rules: scoring.StandardLineup(), Notify: s.srv}
	if _, err := sc.Recompute(ctx, gw, changed, true); err != nil {
		s.t.Fatal(err)
	}
}

func runSeason(t *testing.T, pool *pgxpool.Pool) {
	srv, clock := newTestServerOn(pool)
	srv.log = testOptions().Logger
	ctx := context.Background()
	s := &season{t: t, srv: srv, clock: clock, h: newHandler(testOptions(), newRoutes(testOptions(), srv))}
	s.runner = &jobs.Runner{Leagues: srv.leagues.Repo(), Stats: srv.stats, Now: srv.now, Notify: srv, Log: testOptions().Logger}

	// Before the season: the demo season's clubs, players, gameweeks and
	// fixtures, no stats yet. Monday 10 August, gameweek 1's deadline is
	// Friday 14 August 17:30.
	clock.t = time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	testToken = s.refresh(testToken)
	src := demo.Source(testOptions().Logger)
	s.ingest = stats.NewIngester(srv.stats, demo.ProviderName, srv.now)
	b, _ := src.Bootstrap(ctx)
	fixtures, _ := src.Fixtures(ctx)
	status, _ := src.GameweekStatus(ctx)
	if err := s.ingest.Season(ctx, b, fixtures, status); err != nil {
		t.Fatal(err)
	}
	// The demo calendar is as of gameweek 7: mark gameweeks 2 on (and
	// their fixtures) as not played yet; play() finishes each one as it is
	// ingested. A gameweek counts in the standings once all its fixtures
	// are finished.
	stored, _ := srv.stats.Fixtures(ctx)
	s.fixtures = map[int][]stats.Fixture{}
	var unplayed []stats.Fixture
	for _, f := range stored {
		s.fixtures[f.Gameweek] = append(s.fixtures[f.Gameweek], f)
		if f.Gameweek >= 2 {
			f.Status, f.HomeScore, f.AwayScore = stats.FixtureScheduled, nil, nil
			unplayed = append(unplayed, f)
		}
	}
	gws, _ := srv.stats.Gameweeks(ctx)
	s.gameweeks = map[int]stats.Gameweek{}
	var open []stats.Gameweek
	for _, g := range gws {
		s.gameweeks[g.Number] = g
		if g.Number >= 2 {
			g.Finished, g.BonusAdded, g.DataChecked = false, false, false
			open = append(open, g)
		}
	}
	if err := srv.stats.Apply(ctx, stats.Batch{Gameweeks: open, Fixtures: unplayed}); err != nil {
		t.Fatal(err)
	}
	if err := srv.stats.SavePollState(ctx, stats.PollState{Mode: stats.ModeIdle, LastGoodAt: clock.t, NextPollAt: clock.t.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}

	// ------------------------------------------------------------ league
	s.tokens, s.users = []string{testToken}, []string{userOf(t, srv, testToken)}
	var created LeagueResponse
	s.call(0, "POST", "/v1/leagues", `{"name":"Mini Season","teamCount":5,"teamName":"Commish FC","settings":{"draft":{"pickTimerSeconds":30}}}`, 201, &created)
	s.lid = created.Data.League.Id
	// A realtime client on the league, as a connected socket would be.
	// Delivery to the hub is synchronous here, so every event is in
	// client.Send (or already read) when a request returns; the reader only
	// keeps the buffer from filling, and the client must never be dropped.
	var (
		mu     sync.Mutex
		heard  []string
		kicked string
	)
	client := srv.hub.Connect(userOf(t, srv, testToken), "", func(_ int, reason string) {
		mu.Lock()
		kicked = reason
		mu.Unlock()
	})
	if err := srv.hub.Subscribe(ctx, client, s.lid, nil); err != nil {
		t.Fatal(err)
	}
	record := func(f []byte) {
		var m struct{ Type string }
		_ = json.Unmarshal(f, &m)
		mu.Lock()
		heard = append(heard, m.Type)
		mu.Unlock()
	}
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case f := <-client.Send:
				record(f)
			case <-done:
				return
			}
		}
	}()
	drain := func() {
		close(done)
		<-stopped
		for len(client.Send) > 0 {
			record(<-client.Send)
		}
		mu.Lock()
		defer mu.Unlock()
		if kicked != "" {
			t.Errorf("the realtime client was dropped: %s", kicked)
		}
	}
	var inv InviteResponse
	s.call(0, "POST", s.path("/invites"), `{}`, 201, &inv)
	for i := 1; i < 5; i++ {
		s.tokens = append(s.tokens, signIn(srv, fmt.Sprintf("Manager %d", i), fmt.Sprintf("m%d@example.com", i)))
		s.users = append(s.users, userOf(t, srv, s.tokens[i]))
		s.call(i, "POST", "/v1/invites/"+inv.Data.Code+"/accept", fmt.Sprintf(`{"teamName":"Team %d"}`, i), 201, nil)
	}
	var home LeagueHomeResponse
	s.call(1, "GET", s.path(""), "", 200, &home)
	if home.Data.League.MemberCount != 5 || home.Data.League.Status != LeagueStatusPreDraft {
		t.Fatalf("league = %+v", home.Data.League)
	}

	// ------------------------------------------------------------ draft
	var members MemberListResponse
	s.call(0, "GET", s.path("/members"), "", 200, &members)
	teamOf := map[string]int{} // team id -> user index
	for i, tok := range s.tokens {
		uid := userOf(t, srv, tok)
		for _, m := range members.Data {
			if m.User.Id == uid {
				teamOf[m.Team.Id] = i
			}
		}
	}
	// Join order is the round-1 order: user 0 first.
	var d DraftResponse
	s.call(0, "PATCH", s.path("/draft"), `{"pickTimerSeconds":30}`, 200, &d)
	s.call(1, "PUT", s.path("/draft/autopick"), `{"enabled":true}`, 200, nil)
	s.call(2, "PUT", s.path("/draft/autopick"), `{"enabled":true}`, 200, nil)
	s.call(0, "POST", s.path("/draft/start"), "", 200, &d)
	if d.Data.Status != DraftStatusInProgress || d.Data.CurrentPick == nil || teamOf[d.Data.CurrentPick.Team.Id] != 0 {
		t.Fatalf("started = %+v", d.Data.CurrentPick)
	}
	// User 0 picks the best goalkeeper by hand.
	var players PlayerListResponse
	s.call(0, "GET", s.path("/players?status=available&position=GKP&sort=draft_rank&limit=1"), "", 200, &players)
	s.call(0, "POST", s.path("/draft/picks"), `{"playerId":"`+players.Data[0].Player.Id+`"}`, 201, nil)
	// Users 1 and 2 are on autopick and picked at once; user 3 is on the
	// clock and misses it: the tick autopicks and turns autopick on.
	s.call(3, "GET", s.path("/draft"), "", 200, &d)
	if len(d.Data.Picks) != 3 || teamOf[d.Data.CurrentPick.Team.Id] != 3 {
		t.Fatalf("after the autopicks: %d picks, on the clock %v", len(d.Data.Picks), d.Data.CurrentPick)
	}
	clock.Advance(31 * time.Second)
	if res := s.tick(); res.ClocksSettled != 1 {
		t.Fatalf("tick = %+v", res)
	}
	s.call(3, "GET", s.path("/draft"), "", 200, &d)
	if !d.Data.Order[3].Autopick || d.Data.Picks[3].Method != DraftPickMethodRank {
		t.Fatalf("missed clock: %+v %+v", d.Data.Order[3], d.Data.Picks[3])
	}
	// Everyone else goes on autopick; the draft completes.
	s.call(4, "PUT", s.path("/draft/autopick"), `{"enabled":true}`, 200, nil)
	s.call(0, "PUT", s.path("/draft/autopick"), `{"enabled":true}`, 200, nil)
	s.call(0, "GET", s.path("/draft"), "", 200, &d)
	if d.Data.Status != DraftStatusComplete || len(d.Data.Picks) != 75 {
		t.Fatalf("draft = %s with %d picks", d.Data.Status, len(d.Data.Picks))
	}
	var sched ScheduleResponse
	s.call(2, "GET", s.path("/schedule"), "", 200, &sched)
	if len(sched.Data) != 38 || len(sched.Data[0].Matchups) != 3 {
		t.Fatalf("schedule: %d weeks, %d matchups in the first", len(sched.Data), len(sched.Data[0].Matchups))
	}

	// ------------------------------------------------------------ lineups
	me := members.Data[0].Team.Id
	for _, m := range members.Data {
		if teamOf[m.Team.Id] == 0 {
			me = m.Team.Id
		}
	}
	var lu LineupResponse
	s.call(0, "GET", s.path("/teams/"+me+"/lineup?gameweek=1"), "", 200, &lu)
	ids := func(es []LineupEntry) []string {
		var out []string
		for _, e := range es {
			out = append(out, e.Player.Id)
		}
		return out
	}
	starters, bench := ids(lu.Data.Starters), ids(lu.Data.Bench)
	// 4-4-2 to 3-5-2: the fourth defender for the bench midfielder.
	for i, e := range lu.Data.Bench {
		if e.Player.Position == MID {
			starters[4], bench[i] = bench[i], starters[4]
			break
		}
	}
	q := func(ids []string) string { return `["` + strings.Join(ids, `","`) + `"]` }
	s.call(0, "PUT", s.path("/teams/"+me+"/lineup"), `{"gameweek":1,"starters":`+q(starters)+`,"bench":`+q(bench)+`}`, 200, &lu)
	if lu.Data.Formation != "3-5-2" {
		t.Fatalf("formation = %s", lu.Data.Formation)
	}
	s.call(1, "GET", s.path("/teams/"+me+"/lineup?gameweek=1"), "", 403, nil) // private until the deadline

	// ------------------------------------------------------------ gameweek 1
	s.setClock(time.Date(2026, 8, 17, 9, 0, 0, 0, time.UTC)) // after GW1, before GW2's waivers
	if res := s.tick(); res.LineupLocks != 1 {
		t.Fatalf("lock tick = %+v", res)
	}
	s.call(0, "PUT", s.path("/teams/"+me+"/lineup"), `{"gameweek":1,"starters":`+q(starters)+`,"bench":`+q(bench)+`}`, 409, nil)
	s.play(1)
	var ms MatchupListResponse
	s.call(1, "GET", s.path("/gameweeks/1/matchups"), "", 200, &ms)
	total := 0
	for _, m := range ms.Data {
		if m.State != ScoreStateFinal && m.State != ScoreStateOfficial {
			t.Fatalf("gameweek 1 state = %s", m.State)
		}
		for _, side := range []MatchupSide{m.Home, m.Away} {
			if ts, err := side.AsTeamScore(); err == nil && ts.Kind == TeamScoreKindTeam {
				total += ts.Points
			}
		}
	}
	if total == 0 {
		t.Fatal("no points in gameweek 1")
	}
	s.call(1, "GET", s.path("/teams/"+me+"/lineup?gameweek=1"), "", 200, &lu) // public after the deadline
	if lu.Data.TotalPoints == nil || lu.Data.Formation != "3-5-2" {
		t.Fatalf("locked lineup = %+v", lu.Data)
	}
	var table StandingsResponse
	s.call(2, "GET", s.path("/standings"), "", 200, &table)
	for _, r := range table.Data {
		if r.Played != 1 {
			t.Fatalf("standings after gameweek 1: %+v", r)
		}
	}

	// ------------------------------------------------------------ waivers
	// Waiver period for gameweek 2: claim the best unowned defender.
	s.call(1, "GET", s.path("/players?status=waivers&position=DEF&sort=draft_rank&limit=1"), "", 200, &players)
	target := players.Data[0].Player.Id
	var roster TeamDetailResponse
	team1 := ""
	for id, i := range teamOf {
		if i == 1 {
			team1 = id
		}
	}
	s.call(1, "GET", s.path("/teams/"+team1), "", 200, &roster)
	drop := ""
	for _, e := range roster.Data.Roster {
		if e.Player.Position == DEF {
			drop = e.Player.Id
		}
	}
	var claim WaiverClaimResponse
	s.call(1, "POST", s.path("/waivers/claims"), `{"addPlayerId":"`+target+`","dropPlayerId":"`+drop+`"}`, 201, &claim)
	s.setClock(time.Date(2026, 8, 20, 17, 30, 0, 0, time.UTC)) // gameweek 2's waiver run
	if res := s.tick(); res.WaiverRuns != 1 {
		t.Fatalf("waiver tick = %+v", res)
	}
	var claims WaiverClaimListResponse
	s.call(1, "GET", s.path("/waivers/claims?status=processed"), "", 200, &claims)
	if len(claims.Data) != 1 || claims.Data[0].Status != WaiverClaimStatusWon {
		t.Fatalf("claims = %+v", claims.Data)
	}
	// The next run (gameweek 3) starts from the reverse of the standings
	// after gameweek 1, the last finished one: fewest league points, then
	// fewest points for, then the earliest to join (users joined in order).
	var ws WaiverStateResponse
	s.call(1, "GET", s.path("/waivers"), "", 200, &ws)
	want := slices.Clone(table.Data)
	slices.SortFunc(want, func(a, b StandingRow) int {
		return cmp.Or(cmp.Compare(a.LeaguePoints, b.LeaguePoints), cmp.Compare(a.PointsFor, b.PointsFor),
			cmp.Compare(teamOf[a.Team.Id], teamOf[b.Team.Id]))
	})
	var got, wantIDs []string
	for i := range want {
		wantIDs = append(wantIDs, want[i].Team.Id)
		got = append(got, ws.Data.Priority[i].Team.Id)
	}
	if ws.Data.LastRunAt == nil || ws.Data.Order != ReverseStandings || ws.Data.NextRunGameweek == nil ||
		*ws.Data.NextRunGameweek != 3 || !slices.Equal(got, wantIDs) {
		t.Fatalf("waiver state = %+v, want order %v", ws.Data, wantIDs)
	}
	// Free agency until Friday's deadline: user 2 adds a forward.
	s.call(2, "GET", s.path("/players?status=free_agent&position=FWD&sort=draft_rank&limit=1"), "", 200, &players)
	fa := players.Data[0].Player.Id
	team2 := ""
	for id, i := range teamOf {
		if i == 2 {
			team2 = id
		}
	}
	s.call(2, "GET", s.path("/teams/"+team2), "", 200, &roster)
	fwd := ""
	for _, e := range roster.Data.Roster {
		if e.Player.Position == FWD {
			fwd = e.Player.Id
		}
	}
	s.call(2, "POST", s.path("/free-agent-moves"), `{"addPlayerId":"`+fa+`","dropPlayerId":"`+fwd+`"}`, 201, nil)

	// ------------------------------------------------------------ trade
	// Gameweek 2's trade deadline (Wednesday 17:30) has passed, so a trade
	// now counts from gameweek 3.
	var mine, theirs TeamDetailResponse
	s.call(0, "GET", s.path("/teams/"+me), "", 200, &mine)
	s.call(0, "GET", s.path("/teams/"+team2), "", 200, &theirs)
	gk := func(r TeamDetailResponse) string {
		for _, e := range r.Data.Roster {
			if e.Player.Position == GKP {
				return e.Player.Id
			}
		}
		return ""
	}
	var tr TradeResponse
	s.call(0, "POST", s.path("/trades"), `{"receivingTeamId":"`+team2+`","offeredPlayerIds":["`+gk(mine)+`"],"requestedPlayerIds":["`+gk(theirs)+`"]}`, 201, &tr)
	s.call(2, "POST", s.path("/trades/"+tr.Data.Id+"/accept"), "", 200, &tr)
	if tr.Data.Status != TradeStatusProcessed || tr.Data.EffectiveGameweek == nil || *tr.Data.EffectiveGameweek != 3 {
		t.Fatalf("trade = %+v", tr.Data)
	}
	var feed TransactionListResponse
	s.call(3, "GET", s.path("/transactions"), "", 200, &feed)
	var kinds []string
	for _, x := range feed.Data {
		raw, _ := x.MarshalJSON()
		var k struct{ Kind string }
		_ = json.Unmarshal(raw, &k)
		kinds = append(kinds, k.Kind)
	}
	slices.Sort(kinds)
	// The draft's missed clock is in the feed too.
	if !slices.Equal(kinds, []string{"draft_autopick", "free_agent", "trade", "waiver"}) {
		t.Fatalf("feed kinds = %v", kinds)
	}

	// ------------------------------------------------------------ gameweek 2
	s.setClock(time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC))
	s.tick()
	s.play(2)
	s.call(4, "GET", s.path("/standings"), "", 200, &table)
	for _, r := range table.Data {
		if r.Played != 2 {
			t.Fatalf("standings after gameweek 2: %+v", r)
		}
	}
	// The claim and the free agent count in gameweek 2; the trade from 3.
	g, _ := srv.leagues.Repo().Game(ctx, s.lid)
	if g.OwnerAt(2)[target] != team1 || g.OwnerAt(2)[fa] != team2 || g.OwnerAt(2)[gk(mine)] != me || g.OwnerAt(3)[gk(mine)] != team2 {
		t.Fatal("roster moves did not take effect from the right gameweeks")
	}
	s.call(0, "GET", s.path("/teams/"+me+"/lineup?gameweek=2"), "", 200, &lu)
	if lu.Data.Formation != "3-5-2" {
		t.Fatalf("the lineup did not carry over: %s", lu.Data.Formation)
	}

	// The realtime client heard every kind of event along the way.
	drain()
	slices.Sort(heard)
	heard = slices.Compact(heard)
	for _, want := range []string{"draft.updated", "draft.pick_made", "league.updated", "lineup.locked",
		"matchups.updated", "waivers.processed", "trade.updated"} {
		if _, ok := slices.BinarySearchFunc(heard, want, cmp.Compare[string]); !ok {
			t.Errorf("no %s event (heard %v)", want, heard)
		}
	}
}
