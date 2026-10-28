package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
)

// Sockets are authorized when they connect and subscribe; these tests
// prove they are also cut off when that authorization goes away.

func (e *rtEnv) call(t *testing.T, token, method, path, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(method, e.url+path, strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
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

// expectClosed reads (skipping event frames) until the server closes the
// socket and checks the close code.
func expectClosed(t *testing.T, c *websocket.Conn, code websocket.StatusCode) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for {
		_, data, err := c.Read(ctx)
		if err == nil {
			continue // an event that raced the revocation
		}
		if got := websocket.CloseStatus(err); got != code {
			t.Fatalf("socket still open or closed with %d (want %d): %v (last frame %s)", got, code, err, data)
		}
		return
	}
}

// expectOpen proves a socket still answers.
func (e *rtEnv) expectOpen(t *testing.T, c *websocket.Conn) {
	t.Helper()
	send(t, c, `{"op":"ping"}`)
	for {
		if m := e.read(t, c); m["type"] == "pong" {
			return
		}
	}
}

func TestRealtimeLeaveClosesSocket(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	c := e.dial(t, e.ticket(t, e.tokens[1], `{"leagueId":"`+lid+`"}`))
	keep := e.dial(t, e.ticket(t, e.tokens[2], `{"leagueId":"`+lid+`"}`))
	if res := e.call(t, e.tokens[1], "POST", "/v1/leagues/"+lid+"/leave", ""); res.StatusCode != 204 {
		t.Fatalf("leave = %d", res.StatusCode)
	}
	expectClosed(t, c, closeForbidden)
	e.expectOpen(t, keep)
}

func TestRealtimeRemoveMemberClosesSocket(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	c := e.dial(t, e.ticket(t, e.tokens[3], `{"leagueId":"`+lid+`"}`))
	uid := userOf(t, e.srv, e.tokens[3])
	if res := e.call(t, e.tokens[0], "DELETE", "/v1/leagues/"+lid+"/members/"+uid, ""); res.StatusCode != 204 {
		t.Fatalf("remove = %d", res.StatusCode)
	}
	expectClosed(t, c, closeForbidden)
}

func TestRealtimeDeleteMeClosesSockets(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	c := e.dial(t, e.ticket(t, e.tokens[2], `{"leagueId":"`+lid+`"}`))
	bare := e.dial(t, e.ticket(t, e.tokens[2], `{}`))
	if res := e.call(t, e.tokens[2], "DELETE", "/v1/me", ""); res.StatusCode != 204 {
		t.Fatalf("delete me = %d", res.StatusCode)
	}
	expectClosed(t, c, closeSessionEnded)
	expectClosed(t, bare, closeSessionEnded)
}

func TestRealtimeLogoutClosesThatSessionsSockets(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	uid := userOf(t, e.srv, e.tokens[1])
	other, err := e.srv.auth.Issue(context.Background(), uid) // the same user on another device
	if err != nil {
		t.Fatal(err)
	}
	c := e.dial(t, e.ticket(t, e.tokens[1], `{"leagueId":"`+lid+`"}`))
	keep := e.dial(t, e.ticket(t, other.AccessToken, `{"leagueId":"`+lid+`"}`))
	if res := e.call(t, e.tokens[1], "POST", "/v1/auth/logout", ""); res.StatusCode != 204 {
		t.Fatalf("logout = %d", res.StatusCode)
	}
	expectClosed(t, c, closeSessionEnded)
	e.expectOpen(t, keep)
}

func TestRealtimeRefreshReuseClosesSockets(t *testing.T) {
	e := newRTEnv(t)
	lid := e.league.ID
	uid := userOf(t, e.srv, e.tokens[1])
	tk, err := e.srv.auth.Issue(context.Background(), uid)
	if err != nil {
		t.Fatal(err)
	}
	c := e.dial(t, e.ticket(t, tk.AccessToken, `{"leagueId":"`+lid+`"}`))
	body, _ := json.Marshal(map[string]string{"refreshToken": tk.RefreshToken})
	if res := e.call(t, "", "POST", "/v1/auth/refresh", string(body)); res.StatusCode != 200 {
		t.Fatalf("refresh = %d", res.StatusCode)
	}
	e.expectOpen(t, c) // a normal rotation keeps the socket
	// The rotated-out token again: someone has a copy, the session goes.
	if res := e.call(t, "", "POST", "/v1/auth/refresh", string(body)); res.StatusCode != 401 {
		t.Fatalf("reuse = %d", res.StatusCode)
	}
	expectClosed(t, c, closeSessionEnded)
}

// In production the ticket URL never comes from X-Forwarded-Host/Proto,
// which any client can send.
func TestTicketURLIgnoresForwardedHeadersInProduction(t *testing.T) {
	ticketURL := func(production bool) string {
		srv, _ := newTestServer()
		opts := testOptions()
		opts.Production = production
		h := newHandler(opts, newRoutes(opts, srv))
		rec := doAs(h, testToken, "POST", "/v1/realtime/tickets", `{}`,
			"X-Forwarded-Host", "evil.example", "X-Forwarded-Proto", "https")
		var out RealtimeTicketResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != 201 {
			t.Fatalf("ticket = %d %s", rec.Code, rec.Body)
		}
		return out.Data.Url
	}
	if u := ticketURL(false); !strings.HasPrefix(u, "wss://evil.example/") {
		t.Errorf("development ticket url = %s (forwarded headers are honoured locally)", u)
	}
	if u := ticketURL(true); strings.Contains(u, "evil.example") || !strings.HasPrefix(u, "ws://example.com/v1/realtime?") {
		t.Errorf("production ticket url = %s", u)
	}
}

func TestRealtimeSocketLifetimeIsCapped(t *testing.T) {
	e := newRTEnv(t)
	e.srv.wsMaxLifetime = 300 * time.Millisecond
	c := e.dial(t, e.ticket(t, e.tokens[1], `{"leagueId":"`+e.league.ID+`"}`))
	expectClosed(t, c, closeSessionEnded)
}

// redeemHook runs after a ticket is redeemed, before the socket is
// registered with the hub: the window a revocation can race.
type redeemHook struct {
	realtime.Store
	after func(realtime.Ticket)
}

func (r *redeemHook) RedeemTicket(ctx context.Context, ticket string) (realtime.Ticket, error) {
	tk, err := r.Store.RedeemTicket(ctx, ticket)
	if err == nil {
		r.after(tk)
	}
	return tk, err
}

// A logout (or refresh-token reuse) that lands after the ticket is
// redeemed but before the socket is registered finds no socket to close;
// the socket must still not survive it.
func TestRealtimeRevokeBeforeRegisterClosesSocket(t *testing.T) {
	e := newRTEnv(t)
	tk := e.ticket(t, e.tokens[1], `{"leagueId":"`+e.league.ID+`"}`)
	e.srv.rt = &redeemHook{Store: e.srv.rt, after: func(realtime.Ticket) {
		req, _ := http.NewRequest("POST", e.url+"/v1/auth/logout", nil)
		req.Header.Set("Authorization", "Bearer "+e.tokens[1])
		res, err := http.DefaultClient.Do(req)
		if err != nil || res.StatusCode != 204 {
			t.Errorf("logout = %v %v", res, err)
			return
		}
		res.Body.Close()
	}}
	c := e.dial(t, tk)
	expectClosed(t, c, closeSessionEnded)
}
