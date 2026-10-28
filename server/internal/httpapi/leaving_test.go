package httpapi

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// hookRepo runs before once, just before the next transaction starts:
// another request committing in between two steps of an operation.
type hookRepo struct {
	league.Repo
	before func()
}

func (r *hookRepo) Tx(ctx context.Context, fn func(tx league.Tx) error) error {
	if b := r.before; b != nil {
		r.before = nil
		b()
	}
	return r.Repo.Tx(ctx, fn)
}

// A claim or an offer the leaving manager makes while the departure is
// being processed cannot survive on the orphaned team: withdrawing the
// pending moves and removing the member happen under one league lock
// (leave, removal by the commissioner and account deletion).
func TestLeavingIsAtomicWithPendingMoves(t *testing.T) {
	for _, store := range []string{"memory", "postgres"} {
		for _, how := range []string{"leave", "removed", "deleted"} {
			t.Run(store+"/"+how, func(t *testing.T) {
				var pool *pgxpool.Pool
				if store == "postgres" {
					pool = storetest.New(t)
				}
				srv, _ := newTestServerOn(pool)
				ctx := context.Background()
				seeded, err := demo.Seed(ctx, demo.Deps{Auth: srv.auth, Leagues: srv.leagues, Stats: srv.stats, Now: srv.now, Log: testOptions().Logger})
				if err != nil {
					t.Fatal(err)
				}
				commish, priya := seeded.UserIDs[0], seeded.UserIDs[1]
				l, _ := srv.leagues.Get(ctx, priya, seeded.LeagueID)
				team, _ := l.TeamOf(priya)
				other, _ := l.TeamOf(commish)
				st, _ := srv.leagues.Repo().State(ctx, l.ID)
				w, _ := league.SeasonWorld(ctx, srv.stats)
				own := st.Game.OwnerAt(9)
				first := func(teamID string, pos scoring.Position) string {
					var ids []string
					for _, id := range st.Game.RosterAt(teamID, 9) {
						if w.Players[id].Position == pos {
							ids = append(ids, id)
						}
					}
					slices.Sort(ids)
					return ids[0]
				}
				var add string
				for id, p := range w.Players {
					if p.Position == scoring.DEF && own[id] == "" && (add == "" || id < add) {
						add = id
					}
				}

				hr := &hookRepo{Repo: srv.leagues.Repo()}
				srv.leagues = league.NewService(hr, srv.now).WithStats(srv.stats)
				hr.before = func() {
					if _, _, err := srv.waiverService().CreateClaim(ctx, priya, l.ID, add, first(team.ID, scoring.DEF)); err != nil {
						t.Errorf("claim: %v", err)
					}
					if _, _, err := srv.tradeService().Propose(ctx, priya, l.ID, other.ID,
						[]string{first(team.ID, scoring.MID)}, []string{first(other.ID, scoring.MID)}, ""); err != nil {
						t.Errorf("offer: %v", err)
					}
				}
				h := newHandler(testOptions(), newRoutes(testOptions(), srv))
				tok := func(uid string) string {
					tk, err := srv.auth.Issue(ctx, uid)
					if err != nil {
						t.Fatal(err)
					}
					return tk.AccessToken
				}
				var rec = map[string]func() int{
					"leave": func() int { return doAs(h, tok(priya), http.MethodPost, "/v1/leagues/"+l.ID+"/leave", "").Code },
					"removed": func() int {
						return doAs(h, tok(commish), http.MethodDelete, "/v1/leagues/"+l.ID+"/members/"+priya, "").Code
					},
					"deleted": func() int { return doAs(h, tok(priya), http.MethodDelete, "/v1/me", "").Code },
				}[how]()
				if rec != http.StatusNoContent {
					t.Fatalf("%s = %d", how, rec)
				}
				if hr.before != nil {
					t.Fatal("the concurrent request never ran")
				}
				claims, _ := hr.Claims(ctx, l.ID, team.ID)
				for _, c := range claims {
					if c.Status == league.ClaimPending {
						t.Errorf("claim %s still pending on the orphaned team", c.ID)
					}
				}
				trades, _ := hr.Trades(ctx, l.ID)
				for _, tr := range trades {
					if tr.Status == league.TradeOffered && (tr.OfferingTeamID == team.ID || tr.ReceivingTeamID == team.ID) {
						t.Errorf("offer %s still open on the orphaned team", tr.ID)
					}
				}
			})
		}
	}
}
