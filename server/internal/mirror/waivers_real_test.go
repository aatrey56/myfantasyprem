package mirror_test

import (
	"cmp"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// FPL's own waiver runs of gameweeks 1-6 in the real capture, replayed
// with our engine: for each run, the squads as they stood before it, the
// claims FPL received (each team's in its own order) and our reverse
// standings order (league.WaiverOrder, from our scores and the draft).
// Every claim must be decided as FPL decided it (won, or lost because the
// player in or out was gone) and in FPL's processing order (`index`), so
// the start order, the tie-breaks and "a winner goes to the back for the
// rest of the run" are all checked against FPL (ADR 0008 rule 12).
func TestRealWaiverRunsReplayWithZeroDiffs(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		_, st, _ := importReal(t, e)
		if rule := st.League.Settings.WaiverRule(); rule != league.WaiverReverseStandings {
			t.Fatalf("imported league's waiver order %q, want reverse_standings", rule)
		}
		w, err := league.SeasonWorld(ctx, e.stats)
		if err != nil {
			t.Fatal(err)
		}
		players, err := e.stats.ExternalIDs(ctx, mirror.Provider, "player")
		if err != nil {
			t.Fatal(err)
		}
		team := func(ext string) string {
			id, ok := st.Ref(league.RefTeam, mirror.Provider, ext)
			if !ok {
				t.Fatalf("no team for entry %s", ext)
			}
			return id
		}
		txs, err := e.src.Transactions(ctx, fmt.Sprint(realLeague))
		if err != nil {
			t.Fatal(err)
		}
		trades, err := e.src.Trades(ctx, fmt.Sprint(realLeague))
		if err != nil {
			t.Fatal(err)
		}
		checked := 0
		for gw := 1; gw <= realGameweeks; gw++ {
			var claims, frees []provider.Transaction
			for _, x := range txs {
				switch {
				case x.Gameweek != gw:
				case x.Kind == provider.TransactionWaiver:
					claims = append(claims, x)
				case x.Kind == provider.TransactionFreeAgent && x.Result == provider.ClaimApproved:
					frees = append(frees, x)
				}
			}
			slices.SortFunc(claims, func(a, b provider.Transaction) int { return cmp.Compare(a.Index, b.Index) })
			runAt := w.Gameweeks[gw].WaiverDeadlineAt

			// Squads before the run: gameweek gw's, with the moves made at
			// and after the run undone, newest first (free agents after
			// the run, then the run's won claims, then trades processed
			// after it). Trades processed before the run stay.
			owner := st.Game.OwnerAt(gw)
			undo := func(teamID, in, out string) {
				if owner[in] != teamID || owner[out] != "" {
					t.Fatalf("gw%d: cannot undo %s in %s out on %s", gw, in, out, teamID)
				}
				delete(owner, in)
				owner[out] = teamID
			}
			slices.SortFunc(frees, func(a, b provider.Transaction) int { return b.ProcessedAt.Compare(a.ProcessedAt) })
			for _, x := range frees {
				if !x.ProcessedAt.After(runAt) {
					t.Fatalf("gw%d: free agent %s before the run", gw, x.ExternalID)
				}
				undo(team(x.LeagueEntry), players[x.PlayerIn], players[x.PlayerOut])
			}
			for _, x := range slices.Backward(claims) {
				if x.Result == provider.ClaimApproved {
					undo(team(x.LeagueEntry), players[x.PlayerIn], players[x.PlayerOut])
				}
			}
			for _, tr := range trades {
				if tr.State != provider.TradeProcessed || tr.Gameweek != gw || tr.ResolvedAt.Before(runAt) {
					continue
				}
				t.Fatalf("gw%d: trade %s processed after the run; undo it", gw, tr.ExternalID)
			}

			// The run, on a copy of the league with nothing but that.
			run := &league.State{
				League: st.League, Draft: st.Draft, Game: &league.Game{Matchups: st.Game.Matchups},
				Runs: map[league.RunKey]time.Time{},
			}
			for pid, tm := range owner {
				run.Game.Acquire(tm, pid, gw, "draft")
			}
			for _, x := range claims {
				run.Claims = append(run.Claims, league.Claim{
					ID: x.ExternalID, TeamID: team(x.LeagueEntry), AddPlayerID: players[x.PlayerIn], DropPlayerID: players[x.PlayerOut],
					Order: *x.Priority, Gameweek: gw, Status: league.ClaimPending, CreatedAt: x.ProcessedAt,
				})
			}
			order := league.WaiverOrder(run, w, gw)
			waivers.Process(run, w, gw, runAt)

			name := func(id string) string { tm, _ := st.League.Team(id); return tm.Name }
			var names []string
			for _, id := range order {
				names = append(names, name(id))
			}
			t.Logf("gw%d run: %d claims; order %v", gw, len(claims), names)
			if len(run.NewTxns) != len(claims) {
				t.Errorf("gw%d: %d claims decided, FPL %d", gw, len(run.NewTxns), len(claims))
				continue
			}
			for i, x := range claims {
				want := fmt.Sprintf("%s %s for %s %s", name(team(x.LeagueEntry)), players[x.PlayerIn], players[x.PlayerOut],
					map[provider.ClaimResult]string{
						provider.ClaimApproved:                   "won",
						provider.ClaimDeniedPlayerInUnavailable:  "lost " + league.ClaimInUnavailable,
						provider.ClaimDeniedPlayerOutUnavailable: "lost " + league.ClaimOutUnavailable,
					}[x.Result])
				got := run.NewTxns[i]
				res := "won"
				if !got.Approved {
					res = "lost " + got.Reason
				}
				if g := fmt.Sprintf("%s %s for %s %s", name(got.TeamID), got.PlayerIn, got.PlayerOut, res); g != want {
					t.Errorf("gw%d claim %d (FPL index %d): ours %q, FPL %q", gw, i+1, x.Index, g, want)
				}
				checked++
			}
		}
		if checked != 177 {
			t.Errorf("%d claims checked, want all 177", checked)
		}
	})
}
