package realtime

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"sync"
)

// SendBuffer is how many messages may wait for a slow client before the
// hub disconnects it (backpressure).
const SendBuffer = 64

// WebSocket close codes the hub asks the transport to send.
const (
	// CloseTryAgainLater: the client could not keep up (1013).
	CloseTryAgainLater = 1013
	// CloseSessionEnded: the session the socket was opened with ended
	// (logout, refresh-token reuse, account deletion, the socket's
	// lifetime cap). Reconnect with a new ticket, which needs a valid
	// access token.
	CloseSessionEnded = 4401
	// CloseForbidden: the user is not (or no longer) a member of a
	// subscribed league.
	CloseForbidden = 4403
)

// ErrRevoked: a subscribe raced a revocation of the user's membership.
var ErrRevoked = errors.New("realtime: membership revoked during subscribe")

// Membership resolves a user's team in a league ("" for a member without a
// team) or fails when the user is not a member.
type Membership func(ctx context.Context, userID, leagueID string) (teamID string, err error)

// Hub fans events out to the connected clients subscribed to a league.
type Hub struct {
	store  Store
	member Membership

	mu      sync.Mutex
	clients map[*Client]bool
	leagues map[string]map[*Client]bool
	// epoch counts a user's revocations while one of their subscribes is
	// in flight (inflight), so a subscribe whose membership check raced
	// one fails instead of caching a stale team. Both entries go when the
	// user's last in-flight subscribe ends, so neither map grows with
	// every user ever revoked.
	epoch    map[string]uint64
	inflight map[string]int
}

// NewHub returns a Hub that replays from store and checks membership.
func NewHub(store Store, member Membership) *Hub {
	return &Hub{
		store: store, member: member,
		clients: map[*Client]bool{}, leagues: map[string]map[*Client]bool{},
		epoch: map[string]uint64{}, inflight: map[string]int{},
	}
}

// Client is one connection's subscriptions and outgoing queue.
type Client struct {
	UserID string
	// SessionID is the session whose ticket opened the connection.
	SessionID string
	// Send carries the frames to write, in order. The transport drains it.
	Send chan []byte

	// kick is the transport's close callback, set by Connect before the
	// client is published and never changed.
	kick      func(code int, reason string)
	hub       *Hub
	mu        sync.Mutex
	closed    bool
	teams     map[string]string  // league -> the user's team
	last      map[string]int64   // league -> last seq handled
	replaying map[string][]Event // league -> live events held during a replay
}

// Connect registers a client for the user and session of a ticket. kick
// closes the connection with a close code (CloseTryAgainLater,
// CloseSessionEnded, CloseForbidden) and reason; the hub calls it at most
// once per client, in a goroutine. It is in place before the client is
// visible to Revoke, so a revocation right after Connect still closes the
// socket. A nil kick does nothing.
func (h *Hub) Connect(userID, sessionID string, kick func(code int, reason string)) *Client {
	if kick == nil {
		kick = func(int, string) {}
	}
	c := &Client{
		UserID: userID, SessionID: sessionID, Send: make(chan []byte, SendBuffer), kick: kick, hub: h,
		teams: map[string]string{}, last: map[string]int64{}, replaying: map[string][]Event{},
	}
	h.mu.Lock()
	h.clients[c] = true
	h.mu.Unlock()
	return c
}

// Disconnect removes the client from every league.
func (h *Hub) Disconnect(c *Client) {
	h.mu.Lock()
	h.dropLocked(c)
	h.mu.Unlock()
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
}

func (h *Hub) dropLocked(c *Client) {
	delete(h.clients, c)
	for id, subs := range h.leagues {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.leagues, id)
		}
	}
}

// Subscribed counts the clients subscribed to a league (metrics, tests).
func (h *Hub) Subscribed(leagueID string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.leagues[leagueID])
}

// Connected counts the open clients (metrics, tests).
func (h *Hub) Connected() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

// Subscribe adds a league to the client after a membership check. With
// afterSeq it first replays the kept events after it; live events that
// arrive meanwhile are held and sent after, without duplicates. It fails
// with ErrRevoked when the user's membership was revoked while it ran.
func (h *Hub) Subscribe(ctx context.Context, c *Client, leagueID string, afterSeq *int64) error {
	h.mu.Lock()
	h.inflight[c.UserID]++
	epoch := h.epoch[c.UserID]
	h.mu.Unlock()
	defer h.subscribeDone(c.UserID)
	team, err := h.member(ctx, c.UserID, leagueID)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrRevoked
	}
	c.teams[leagueID] = team
	if afterSeq != nil {
		c.last[leagueID] = *afterSeq
		c.replaying[leagueID] = []Event{}
	} else if _, ok := c.last[leagueID]; !ok {
		c.last[leagueID] = 0
	}
	c.mu.Unlock()

	h.mu.Lock()
	if h.epoch[c.UserID] != epoch || !h.clients[c] {
		h.mu.Unlock()
		h.Unsubscribe(c, leagueID)
		return ErrRevoked
	}
	if h.leagues[leagueID] == nil {
		h.leagues[leagueID] = map[*Client]bool{}
	}
	h.leagues[leagueID][c] = true
	h.mu.Unlock()

	if afterSeq == nil {
		return nil
	}
	missed, err := h.store.Since(ctx, leagueID, *afterSeq)
	c.mu.Lock()
	defer c.mu.Unlock()
	held := c.replaying[leagueID]
	delete(c.replaying, leagueID)
	all := append(missed, held...)
	slices.SortStableFunc(all, func(a, b Event) int { return cmp.Compare(a.Seq, b.Seq) })
	for _, ev := range all {
		c.enqueueLocked(ev)
	}
	return err
}

// subscribeDone ends one of the user's in-flight subscribes; the last one
// drops the user's epoch.
func (h *Hub) subscribeDone(userID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.inflight[userID]--; h.inflight[userID] <= 0 {
		delete(h.inflight, userID)
		delete(h.epoch, userID)
	}
}

// bumpLocked records a revocation of the user for their in-flight
// subscribes; with none, there is nothing to race.
func (h *Hub) bumpLocked(userID string) {
	if h.inflight[userID] > 0 {
		h.epoch[userID]++
	}
}

// Unsubscribe drops a league.
func (h *Hub) Unsubscribe(c *Client, leagueID string) {
	h.mu.Lock()
	delete(h.leagues[leagueID], c)
	if len(h.leagues[leagueID]) == 0 {
		delete(h.leagues, leagueID)
	}
	h.mu.Unlock()
	c.mu.Lock()
	delete(c.teams, leagueID)
	delete(c.last, leagueID)
	delete(c.replaying, leagueID)
	c.mu.Unlock()
}

// Revoke closes the local sockets r covers (see Revocation): a session's
// or a user's with CloseSessionEnded, a user's subscribed to a league
// with CloseForbidden. Nothing more is sent to them.
func (h *Hub) Revoke(r Revocation) {
	code, reason := CloseSessionEnded, "session ended"
	h.mu.Lock()
	var hit []*Client
	switch {
	case r.SessionID != "":
		for c := range h.clients {
			if c.SessionID == r.SessionID {
				hit = append(hit, c)
			}
		}
	case r.UserID != "" && r.LeagueID == "":
		h.bumpLocked(r.UserID)
		for c := range h.clients {
			if c.UserID == r.UserID {
				hit = append(hit, c)
			}
		}
	case r.UserID != "":
		code, reason = CloseForbidden, "no longer a member of "+r.LeagueID
		h.bumpLocked(r.UserID)
		for c := range h.leagues[r.LeagueID] {
			if c.UserID == r.UserID {
				hit = append(hit, c)
			}
		}
	}
	for _, c := range hit {
		h.dropLocked(c)
	}
	h.mu.Unlock()
	for _, c := range hit {
		c.close(code, reason)
	}
}

// Kick closes the client with a close code and reason, once (the socket's
// lifetime cap, a slow consumer): nothing more is delivered to it.
func (c *Client) Kick(code int, reason string) { c.close(code, reason) }

// close stops all delivery to c and asks the transport to close it, once.
func (c *Client) close(code int, reason string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closeLocked(code, reason)
}

func (c *Client) closeLocked(code int, reason string) {
	if c.closed {
		return
	}
	c.closed = true
	go c.kick(code, reason)
}

// Publish hands an event to every subscribed client of its league.
func (h *Hub) Publish(ev Event) {
	h.mu.Lock()
	clients := make([]*Client, 0, len(h.leagues[ev.LeagueID]))
	for c := range h.leagues[ev.LeagueID] {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		c.deliver(ev)
	}
}

func (c *Client) deliver(ev Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if held, ok := c.replaying[ev.LeagueID]; ok {
		c.replaying[ev.LeagueID] = append(held, ev)
		return
	}
	c.enqueueLocked(ev)
}

// enqueueLocked queues ev's frame unless it was handled already; a full
// queue disconnects the client.
func (c *Client) enqueueLocked(ev Event) {
	last, subscribed := c.last[ev.LeagueID]
	if c.closed || !subscribed || ev.Seq <= last {
		return
	}
	c.last[ev.LeagueID] = ev.Seq
	frame, ok := ev.Frame(c.teams[ev.LeagueID])
	if !ok {
		return
	}
	select {
	case c.Send <- frame:
	default:
		c.closeLocked(CloseTryAgainLater, "slow consumer")
	}
}

// Publisher stores events and, without a listener, delivers them to a
// local hub at once.
type Publisher struct {
	Store Store
	// Hub is the local hub when no Listen loop delivers events (memory
	// mode); nil otherwise.
	Hub *Hub
}

// Publish stores ev (assigning its seq) and delivers it locally if set up.
func (p *Publisher) Publish(ctx context.Context, ev Event) (Event, error) {
	stored, err := p.Store.Append(ctx, ev)
	if err != nil {
		return stored, err
	}
	if p.Hub != nil {
		p.Hub.Publish(stored)
	}
	return stored, nil
}
