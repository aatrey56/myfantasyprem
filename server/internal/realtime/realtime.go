package realtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Replay is how long events are kept for replay.
const Replay = 5 * time.Minute

// TicketTTL is how long a connection ticket is valid.
const TicketTTL = 60 * time.Second

// ErrBadTicket: unknown, expired or already used.
var ErrBadTicket = errors.New("realtime: invalid or expired ticket")

// Ticket is what a connection ticket stands for: the user, the session
// (bearer token) it was issued to, and optionally a league to subscribe
// at once.
type Ticket struct {
	UserID, SessionID, LeagueID string
}

// Revocation takes away what sockets were authorized with:
//
//   - SessionID set: every socket opened with a ticket of that session
//     closes (logout, refresh-token reuse);
//   - UserID set, LeagueID empty: every socket of the user closes
//     (account deletion);
//   - UserID and LeagueID set: the user's sockets subscribed to that
//     league close, and a subscribe racing the change fails (leaving or
//     being removed; the team is no longer theirs).
type Revocation struct {
	UserID    string `json:"userId,omitempty"`
	SessionID string `json:"sessionId,omitempty"`
	LeagueID  string `json:"leagueId,omitempty"`
}

// Event is one server-to-client event of a league.
type Event struct {
	LeagueID string
	Seq      int64
	Type     string
	SentAt   time.Time
	// Body is the event's own fields as a JSON object, for example
	// {"gameweek":7}, without type, leagueId, seq and sentAt.
	Body json.RawMessage
	// PerTeam overrides Body for a team's members (a trade's viewer
	// actions differ by team).
	PerTeam map[string]json.RawMessage
	// Audience limits delivery to these teams' members; nil is everyone.
	Audience []string
}

// Frame is the message a member of teamID ("" for a member without a
// team) receives, and false when the event is not for them.
func (e *Event) Frame(teamID string) ([]byte, bool) {
	if e.Audience != nil && !slices.Contains(e.Audience, teamID) {
		return nil, false
	}
	body := e.Body
	if b, ok := e.PerTeam[teamID]; ok && teamID != "" {
		body = b
	}
	head, _ := json.Marshal(struct {
		Type     string    `json:"type"`
		LeagueID string    `json:"leagueId"`
		Seq      int64     `json:"seq"`
		SentAt   time.Time `json:"sentAt"`
	}{e.Type, e.LeagueID, e.Seq, e.SentAt.UTC()})
	body = bytes.TrimSpace(body)
	if len(body) < 2 || body[0] != '{' || string(body) == "{}" {
		return head, true
	}
	out := make([]byte, 0, len(head)+len(body))
	out = append(out, head[:len(head)-1]...)
	out = append(out, ',')
	return append(out, body[1:]...), true
}

// Store keeps the per-league sequence, the replay buffer and tickets.
type Store interface {
	// Append stores ev with the league's next seq and SentAt = now and
	// returns it as stored. On Postgres it also notifies other instances.
	Append(ctx context.Context, ev Event) (Event, error)
	// Since returns the kept events of a league after seq, in order.
	Since(ctx context.Context, leagueID string, afterSeq int64) ([]Event, error)
	// Get returns one kept event.
	Get(ctx context.Context, leagueID string, seq int64) (Event, bool, error)
	// Prune drops events sent before the cutoff.
	Prune(ctx context.Context, before time.Time) (int, error)
	// IssueTicket makes a single-use ticket for t, valid for ttl.
	IssueTicket(ctx context.Context, t Ticket, ttl time.Duration) (string, time.Time, error)
	// RedeemTicket uses a ticket up and returns what it stands for.
	RedeemTicket(ctx context.Context, ticket string) (Ticket, error)
	// Revoke drops the unused tickets r covers and, on Postgres, tells
	// every instance's Listen loop (ControlChannel) to apply r to its hub.
	Revoke(ctx context.Context, r Revocation) error
}

func newTicket() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("realtime: crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

func hashTicket(t string) string {
	sum := sha256.Sum256([]byte(t))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// Memory is the in-process Store (tests, the database-less dev server).
type Memory struct {
	now func() time.Time

	mu      sync.Mutex
	seq     map[string]int64
	events  map[string][]Event
	tickets map[string]memTicket
}

type memTicket struct {
	Ticket
	expiresAt time.Time
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty Memory store on clock now.
func NewMemory(now func() time.Time) *Memory {
	return &Memory{now: now, seq: map[string]int64{}, events: map[string][]Event{}, tickets: map[string]memTicket{}}
}

func (m *Memory) Append(_ context.Context, ev Event) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq[ev.LeagueID]++
	ev.Seq, ev.SentAt = m.seq[ev.LeagueID], m.now().UTC()
	m.events[ev.LeagueID] = append(m.events[ev.LeagueID], ev)
	return ev, nil
}

func (m *Memory) Since(_ context.Context, leagueID string, afterSeq int64) ([]Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Event
	for _, ev := range m.events[leagueID] {
		if ev.Seq > afterSeq {
			out = append(out, ev)
		}
	}
	return out, nil
}

func (m *Memory) Get(_ context.Context, leagueID string, seq int64) (Event, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ev := range m.events[leagueID] {
		if ev.Seq == seq {
			return ev, true, nil
		}
	}
	return Event{}, false, nil
}

func (m *Memory) Prune(_ context.Context, before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for id, evs := range m.events {
		kept := slices.DeleteFunc(evs, func(ev Event) bool { return ev.SentAt.Before(before) })
		n += len(evs) - len(kept)
		m.events[id] = kept
	}
	for h, t := range m.tickets {
		if !m.now().Before(t.expiresAt) {
			delete(m.tickets, h)
		}
	}
	return n, nil
}

func (m *Memory) IssueTicket(_ context.Context, tk Ticket, ttl time.Duration) (string, time.Time, error) {
	t := newTicket()
	exp := m.now().Add(ttl).UTC()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tickets[hashTicket(t)] = memTicket{Ticket: tk, expiresAt: exp}
	return t, exp, nil
}

func (m *Memory) RedeemTicket(_ context.Context, ticket string) (Ticket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := hashTicket(ticket)
	t, ok := m.tickets[h]
	delete(m.tickets, h)
	if !ok || !m.now().Before(t.expiresAt) {
		return Ticket{}, ErrBadTicket
	}
	return t.Ticket, nil
}

// Revoke drops the tickets r covers. There is one process: the
// Publisher applies r to the local hub itself.
func (m *Memory) Revoke(_ context.Context, r Revocation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for h, t := range m.tickets {
		if r.coversTicket(t.Ticket) {
			delete(m.tickets, h)
		}
	}
	return nil
}

// coversTicket reports whether r takes away what t stands for.
func (r Revocation) coversTicket(t Ticket) bool {
	switch {
	case r.SessionID != "":
		return t.SessionID == r.SessionID
	case r.UserID != "" && r.LeagueID == "":
		return t.UserID == r.UserID
	case r.UserID != "":
		return t.UserID == r.UserID && t.LeagueID == r.LeagueID
	}
	return false
}

// notification is the NOTIFY payload: league id and seq.
func notification(leagueID string, seq int64) string {
	return leagueID + ":" + strconv.FormatInt(seq, 10)
}
