package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
)

// The realtime channel's HTTP side (docs/realtime.md, ADR 0011): the
// ticket operation and the WebSocket endpoint, which is not an OpenAPI
// operation (x-realtime) and authenticates with the ticket instead of a
// bearer token.

const (
	realtimePath = "/v1/realtime"
	// wsPing is how often the server pings (WebSocket ping frames, for
	// clients that answer them); wsIdle drops a socket silent that long.
	wsPing      = 25 * time.Second
	wsIdle      = 60 * time.Second
	wsWrite     = 10 * time.Second
	wsReadLimit = 4096
	// Close codes: 4403 not (or no longer) a member of a league, 4400 a
	// malformed message, 4401 the session ended (logout, refresh-token
	// reuse, account deletion, the socket's lifetime cap; a bad ticket is
	// an HTTP 401 before the upgrade instead).
	closeForbidden    websocket.StatusCode = realtime.CloseForbidden
	closeBadMessage   websocket.StatusCode = 4400
	closeSessionEnded websocket.StatusCode = realtime.CloseSessionEnded
)

// ticketRoutes authenticate with a realtime ticket, not a bearer token.
var ticketRoutes = map[string]bool{"GET " + realtimePath: true}

// membership is the hub's check: the user's team in the league ("" for a
// commissioner without one), or an error when they are not in it.
func (s *Server) membership(ctx context.Context, userID, leagueID string) (string, error) {
	l, err := s.leagues.Get(ctx, userID, leagueID)
	if err != nil {
		return "", err
	}
	t, _ := l.TeamOf(userID)
	return t.ID, nil
}

// revokeRealtime closes the sockets r covers: this instance's at once,
// every instance's through the store (Postgres NOTIFY), and drops the
// unused tickets it covers. Call it after the change is committed.
// Failures are logged: the sockets' lifetime cap is the backstop.
func (s *Server) revokeRealtime(ctx context.Context, r realtime.Revocation) {
	s.hub.Revoke(r)
	if err := s.rt.Revoke(context.WithoutCancel(ctx), r); err != nil {
		s.log.ErrorContext(ctx, "realtime revoke failed", "user", r.UserID, "league", r.LeagueID, "err", err)
	}
}

type originKey struct{}

// withOrigin records the request's scheme and host for absolute URLs
// (the ticket's WebSocket url, when REALTIME_URL is not set). Outside
// production it honours X-Forwarded-Proto and X-Forwarded-Host (local
// proxies); in production, where REALTIME_URL is required, it never does:
// any client can send them.
func withOrigin(trustForwarded bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scheme := "http"
		if r.TLS != nil || (trustForwarded && strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")) {
			scheme = "https"
		}
		host := r.Host
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" && trustForwarded {
			host = fh
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), originKey{}, scheme+"://"+host)))
	})
}

// wsURL is the WebSocket URL for a ticket: REALTIME_URL when configured,
// otherwise this request's host with ws or wss.
func (s *Server) wsURL(ctx context.Context, ticket string) string {
	base := s.realtimeURL
	if base == "" {
		origin, _ := ctx.Value(originKey{}).(string)
		if origin == "" {
			origin = "http://localhost:8080"
		}
		base = "ws" + strings.TrimPrefix(origin, "http") + realtimePath
	}
	return base + "?ticket=" + url.QueryEscape(ticket)
}

func (s *Server) CreateRealtimeTicket(ctx context.Context, r CreateRealtimeTicketRequestObject) (CreateRealtimeTicketResponseObject, error) {
	leagueID := ""
	if r.Body != nil && r.Body.LeagueId != nil {
		leagueID = *r.Body.LeagueId
		if d := lg(leagueID); d != "" {
			return CreateRealtimeTicket400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
		}
		if _, err := s.membership(ctx, userFrom(ctx), leagueID); err != nil {
			return failLeague[CreateRealtimeTicketdefaultApplicationProblemPlusJSONResponse](ctx, err)
		}
	}
	ticket, exp, err := s.rt.IssueTicket(ctx,
		realtime.Ticket{UserID: userFrom(ctx), SessionID: sessionFrom(ctx), LeagueID: leagueID}, realtime.TicketTTL)
	if err != nil {
		return nil, err
	}
	return CreateRealtimeTicket201JSONResponse{
		Body: RealtimeTicketResponse{
			Data: RealtimeTicket{Ticket: ticket, Url: s.wsURL(ctx, ticket), ExpiresAt: exp},
			Meta: metaAt(s.now()),
		},
		Headers: CreateRealtimeTicket201ResponseHeaders{Location: realtimePath},
	}, nil
}

// clientMessage is RealtimeClientMessage.
type clientMessage struct {
	Op       string `json:"op"`
	LeagueID string `json:"leagueId"`
	AfterSeq *int64 `json:"afterSeq"`
}

// serveRealtime upgrades to a WebSocket after redeeming the ticket, then
// runs the socket: one writer (events, pongs, pings) and this reader.
func (s *Server) serveRealtime(w http.ResponseWriter, r *http.Request) {
	tk, err := s.rt.RedeemTicket(r.Context(), r.URL.Query().Get("ticket"))
	if errors.Is(err, realtime.ErrBadTicket) {
		writeProblem(w, http.StatusUnauthorized, "unauthorized", "Unauthorized", "The realtime ticket is missing, expired or used. Get a new one.")
		return
	}
	if clientCanceled(r, err) {
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "redeem realtime ticket", "err", err)
		writeInternalError(w)
		return
	}
	// cmd/api's WriteTimeout (and ReadTimeout) set deadlines on the
	// connection that a hijacked socket would keep: clear them first.
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})
	_ = rc.SetReadDeadline(time.Time{})
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: s.wsOrigins, InsecureSkipVerify: s.wsAnyOrigin,
	})
	if err != nil {
		return // Accept answered the request
	}
	defer conn.CloseNow()
	conn.SetReadLimit(wsReadLimit)

	ctx, cancel := context.WithCancel(s.baseCtx)
	defer cancel()
	client := s.hub.Connect(tk.UserID, tk.SessionID, func(code int, reason string) {
		_ = conn.Close(websocket.StatusCode(code), reason)
		cancel()
	})
	defer s.hub.Disconnect(client)
	// A logout or refresh-token reuse committed after the ticket was
	// redeemed but before Connect found no socket to close. Revocations
	// commit before they reach the hub, so checking the session now, with
	// the socket registered, closes that gap: a later one finds it.
	if ok, err := s.auth.SessionActive(ctx, tk.SessionID); err != nil || !ok {
		if err != nil {
			s.log.ErrorContext(r.Context(), "realtime session check", "err", err)
			_ = conn.Close(websocket.StatusInternalError, "try again")
			return
		}
		_ = conn.Close(closeSessionEnded, "session ended")
		return
	}
	connID := ids.ULID()
	s.log.InfoContext(ctx, "realtime connected", "conn", connID, "user", tk.UserID)
	defer s.log.InfoContext(context.Background(), "realtime disconnected", "conn", connID)
	// No socket outlives an access token's lifetime: then the client
	// needs a new ticket, which needs a session that is still valid.
	lifetime := time.AfterFunc(s.wsMaxLifetime, func() {
		client.Kick(realtime.CloseSessionEnded, "session expired; reconnect with a new ticket")
	})
	defer lifetime.Stop()

	if tk.LeagueID != "" {
		if err := s.hub.Subscribe(ctx, client, tk.LeagueID, nil); err != nil {
			_ = conn.Close(closeForbidden, "not a member of "+tk.LeagueID)
			return
		}
	}
	go s.writeLoop(ctx, cancel, conn, client)

	for {
		rctx, rcancel := context.WithTimeout(ctx, wsIdle)
		typ, data, err := conn.Read(rctx)
		rcancel()
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		var m clientMessage
		if json.Unmarshal(data, &m) != nil {
			_ = conn.Close(closeBadMessage, "messages are JSON objects with an op")
			return
		}
		switch m.Op {
		case "subscribe":
			if !ids.Valid("lg", m.LeagueID) {
				_ = conn.Close(closeBadMessage, "subscribe needs a leagueId")
				return
			}
			if err := s.hub.Subscribe(ctx, client, m.LeagueID, m.AfterSeq); err != nil {
				_ = conn.Close(closeForbidden, "not a member of "+m.LeagueID)
				return
			}
		case "unsubscribe":
			s.hub.Unsubscribe(client, m.LeagueID)
		case "ping":
			pong, _ := json.Marshal(struct {
				Type       string    `json:"type"`
				ServerTime time.Time `json:"serverTime"`
			}{"pong", s.now().UTC()})
			select {
			case client.Send <- pong:
			default:
				client.Kick(realtime.CloseTryAgainLater, "slow consumer")
				return
			}
		}
		// Unknown ops are ignored (open enum).
	}
}

// writeLoop is the socket's only writer.
func (s *Server) writeLoop(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, client *realtime.Client) {
	defer cancel()
	ping := time.NewTicker(wsPing)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusGoingAway, "server shutting down")
			return
		case frame := <-client.Send:
			wctx, done := context.WithTimeout(ctx, wsWrite)
			err := conn.Write(wctx, websocket.MessageText, frame)
			done()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, done := context.WithTimeout(ctx, wsWrite)
			err := conn.Ping(pctx)
			done()
			if err != nil {
				return
			}
		}
	}
}

// originPatterns turns allowed origins (scheme://host) into the host
// patterns websocket.Accept checks; "*" allows any origin.
func originPatterns(allowed []string) (patterns []string, any bool) {
	for _, o := range allowed {
		if o == "*" {
			return nil, true
		}
		if u, err := url.Parse(o); err == nil && u.Host != "" {
			patterns = append(patterns, u.Host)
		}
	}
	return patterns, false
}
