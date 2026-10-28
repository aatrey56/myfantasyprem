package httpapi

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// Deleting an account mid-season cancels the user's pending claims and
// open offers, orphans the team (it keeps playing), hands the
// commissioner role on and deletes the user.
func TestDeleteAccountMidSeason(t *testing.T) {
	srv, _ := newTestServer()
	ctx := context.Background()
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	seeded, err := demo.Seed(ctx, demo.Deps{Auth: srv.auth, Leagues: srv.leagues, Stats: srv.stats, Now: srv.now, Log: testOptions().Logger})
	if err != nil {
		t.Fatal(err)
	}
	me := seeded.UserIDs[0] // the commissioner
	tok, _ := srv.auth.Issue(ctx, me)
	l, _ := srv.leagues.Get(ctx, me, seeded.LeagueID)
	mine, _ := l.TeamOf(me)
	other := l.Teams[1]
	st, _ := srv.leagues.Repo().State(ctx, l.ID)
	w, _ := league.SeasonWorld(ctx, srv.stats)
	at := func(team string, pos scoring.Position) string {
		var ids []string
		for _, id := range st.Game.RosterAt(team, 9) {
			if w.Players[id].Position == pos {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids[0]
	}
	own := st.Game.OwnerAt(9)
	var free string
	for id, p := range w.Players {
		if p.Position == scoring.FWD && own[id] == "" {
			free = id
			break
		}
	}
	_, claim, err := srv.waiverService().CreateClaim(ctx, me, l.ID, free, at(mine.ID, scoring.FWD))
	if err != nil {
		t.Fatal(err)
	}
	_, offer, err := srv.tradeService().Propose(ctx, me, l.ID, other.ID, []string{at(mine.ID, scoring.MID)}, []string{at(other.ID, scoring.MID)}, "")
	if err != nil {
		t.Fatal(err)
	}

	if rec := doAs(h, tok.AccessToken, http.MethodDelete, "/v1/me", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if rec := doAs(h, tok.AccessToken, http.MethodGet, "/v1/me", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("token still works: %d", rec.Code)
	}
	claims, _ := srv.leagues.Repo().Claims(ctx, l.ID, mine.ID)
	if len(claims) != 1 || claims[0].ID != claim.ID || claims[0].Status != league.ClaimCancelled {
		t.Fatalf("claims = %+v", claims)
	}
	trades, _ := srv.leagues.Repo().Trades(ctx, l.ID)
	if len(trades) != 1 || trades[0].ID != offer.ID || trades[0].Status != league.TradeWithdrawn {
		t.Fatalf("trades = %+v", trades)
	}
	after, _ := srv.leagues.Repo().Get(ctx, l.ID)
	team, _ := after.Team(mine.ID)
	if team.OwnerID != "" || after.CommissionerID == me || after.CommissionerID == "" {
		t.Fatalf("after: team owner %q, commissioner %q", team.OwnerID, after.CommissionerID)
	}
	if g, _ := srv.leagues.Repo().Game(ctx, l.ID); len(g.RosterAt(mine.ID, 9)) != 15 {
		t.Fatal("the orphaned team lost its roster")
	}
}

// A manager who leaves mid-season (or is removed) has their pending claims
// cancelled first; the team stays, orphaned.
func TestLeaveCancelsPendingMoves(t *testing.T) {
	srv, _ := newTestServer()
	ctx := context.Background()
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	seeded, err := demo.Seed(ctx, demo.Deps{Auth: srv.auth, Leagues: srv.leagues, Stats: srv.stats, Now: srv.now, Log: testOptions().Logger})
	if err != nil {
		t.Fatal(err)
	}
	priya := seeded.UserIDs[1]
	tok, _ := srv.auth.Issue(ctx, priya)
	l, _ := srv.leagues.Get(ctx, priya, seeded.LeagueID)
	team, _ := l.TeamOf(priya)
	st, _ := srv.leagues.Repo().State(ctx, l.ID)
	w, _ := league.SeasonWorld(ctx, srv.stats)
	own := st.Game.OwnerAt(9)
	var drop, add string
	for _, id := range st.Game.RosterAt(team.ID, 9) {
		if w.Players[id].Position == scoring.DEF {
			drop = id
		}
	}
	for id, p := range w.Players {
		if p.Position == scoring.DEF && own[id] == "" {
			add = id
			break
		}
	}
	if _, _, err := srv.waiverService().CreateClaim(ctx, priya, l.ID, add, drop); err != nil {
		t.Fatal(err)
	}
	if rec := doAs(h, tok.AccessToken, http.MethodPost, "/v1/leagues/"+l.ID+"/leave", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("leave = %d %s", rec.Code, rec.Body)
	}
	claims, _ := srv.leagues.Repo().Claims(ctx, l.ID, team.ID)
	if len(claims) != 1 || claims[0].Status != league.ClaimCancelled {
		t.Fatalf("claims = %+v", claims)
	}
}

// Account deletion is one transaction on Postgres: when deleting the
// account fails, the user has not left their leagues either.
func TestDeleteAccountIsAtomic(t *testing.T) {
	pool := storetest.New(t)
	srv, _ := newTestServerOn(pool)
	ctx := context.Background()
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	commish := userOf(t, srv, testToken)
	l, err := srv.leagues.Create(ctx, commish, "Atomic League", 4, "One", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := srv.leagues.CreateInvite(ctx, commish, l.ID, nil, nil)
	friend := signIn(srv, "Priya N.", "priya@example.com")
	fid := userOf(t, srv, friend)
	if _, _, err := srv.leagues.AcceptInvite(ctx, fid, inv.Code, "Two"); err != nil {
		t.Fatal(err)
	}
	// The users row refuses to go, after the leagues were left.
	if _, err := pool.Exec(ctx, `
		CREATE FUNCTION refuse_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'refused'; END $$;
		CREATE TRIGGER refuse_user_delete BEFORE DELETE ON users FOR EACH ROW EXECUTE FUNCTION refuse_delete();`); err != nil {
		t.Fatal(err)
	}
	if rec := doAs(h, friend, http.MethodDelete, "/v1/me", ""); rec.Code != http.StatusInternalServerError {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	got, err := srv.leagues.Get(ctx, fid, l.ID)
	if err != nil {
		t.Fatalf("the failed deletion still took the user out of the league: %v", err)
	}
	if _, ok := got.TeamOf(fid); !ok {
		t.Fatal("the failed deletion still took the user's team")
	}
}

// Without client ids configured, Apple and Google sign-in answer 503.
func TestProviderSignInNotConfigured(t *testing.T) {
	h := newTestHandler()
	p := decodeProblem(t, doAnon(h, http.MethodPost, "/v1/auth/google", `{"idToken":"eyJ.x.y"}`), http.StatusServiceUnavailable)
	if problemType(p) != problemTypeBase+"sign-in-unavailable" {
		t.Fatalf("type = %s", problemType(p))
	}
}
