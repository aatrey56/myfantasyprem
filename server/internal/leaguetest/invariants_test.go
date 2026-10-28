package leaguetest_test

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
	"github.com/jackc/pgx/v5/pgxpool"
	"pgregory.net/rapid"
)

// Property test across the roster-changing rules: whatever sequence of
// draft picks, autopicks, missed clocks, waiver claims, free-agent moves,
// waiver runs and trades happens, in every gameweek
//
//   - each player has at most one owner (ADR 0008: one owner per player),
//   - every roster is exactly 2 GKP, 5 DEF, 5 MID, 3 FWD (RESEARCH.md §7),
//
// and during the draft every pick is made by the team the snake puts on
// the clock. Expected behaviour comes from the rules; nothing here asks the
// code what the answer should be.

var squad = map[scoring.Position]int{scoring.GKP: 2, scoring.DEF: 5, scoring.MID: 5, scoring.FWD: 3}

// expected reports whether err is a rule refusal (a *league.Error), which
// random actions are allowed to hit.
func expected(err error) bool {
	var le *league.Error
	return err == nil || errors.As(err, &le)
}

func checkRosters(rt *rapid.T, st *league.State, w *league.World, gws ...int) {
	for _, gw := range gws {
		owners := map[string][]string{}
		for _, r := range st.Game.Rosters {
			if r.OnRosterAt(gw) {
				owners[r.PlayerID] = append(owners[r.PlayerID], r.TeamID)
			}
		}
		for p, teams := range owners {
			if len(teams) > 1 {
				rt.Fatalf("gameweek %d: player %s owned by %v", gw, p, teams)
			}
		}
		for _, tm := range st.League.Teams {
			counts := map[scoring.Position]int{}
			for _, id := range st.Game.RosterAt(tm.ID, gw) {
				counts[w.Players[id].Position]++
			}
			for pos, n := range squad {
				if counts[pos] != n {
					rt.Fatalf("gameweek %d: team %s has %d %s, want %d (%v)", gw, tm.ID, counts[pos], pos, n, counts)
				}
			}
		}
	}
}

// TestRosterInvariants runs on the memory stores and, when
// TEST_DATABASE_URL is set, on Postgres (one database for every check:
// each check makes its own users and league).
func TestRosterInvariants(t *testing.T) {
	if testing.Short() {
		t.Skip("slow property test")
	}
	t.Run("memory", func(t *testing.T) { rosterInvariants(t, nil) })
	t.Run("postgres", func(t *testing.T) { rosterInvariants(t, storetest.New(t)) })
}

func rosterInvariants(t *testing.T, pool *pgxpool.Pool) {
	rapid.Check(t, func(rt *rapid.T) {
		e := leaguetest.New(t, pool)
		w := e.World(t)
		// Players in draft-rank order (unique in the demo season), not id
		// order: ids are random ULIDs, so sorting by id would hand the same
		// seed different players on every run and a failure could not be
		// replayed.
		var all []string
		for id := range w.Players {
			all = append(all, id)
		}
		slices.SortFunc(all, func(a, b string) int { return cmp.Compare(w.Players[a].DraftRank, w.Players[b].DraftRank) })
		review := rapid.SampledFrom([]string{league.ReviewNone, league.ReviewCommissioner, league.ReviewLeagueVote}).Draw(rt, "review")
		n := rapid.IntRange(4, 8).Draw(rt, "teams")
		l, users := e.League(t, n, league.SettingsInput{TradeReview: &review})
		ctx := e.Ctx

		// Draft.
		ds := draft.New(e.Repo, e.Stats, e.Clock.Now, nil)
		v, err := ds.Start(ctx, users[0], l.ID)
		if err != nil {
			rt.Fatal(err)
		}
		order := v.Draft.Order
		owner := map[string]string{} // team -> user
		for i, tm := range l.Teams {
			owner[tm.ID] = users[i]
		}
		for step := 0; ; step++ {
			v, err = ds.Get(ctx, users[0], l.ID)
			if err != nil {
				rt.Fatal(err)
			}
			for k, p := range v.Draft.Picks {
				// Hand-built snake: odd rounds run forward, even rounds reversed.
				round, idx := k/len(order), k%len(order)
				if round%2 == 1 {
					idx = len(order) - 1 - idx
				}
				if want := order[idx]; p.TeamID != want || p.Overall != k+1 {
					rt.Fatalf("pick %d by %s, snake says %s", k+1, p.TeamID, want)
				}
			}
			if v.Draft.Status == league.DraftComplete {
				break
			}
			if step > 400 { // let autopick finish
				for _, u := range users {
					if _, err := ds.SetAutopick(ctx, u, l.ID, true); !expected(err) {
						rt.Fatal(err)
					}
				}
				continue
			}
			c := v.Current()
			switch rapid.IntRange(0, 4).Draw(rt, "draft action") {
			case 0, 1: // the team on the clock picks someone (maybe taken or a full position)
				_, _, err = ds.Pick(ctx, owner[c.TeamID], l.ID, rapid.SampledFrom(all).Draw(rt, "player"))
			case 2: // someone else tries, or the commissioner
				_, _, err = ds.Pick(ctx, rapid.SampledFrom(users).Draw(rt, "user"), l.ID, rapid.SampledFrom(all).Draw(rt, "player"))
			case 3: // the clock runs out
				e.Clock.Advance(time.Duration(v.Draft.PickTimerS+1) * time.Second)
				_, err = ds.Expire(ctx, l.ID)
			case 4:
				_, err = ds.SetAutopick(ctx, rapid.SampledFrom(users).Draw(rt, "user"), l.ID, rapid.Bool().Draw(rt, "on"))
			}
			if !expected(err) {
				rt.Fatal(err)
			}
		}
		st := e.State(t, l.ID)
		first := w.UpcomingGW(e.Clock.Now())
		checkRosters(rt, st, w, first, first+1)

		// Season: waivers, free agency and trades in any order, with time
		// moving on through waiver runs and deadlines.
		ws := waivers.New(e.Repo, e.Stats, e.Clock.Now, nil)
		ts := trades.New(e.Repo, e.Stats, e.Clock.Now, nil)
		samePos := func(label string, team string, gw int) (string, string) {
			drop := rapid.SampledFrom(e.State(t, l.ID).Game.RosterAt(team, gw)).Draw(rt, label+" drop")
			var same []string
			for _, id := range all {
				if w.Players[id].Position == w.Players[drop].Position {
					same = append(same, id)
				}
			}
			return rapid.SampledFrom(same).Draw(rt, label+" add"), drop
		}
		var trace []string
		defer func() {
			if r := recover(); r != nil {
				t.Log(strings.Join(trace, "\n"))
				panic(r)
			}
		}()
		steps := rapid.IntRange(10, 60).Draw(rt, "season steps")
		for range steps {
			now := e.Clock.Now()
			gw := min(w.UpcomingGW(now), 38)
			i := rapid.IntRange(0, n-1).Draw(rt, "team")
			team, user := l.Teams[i].ID, users[i]
			var err error
			action := rapid.IntRange(0, 7).Draw(rt, "season action")
			switch action {
			case 0:
				add, drop := samePos("claim", team, gw)
				_, _, err = ws.CreateClaim(ctx, user, l.ID, add, drop)
			case 1:
				add, drop := samePos("free agent", team, gw)
				_, _, err = ws.AddFreeAgent(ctx, user, l.ID, add, drop)
			case 2:
				e.Clock.Advance(time.Duration(rapid.IntRange(1, 72).Draw(rt, "hours")) * time.Hour)
				if _, err = ws.RunDue(ctx, l.ID); !expected(err) {
					break
				}
				_, err = ts.CloseDue(ctx, l.ID)
			case 3, 4:
				j := rapid.IntRange(0, n-1).Draw(rt, "other team")
				k := rapid.IntRange(1, 3).Draw(rt, "players")
				pos := rapid.SampledFrom([]scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD}).Draw(rt, "position")
				pick := func(of string) []string {
					var ids []string
					for _, id := range e.State(t, l.ID).Game.RosterAt(of, gw) {
						if w.Players[id].Position == pos {
							ids = append(ids, id)
						}
					}
					return ids[:min(k, len(ids))]
				}
				_, _, err = ts.Propose(ctx, user, l.ID, l.Teams[j].ID, pick(team), pick(l.Teams[j].ID), "")
			case 5, 6:
				v, lerr := ts.List(ctx, user, l.ID)
				if lerr != nil {
					rt.Fatal(lerr)
				}
				for _, tr := range v.Trades {
					switch tr.Status {
					case league.TradeOffered:
						if tr.ReceivingTeamID == team {
							_, _, err = ts.Accept(ctx, user, l.ID, tr.ID)
						}
					case league.TradeInReview:
						if tr.ReviewMode == league.ReviewCommissioner {
							_, _, err = ts.Review(ctx, users[0], l.ID, tr.ID, trades.DecisionApprove, "")
						} else if tr.OfferingTeamID != team && tr.ReceivingTeamID != team {
							_, _, err = ts.Vote(ctx, user, l.ID, tr.ID, rapid.SampledFrom([]string{trades.VoteAllow, trades.VoteVeto}).Draw(rt, "vote"))
						}
					}
					if !expected(err) {
						break
					}
				}
			case 7:
				_, err = ws.Run(ctx, l.ID, gw)
			}
			if !expected(err) {
				rt.Fatalf("unexpected error: %v", err)
			}
			trace = append(trace, fmt.Sprintf("%s gw%d team %d action %d: %v", now.Format(time.RFC3339), gw, i, action, err))
			st := e.State(t, l.ID)
			for _, tr := range st.Trades {
				trace = append(trace, fmt.Sprintf("   trade %s %s->%s off %v req %v eff %d", tr.Status, tr.OfferingTeamID[len(tr.OfferingTeamID)-4:], tr.ReceivingTeamID[len(tr.ReceivingTeamID)-4:], tr.Offered, tr.Requested, tr.EffectiveGW))
			}
			up := w.UpcomingGW(e.Clock.Now())

			checkRosters(rt, st, w, slices.Compact([]int{min(gw, 38), min(up, 38), min(up+1, 38)})...)
		}
	})
}
