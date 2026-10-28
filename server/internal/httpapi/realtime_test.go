package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/pb33f/libopenapi-validator/schema_validation"
	"github.com/pb33f/libopenapi/datamodel/high/base"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// rtEnv is a live HTTP server (with cmd/api's timeouts, shortened) on the
// memory stores, the demo season loaded and a full 4-team league.
type rtEnv struct {
	srv     *Server
	url     string
	tokens  []string // league members; tokens[0] is the commissioner
	league  *league.League
	schema  *base.Schema
	outside string
}

func newRTEnv(t *testing.T) *rtEnv {
	t.Helper()
	srv, _ := newTestServer()
	// Real time: the WebSocket deadlines are real.
	srv.now = func() time.Time { return time.Now().UTC() }
	srv.log = testOptions().Logger
	if _, err := demo.LoadStats(context.Background(), srv.stats, testOptions().Logger, srv.now); err != nil {
		t.Fatal(err)
	}
	// testToken was issued on the fake clock (14:00 today) and expires on
	// the real one: sign the commissioner in again now.
	commishToken := signIn(srv, "Sam P.", "sam@example.com")
	e := &rtEnv{srv: srv, tokens: []string{commishToken}}
	commish := userOf(t, srv, commishToken)
	l, err := srv.leagues.Create(context.Background(), commish, "Live League", 4, "One", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := srv.leagues.CreateInvite(context.Background(), commish, l.ID, nil, nil)
	for i := range 3 {
		tok := signIn(srv, "Player", "p"+string(rune('a'+i))+"@example.com")
		e.tokens = append(e.tokens, tok)
		if l, _, err = srv.leagues.AcceptInvite(context.Background(), userOf(t, srv, tok), inv.Code, "Side "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
	}
	e.league = l
	e.outside = signIn(srv, "Stranger", "stranger@example.com")
	opts := testOptions()
	hs := httptest.NewUnstartedServer(newHandler(opts, newRoutes(opts, srv)))
	// cmd/api sets these (15 s write); short here, to prove the socket
	// clears them.
	hs.Config.WriteTimeout = 300 * time.Millisecond
	hs.Config.ReadTimeout = 300 * time.Millisecond
	hs.Start()
	t.Cleanup(func() { srv.stop(); hs.Close() })
	e.url = hs.URL

	doc, _ := loadSpec(t)
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatal(err)
	}
	e.schema = model.Model.Components.Schemas.GetOrZero("RealtimeServerMessage").Schema()
	return e
}

// ticket gets a ticket over HTTP.
func (e *rtEnv) ticket(t *testing.T, token, body string) RealtimeTicket {
	t.Helper()
	req, _ := http.NewRequest("POST", e.url+"/v1/realtime/tickets", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out RealtimeTicketResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil || res.StatusCode != 201 {
		t.Fatalf("ticket: %d %v", res.StatusCode, err)
	}
	return out.Data
}

func (e *rtEnv) dial(t *testing.T, tk RealtimeTicket) *websocket.Conn {
	t.Helper()
	if !strings.HasPrefix(tk.Url, "ws://") || !strings.Contains(tk.Url, "/v1/realtime?ticket=") {
		t.Fatalf("ticket url = %s", tk.Url)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, tk.Url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return c
}

// read returns the next message as a map, validated against the
// contract's RealtimeServerMessage.
func (e *rtEnv) read(t *testing.T, c *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if ok, errs := schema_validation.NewSchemaValidator().ValidateSchemaString(e.schema, string(data)); !ok {
		for _, ve := range errs {
			t.Errorf("message does not match RealtimeServerMessage: %s: %s", ve.Message, ve.Reason)
		}
		t.Fatalf("message: %s", data)
	}
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	return m
}

func send(t *testing.T, c *websocket.Conn, msg string) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageText, []byte(msg)); err != nil {
		t.Fatal(err)
	}
}

func (e *rtEnv) post(t *testing.T, token, path, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("POST", e.url+path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestRealtimeDraftEvents(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	c := e.dial(t, e.ticket(t, e.tokens[1], `{"leagueId":"`+lid+`"}`))

	// Past cmd/api's write and read timeouts the socket still works.
	time.Sleep(500 * time.Millisecond)
	send(t, c, `{"op":"ping"}`)
	if m := e.read(t, c); m["type"] != "pong" {
		t.Fatalf("pong = %v", m)
	}

	if res := e.post(t, e.tokens[0], "/v1/leagues/"+lid+"/draft/start", ""); res.StatusCode != 200 {
		t.Fatalf("start = %d", res.StatusCode)
	}
	var types []string
	var lastSeq float64
	for len(types) < 2 {
		m := e.read(t, c)
		types = append(types, m["type"].(string))
		if m["seq"].(float64) <= lastSeq {
			t.Fatalf("seq went back: %v after %v", m["seq"], lastSeq)
		}
		lastSeq = m["seq"].(float64)
	}
	if types[0] != "draft.updated" || types[1] != "league.updated" {
		t.Fatalf("types = %v", types)
	}
	// The commissioner picks for the team on the clock.

	w, _ := league.SeasonWorld(context.Background(), e.srv.stats)
	var best string
	for id, p := range w.Players {
		if best == "" || p.DraftRank < w.Players[best].DraftRank {
			best = id
		}
	}
	if res := e.post(t, e.tokens[0], "/v1/leagues/"+lid+"/draft/picks", `{"playerId":"`+best+`"}`); res.StatusCode != 201 {
		t.Fatalf("pick = %d", res.StatusCode)
	}
	m := e.read(t, c)
	if m["type"] != "draft.pick_made" || m["pick"].(map[string]any)["overall"].(float64) != 1 || m["currentPick"] == nil {
		t.Fatalf("pick event = %v", m)
	}

	// A second client reconnecting with afterSeq gets what it missed.
	c2 := e.dial(t, e.ticket(t, e.tokens[2], `{}`))
	send(t, c2, `{"op":"subscribe","leagueId":"`+lid+`","afterSeq":1}`)
	var replayed []string
	for len(replayed) < 2 {
		replayed = append(replayed, e.read(t, c2)["type"].(string))
	}
	if replayed[0] != "league.updated" || replayed[1] != "draft.pick_made" {
		t.Fatalf("replay = %v", replayed)
	}
}

func TestRealtimeRefusals(t *testing.T) {
	e := newRTEnv(t)
	// A bad or used ticket is refused before the upgrade.
	res, err := http.Get(e.url + "/v1/realtime?ticket=nope")
	if err != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad ticket: %v %v", res.StatusCode, err)
	}
	tk := e.ticket(t, e.outside, `{}`)
	c := e.dial(t, tk)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := websocket.Dial(ctx, tk.Url, nil); err == nil {
		t.Fatal("a ticket worked twice")
	}
	// Subscribing to a league one is not in closes with 4403.
	send(t, c, `{"op":"subscribe","leagueId":"`+e.league.ID+`"}`)
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != 4403 {
		t.Fatalf("close = %v", err)
	}
	// A ticket for a league one is not in is refused at once.
	if res := e.post(t, e.outside, "/v1/realtime/tickets", `{"leagueId":"`+e.league.ID+`"}`); res.StatusCode != 403 {
		t.Fatalf("ticket for another league = %d", res.StatusCode)
	}
}

// Trade events reach the two teams only until the trade is accepted, and
// each sees its own viewerActions.
func TestRealtimeTradeAudience(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	ctx := context.Background()
	for i, tok := range e.tokens {
		if _, err := e.srv.drafts().SetAutopick(ctx, userOf(t, e.srv, tok), lid, true); err != nil && i > 0 {
			t.Fatal(err)
		}
	}
	if _, err := e.srv.drafts().Start(ctx, userOf(t, e.srv, e.tokens[0]), lid); err != nil {
		t.Fatal(err)
	}
	st, _ := e.srv.leagues.Repo().State(ctx, lid)
	if st.League.Status != league.StatusInSeason {
		t.Fatalf("draft did not finish: %s", st.League.Status)
	}
	teams := st.League.Teams
	gw := st.Game.Rosters[0].FromGW
	gk := func(team string) string {
		w, _ := league.SeasonWorld(ctx, e.srv.stats)
		for _, id := range st.Game.RosterAt(team, gw) {
			if w.Players[id].Position == "GKP" {
				return id
			}
		}
		return ""
	}
	receiver := e.dial(t, e.ticket(t, e.tokens[1], `{"leagueId":"`+lid+`"}`))
	other := e.dial(t, e.ticket(t, e.tokens[2], `{"leagueId":"`+lid+`"}`))
	body := `{"receivingTeamId":"` + teams[1].ID + `","offeredPlayerIds":["` + gk(teams[0].ID) + `"],"requestedPlayerIds":["` + gk(teams[1].ID) + `"]}`
	if res := e.post(t, e.tokens[0], "/v1/leagues/"+lid+"/trades", body); res.StatusCode != 201 {
		t.Fatalf("propose = %d", res.StatusCode)
	}
	m := e.read(t, receiver)
	tr := m["trade"].(map[string]any)
	if m["type"] != "trade.updated" || len(tr["viewerActions"].([]any)) != 2 {
		t.Fatalf("receiver event = %v", m)
	}
	// The other team hears nothing about the offer: its next message is
	// the pong to its ping.
	send(t, other, `{"op":"ping"}`)
	if m := e.read(t, other); m["type"] != "pong" {
		t.Fatalf("other team got %v", m)
	}
}
