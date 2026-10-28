package league

import (
	"io"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// The scoring golden test runs on the hand-crafted FPL fixtures in
// provider/fpl/testdata: gameweek 1 is final, gameweek 2 is live (one
// match over with bonus not yet added, one in play). Player numbers below
// are FPL element codes minus 1000; every expected total was worked out by
// hand from the per-player points (themselves checked against FPL's own
// explain totals in the stats golden test).

// Team A: P1 | P2 P14 P16 P18 | P3 P21 P23 P25 | P4 P28 ; bench P13 P15 P24 P30
// Team B: P5 | P6 P10 P17 P19 | P7 P11 P22 | P8 P12 P29 ; bench P9 P20 P26 P27
var (
	lineupA = []int{1, 2, 14, 16, 18, 3, 21, 23, 25, 4, 28, 13, 15, 24, 30}
	lineupB = []int{5, 6, 10, 17, 19, 7, 11, 22, 8, 12, 29, 9, 20, 26, 27}
)

// scoringFixture loads the stats and a two-team in-season league.
func scoringFixture(t *testing.T, repo Repo, statsRepo stats.Repo, matchups func(a, b string) []Matchup) (*League, map[int]string) {
	t.Helper()
	src := fpl.New(fpl.Options{
		Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata")},
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)), MinInterval: -1,
	})
	in := stats.NewIngester(statsRepo, "fpl", time.Now)
	b, err := src.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, _ := src.Fixtures(ctx)
	st, _ := src.GameweekStatus(ctx)
	if err := in.Season(ctx, b, fixtures, st); err != nil {
		t.Fatal(err)
	}
	for _, gw := range []int{1, 2} {
		live, err := src.EventLive(ctx, gw)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := in.Live(ctx, live, st); err != nil {
			t.Fatal(err)
		}
	}
	codes, _ := statsRepo.ExternalIDs(ctx, "fpl", "player")
	pid := map[int]string{}
	for n := 1; n <= 30; n++ {
		pid[n] = codes[strconv.Itoa(1000+n)]
	}

	svc := NewService(repo, time.Now)
	l, err := svc.Create(ctx, usrA, "Golden League", 4, "Team A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := svc.CreateInvite(ctx, usrA, l.ID, nil, nil)
	l, _, err = svc.AcceptInvite(ctx, usrB, inv.Code, "Team B")
	if err != nil {
		t.Fatal(err)
	}
	a, bt := l.Teams[0].ID, l.Teams[1].ID
	g := &Game{Matchups: matchups(a, bt)}
	for team, lineup := range map[string][]int{a: lineupA, bt: lineupB} {
		var slots []string
		for _, n := range lineup {
			slots = append(slots, pid[n])
			g.Rosters = append(g.Rosters, RosterSlot{TeamID: team, PlayerID: pid[n], FromGW: 1, AcquiredVia: "draft"})
		}
		g.Lineups = append(g.Lineups, Lineup{TeamID: team, FromGW: 1, Slots: slots, UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)})
	}
	if err := repo.Tx(ctx, func(tx Tx) error { return tx.StartSeason(ctx, l.ID, g) }); err != nil {
		t.Fatal(err)
	}
	return l, pid
}

// statsImpls pairs each league Repo with a stats Repo on the same database.
func scoringImpls(t *testing.T, test func(t *testing.T, repo Repo, statsRepo stats.Repo)) {
	t.Run("memory", func(t *testing.T) { test(t, NewMemory(), stats.NewMemory()) })
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		seedUsers(t, pool)
		test(t, NewPostgres(pool), stats.NewPostgres(pool))
	})
}

func TestGoldenTeamScores(t *testing.T) {
	scoringImpls(t, func(t *testing.T, repo Repo, statsRepo stats.Repo) {
		l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup {
			return []Matchup{
				{ID: ids.New("mu"), Gameweek: 1, HomeTeamID: a, AwayTeamID: b, State: scoring.StateUpcoming},
				{ID: ids.New("mu"), Gameweek: 2, HomeTeamID: a, AwayTeamID: "", State: scoring.StateUpcoming},
				{ID: ids.New("mu"), Gameweek: 2, HomeTeamID: b, AwayTeamID: "", State: scoring.StateUpcoming},
			}
		})
		a, b := l.Teams[0].ID, l.Teams[1].ID
		sc := &Scorer{Leagues: repo, Stats: statsRepo, Rules: scoring.StandardLineup()}
		for _, gw := range []int{1, 2} {
			if _, err := sc.Recompute(ctx, gw, nil, true); err != nil {
				t.Fatal(err)
			}
		}
		g, err := repo.Game(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		check := func(team string, gw, points, bench int, state scoring.ScoreState, subs []scoring.Sub) {
			t.Helper()
			s, ok := g.Score(team, gw)
			if !ok || s.Points != points || s.BenchPoints != bench || s.State != state || s.Rev != 1 {
				t.Errorf("team %s gw %d = %+v, want %d points, %d bench, %s", team[:6], gw, s, points, bench, state)
			}
			if !slices.Equal(s.AutoSubs, subs) {
				t.Errorf("team %s gw %d subs = %+v, want %+v", team[:6], gw, s.AutoSubs, subs)
			}
		}
		// Gameweek 1, final. A: 42 from the XI, then P15 (+1) for P18 and P24
		// (+3) for P25, who did not play. B: 33, then P20 (+1) for P17 and
		// P26 (+1) for the forward P8, keeping 4-4-2 legal; P9 (11) stays on
		// the bench.
		check(a, 1, 46, 0, scoring.StateFinal, []scoring.Sub{
			{OutSlot: 5, InSlot: 13, OutPlayerID: pid[18], InPlayerID: pid[15], Status: scoring.SubConfirmed},
			{OutSlot: 9, InSlot: 14, OutPlayerID: pid[25], InPlayerID: pid[24], Status: scoring.SubConfirmed},
		})
		check(b, 1, 35, 11, scoring.StateFinal, []scoring.Sub{
			{OutSlot: 4, InSlot: 13, OutPlayerID: pid[17], InPlayerID: pid[20], Status: scoring.SubConfirmed},
			{OutSlot: 9, InSlot: 14, OutPlayerID: pid[8], InPlayerID: pid[26], Status: scoring.SubConfirmed},
		})
		// Gameweek 2, live, with provisional bonus. A's non-players are on
		// Northgate, still playing: no sub yet. B's P19 cannot play any more,
		// but the first eligible bench outfielder still to play (P27, Cliffton)
		// has not, so the projection waits.
		check(a, 2, 41, 4, scoring.StateLive, nil) // bench: P15 2 + P24 2
		check(b, 2, 36, 2, scoring.StateLive, nil)

		ms := g.MatchupsIn(1)
		if len(ms) != 1 || ms[0].HomePoints != 46 || ms[0].AwayPoints != 35 || ms[0].State != scoring.StateFinal {
			t.Errorf("gameweek 1 matchup = %+v", ms)
		}
		for _, m := range g.MatchupsIn(2) {
			// League average: floor((41 + 36) / 2) = 38.
			if m.AwayTeamID != "" || m.AwayPoints != 38 || m.State != scoring.StateLive {
				t.Errorf("gameweek 2 matchup = %+v", m)
			}
		}

		// Standings count only completed gameweeks.
		w, err := LoadWorld(ctx, statsRepo)
		if err != nil {
			t.Fatal(err)
		}
		rows := Standings(l, g, w)
		if len(rows) != 2 || rows[0].TeamID != a || rows[0].Wins != 1 || rows[0].PointsFor != 46 ||
			rows[0].LeaguePoints() != 3 || rows[1].Losses != 1 || rows[1].Played != 1 || len(rows[0].Form) != 1 {
			t.Errorf("standings = %+v", rows)
		}

		// A second recompute with nothing new changes nothing.
		res, err := sc.Recompute(ctx, 2, nil, false)
		if err != nil || res.Leagues != 0 || res.Skipped != 1 {
			t.Errorf("idle recompute = %+v %v", res, err)
		}
		res, err = sc.Recompute(ctx, 2, []string{pid[3]}, false)
		if err != nil || res.Leagues != 1 || len(res.ChangedTeams) != 0 {
			t.Errorf("recompute with unchanged scores = %+v %v", res, err)
		}
	})
}

// A starter with 0 minutes becomes auto-sub eligible when his club's
// fixture ends (ADR 0008 rule 10). He has no stats row, so nothing about
// him changes upstream; the fixture's status does. That alone must rescore
// every league with a rostered player from either club, even while the
// gameweek's state stays live.
func TestFixtureEndingRescoresLeagues(t *testing.T) {
	scoringImpls(t, func(t *testing.T, repo Repo, statsRepo stats.Repo) {
		scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup {
			return []Matchup{{ID: ids.New("mu"), Gameweek: 2, HomeTeamID: a, AwayTeamID: b, State: scoring.StateUpcoming}}
		})
		src := fpl.New(fpl.Options{
			Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata")},
			Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)), MinInterval: -1,
		})
		in := stats.NewIngester(statsRepo, "fpl", time.Now)
		st, _ := src.GameweekStatus(ctx)
		sc := &Scorer{Leagues: repo, Stats: statsRepo, Rules: scoring.StandardLineup()}

		// An earlier poll: Ashford v Brookvale (2603) still in play.
		live, err := src.EventLive(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		for i := range live.Fixtures {
			if live.Fixtures[i].ExternalID == "2603" {
				live.Fixtures[i].Status = provider.FixtureLive
			}
		}
		if _, err := in.Live(ctx, live, st); err != nil {
			t.Fatal(err)
		}
		if _, err := sc.Recompute(ctx, 2, nil, true); err != nil {
			t.Fatal(err)
		}

		// Now 2603 is over: no player's row changes, the gameweek is still
		// live (2604 is in play), but Ashford's and Brookvale's 0-minute
		// starters can now be subbed.
		live, err = src.EventLive(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		changed, err := in.Live(ctx, live, st)
		if err != nil {
			t.Fatal(err)
		}
		res, err := sc.Recompute(ctx, 2, changed, false)
		if err != nil || res.Leagues != 1 || res.Skipped != 0 {
			t.Errorf("fixture end: recompute = %+v %v, want the league rescored", res, err)
		}
	})
}

// The formation shown is the XI that scores: after auto-subs. Team B
// starts 4-3-3 in gameweek 1; the forward P8 did not play and the
// midfielder P26 comes on, so the XI that counts is 4-4-2.
func TestFormationFollowsAutoSubs(t *testing.T) {
	repo, statsRepo := NewMemory(), stats.NewMemory()
	l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup { return nil })
	g, _ := repo.Game(ctx, l.ID)
	w, err := LoadWorld(ctx, statsRepo)
	if err != nil {
		t.Fatal(err)
	}
	r := ScoreTeam(scoring.StandardLineup(), g, w, l.Teams[1].ID, 1)
	if len(r.Subs) != 2 || r.Subs[1].OutPlayerID != pid[8] || r.Subs[1].InPlayerID != pid[26] {
		t.Fatalf("subs = %+v", r.Subs)
	}
	if r.Formation != "4-4-2" {
		t.Errorf("formation = %s, want 4-4-2 (4-3-3 with P26 on for P8)", r.Formation)
	}
}

func TestScoreTeamSlotsAndDefaultLineup(t *testing.T) {
	repo, statsRepo := NewMemory(), stats.NewMemory()
	l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup { return nil })
	g, _ := repo.Game(ctx, l.ID)
	w, err := LoadWorld(ctx, statsRepo)
	if err != nil {
		t.Fatal(err)
	}
	r := ScoreTeam(scoring.StandardLineup(), g, w, l.Teams[0].ID, 1)
	if r.Formation != "4-4-2" || len(r.Slots) != 15 || r.Slots[4].AutoSub != AutoSubConfirmedOut ||
		r.Slots[12].AutoSub != AutoSubConfirmedIn || r.Slots[1].Points != 12 {
		t.Errorf("result = %+v", r)
	}
	saved, _ := g.LineupAt(l.Teams[0].ID, 1)
	for i, s := range r.Slots {
		if s.Slot != i+1 || s.PlayerID != saved.Slots[i] {
			t.Errorf("slot %d = %+v, want slot number %d holding %s", i, s, i+1, saved.Slots[i])
		}
	}
	if r2 := ScoreTeam(scoring.StandardLineup(), g, w, l.Teams[0].ID, 2); !r2.Slots[0].BonusProvisional {
		t.Errorf("gameweek 2 keeper bonus should be provisional: %+v", r2.Slots[0])
	}

	// A saved lineup that no longer matches the roster falls back to the
	// default, which is always a legal XI.
	g.Lineups[0].Slots[0] = pid[9]
	eff := EffectiveLineup(g, w, g.Lineups[0].TeamID, 1)
	roster := map[string]scoring.Position{}
	for _, id := range g.RosterAt(g.Lineups[0].TeamID, 1) {
		roster[id] = w.Players[id].Position
	}
	if v := scoring.StandardLineup().ValidateLineup(roster, eff.Slots[:11], eff.Slots[11:]); len(v) != 0 {
		t.Errorf("default lineup is illegal: %+v", v)
	}
}

func TestRoundRobin(t *testing.T) {
	for _, n := range []int{4, 5, 10, 11} {
		var teams []string
		for i := 0; i < n; i++ {
			teams = append(teams, string(rune('a'+i)))
		}
		ms := RoundRobin(teams, 1, 38)
		pairs := map[[2]string]int{}
		for gw := 1; gw <= 38; gw++ {
			seen := map[string]bool{}
			avg := 0
			for _, m := range ms {
				if m.Gameweek != gw {
					continue
				}
				for _, side := range []string{m.HomeTeamID, m.AwayTeamID} {
					if side == "" {
						avg++
						continue
					}
					if seen[side] {
						t.Fatalf("n=%d gw %d: %s plays twice", n, gw, side)
					}
					seen[side] = true
				}
				if m.HomeTeamID == "" {
					t.Fatalf("average listed at home")
				}
				k := [2]string{min(m.HomeTeamID, m.AwayTeamID), max(m.HomeTeamID, m.AwayTeamID)}
				pairs[k]++
			}
			if len(seen) != n || avg != n%2 {
				t.Fatalf("n=%d gw %d: %d teams play, %d average sides", n, gw, len(seen), avg)
			}
		}
		// A cycle is c = n-1 gameweeks (n with the average side for odd
		// n) and every pair meets once per cycle, so over 38 gameweeks a
		// pair meets floor(38/c) or ceil(38/c) times (n=4: 12 or 13).
		c := n - 1 + n%2
		lo, hi := 38/c, (38+c-1)/c
		for i := 0; i < n; i++ {
			for j := i + 1; j < n; j++ {
				if got := pairs[[2]string{teams[i], teams[j]}]; got < lo || got > hi {
					t.Errorf("n=%d: %s v %s meet %d times, want %d..%d", n, teams[i], teams[j], got, lo, hi)
				}
			}
		}
	}
}

// FPL: "Playing in a Gameweek means making an appearance on the pitch or
// receiving a yellow / red card." In gameweek 1 Team A's P18 (DEF, slot 5)
// did not play and P15 came on for him. Booked on the bench instead
// (0 minutes, a yellow in 2601), he played: he scores -1 and is not
// subbed. The first sub goes to P25 (MID, slot 9), and the first bench
// outfielder who played and keeps the XI legal is now P15 (DEF, +1;
// 5-3-2), so P24 (+3) stays on the bench. Team A: 42 from the XI,
// -1 for P18, +1 for P15 = 42; bench P24's 3.
func TestBookedOnTheBenchCountsAsPlaying(t *testing.T) {
	scoringImpls(t, func(t *testing.T, repo Repo, statsRepo stats.Repo) {
		l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup {
			return []Matchup{{ID: ids.New("mu"), Gameweek: 1, HomeTeamID: a, AwayTeamID: b, State: scoring.StateUpcoming}}
		})
		src := fpl.New(fpl.Options{
			Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata")},
			Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)), MinInterval: -1,
		})
		st, _ := src.GameweekStatus(ctx)
		live, err := src.EventLive(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		booked := provider.PlayerLive{PlayerExternalID: "1018", YellowCards: 1, BPS: -3, TotalPoints: -1,
			Explain: []provider.FixtureExplain{{FixtureExternalID: "2601", Lines: []provider.ExplainLine{
				{Stat: provider.StatYellowCards, Value: 1, Points: -1},
			}}}}
		found := false
		for i := range live.Players {
			if live.Players[i].PlayerExternalID == "1018" {
				live.Players[i], found = booked, true
			}
		}
		if !found {
			live.Players = append(live.Players, booked)
		}
		if _, err := stats.NewIngester(statsRepo, "fpl", time.Now).Live(ctx, live, st); err != nil {
			t.Fatal(err)
		}
		sc := &Scorer{Leagues: repo, Stats: statsRepo, Rules: scoring.StandardLineup()}
		if _, err := sc.Recompute(ctx, 1, nil, true); err != nil {
			t.Fatal(err)
		}
		g, err := repo.Game(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		s, _ := g.Score(l.Teams[0].ID, 1)
		want := []scoring.Sub{{OutSlot: 9, InSlot: 13, OutPlayerID: pid[25], InPlayerID: pid[15], Status: scoring.SubConfirmed}}
		if s.Points != 42 || s.BenchPoints != 3 || !slices.Equal(s.AutoSubs, want) {
			t.Errorf("team A gw 1 = %d points, %d bench, subs %+v; want 42, 3, %+v", s.Points, s.BenchPoints, s.AutoSubs, want)
		}
	})
}
