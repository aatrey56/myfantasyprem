package mirror_test

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/lineup"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

func (e *env) state(t *testing.T, leagueID string) *league.State {
	t.Helper()
	st, err := e.leagues.State(ctx, leagueID)
	if err != nil || st == nil {
		t.Fatalf("state: %v %v", st, err)
	}
	return st
}

// rosterKeys lists a State's roster slots without their ids, sorted.
func rosterKeys(st *league.State) []string {
	var out []string
	for _, r := range st.Game.Rosters {
		out = append(out, fmt.Sprintf("%s %s %d-%d %s", r.TeamID, r.PlayerID, r.FromGW, r.ToGW, r.AcquiredVia))
	}
	slices.Sort(out)
	return out
}

// lineupKeys lists a State's saved lineups, sorted.
func lineupKeys(st *league.State) []string {
	var out []string
	for _, l := range st.Game.Lineups {
		out = append(out, fmt.Sprintf("%s gw%d %v %s", l.TeamID, l.FromGW, l.Slots, l.UpdatedAt.UTC().Format(time.RFC3339)))
	}
	slices.Sort(out)
	return out
}

func txnIDs(t *testing.T, e *env, leagueID string) []string {
	t.Helper()
	txns, err := e.leagues.Transactions(ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range txns {
		out = append(out, x.ID)
	}
	slices.Sort(out)
	return out
}

// freeLike is an unowned player of pos at gameweek gw, other than not.
func freeLike(t *testing.T, w *league.World, st *league.State, gw int, pos scoring.Position, not ...string) string {
	t.Helper()
	owned := st.Game.OwnerAt(gw)
	var ids []string
	for id, p := range w.Players {
		if _, taken := owned[id]; !taken && p.Position == pos && !slices.Contains(not, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		t.Fatalf("no free %s", pos)
	}
	slices.Sort(ids)
	return ids[0]
}

// The league is played here after the import (a waiver run, a free agent,
// a pending claim, an open trade, a saved lineup, a renamed team); the
// commissioner's re-sync puts FPL's state back: squads, ownership, the
// waiver order, the feed, trades and lineups, with the open trade failed
// and the pending claim cancelled, a notice saying so, claimed seats kept,
// and the scores recomputed (the report is as clean as after the import).
// A second re-sync changes nothing more.
func TestResyncResetsToFPLAndCancelsPendingMoves(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, imported, importRep := importReal(t, e)
		l := imported.League
		lid := l.ID
		member := claimSeat(t, e, l, 1)
		importedTxns := txnIDs(t, e, lid)
		w, err := league.SeasonWorld(ctx, e.stats)
		if err != nil {
			t.Fatal(err)
		}
		mine, _ := l.TeamOf(owner)
		theirs := l.Teams[1]
		ws := waivers.New(e.leagues, e.stats, e.now, league.NopNotifier{})
		ts := trades.New(e.leagues, e.stats, e.now, league.NopNotifier{})
		ls := lineup.New(e.leagues, e.stats, e.now, league.NopNotifier{})
		ms := league.NewService(e.leagues, e.now)
		pos := func(id string) scoring.Position { return w.Players[id].Position }

		// Played here: a claim won at gameweek 7's waiver run.
		myRoster := imported.Game.RosterAt(mine.ID, 7)
		theirRoster := imported.Game.RosterAt(theirs.ID, 7)
		x := freeLike(t, w, imported, 7, pos(myRoster[1]))
		if _, _, err := ws.CreateClaim(ctx, owner, lid, x, myRoster[1]); err != nil {
			t.Fatalf("claim: %v", err)
		}
		*e.clock = w.Gameweeks[7].WaiverDeadlineAt.Add(time.Minute)
		if ran, err := ws.RunDue(ctx, lid); err != nil || !ran {
			t.Fatalf("waiver run: %v %v", ran, err)
		}
		// A free agent, a claim for gameweek 8 (pending), an open trade, a
		// saved lineup and a renamed team.
		played := e.state(t, lid)
		y := freeLike(t, w, played, 7, pos(myRoster[2]), x)
		if _, _, err := ws.AddFreeAgent(ctx, owner, lid, y, myRoster[2]); err != nil {
			t.Fatalf("free agent: %v", err)
		}
		var drop string
		for _, id := range theirRoster {
			if pos(id) == pos(myRoster[1]) {
				drop = id
				break
			}
		}
		if _, _, err := ws.CreateClaim(ctx, member, lid, myRoster[1], drop); err != nil {
			t.Fatalf("pending claim: %v", err)
		}
		var requested string
		for _, id := range theirRoster {
			if id != drop && pos(id) == pos(myRoster[3]) {
				requested = id
				break
			}
		}
		if _, _, err := ts.Propose(ctx, owner, lid, theirs.ID, myRoster[3:4], []string{requested}, ""); err != nil {
			t.Fatalf("trade: %v", err)
		}
		played = e.state(t, lid)
		lu := league.EffectiveLineup(played.Game, w, mine.ID, 8)
		if err := ls.Set(ctx, owner, lid, mine.ID, 8, lu.Slots[:11], lu.Slots[11:]); err != nil {
			t.Fatalf("lineup: %v", err)
		}
		if _, err := ms.RenameTeam(ctx, owner, lid, mine.ID, "Played Here"); err != nil {
			t.Fatalf("rename: %v", err)
		}
		played = e.state(t, lid)
		if slices.Equal(rosterKeys(played), rosterKeys(imported)) || len(played.Locks) == 0 ||
			slices.Equal(txnIDs(t, e, lid), importedTxns) {
			t.Fatal("nothing was played here")
		}

		// Commissioner only, confirmed, one at a time.
		if _, err := e.svc.RequestResync(ctx, member, lid, true); slug(err) != "not-commissioner" {
			t.Errorf("member re-syncs: %v", err)
		}
		if _, err := e.svc.RequestResync(ctx, owner, lid, false); slug(err) != "invalid-request" {
			t.Errorf("unconfirmed: %v", err)
		}
		rec := &recording{}
		e.svc.Sched = rec
		v, err := e.svc.RequestResync(ctx, owner, lid, true)
		if err != nil {
			t.Fatal(err)
		}
		if v.Record.Pending != mirror.PendingResync || !v.Record.RequestedAt.Equal(e.now()) ||
			len(rec.jobs) != 1 || rec.jobs[0] != (mirror.ResyncArgs{LeagueID: lid, At: e.now()}) {
			t.Fatalf("requested: %+v, jobs %+v", v.Record, rec.jobs)
		}
		if _, err := e.svc.RequestResync(ctx, owner, lid, true); slug(err) != "sync-pending" {
			t.Errorf("second request: %v", err)
		}
		if _, err := e.svc.RequestCheck(ctx, owner, lid); slug(err) != "sync-pending" {
			t.Errorf("check while a re-sync is pending: %v", err)
		}
		e.svc.Sched = nil

		// The job: FPL's state is back.
		*e.clock = e.clock.Add(time.Minute)
		if err := e.svc.Resync(ctx, lid); err != nil {
			t.Fatal(err)
		}
		after := e.state(t, lid)
		if !slices.Equal(rosterKeys(after), rosterKeys(imported)) {
			t.Error("rosters differ from FPL's")
		}
		if !slices.Equal(lineupKeys(after), lineupKeys(imported)) {
			t.Error("lineups differ from FPL's")
		}
		if !slices.Equal(after.Priority, imported.Priority) {
			t.Errorf("waiver order %v, want FPL's %v", after.Priority, imported.Priority)
		}
		if got := txnIDs(t, e, lid); !slices.Equal(got, importedTxns) {
			t.Errorf("feed has %d entries, want FPL's %d", len(got), len(importedTxns))
		}
		if len(after.Locks) != 0 || len(after.Claims) != 0 || len(after.Trades) != 0 {
			t.Errorf("left open: %d locks, %d claims, %d trades", len(after.Locks), len(after.Claims), len(after.Trades))
		}
		if tm, _ := after.League.Team(mine.ID); tm.Name != imported.League.Teams[0].Name || tm.OwnerID != owner {
			t.Errorf("my team = %+v", tm)
		}
		if tm, _ := after.League.Team(theirs.ID); tm.OwnerID != member {
			t.Errorf("the claimed seat lost its manager: %+v", tm)
		}
		trs, _ := e.leagues.Trades(ctx, lid)
		var failed []league.Trade
		for _, tr := range trs {
			if tr.FailureReason == league.TradeFailResynced {
				failed = append(failed, tr)
			}
		}
		if len(failed) != 1 || failed[0].Status != league.TradeFailed {
			t.Errorf("failed trades = %+v", failed)
		}
		myClaims, _ := e.leagues.Claims(ctx, lid, mine.ID)
		theirClaims, _ := e.leagues.Claims(ctx, lid, theirs.ID)
		if len(myClaims) != 0 || len(theirClaims) != 1 || theirClaims[0].Status != league.ClaimCancelled {
			t.Errorf("claims: mine %+v, theirs %+v", myClaims, theirClaims)
		}
		v, err = e.svc.View(ctx, member, lid)
		if err != nil {
			t.Fatal(err)
		}
		if r := v.Record; r.Pending != "" || r.RequestedAt != nil || r.LastError != "" || !r.LastSyncedAt.Equal(e.now()) ||
			r.Note != "Re-synced from FPL. 1 open trade and 1 waiver claim were cancelled." {
			t.Errorf("record = %+v", r)
		}
		rep, err := e.svc.Report(ctx, lid)
		if err != nil || rep.Status != importRep.Status {
			t.Errorf("report after the re-sync = %v (import's %s), %v", rep.Status, importRep.Status, err)
		}

		// Idempotent: nothing left to cancel, nothing changes.
		*e.clock = e.clock.Add(time.Minute)
		if err := e.svc.Resync(ctx, lid); err != nil {
			t.Fatal(err)
		}
		again := e.state(t, lid)
		if !slices.Equal(rosterKeys(again), rosterKeys(after)) || !slices.Equal(lineupKeys(again), lineupKeys(after)) ||
			!slices.Equal(again.Priority, after.Priority) || !slices.Equal(txnIDs(t, e, lid), importedTxns) {
			t.Error("a second re-sync changed the league")
		}
		if r, _ := e.svc.Store.Sync(ctx, lid); r.Note != "Re-synced from FPL. No open trades or waiver claims needed cancelling." {
			t.Errorf("second note = %q", r.Note)
		}
	})
}

// A check compares the league with FPL and changes nothing: a move made
// here shows as a difference, and the league keeps it.
func TestCheckReportsDifferencesWithoutChangingTheLeague(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, imported, _ := importReal(t, e)
		lid := imported.League.ID
		w, err := league.SeasonWorld(ctx, e.stats)
		if err != nil {
			t.Fatal(err)
		}
		mine, _ := imported.League.TeamOf(owner)
		roster := imported.Game.RosterAt(mine.ID, 7)
		ws := waivers.New(e.leagues, e.stats, e.now, league.NopNotifier{})
		*e.clock = w.Gameweeks[7].WaiverDeadlineAt.Add(time.Minute)
		if _, err := ws.RunDue(ctx, lid); err != nil {
			t.Fatal(err)
		}
		added := freeLike(t, w, imported, 7, w.Players[roster[0]].Position)
		if _, _, err := ws.AddFreeAgent(ctx, owner, lid, added, roster[0]); err != nil {
			t.Fatal(err)
		}
		played := e.state(t, lid)

		if _, err := e.svc.RequestCheck(ctx, owner, lid); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Check(ctx, lid); err != nil {
			t.Fatal(err)
		}
		rep, err := e.svc.Report(ctx, lid)
		if err != nil || rep.Status != mirror.ReportMismatch || len(mismatches(rep, mirror.KindOwner)) == 0 {
			t.Fatalf("check report = %+v %v", rep, err)
		}
		if after := e.state(t, lid); !slices.Equal(rosterKeys(after), rosterKeys(played)) {
			t.Error("a check changed the league")
		}
		if r, _ := e.svc.Store.Sync(ctx, lid); r.Pending != "" || r.LastError != "" || !r.LastAttemptAt.Equal(e.now()) ||
			r.LastSyncedAt.Equal(e.now()) {
			t.Errorf("record after the check = %+v", r)
		}
	})
}

// A re-sync that cannot reach FPL keeps the league as it was and says why.
func TestFailedResyncKeepsTheLeague(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, st, _ := importReal(t, e)
		e.svc.Source = failing{e.src}
		*e.clock = e.clock.Add(20 * time.Minute)
		if _, err := e.svc.RequestResync(ctx, owner, st.League.ID, true); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.Resync(ctx, st.League.ID); err != nil {
			t.Fatal(err)
		}
		rec, _ := e.svc.Store.Sync(ctx, st.League.ID)
		if rec.LastError == "" || rec.Pending != "" || !rec.LastSyncedAt.Before(e.now()) || !rec.LastAttemptAt.Equal(e.now()) {
			t.Errorf("after the failure: %+v", rec)
		}
		after := e.state(t, st.League.ID)
		if !slices.Equal(rosterKeys(after), rosterKeys(st)) || after.League.Name != st.League.Name {
			t.Error("a failed re-sync changed the league")
		}
		// The next request is allowed at once.
		if _, err := e.svc.RequestResync(ctx, owner, st.League.ID, true); err != nil {
			t.Errorf("request after a failure: %v", err)
		}
	})
}
