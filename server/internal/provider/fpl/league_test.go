package fpl

import (
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// testdata/real-gw6 also holds a real FPL Draft league's public
// endpoints, captured once on 14 October 2026 (gameweek 6 finished, 7
// next) and anonymised by scripts/anonymise-fpl-league.mjs: invented
// names, every id and number as captured.

const realLeague = "90001"

var _ provider.LeagueMirrorSource = (*Client)(nil)

func TestRealLeagueDetailsDecodes(t *testing.T) {
	d, err := realClient().LeagueDetails(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	if d.ExternalID != realLeague || d.Name != "Fixture Draft League" || d.Scoring != provider.ScoringH2H ||
		d.TradeMode != provider.TradeModeCommissioner || d.TradeModeSource != "m" ||
		d.TransactionMode != provider.TransactionModeWaivers || !d.DraftCompleted || d.DraftGameweek != 1 ||
		d.PickTimeSeconds != 120 || d.StartGameweek != 1 || d.EndGameweek != 38 {
		t.Errorf("details = %+v", d)
	}
	if d.DraftCompletedAt == nil || !d.DraftCompletedAt.Equal(time.Date(2026, 8, 15, 22, 59, 53, 207870000, time.UTC)) {
		t.Errorf("draft completed at %v", d.DraftCompletedAt)
	}
	if len(d.Teams) != 12 || len(d.Matches) != 228 || len(d.Standings) != 12 {
		t.Fatalf("teams %d matches %d standings %d", len(d.Teams), len(d.Matches), len(d.Standings))
	}
	// The admin is entry 900001, league entry 800001.
	if d.AdminEntry != "800001" {
		t.Errorf("admin entry %q", d.AdminEntry)
	}
	team := map[string]provider.LeagueEntry{}
	picks := map[int]bool{}
	for _, e := range d.Teams {
		team[e.ExternalID] = e
		picks[e.WaiverPick] = true
		if e.IsAverage || e.EntryID == "" || e.TeamName == "" || e.ManagerName == "" || e.JoinedAt == nil {
			t.Errorf("team %+v", e)
		}
	}
	if len(picks) != 12 || !picks[1] || !picks[12] {
		t.Errorf("waiver picks %v", picks)
	}
	if e := team["800001"]; e.EntryID != "900001" || e.ManagerName != "Alex Applegate" || e.ShortName != "AA" {
		t.Errorf("seat 800001 = %+v", e)
	}
	finished := 0
	for _, m := range d.Matches {
		if _, ok := team[m.HomeEntry]; !ok {
			t.Fatalf("match %+v: unknown home", m)
		}
		if m.Finished {
			finished++
			if m.Gameweek > 6 || !m.Started {
				t.Errorf("match %+v", m)
			}
		}
	}
	if finished != 36 {
		t.Errorf("%d finished matches", finished)
	}
	m := d.Matches[0]
	if m.Gameweek != 1 || m.HomeEntry != "800008" || m.HomePts != 38 || m.AwayEntry != "800001" || m.AwayPts != 31 {
		t.Errorf("first match = %+v", m)
	}
	if s := d.Standings[0]; s.Rank != 1 || s.LeagueEntry != "800007" || s.Wins != 6 || s.Losses != 0 || s.PointsFor != 265 ||
		s.PointsAgainst != 209 || s.Total != 18 {
		t.Errorf("first standing = %+v", s)
	}
}

func TestRealLeagueOwnershipDraftAndMoves(t *testing.T) {
	c := realClient()
	b, err := c.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codeOf := map[int64]string{}
	for _, p := range b.Players {
		codeOf[p.SeasonElementID] = p.ExternalID
	}
	d, err := c.LeagueDetails(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	seats := map[string]bool{}
	for _, e := range d.Teams {
		seats[e.ExternalID] = true
	}

	es, err := c.ElementStatus(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	owned := 0
	for _, e := range es {
		if e.OwnerEntry != nil {
			owned++
			if !seats[*e.OwnerEntry] || e.Ownership != provider.OwnershipOwned {
				t.Errorf("status %+v owner %s", e, *e.OwnerEntry)
			}
		}
	}
	if len(es) != 667 || owned != 180 {
		t.Errorf("%d statuses, %d owned", len(es), owned)
	}

	choices, err := c.DraftChoices(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	if len(choices) != 180 {
		t.Fatalf("%d choices", len(choices))
	}
	if ch := choices[0]; ch.Overall != 1 || ch.Round != 1 || ch.Pick != 1 || ch.LeagueEntry == "" || !seats[ch.LeagueEntry] ||
		ch.PlayerExternalID != codeOf[411] || ch.WasAuto {
		t.Errorf("first choice = %+v", ch)
	}
	for i, ch := range choices {
		if ch.Overall != i+1 {
			t.Fatalf("choice %d has overall %d", i, ch.Overall)
		}
	}

	txns, err := c.Transactions(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	count := map[string]int{}
	for _, x := range txns {
		count[x.SourceState]++
		if !seats[x.LeagueEntry] || x.PlayerIn == "" || x.PlayerOut == "" || x.Gameweek < 1 || x.Gameweek > 6 {
			t.Errorf("transaction %+v", x)
		}
	}
	if len(txns) != 199 || count["w/a"] != 73 || count["w/di"] != 81 || count["w/do"] != 23 || count["f/a"] != 22 {
		t.Errorf("%d transactions: %v", len(txns), count)
	}
	x := txns[0]
	if x.ExternalID != "463248" || x.Kind != provider.TransactionWaiver || x.Result != provider.ClaimApproved ||
		x.PlayerIn != codeOf[31] || x.PlayerOut != codeOf[9] || x.Priority == nil || *x.Priority != 1 || x.Index != 3 {
		t.Errorf("first transaction = %+v", x)
	}

	trades, err := c.Trades(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 3 {
		t.Fatalf("%d trades", len(trades))
	}
	if tr := trades[0]; tr.State != provider.TradeVetoed || tr.SourceState != "v" || tr.Gameweek != 5 {
		t.Errorf("vetoed trade = %+v", tr)
	}
	// element_out leaves the offering entry, element_in arrives there.
	tr := trades[2]
	if tr.ExternalID != "322640" || tr.State != provider.TradeProcessed || tr.Gameweek != 6 || !seats[tr.OfferEntry] ||
		!seats[tr.ReceiveEntry] || len(tr.Offered) != 3 || tr.Offered[0] != codeOf[566] || tr.Requested[0] != codeOf[564] ||
		tr.ResolvedAt == nil {
		t.Errorf("processed trade = %+v", tr)
	}
}

// testdata/real-gw6-pending is the same league later that day: trade
// 343149 was accepted and waits for the commissioner (state "a").
func TestRealPendingTradeDecodes(t *testing.T) {
	fsys := overlayFS{top: os.DirFS("testdata/real-gw6-pending"), base: os.DirFS("testdata/real-gw6")}
	c := New(Options{Transport: DirTransport{FS: fsys}, Logger: quiet(), MinInterval: -1})
	boot, err := c.Bootstrap(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codeOf := map[int64]string{}
	for _, p := range boot.Players {
		codeOf[p.SeasonElementID] = p.ExternalID
	}
	trades, err := c.Trades(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	if len(trades) != 4 {
		t.Fatalf("%d trades", len(trades))
	}
	tr := trades[3]
	if tr.ExternalID != "343149" || tr.State != provider.TradeAccepted || tr.SourceState != "a" || tr.Gameweek != 7 ||
		tr.OfferEntry == tr.ReceiveEntry || len(tr.Offered) != 5 || tr.Offered[0] != codeOf[5] || tr.Requested[0] != codeOf[93] ||
		tr.ResolvedAt == nil || !tr.ResolvedAt.Equal(time.Date(2026, 10, 14, 19, 0, 50, 27502000, time.UTC)) {
		t.Errorf("accepted trade = %+v", tr)
	}
	status, err := c.ElementStatus(ctx, realLeague)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range status {
		if s.InAcceptedTrade {
			n++
		}
	}
	if n != 10 {
		t.Errorf("%d players in an accepted trade, want 10", n)
	}
}

// FPL Draft's transaction_mode is the phase of the gameweek: "waivers"
// before the waiver run, "free-agency" after it. Both are the
// waivers-then-free-agency model; anything else is unknown, kept as sent.
func TestTransactionModes(t *testing.T) {
	for mode, want := range map[string]provider.TransactionMode{
		"waivers": provider.TransactionModeWaivers, "free-agency": provider.TransactionModeWaivers,
		"free_agency": provider.TransactionModeUnknown, "trades-only": provider.TransactionModeUnknown, "": provider.TransactionModeUnknown,
	} {
		c := synthetic(map[string]string{"league/5/details.json": strings.Replace(oddDetails,
			`"transaction_mode":"waivers"`, `"transaction_mode":"`+mode+`"`, 1)})
		d, err := c.LeagueDetails(ctx, "5")
		if err != nil {
			t.Fatalf("%q: %v", mode, err)
		}
		if d.TransactionMode != want || d.TransactionModeSource != mode {
			t.Errorf("%q: mode %v (source %q), want %v", mode, d.TransactionMode, d.TransactionModeSource, want)
		}
	}
}

// Every FPL Draft trade code we have seen decodes to its state; any other
// code is unknown, with the code kept.
func TestTradeStates(t *testing.T) {
	for code, want := range map[string]provider.TradeState{
		"p": provider.TradeProcessed, "v": provider.TradeVetoed, "a": provider.TradeAccepted,
		"o": provider.TradeUnknown, "x": provider.TradeUnknown, "": provider.TradeUnknown,
	} {
		c := synthetic(map[string]string{"league/5/details.json": oddDetails, "draft/league/5/trades.json": `{"trades":[
		  {"id":1,"event":1,"offered_entry":900,"received_entry":901,"state":"` + code + `","offer_time":"2026-08-19T14:51:12Z",
		   "tradeitem_set":[{"element_in":1,"element_out":2}]}]}`})
		if _, err := c.Bootstrap(ctx); err != nil {
			t.Fatal(err)
		}
		trades, err := c.Trades(ctx, "5")
		if err != nil {
			t.Fatalf("%q: %v", code, err)
		}
		if trades[0].State != want || trades[0].SourceState != code {
			t.Errorf("%q: %s (%q), want %s", code, trades[0].State, trades[0].SourceState, want)
		}
	}
}

// overlayFS serves top's files over base's.
type overlayFS struct{ top, base fs.FS }

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := o.top.Open(name); err == nil {
		return f, nil
	}
	return o.base.Open(name)
}

func TestRealEntryPicks(t *testing.T) {
	c := realClient()
	if _, err := c.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := c.EntryEventPicks(ctx, "900001", 6)
	if err != nil {
		t.Fatal(err)
	}
	if p.EntryID != "900001" || p.Gameweek != 6 || len(p.Picks) != 15 || len(p.AutoSubs) != 1 || p.Points != nil {
		t.Fatalf("picks = %+v", p)
	}
	for i, pk := range p.Picks {
		if pk.Slot != i+1 || pk.PlayerExternalID == "" {
			t.Errorf("pick %d = %+v", i, pk)
		}
	}
	// Gameweek 7's lineups are private until its deadline.
	if _, err := c.EntryEventPicks(ctx, "900001", 7); !errors.Is(err, provider.ErrNotYetAvailable) {
		t.Errorf("gameweek 7: %v, want ErrNotYetAvailable", err)
	}
	if _, err := c.EntryEventPicks(ctx, "999", 6); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("unknown entry: %v, want ErrNotFound", err)
	}
}

// synthetic serves a minimal bootstrap and the given league files.
func synthetic(files map[string]string) *Client {
	fs := fstest.MapFS{"bootstrap-static.json": {Data: []byte(`{"elements":[
	  {"id":1,"code":101,"web_name":"A","team":1,"element_type":1},
	  {"id":2,"code":102,"web_name":"B","team":2,"element_type":2}],
	 "element_types":[],"teams":[{"id":1,"code":11,"name":"One","short_name":"ONE"},{"id":2,"code":12,"name":"Two","short_name":"TWO"}],
	 "events":{"current":1,"next":2,"data":[{"id":1,"deadline_time":"2026-08-21T17:30:00Z"}]},"fixtures":[],"settings":{}}`)}}
	for k, v := range files {
		fs[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return New(Options{Transport: DirTransport{FS: fs}, Logger: quiet(), MinInterval: -1})
}

const oddDetails = `{"league":{"id":5,"name":"Odd","scoring":"h","trades":"x","transaction_mode":"waivers","start_event":1,"stop_event":38,
  "admin_entry":900,"draft_pick_time_limit":60,"drafts":[{"draft_completed":null,"event":1}]},
 "league_entries":[
  {"id":1,"entry_id":900,"entry_name":"T1","player_first_name":"F","player_last_name":"L","short_name":"FL","waiver_pick":2},
  {"id":2,"entry_id":901,"entry_name":"T2","player_first_name":"G","player_last_name":"M","short_name":"GM","waiver_pick":1},
  {"id":3,"entry_id":null,"entry_name":"Average","player_first_name":"Average","player_last_name":"","short_name":"AV","waiver_pick":0}],
 "matches":[{"event":1,"league_entry_1":1,"league_entry_1_points":10,"league_entry_2":3,"league_entry_2_points":9,"started":true,"finished":true}],
 "standings":[{"league_entry":1,"rank":1,"matches_won":1,"matches_drawn":0,"matches_lost":0,"points_for":10,"points_against":9,"total":3}]}`

func TestLeagueDetailsQuirks(t *testing.T) {
	c := synthetic(map[string]string{"league/5/details.json": oddDetails})
	d, err := c.LeagueDetails(ctx, "5")
	if err != nil {
		t.Fatal(err)
	}
	if d.DraftCompleted || d.TradeMode != provider.TradeModeUnknown || d.TradeModeSource != "x" || d.AdminEntry != "1" {
		t.Errorf("details = %+v", d)
	}
	if av := d.Teams[2]; !av.IsAverage || av.EntryID != "" {
		t.Errorf("average = %+v", av)
	}
}

func TestLeagueResponsesAreValidated(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"match names an unknown entry": {"league/5/details.json": strings.Replace(oddDetails, `"league_entry_2":3`, `"league_entry_2":7`, 1)},
		"duplicate entry":              {"league/5/details.json": strings.Replace(oddDetails, `"id":2,"entry_id":901`, `"id":1,"entry_id":901`, 1)},
		"choice for unknown entry": {"league/5/details.json": oddDetails, "draft/5/choices.json": `{"choices":[
		  {"id":1,"index":1,"round":1,"pick":1,"entry":555,"element":1,"was_auto":false,"choice_time":"2026-08-15T20:30:09Z"}]}`},
		"owner of unknown element": {"league/5/details.json": oddDetails, "league/5/element-status.json": `{"element_status":[
		  {"element":77,"owner":900,"status":"o","in_accepted_trade":false}]}`},
		"transaction with unknown kind": {"league/5/details.json": oddDetails, "draft/league/5/transactions.json": `{"transactions":[
		  {"id":1,"entry":900,"element_in":1,"element_out":2,"event":1,"kind":"q","result":"a","index":1,"priority":1,"added":"2026-08-19T14:51:12Z"}]}`},
		"trade with uneven items": {"league/5/details.json": oddDetails, "draft/league/5/trades.json": `{"trades":[
		  {"id":1,"event":1,"offered_entry":900,"received_entry":901,"state":"p","offer_time":"2026-08-19T14:51:12Z","tradeitem_set":[]}]}`},
	} {
		t.Run(name, func(t *testing.T) {
			c := synthetic(files)
			var err error
			switch {
			case files["draft/5/choices.json"] != "":
				_, err = c.DraftChoices(ctx, "5")
			case files["league/5/element-status.json"] != "":
				_, err = c.ElementStatus(ctx, "5")
			case files["draft/league/5/transactions.json"] != "":
				_, err = c.Transactions(ctx, "5")
			case files["draft/league/5/trades.json"] != "":
				_, err = c.Trades(ctx, "5")
			default:
				_, err = c.LeagueDetails(ctx, "5")
			}
			if !errors.Is(err, provider.ErrInvalid) || !errors.Is(err, provider.ErrUpstream) {
				t.Errorf("err = %v, want ErrInvalid", err)
			}
		})
	}
}

// FPL may answer a gameweek's picks before its deadline with a 404 whose
// body says "No pick history": still not yet available, not unknown.
func TestPicksBeforeTheDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/entry/1/event/7":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`"No pick history"`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"Not found."}`))
		}
	}))
	defer srv.Close()
	c := New(Options{BaseURL: srv.URL + "/api", Logger: quiet(), MinInterval: -1})
	c.lastBoot = &bootstrapIndex{elementCode: map[int64]string{}}
	if _, err := c.EntryEventPicks(ctx, "1", 7); !errors.Is(err, provider.ErrNotYetAvailable) {
		t.Errorf("404 No pick history: %v", err)
	}
	if _, err := c.EntryEventPicks(ctx, "2", 7); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("plain 404: %v", err)
	}
}
