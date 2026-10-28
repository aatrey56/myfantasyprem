package mirror_test

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
)

// The real league again, captured later on 14 October 2026: a 5-for-5
// trade (343149) was accepted at 19:00 and waits for the commissioner's
// approval in FPL (state "a"); element-status marks its ten players
// in_accepted_trade. Only the trades and element-status files differ
// from real-gw6 (testdata/real-gw6-pending).

const pendingDir = "../provider/fpl/testdata/real-gw6-pending"

const (
	pendingTrade = "343149"
	tradesFile   = "draft/league/90001/trades.json"
	statusFile   = "league/90001/element-status.json"
	// The trade's teams by FPL entry id: element_out leaves the offering
	// entry, element_in arrives there.
	offerEntry   = 900008
	receiveEntry = 900003
)

var (
	pendingOut = []string{"5", "504", "237", "526", "26"}
	pendingIn  = []string{"93", "637", "628", "125", "167"}
)

// Wednesday 14 October 2026, 20:00 UTC: after the trade was accepted.
var pendingNow = time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC)

// fplStage is what FPL says about trade 343149: "" (the earlier capture,
// no such trade), "a" (accepted, waiting), "a-" (accepted, without its
// first pair of players), "p" (processed), "v" (vetoed), or a code we do
// not know: "x" with nobody moved, any other with the players swapped.
type fplStage struct{ state string }

// fs serves the capture as FPL shows it at the stage.
func (s *fplStage) fs(t *testing.T) patchedFS {
	read := func(name string) []byte {
		b, err := os.ReadFile(pendingDir + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	trades, status := read(tradesFile), read(statusFile)
	return patchedFS{base: os.DirFS(realDir), patch: map[string]func([]byte) []byte{
		tradesFile: func(old []byte) []byte {
			switch s.state {
			case "":
				return old
			case "a":
				return trades
			case "a-":
				return dropFirstItem(t, trades, pendingTrade)
			}
			return setTradeState(t, trades, pendingTrade, s.state)
		},
		statusFile: func(old []byte) []byte {
			switch s.state {
			case "", "v", "x":
				return old // nothing pending; nobody moved
			case "a", "a-":
				return status
			}
			return processedStatus(t, old)
		},
	}}
}

func setTradeState(t *testing.T, b []byte, id, state string) []byte {
	t.Helper()
	var doc struct {
		Trades []map[string]any `json:"trades"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, x := range doc.Trades {
		if jsonID(x["id"]) == id {
			x["state"] = state
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func dropFirstItem(t *testing.T, b []byte, id string) []byte {
	t.Helper()
	var doc struct {
		Trades []map[string]any `json:"trades"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, x := range doc.Trades {
		if jsonID(x["id"]) == id {
			x["tradeitem_set"] = x["tradeitem_set"].([]any)[1:]
		}
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// processedStatus is element-status after FPL processed the trade: the
// ten players swapped owners and none is in an accepted trade.
func processedStatus(t *testing.T, b []byte) []byte {
	t.Helper()
	var doc struct {
		Status []map[string]any `json:"element_status"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	for _, x := range doc.Status {
		id := jsonID(x["element"])
		switch {
		case slices.Contains(pendingOut, id):
			x["owner"] = receiveEntry
		case slices.Contains(pendingIn, id):
			x["owner"] = offerEntry
		}
		x["in_accepted_trade"] = false
	}
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func jsonID(v any) string {
	f, _ := v.(float64)
	return strconv.FormatInt(int64(f), 10)
}

// pendingEnvs runs test on the real capture with trade 343149 at a stage
// the test can change between syncs.
func pendingEnvs(t *testing.T, test func(t *testing.T, e *env, stage *fplStage)) {
	stage := &fplStage{}
	realEnvsFS(t, stage.fs(t), func(t *testing.T, e *env) {
		stage.state = "a"
		*e.clock = pendingNow
		test(t, e, stage)
	})
}

// teamOfEntry is the league's team for an FPL entry id.
func teamOfEntry(t *testing.T, l *league.League, entry int64) league.Team {
	t.Helper()
	for _, tm := range l.Teams {
		if tm.FPLEntryID == entry {
			return tm
		}
	}
	t.Fatalf("no team for entry %d", entry)
	return league.Team{}
}

// ours maps FPL element ids to our player ids (through the players'
// codes, which our stats key them by).
func ours(t *testing.T, e *env, elements []string) []string {
	t.Helper()
	b, err := os.ReadFile(realDir + "/bootstrap-static.json")
	if err != nil {
		t.Fatal(err)
	}
	var boot struct {
		Elements []struct {
			ID   int64 `json:"id"`
			Code int64 `json:"code"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(b, &boot); err != nil {
		t.Fatal(err)
	}
	code := map[string]string{}
	for _, el := range boot.Elements {
		code[strconv.FormatInt(el.ID, 10)] = strconv.FormatInt(el.Code, 10)
	}
	m, err := e.stats.ExternalIDs(ctx, "fpl", "player")
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, len(elements))
	for i, id := range elements {
		if out[i] = m[code[id]]; out[i] == "" {
			t.Fatalf("player %s unknown", id)
		}
	}
	return out
}

// importedTrade is the league's trade imported from FPL trade ext.
func importedTrade(t *testing.T, e *env, leagueID, ext string) league.Trade {
	t.Helper()
	st, err := e.leagues.State(ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := st.Ref(league.RefTrade, mirror.Provider, ext)
	if !ok {
		t.Fatalf("trade %s not imported", ext)
	}
	all, err := e.leagues.Trades(ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range all {
		if tr.ID == id {
			return tr
		}
	}
	t.Fatalf("trade %s (%s) not stored", ext, id)
	return league.Trade{}
}

func byStatus(rep *mirror.Report, status string) []mirror.Item {
	var out []mirror.Item
	for _, it := range rep.Items {
		if it.Status == status {
			out = append(out, it)
		}
	}
	return out
}

func tradeFeed(t *testing.T, e *env, leagueID string) []league.Txn {
	t.Helper()
	txns, err := e.leagues.Transactions(ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(txns, func(x league.Txn) bool { return x.Kind != league.TxnTrade })
}

// An accepted FPL trade is imported as a pending trade in review: both
// teams and all ten players, nobody moved, visible to the whole league,
// decided by FPL (no actions here); its players are "pending on FPL", not
// a mismatch.
func TestRealPendingTradeIsImportedAsPending(t *testing.T) {
	pendingEnvs(t, func(t *testing.T, e *env, _ *fplStage) {
		owner, st, rep := importReal(t, e)
		l := st.League
		for _, it := range rep.Items {
			if it.Status == mirror.ItemMismatch || it.Status == mirror.ItemUnverified {
				t.Errorf("%s %s: ours %q, FPL %q %s", strings.ToUpper(it.Status), it.Key, it.Ours, it.Theirs, it.Note)
			}
		}
		if rep.Status != mirror.ReportClean {
			t.Errorf("report status %s, want clean", rep.Status)
		}
		offer, receive := teamOfEntry(t, l, offerEntry), teamOfEntry(t, l, receiveEntry)
		pending := byStatus(rep, mirror.ItemPending)
		if len(pending) != 1 || pending[0].Kind != mirror.KindPendingTrade ||
			!strings.Contains(pending[0].Note, offer.Name) || !strings.Contains(pending[0].Note, receive.Name) {
			t.Fatalf("pending items = %+v", pending)
		}
		if pending[0].Ours != pending[0].Theirs {
			t.Errorf("pending item differs: ours %q, FPL %q", pending[0].Ours, pending[0].Theirs)
		}

		tr := importedTrade(t, e, l.ID, pendingTrade)
		out, in := ours(t, e, pendingOut), ours(t, e, pendingIn)
		if tr.Status != league.TradeInReview || tr.ReviewMode != league.ReviewCommissioner || tr.ReviewEndsAt != nil ||
			tr.ProcessedAt != nil || tr.OfferingTeamID != offer.ID || tr.ReceivingTeamID != receive.ID ||
			!slices.Equal(tr.Offered, out) || !slices.Equal(tr.Requested, in) || tr.EffectiveGW != 7 {
			t.Fatalf("pending trade = %+v", tr)
		}
		// Nobody moved, and no feed entry until FPL decides.
		after, _ := e.leagues.State(ctx, l.ID)
		owners := after.Game.OwnerAt(7)
		for _, id := range out {
			if owners[id] != offer.ID {
				t.Errorf("player %s moved before FPL processed the trade", id)
			}
		}
		for _, id := range in {
			if owners[id] != receive.ID {
				t.Errorf("player %s moved before FPL processed the trade", id)
			}
		}
		if n := len(tradeFeed(t, e, l.ID)); n != 3 {
			t.Errorf("%d trade feed entries, want 3", n)
		}

		// The whole league sees it, read-only.
		member := claimSeat(t, e, l, slices.IndexFunc(l.Teams, func(tm league.Team) bool {
			return tm.ID != offer.ID && tm.ID != receive.ID && tm.OwnerID == ""
		}))
		ts := trades.New(e.leagues, e.stats, e.now, nil)
		for _, who := range []string{owner, member} {
			v, err := ts.List(ctx, who, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			i := slices.IndexFunc(v.Trades, func(x league.Trade) bool { return x.ID == tr.ID })
			if i < 0 {
				t.Fatalf("%s does not see the pending trade", who)
			}
			tm, _ := v.League.TeamOf(who)
			if !trades.PendingOnFPL(v.Trades[i]) {
				t.Errorf("not pending on FPL: %+v", v.Trades[i])
			}
			if acts := trades.Actions(v.Trades[i], v.League, who, tm.ID); len(acts) != 0 {
				t.Errorf("actions on a trade pending on FPL: %v", acts)
			}
		}
		if _, _, err := ts.Review(ctx, owner, l.ID, tr.ID, trades.DecisionApprove, ""); slug(err) != "pending-on-fpl" {
			t.Errorf("review of a trade pending on FPL: %v", err)
		}

		// Pending items are not failing: nothing to explain.
		if _, err := e.svc.Explain(ctx, owner, l.ID, pending[0].Key, "fine"); slug(err) != "not-found" {
			t.Errorf("explain a pending item: %v", err)
		}

		// A re-sync while FPL still waits changes nothing.
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		if again := importedTrade(t, e, l.ID, pendingTrade); again.Status != league.TradeInReview {
			t.Errorf("after a resync: %+v", again)
		}
		if n := len(tradeFeed(t, e, l.ID)); n != 3 {
			t.Errorf("%d trade feed entries after a resync, want 3", n)
		}
	})
}

// When FPL processes the trade, the next re-sync applies it: the players
// move from the trade's gameweek, the feed shows it and the squads match.
func TestRealPendingTradeProcessedOnFPL(t *testing.T) {
	pendingEnvs(t, func(t *testing.T, e *env, stage *fplStage) {
		_, st, _ := importReal(t, e)
		l := st.League
		stage.state = "p"
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		rep, err := e.svc.Report(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range rep.Items {
			if it.Status != mirror.ItemOK {
				t.Errorf("%s %s: ours %q, FPL %q %s", strings.ToUpper(it.Status), it.Key, it.Ours, it.Theirs, it.Note)
			}
		}
		tr := importedTrade(t, e, l.ID, pendingTrade)
		if tr.Status != league.TradeProcessed || tr.ProcessedAt == nil {
			t.Fatalf("trade after FPL processed it = %+v", tr)
		}
		offer, receive := teamOfEntry(t, l, offerEntry), teamOfEntry(t, l, receiveEntry)
		after, _ := e.leagues.State(ctx, l.ID)
		before6, now7 := after.Game.OwnerAt(6), after.Game.OwnerAt(7)
		for _, id := range ours(t, e, pendingOut) {
			if before6[id] != offer.ID || now7[id] != receive.ID {
				t.Errorf("player %s: gw6 %s, gw7 %s", id, before6[id], now7[id])
			}
		}
		for _, id := range ours(t, e, pendingIn) {
			if before6[id] != receive.ID || now7[id] != offer.ID {
				t.Errorf("player %s: gw6 %s, gw7 %s", id, before6[id], now7[id])
			}
		}
		feed := tradeFeed(t, e, l.ID)
		if len(feed) != 4 || !slices.ContainsFunc(feed, func(x league.Txn) bool {
			return x.TradeID == tr.ID && x.Status == league.TradeProcessed && x.Gameweek == 7
		}) {
			t.Errorf("trade feed = %+v", feed)
		}
		// Applied once.
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		if n := len(tradeFeed(t, e, l.ID)); n != 4 {
			t.Errorf("%d trade feed entries after a resync, want 4", n)
		}
	})
}

// When FPL vetoes it, the next re-sync marks it vetoed; nobody moves.
func TestRealPendingTradeVetoedOnFPL(t *testing.T) {
	pendingEnvs(t, func(t *testing.T, e *env, stage *fplStage) {
		_, st, _ := importReal(t, e)
		l := st.League
		stage.state = "v"
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		rep, _ := e.svc.Report(ctx, l.ID)
		if rep.Status != mirror.ReportClean || len(byStatus(rep, mirror.ItemPending)) != 0 {
			t.Errorf("report after the veto: %s, %d pending", rep.Status, len(byStatus(rep, mirror.ItemPending)))
		}
		tr := importedTrade(t, e, l.ID, pendingTrade)
		if tr.Status != league.TradeVetoed || tr.ProcessedAt == nil {
			t.Fatalf("trade after FPL vetoed it = %+v", tr)
		}
		after, _ := e.leagues.State(ctx, l.ID)
		if !slices.EqualFunc(after.Game.Rosters, st.Game.Rosters, func(a, b league.RosterSlot) bool { return a == b }) {
			t.Error("a vetoed trade moved players")
		}
		feed := tradeFeed(t, e, l.ID)
		if len(feed) != 4 || !slices.ContainsFunc(feed, func(x league.Txn) bool {
			return x.TradeID == tr.ID && x.Status == league.TradeVetoed
		}) {
			t.Errorf("trade feed = %+v", feed)
		}
	})
}

// A real disagreement is still a mismatch: FPL flags a player we have in
// no pending trade, or our pending trade's players are not flagged.
func TestRealPendingTradeDisagreementIsAMismatch(t *testing.T) {
	pendingEnvs(t, func(t *testing.T, e *env, stage *fplStage) {
		owner, st, _ := importReal(t, e)
		l := st.League
		// FPL's pending trade has other players than ours.
		stage.state = "a-"
		rep := verifyStored(t, e, l.ID)
		if got := mismatches(rep, mirror.KindPendingTrade); len(got) == 0 {
			t.Error("a pending trade with different players is not a mismatch")
		}
		mustBlock(t, e, owner, l.ID)

		// FPL no longer lists the trade, but we still have it pending.
		stage.state = ""
		rep = verifyStored(t, e, l.ID)
		if got := mismatches(rep, mirror.KindPendingTrade); len(got) == 0 {
			t.Error("a pending trade FPL does not show is not a mismatch")
		}
	})
}

// A re-sync cancels the open trades made here and brings the trade
// pending on FPL back exactly as it was; the re-sync after FPL processes
// it applies it.
func TestResyncKeepsATradePendingOnFPL(t *testing.T) {
	pendingEnvs(t, func(t *testing.T, e *env, stage *fplStage) {
		owner, st, _ := importReal(t, e)
		l := st.League
		fplTrade := importedTrade(t, e, l.ID, pendingTrade)
		w, err := league.SeasonWorld(ctx, e.stats)
		if err != nil {
			t.Fatal(err)
		}

		// A trade made here, between teams outside FPL's pending trade.
		offer, receive := teamOfEntry(t, l, offerEntry), teamOfEntry(t, l, receiveEntry)
		mine, _ := l.TeamOf(owner)
		if mine.ID == offer.ID || mine.ID == receive.ID {
			t.Fatalf("the importer's team is in FPL's trade")
		}
		other := l.Teams[slices.IndexFunc(l.Teams, func(tm league.Team) bool {
			return tm.ID != mine.ID && tm.ID != offer.ID && tm.ID != receive.ID
		})]
		give := st.Game.RosterAt(mine.ID, 7)[0]
		get := st.Game.RosterAt(other.ID, 7)[slices.IndexFunc(st.Game.RosterAt(other.ID, 7), func(id string) bool {
			return w.Players[id].Position == w.Players[give].Position
		})]
		ts := trades.New(e.leagues, e.stats, e.now, league.NopNotifier{})
		_, here, err := ts.Propose(ctx, owner, l.ID, other.ID, []string{give}, []string{get}, "")
		if err != nil {
			t.Fatalf("trade made here: %v", err)
		}

		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		all, err := e.leagues.Trades(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(all, func(x league.Trade) bool { return x.ID == here.ID })
		if i < 0 || all[i].Status != league.TradeFailed || all[i].FailureReason != league.TradeFailResynced {
			t.Errorf("the trade made here after a re-sync: %+v", all)
		}
		if again := importedTrade(t, e, l.ID, pendingTrade); !reflect.DeepEqual(again, fplTrade) {
			t.Errorf("the trade pending on FPL changed:\n got %+v\nwant %+v", again, fplTrade)
		}
		rec, _ := e.svc.Store.Sync(ctx, l.ID)
		if rec.Note != "Re-synced from FPL. 1 open trade was cancelled." {
			t.Errorf("note = %q", rec.Note)
		}
		rep, _ := e.svc.Report(ctx, l.ID)
		if rep.Status != mirror.ReportClean || len(byStatus(rep, mirror.ItemPending)) != 1 {
			t.Errorf("report after the re-sync: %s, %d pending", rep.Status, len(byStatus(rep, mirror.ItemPending)))
		}

		// FPL processes it: the next re-sync applies it.
		stage.state = "p"
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, l.ID); err != nil {
			t.Fatal(err)
		}
		if tr := importedTrade(t, e, l.ID, pendingTrade); tr.Status != league.TradeProcessed || tr.ProcessedAt == nil {
			t.Fatalf("trade after FPL processed it = %+v", tr)
		}
		after, _ := e.leagues.State(ctx, l.ID)
		owners := after.Game.OwnerAt(7)
		for _, id := range ours(t, e, pendingOut) {
			if owners[id] != receive.ID {
				t.Errorf("player %s on %s after the processed trade", id, owners[id])
			}
		}
		for _, id := range ours(t, e, pendingIn) {
			if owners[id] != offer.ID {
				t.Errorf("player %s on %s after the processed trade", id, owners[id])
			}
		}
	})
}

// FPL documents trade states (rejected, withdrawn, expired, invalid) whose
// codes no capture has shown. One such closed trade that moved nobody needs
// no counterpart here, like a declined trade: the import and the report
// stay clean. One that moved the players is still flagged as unverified.
func TestRealTradeInAnUnknownState(t *testing.T) {
	for _, c := range []struct {
		code       string
		unverified int
	}{{"x", 0}, {"y", 1}} {
		t.Run(c.code, func(t *testing.T) {
			pendingEnvs(t, func(t *testing.T, e *env, stage *fplStage) {
				stage.state = c.code
				_, _, rep := importReal(t, e)
				var flagged []mirror.Item
				for _, it := range rep.Items {
					if it.Key == "trade-state:"+pendingTrade {
						flagged = append(flagged, it)
					}
				}
				if len(flagged) != c.unverified || (c.unverified == 0 && rep.Status != mirror.ReportClean) {
					t.Errorf("report %s, trade-state items %+v", rep.Status, flagged)
				}
			})
		})
	}
}

// Whether an unknown-state trade moved anybody is decided from each
// gameweek's picks since the trade, not only today's owners: here the
// trade counts from gameweek 6, the last one with picks. Players swapped
// in gameweek 6 and swapped back since look unmoved in element-status;
// the trade is still flagged. So is one whose gameweek-6 picks FPL does
// not show. Only one whose players stayed put in every gameweek is clean.
func TestRealUnknownTradeStateReadsSquadHistory(t *testing.T) {
	const (
		offerPicks   = "entry/900008/event/6.json"
		receivePicks = "entry/900003/event/6.json"
	)
	swap := func(from, to string) func([]byte) []byte {
		return func(b []byte) []byte {
			var doc map[string]any
			if err := json.Unmarshal(b, &doc); err != nil {
				t.Fatal(err)
			}
			for _, pk := range doc["picks"].([]any) {
				if pk := pk.(map[string]any); jsonID(pk["element"]) == from {
					pk["element"], _ = strconv.Atoi(to)
				}
			}
			out, err := json.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	for _, c := range []struct {
		name    string
		patch   map[string]func([]byte) []byte
		flagged bool
	}{
		{"nobody ever moved", nil, false},
		{"moved in gameweek 6 and back since", map[string]func([]byte) []byte{
			offerPicks:   swap(pendingOut[0], pendingIn[0]),
			receivePicks: swap(pendingIn[0], pendingOut[0]),
		}, true},
		{"gameweek 6 picks missing", map[string]func([]byte) []byte{offerPicks: nil}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			stage := &fplStage{state: "x"}
			fsys := stage.fs(t)
			trades := fsys.patch[tradesFile]
			fsys.patch[tradesFile] = func(b []byte) []byte {
				var doc struct {
					Trades []map[string]any `json:"trades"`
				}
				if err := json.Unmarshal(trades(b), &doc); err != nil {
					t.Fatal(err)
				}
				for _, x := range doc.Trades {
					if jsonID(x["id"]) == pendingTrade {
						x["event"] = 6
					}
				}
				out, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			for name, f := range c.patch {
				fsys.patch[name] = f
			}
			realEnvsFS(t, fsys, func(t *testing.T, e *env) {
				*e.clock = pendingNow
				_, _, rep := importReal(t, e)
				var flagged []mirror.Item
				for _, it := range rep.Items {
					if it.Key == "trade-state:"+pendingTrade {
						flagged = append(flagged, it)
					}
				}
				if (len(flagged) == 1) != c.flagged || len(flagged) > 1 || (!c.flagged && rep.Status != mirror.ReportClean) {
					t.Errorf("report %s, trade-state items %+v", rep.Status, flagged)
				}
			})
		})
	}
}

// A move and move-back between two deadlines shows in no gameweek's
// picks, so an unknown-state trade is a no-op only if no other FPL
// transaction or trade touched its players at or after its offer time.
// Here the snapshots stay as captured: a later free-agent move, or a
// later vetoed trade, naming one of its players keeps it flagged.
func TestRealUnknownTradeStateReadsLaterMoves(t *testing.T) {
	const transactionsFile = "draft/league/90001/transactions.json"
	later := "2026-10-14T19:30:00Z" // after the trade's offer time
	freeAgent := func(b []byte) []byte {
		var doc map[string]any
		if err := json.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		doc["transactions"] = append(doc["transactions"].([]any), map[string]any{
			"added": later, "element_in": 414, "element_out": 5, "entry": offerEntry,
			"event": 6, "id": 99999999, "index": nil, "kind": "f", "priority": nil, "result": "a",
		})
		out, err := json.Marshal(doc)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	for _, c := range []struct {
		name    string
		patch   func(fsys patchedFS)
		flagged bool
	}{
		{"nothing since", func(patchedFS) {}, false},
		{"no offer time", func(fsys patchedFS) {
			trades := fsys.patch[tradesFile]
			fsys.patch[tradesFile] = func(b []byte) []byte {
				var doc map[string]any
				if err := json.Unmarshal(trades(b), &doc); err != nil {
					t.Fatal(err)
				}
				for _, tr := range doc["trades"].([]any) {
					if m := tr.(map[string]any); m["state"] == "x" {
						m["offer_time"] = nil
					}
				}
				out, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
		}, true},
		{"free-agent move since", func(fsys patchedFS) { fsys.patch[transactionsFile] = freeAgent }, true},
		{"another trade since", func(fsys patchedFS) {
			trades := fsys.patch[tradesFile]
			fsys.patch[tradesFile] = func(b []byte) []byte {
				var doc map[string]any
				if err := json.Unmarshal(trades(b), &doc); err != nil {
					t.Fatal(err)
				}
				doc["trades"] = append(doc["trades"].([]any), map[string]any{
					"event": 6, "id": 399999, "offered_entry": receiveEntry, "offer_time": later,
					"received_entry": offerEntry, "response_time": later, "state": "v",
					"tradeitem_set": []any{map[string]any{"element_in": 5, "element_out": 93}},
				})
				out, err := json.Marshal(doc)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
		}, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			stage := &fplStage{state: "x"}
			fsys := stage.fs(t)
			c.patch(fsys)
			realEnvsFS(t, fsys, func(t *testing.T, e *env) {
				*e.clock = pendingNow
				_, _, rep := importReal(t, e)
				var flagged []mirror.Item
				for _, it := range rep.Items {
					if it.Key == "trade-state:"+pendingTrade {
						flagged = append(flagged, it)
					}
				}
				if (len(flagged) == 1) != c.flagged || len(flagged) > 1 || (!c.flagged && rep.Status != mirror.ReportClean) {
					t.Errorf("report %s, trade-state items %+v", rep.Status, flagged)
				}
			})
		})
	}
}
